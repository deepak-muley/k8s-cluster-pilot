// Package knowledge loads and indexes compatibility rules.
// It does not decide upgrade order. Callers interpret rule types.
package knowledge

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	bundled "github.com/deepak-muley/k8s-cluster-pilot/knowledge"
	"gopkg.in/yaml.v3"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/semver"
)

// Rule types this POC knows how to load. Evaluation is separate.
const (
	RuleVersionSkew      = "version-skew"
	RuleMinorUpgradeStep = "minor-upgrade-step"
	RuleRemovedAPI       = "removed-api"
	RuleAdmissionWebhook = "admission-webhook-prerequisite"
	RulePDBDrain         = "pdb-zero-disruptions"
	RulePatchAdvisory    = "patch-advisory"
)

// Base is a set of compatibility rules.
type Base struct {
	Meta  Meta
	Rules []Rule
	byID  map[string]Rule
}

// Meta describes where a knowledge base came from.
type Meta struct {
	APIVersion  string   `yaml:"apiVersion" json:"apiVersion"`
	Kind        string   `yaml:"kind" json:"kind"`
	Name        string   `yaml:"name" json:"name"`
	RetrievedAt string   `yaml:"retrievedAt" json:"retrievedAt,omitempty"`
	Description string   `yaml:"description" json:"description,omitempty"`
	Sources     []Source `yaml:"sources" json:"sources,omitempty"`
}

// Source is a publication used as evidence.
type Source struct {
	URL         string `yaml:"url" json:"url"`
	Title       string `yaml:"title" json:"title,omitempty"`
	RetrievedAt string `yaml:"retrievedAt" json:"retrievedAt,omitempty"`
}

// Rule is one machine-readable compatibility statement.
type Rule struct {
	ID           string            `yaml:"id" json:"id"`
	RuleType     string            `yaml:"ruleType" json:"ruleType"`
	Severity     string            `yaml:"severity" json:"severity"`
	Component    ComponentSelector `yaml:"component" json:"component"`
	Description  string            `yaml:"description" json:"description"`
	Remediation  string            `yaml:"remediation" json:"remediation,omitempty"`
	Parameters   map[string]any    `yaml:"parameters" json:"parameters,omitempty"`
	Source       Source            `yaml:"source" json:"source"`
	Verification string            `yaml:"verification" json:"verification"`
	Confidence   string            `yaml:"confidence" json:"confidence"`
}

// ComponentSelector names the component a rule talks about.
type ComponentSelector struct {
	Type string `yaml:"type" json:"type"`
	Name string `yaml:"name" json:"name"`
}

// SkewParams is the parameter schema for version-skew rules.
type SkewParams struct {
	Subject               string              `yaml:"subject"`
	RelativeTo            string              `yaml:"relativeTo"`
	NotNewerMinor         bool                `yaml:"notNewerMinor"`
	MaxOlderMinor         int                 `yaml:"maxOlderMinor"`
	MaxMinorDifference    int                 `yaml:"maxMinorDifference"`
	WhenSubjectMinorBelow *SkewLimitException `yaml:"whenSubjectMinorBelow"`
}

// SkewLimitException narrows a skew limit for old subject versions.
type SkewLimitException struct {
	Minor              int `yaml:"minor"`
	MaxOlderMinor      int `yaml:"maxOlderMinor"`
	MaxMinorDifference int `yaml:"maxMinorDifference"`
}

// MinorStepParams limits how many minors one upgrade hop may cross.
type MinorStepParams struct {
	MaxMinorJump int `yaml:"maxMinorJump"`
}

// RemovedAPIParams describes an API version that stopped being served.
type RemovedAPIParams struct {
	Group       string   `yaml:"group"`
	Version     string   `yaml:"version"`
	Kinds       []string `yaml:"kinds"`
	RemovedIn   string   `yaml:"removedIn"`
	Replacement string   `yaml:"replacement"`
}

// LoadBuiltin reads the rules compiled into the binary.
func LoadBuiltin() (*Base, error) {
	return LoadFS(bundled.Rules)
}

// LoadFS reads every YAML document under rules/ plus top-level YAML files.
func LoadFS(fsys fs.FS) (*Base, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	base := &Base{byID: map[string]Rule{}}
	for _, path := range paths {
		b, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if err := base.addYAML(path, b); err != nil {
			return nil, err
		}
	}
	base.index()
	return base, nil
}

// LoadPaths loads YAML files or directories and merges them.
func LoadPaths(paths []string) (*Base, error) {
	base := &Base{byID: map[string]Rule{}}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
					continue
				}
				if err := base.addFile(filepath.Join(path, name)); err != nil {
					return nil, err
				}
			}
			continue
		}
		if err := base.addFile(path); err != nil {
			return nil, err
		}
	}
	base.index()
	return base, nil
}

// Merge adds rules from extra. Duplicate IDs are an error.
func (b *Base) Merge(extra *Base) error {
	if extra == nil {
		return nil
	}
	if b.byID == nil {
		b.index()
	}
	if extra.Meta.Name != "" && b.Meta.Name == "" {
		b.Meta = extra.Meta
	} else {
		b.Meta.Sources = append(b.Meta.Sources, extra.Meta.Sources...)
	}
	for _, rule := range extra.Rules {
		if _, ok := b.byID[rule.ID]; ok {
			return fmt.Errorf("duplicate rule id %q", rule.ID)
		}
		b.Rules = append(b.Rules, rule)
		b.byID[rule.ID] = rule
	}
	return nil
}

// ByID returns the rule with id.
func (b *Base) ByID(id string) (Rule, bool) {
	rule, ok := b.byID[id]
	return rule, ok
}

