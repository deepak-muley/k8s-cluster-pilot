package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Pluto release files are embedded from knowledge/pluto. Istio and cert-manager
// rows are versions of those products, so only component k8s is loaded. Pluto's
// target-versions map is its own sample target and is not the upgrade target.

type plutoSourceFile struct {
	Module  string `yaml:"module"`
	Version string `yaml:"version"`
	URL     string `yaml:"url"`
	SHA256  string `yaml:"sha256"`
}

type plutoVersionFile struct {
	DeprecatedVersions []plutoAPI `yaml:"deprecated-versions"`
}

type plutoAPI struct {
	Name                   string `yaml:"version"`
	Kind                   string `yaml:"kind"`
	DeprecatedIn           string `yaml:"deprecated-in"`
	RemovedIn              string `yaml:"removed-in"`
	ReplacementAPI         string `yaml:"replacement-api"`
	ReplacementAvailableIn string `yaml:"replacement-available-in"`
	Component              string `yaml:"component"`
}

func (b *Base) addPluto(versions, source []byte) error {
	src, err := parsePlutoSource(source)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(versions)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, src.SHA256) {
		return fmt.Errorf("pluto versions.yaml sha256 %s does not match source.yaml %s", got, src.SHA256)
	}
	var file plutoVersionFile
	if err := yaml.Unmarshal(versions, &file); err != nil {
		return fmt.Errorf("pluto versions.yaml: %w", err)
	}
	if len(file.DeprecatedVersions) == 0 {
		return fmt.Errorf("pluto versions.yaml has no deprecated-versions")
	}
	ruleSource := Source{
		URL:         src.URL,
		Title:       "Pluto versions.yaml " + src.Version,
		RetrievedAt: src.Version,
	}
	if b.byID == nil {
		b.byID = map[string]Rule{}
	}
	seen := map[string]plutoAPI{}
	loaded := 0
	for _, item := range file.DeprecatedVersions {
		if item.Component != "k8s" {
			continue
		}
		key := item.Name + "\x00" + item.Kind
		if prev, ok := seen[key]; ok {
			if prev != item {
				return fmt.Errorf("pluto rows for %s %s disagree", item.Name, item.Kind)
			}
			continue
		}
		seen[key] = item
		rule, ok, err := plutoRule(item, ruleSource)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, exists := b.byID[rule.ID]; exists {
			return fmt.Errorf("duplicate rule id %q from pluto", rule.ID)
		}
		if err := validate(rule); err != nil {
			return err
		}
		b.byID[rule.ID] = rule
		b.Rules = append(b.Rules, rule)
		loaded++
	}
	if loaded == 0 {
		return fmt.Errorf("pluto versions.yaml has no kubernetes api rows")
	}
	b.Meta.Sources = append(b.Meta.Sources, ruleSource)
	b.index()
	return nil
}

func parsePlutoSource(raw []byte) (plutoSourceFile, error) {
	var src plutoSourceFile
	if err := yaml.Unmarshal(raw, &src); err != nil {
		return src, fmt.Errorf("pluto source.yaml: %w", err)
	}
	src.Version = strings.TrimSpace(src.Version)
	src.SHA256 = strings.TrimSpace(src.SHA256)
	src.URL = strings.TrimSpace(src.URL)
	if src.Module != "github.com/fairwindsops/pluto/v5" || !strings.HasPrefix(src.Version, "v") || src.URL == "" || src.SHA256 == "" {
		return src, fmt.Errorf("pluto source.yaml is missing module, version, url, or sha256")
	}
	if !strings.Contains(src.URL, "/"+src.Version+"/") {
		return src, fmt.Errorf("pluto source url %q does not contain version %s", src.URL, src.Version)
	}
	return src, nil
}

func plutoRule(item plutoAPI, source Source) (Rule, bool, error) {
	removed := strings.Trim(strings.TrimSpace(item.RemovedIn), `"`)
	deprecated := strings.Trim(strings.TrimSpace(item.DeprecatedIn), `"`)
	if removed == "" && deprecated == "" {
		return Rule{}, false, nil
	}
	group, version, err := splitAPIVersion(item.Name)
	if err != nil {
		return Rule{}, false, fmt.Errorf("pluto api %q: %w", item.Name, err)
	}
	kind := strings.TrimSpace(item.Kind)
	prefix := "removed"
	ruleType := RuleRemovedAPI
	severity := "critical"
	if removed == "" {
		prefix = "deprecated"
		ruleType = RuleDeprecatedAPI
		severity = "medium"
	}
	id := prefix + "-" + slug(group) + "-" + slug(version)
	if group == "" {
		id = prefix + "-" + slug(version)
	}
	if kind != "" {
		id += "-" + slug(kind)
	}
	api := item.Name
	subject := api
	if kind != "" {
		subject = api + " " + kind
	}
	var description, remediation string
	if removed != "" {
		description = fmt.Sprintf("%s is removed in Kubernetes %s in Pluto %s.", subject, removed, source.RetrievedAt)
		remediation = plutoRemediation(item.ReplacementAPI, item.ReplacementAvailableIn)
	} else {
		description = fmt.Sprintf("%s is deprecated in Kubernetes %s in Pluto %s. No removal version is listed.", subject, deprecated, source.RetrievedAt)
		remediation = plutoRemediation(item.ReplacementAPI, item.ReplacementAvailableIn)
	}
	params := map[string]any{
		"group":   group,
		"version": version,
	}
	if kind != "" {
		params["kinds"] = []string{kind}
	}
	if removed != "" {
		params["removedIn"] = removed
	}
	if deprecated != "" {
		params["deprecatedIn"] = deprecated
	}
	if replacement := strings.TrimSpace(item.ReplacementAPI); replacement != "" {
		params["replacement"] = replacement
	}
	name := slug(kind)
	if name == "" {
		name = slug(version)
	}
	return Rule{
		ID:           id,
		RuleType:     ruleType,
		Severity:     severity,
		Component:    ComponentSelector{Type: "kubernetes-api", Name: name},
		Description:  description,
		Remediation:  remediation,
		Parameters:   params,
		Source:       source,
		Verification: "upstream-dataset",
		Confidence:   "high",
	}, true, nil
}

func plutoRemediation(replacement, available string) string {
	replacement = strings.TrimSpace(replacement)
	available = strings.TrimSpace(available)
	if replacement == "" {
		return "Pluto lists no replacement API. Confirm the Kubernetes deprecation guide before this minor."
	}
	text := "Migrate to " + replacement + "."
	if available != "" {
		text += " Pluto lists that replacement as available in " + available + "."
	}
	return text
}

func splitAPIVersion(apiVersion string) (group, version string, err error) {
	apiVersion = strings.TrimSpace(apiVersion)
	if apiVersion == "" {
		return "", "", fmt.Errorf("empty apiVersion")
	}
	group, version, ok := strings.Cut(apiVersion, "/")
	if !ok {
		return "", group, nil
	}
	if group == "" || version == "" || strings.Contains(version, "/") {
		return "", "", fmt.Errorf("unsupported apiVersion %q", apiVersion)
	}
	return group, version, nil
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	dash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
