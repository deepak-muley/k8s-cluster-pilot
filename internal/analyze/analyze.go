// Package analyze evaluates an inventory against the knowledge base.
// It never contacts a cluster and never treats a missing rule as compatibility.
package analyze

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/graph"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/model"
	sem "github.com/deepak-muley/k8s-cluster-explorer/internal/semver"
)

// Analyze builds a read-only analysis document for targetKubernetes.
func Analyze(inv *inventory.Inventory, targetKubernetes string, rules *knowledge.Base) (*model.Document, error) {
	if inv == nil {
		return nil, fmt.Errorf("inventory is required")
	}
	if rules == nil {
		return nil, fmt.Errorf("knowledge base is required")
	}
	target, err := sem.Parse(targetKubernetes)
	if err != nil {
		return nil, fmt.Errorf("target kubernetes version: %w", err)
	}
	findings := make([]model.Finding, 0)
	sequenced := false
	current, currentErr := sem.Parse(inv.Cluster.KubernetesVersion)
	if currentErr != nil {
		findings = append(findings, model.Finding{
			ID:            "kubernetes-version-missing",
			Status:        model.StatusUnknown,
			Severity:      "critical",
			Component:     "kubernetes",
			ComponentType: inventory.TypeKubernetes,
			Title:         "Kubernetes server version is missing or not parseable",
			Detail:        fmt.Sprintf("Cluster version %q cannot be used for skew or upgrade planning. Compatibility was not inferred.", inv.Cluster.KubernetesVersion),
			Confidence:    "unknown",
			Verification:  "insufficient-evidence",
			BlocksPlan:    true,
		})
	} else {
		jump, jumpSequenced := minorJump(current, target, rules)
		sequenced = jumpSequenced
		if jump != nil {
			findings = append(findings, *jump)
		}
		findings = append(findings, evaluateSkew(inv, current, rules)...)
		findings = append(findings, evaluateRemovedAPIs(inv, target, rules)...)
	}
	findings = append(findings, evaluateWebhooks(inv, rules)...)
	findings = append(findings, evaluatePDBs(inv, rules)...)
	findings = append(findings, evaluateHealth(inv)...)
	findings = append(findings, evaluateComponents(inv, target, rules)...)
	findings = append(findings, patchAdvisory(rules)...)

	g := graph.Build(inv, target, rules)
	if len(g.UpgradeOrderCycles) > 0 {
		findings = append(findings, model.Finding{
			ID:           "upgrade-order-cycle",
			Status:       model.StatusIncompatible,
			Severity:     "critical",
			Title:        "Upgrade-order rules contain a cycle",
			Detail:       fmt.Sprintf("Cycles: %v. No linear upgrade order was derived from these rules.", g.UpgradeOrderCycles),
			Confidence:   "high",
			Verification: "official-documentation",
			BlocksPlan:   true,
		})
	}
	sortFindings(findings)
	doc := &model.Document{
		SchemaVersion:     model.SchemaVersion,
		Kind:              model.KindAnalysis,
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		ReadOnly:          true,
		ClusterName:       inv.Cluster.Name,
		CurrentKubernetes: inv.Cluster.KubernetesVersion,
		TargetKubernetes:  targetKubernetes,
		Findings:          findings,
		Graph:             g,
		Steps:             []model.Step{},
	}
	for _, finding := range findings {
		doc.Summary.Add(finding)
	}
	doc.Status = model.DeriveStatus(findings, sequenced)
	return doc, nil
}