// ByType returns rules of ruleType in stable ID order.
func (b *Base) ByType(ruleType string) []Rule {
	var out []Rule
	for _, rule := range b.Rules {
		if rule.RuleType == ruleType {
			out = append(out, rule)
		}
	}
	return out
}

// Skew decodes version-skew parameters.
func (r Rule) Skew() (SkewParams, error) {
	return decodeAs[SkewParams](r.Parameters)
}

// MinorStep decodes minor-upgrade-step parameters.
func (r Rule) MinorStep() (MinorStepParams, error) {
	return decodeAs[MinorStepParams](r.Parameters)
}

// RemovedAPI decodes removed-api parameters.
func (r Rule) RemovedAPI() (RemovedAPIParams, error) {
	return decodeAs[RemovedAPIParams](r.Parameters)
}

// MatchesUsage reports whether this removed-api rule covers the observed API.
// An empty observed kind matches the group and version only.
func (p RemovedAPIParams) MatchesUsage(group, version, kind string) bool {
	if p.Group != group || p.Version != version {
		return false
	}
	if kind == "" || len(p.Kinds) == 0 {
		return true
	}
	for _, item := range p.Kinds {
		if item == kind {
			return true
		}
	}
	return false
}

// AppliesAtTarget reports whether target is at or beyond the removal minor.
func (p RemovedAPIParams) AppliesAtTarget(target semver.Version) (bool, error) {
	removed, err := semver.Parse(p.RemovedIn)
	if err != nil {
		return false, err
	}
	return target.CompareMinor(removed) >= 0, nil
}

// OlderLimit returns the max minors the subject may lag, including the
// documented exception for subjects below a minor threshold.
func (p SkewParams) OlderLimit(subject semver.Version) int {
	limit := p.MaxOlderMinor
	if p.WhenSubjectMinorBelow != nil && subject.Major == 1 && subject.Minor < p.WhenSubjectMinorBelow.Minor && p.WhenSubjectMinorBelow.MaxOlderMinor > 0 {
		limit = p.WhenSubjectMinorBelow.MaxOlderMinor
	}
	return limit
}

// DifferenceLimit returns the symmetric minor skew limit.
func (p SkewParams) DifferenceLimit(subject semver.Version) int {
	limit := p.MaxMinorDifference
	if p.WhenSubjectMinorBelow != nil && subject.Major == 1 && subject.Minor < p.WhenSubjectMinorBelow.Minor && p.WhenSubjectMinorBelow.MaxMinorDifference > 0 {
		limit = p.WhenSubjectMinorBelow.MaxMinorDifference
	}
	return limit
}

func (b *Base) addFile(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return b.addYAML(path, body)
}

func (b *Base) addYAML(path string, body []byte) error {
	dec := yaml.NewDecoder(strings.NewReader(string(body)))
	for {
		var raw map[string]any
		err := dec.Decode(&raw)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("%s: %w", path, err)
		}
		if len(raw) == 0 {
			continue
		}
		kind, _ := raw["kind"].(string)
		encoded, err := yaml.Marshal(raw)
		if err != nil {
			return err
		}
		switch kind {
		case "KnowledgeBase":
			var meta Meta
			if err := yaml.Unmarshal(encoded, &meta); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if b.Meta.Name == "" {
				b.Meta = meta
			} else {
				b.Meta.Sources = append(b.Meta.Sources, meta.Sources...)
			}
		case "CompatibilityRuleSet":
			var set struct {
				Rules []Rule `yaml:"rules"`
			}
			if err := yaml.Unmarshal(encoded, &set); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			for _, rule := range set.Rules {
				if err := validate(rule); err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				if _, ok := b.byID[rule.ID]; ok {
					return fmt.Errorf("%s: duplicate rule id %q", path, rule.ID)
				}
				if b.byID == nil {
					b.byID = map[string]Rule{}
				}
				b.byID[rule.ID] = rule
				b.Rules = append(b.Rules, rule)
			}
		default:
			return fmt.Errorf("%s: unsupported kind %q", path, kind)
		}
	}
}

func (b *Base) index() {
	if b.byID == nil {
		b.byID = map[string]Rule{}
	}
	sort.Slice(b.Rules, func(i, j int) bool { return b.Rules[i].ID < b.Rules[j].ID })
	for _, rule := range b.Rules {
		b.byID[rule.ID] = rule
	}
}

func validate(rule Rule) error {
	if rule.ID == "" || rule.RuleType == "" || rule.Severity == "" || rule.Description == "" {
		return fmt.Errorf("rule %q is missing id, ruleType, severity, or description", rule.ID)
	}
	if rule.Source.URL == "" {
		return fmt.Errorf("rule %q is missing source.url", rule.ID)
	}
	if rule.Source.RetrievedAt == "" {
		return fmt.Errorf("rule %q is missing source.retrievedAt", rule.ID)
	}
	switch rule.Confidence {
	case "high", "medium", "low":
	default:
		return fmt.Errorf("rule %q has confidence %q", rule.ID, rule.Confidence)
	}
	switch rule.Verification {
	case "official-documentation", "observed", "chart-metadata", "insufficient-evidence":
	default:
		return fmt.Errorf("rule %q has verification %q", rule.ID, rule.Verification)
	}
	switch rule.Severity {
	case "critical", "high", "medium", "low", "info":
	default:
		return fmt.Errorf("rule %q has severity %q", rule.ID, rule.Severity)
	}
	return nil
}

func decodeAs[T any](m map[string]any) (T, error) {
	var out T
	if m == nil {
		return out, nil
	}
	b, err := yaml.Marshal(m)
	if err != nil {
		return out, err
	}
	err = yaml.Unmarshal(b, &out)
	return out, err
}