func minorJump(current, target sem.Version, rules *knowledge.Base) (*model.Finding, bool) {
	ruleset := rules.ByType(knowledge.RuleMinorUpgradeStep)
	if len(ruleset) == 0 {
		return &model.Finding{
			ID:            "minor-step-rule-missing",
			Status:        model.StatusUnknown,
			Severity:      "critical",
			Component:     "kube-apiserver",
			ComponentType: inventory.TypeAPIServer,
			Title:         "No minor-upgrade rule is loaded",
			Detail:        "An intermediate version sequence was not invented because the knowledge base has no minor-upgrade-step rule.",
			Confidence:    "unknown",
			Verification:  "insufficient-evidence",
			BlocksPlan:    true,
		}, false
	}
	rule := ruleset[0]
	params, err := rule.MinorStep()
	if err != nil || params.MaxMinorJump < 1 {
		return &model.Finding{
			ID:           rule.ID + "/unreadable",
			RuleID:       rule.ID,
			RuleType:     rule.RuleType,
			Status:       model.StatusUnknown,
			Severity:     "critical",
			Title:        "Minor-upgrade rule could not be read",
			Detail:       "The hop size was not guessed.",
			Confidence:   "unknown",
			Verification: "insufficient-evidence",
			BlocksPlan:   true,
		}, false
	}
	if !current.SameMajor(target) {
		return &model.Finding{
			ID:            rule.ID + "/major",
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusUnknown,
			Severity:      "critical",
			Component:     "kube-apiserver",
			ComponentType: inventory.TypeAPIServer,
			Title:         "Target major version differs from the current cluster",
			Detail:        "The loaded rule limits minor hops inside one major version. A major change was not given a fabricated procedure.",
			Evidence:      evidence(rule),
			Confidence:    rule.Confidence,
			Verification:  rule.Verification,
			BlocksPlan:    true,
		}, false
	}
	lag, _ := current.MinorLag(target)
	if lag < 0 {
		return &model.Finding{
			ID:            "kubernetes-downgrade",
			Status:        model.StatusUnknown,
			Severity:      "high",
			Component:     "kubernetes",
			ComponentType: inventory.TypeKubernetes,
			Title:         "Target Kubernetes version is older than the cluster",
			Detail:        "This knowledge base does not define a supported downgrade path, so none was generated.",
			Confidence:    "unknown",
			Verification:  "insufficient-evidence",
			BlocksPlan:    true,
		}, false
	}
	if lag == 0 {
		if target.HasPatch && current.HasPatch && target.Patch < current.Patch {
			return &model.Finding{
				ID:            "kubernetes-patch-downgrade",
				Status:        model.StatusUnknown,
				Severity:      "high",
				Component:     "kubernetes",
				ComponentType: inventory.TypeKubernetes,
				Title:         "Target patch is older than the current patch",
				Detail:        "Patch downgrades are not defined by the loaded rules.",
				Confidence:    "unknown",
				Verification:  "insufficient-evidence",
				BlocksPlan:    true,
			}, false
		}
		return &model.Finding{
			ID:            rule.ID + "/same-minor",
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusCompatible,
			Severity:      "info",
			Component:     "kube-apiserver",
			ComponentType: inventory.TypeAPIServer,
			Title:         "Target Kubernetes minor matches the current cluster",
			Detail:        fmt.Sprintf("Current %s and target %s are the same minor. No minor hop is required. A patch was not selected.", current.Original, target.Original),
			Evidence:      evidence(rule),
			Confidence:    rule.Confidence,
			Verification:  rule.Verification,
		}, false
	}
	if lag <= params.MaxMinorJump {
		return &model.Finding{
			ID:            rule.ID + "/direct",
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusCompatible,
			Severity:      "low",
			Component:     "kube-apiserver",
			ComponentType: inventory.TypeAPIServer,
			Title:         "Direct kube-apiserver upgrade stays within the minor-hop rule",
			Detail:        fmt.Sprintf("Upgrading from %s to %s crosses %d minor version, and the rule allows %d.", current.MinorString(), target.MinorString(), lag, params.MaxMinorJump),
			Evidence:      evidence(rule),
			Confidence:    rule.Confidence,
			Verification:  rule.Verification,
		}, false
	}
	return &model.Finding{
		ID:             rule.ID + "/skip",
		RuleID:         rule.ID,
		RuleType:       rule.RuleType,
		Status:         model.StatusIncompatible,
		Severity:       "critical",
		Component:      "kube-apiserver",
		ComponentType:  inventory.TypeAPIServer,
		Title:          "Direct upgrade skips a Kubernetes minor version",
		Detail:         fmt.Sprintf("Upgrading kube-apiserver from %s to %s crosses %d minors. The supported hop is %d minor. A sequenced plan upgrades one minor at a time.", current.MinorString(), target.MinorString(), lag, params.MaxMinorJump),
		Remediation:    rule.Remediation,
		Evidence:       evidence(rule),
		Confidence:     rule.Confidence,
		Verification:   rule.Verification,
		ResolvedByPlan: true,
	}, true
}

func evidence(rule knowledge.Rule) []model.Evidence {
	return []model.Evidence{{
		Kind:        "url",
		URL:         rule.Source.URL,
		Title:       rule.Source.Title,
		RetrievedAt: rule.Source.RetrievedAt,
		RuleID:      rule.ID,
		Summary:     strings.TrimSpace(rule.Description),
	}}
}

func sortFindings(findings []model.Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if severityRank(findings[i].Severity) != severityRank(findings[j].Severity) {
			return severityRank(findings[i].Severity) < severityRank(findings[j].Severity)
		}
		if findings[i].Status != findings[j].Status {
			return findings[i].Status < findings[j].Status
		}
		return findings[i].ID < findings[j].ID
	})
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}

func patchAdvisory(rules *knowledge.Base) []model.Finding {
	for _, rule := range rules.ByType(knowledge.RulePatchAdvisory) {
		return []model.Finding{{
			ID:           rule.ID,
			RuleID:       rule.ID,
			RuleType:     rule.RuleType,
			Status:       model.StatusUnknown,
			Severity:     "info",
			Component:    "kubernetes",
			Title:        "Latest patch release was not selected",
			Detail:       strings.TrimSpace(rule.Description),
			Remediation:  strings.TrimSpace(rule.Remediation),
			Evidence:     evidence(rule),
			Confidence:   rule.Confidence,
			Verification: rule.Verification,
		}}
	}
	return nil
}

func constraintNote(expr string, target sem.Version) (note string, violated bool) {
	comparison := target.MinorString() + ".0"
	if target.HasPatch {
		comparison = fmt.Sprintf("%d.%d.%d", target.Major, target.Minor, target.Patch)
	}
	constraint, err := semver.NewConstraint(expr)
	if err != nil {
		return fmt.Sprintf("Chart kubeVersion %q could not be evaluated (%v). It was not treated as compatible.", expr, err), false
	}
	version, err := semver.NewVersion(comparison)
	if err != nil {
		return fmt.Sprintf("Target %q could not be compared with chart kubeVersion %q.", comparison, expr), false
	}
	if !target.HasPatch {
		comparison += " (patch was not specified; .0 was used only for this comparison)"
	}
	if constraint.Check(version) {
		return fmt.Sprintf("Chart kubeVersion %q includes comparison version %s. That is a publisher claim, not verified compatibility.", expr, comparison), false
	}
	return fmt.Sprintf("Chart kubeVersion %q excludes comparison version %s. This is the chart publisher's constraint, not an independent verification.", expr, comparison), true
}
