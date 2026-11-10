package analyze

import (
	"fmt"
	"strings"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/model"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/semver"
)

func evaluateRemovedAPIs(inv *inventory.Inventory, target semver.Version, rules *knowledge.Base) []model.Finding {
	var findings []model.Finding
	for _, rule := range rules.ByType(knowledge.RuleRemovedAPI) {
		params, err := rule.RemovedAPI()
		if err != nil {
			findings = append(findings, unreadable(rule, err))
			continue
		}
		applies, err := params.AppliesAtTarget(target)
		if err != nil {
			findings = append(findings, unreadable(rule, err))
			continue
		}
		if !applies {
			continue
		}
		var usageExamples []string
		usageCount := 0
		for _, usage := range inv.APIUsages {
			if !params.MatchesUsage(usage.Group, usage.Version, usage.Kind) {
				continue
			}
			usageCount += usage.Count
			usageExamples = append(usageExamples, usage.Examples...)
		}
		served := false
		for _, resource := range inv.APIResources {
			if params.MatchesUsage(resource.Group, resource.Version, resource.Kind) {
				served = true
				break
			}
		}
		if usageCount > 0 {
			detail := fmt.Sprintf("%s/%s %s is removed in Kubernetes %s and %d observed reference(s) still use it.", params.Group, params.Version, strings.Join(params.Kinds, ", "), params.RemovedIn, usageCount)
			if len(usageExamples) > 0 {
				detail += " Examples: " + strings.Join(limit(usageExamples, 5), ", ") + "."
			}
			findings = append(findings, model.Finding{
				ID:            rule.ID + "/used",
				RuleID:        rule.ID,
				RuleType:      rule.RuleType,
				Status:        model.StatusIncompatible,
				Severity:      rule.Severity,
				Component:     apiName(params),
				ComponentType: "kubernetes-api",
				Title:         apiName(params) + " is used and is removed in the target",
				Detail:        detail,
				Remediation:   rule.Remediation,
				Evidence:      evidence(rule),
				Confidence:    rule.Confidence,
				Verification:  rule.Verification,
				BlocksPlan:    true,
			})
			continue
		}
		if served {
			findings = append(findings, model.Finding{
				ID:            rule.ID + "/served",
				RuleID:        rule.ID,
				RuleType:      rule.RuleType,
				Status:        model.StatusPotentialRisk,
				Severity:      "medium",
				Component:     apiName(params),
				ComponentType: "kubernetes-api",
				Title:         apiName(params) + " is still served and is removed in the target",
				Detail:        "The apiserver still serves this API. Discovery did not observe object or manifest usage. Usage outside the collected sources is still possible, so this was not marked compatible.",
				Remediation:   rule.Remediation,
				Evidence:      evidence(rule),
				Confidence:    "medium",
				Verification:  rule.Verification,
			})
		}
	}
	return findings
}

func evaluateComponents(inv *inventory.Inventory, target semver.Version, rules *knowledge.Base) []model.Finding {
	var findings []model.Finding
	for _, component := range inv.Components {
		if handledType(component.Type) {
			continue
		}
		version := component.Version
		if version == "" {
			version = "version unknown"
		}
		base := fmt.Sprintf("%s %s (%s) has no compatibility rule for Kubernetes %s. Detection confidence is %s via %s.", component.Name, version, component.Type, target.Original, component.Confidence, component.DetectionMethod)
		if component.ChartVersion != "" || component.AppVersion != "" {
			base += fmt.Sprintf(" Chart version %s and app version %s were recorded separately.", display(component.ChartVersion), display(component.AppVersion))
		}
		if component.Version == "" {
			if notes := evidenceNotes(component.Evidence, 2); len(notes) > 0 {
				base += " Observed: " + strings.Join(notes, "; ") + "."
			}
		}
		if component.KubeVersion != "" {
			note, violated := constraintNote(component.KubeVersion, target)
			if violated {
				findings = append(findings, model.Finding{
					ID:            "chart-kubeversion/" + component.ID,
					Status:        model.StatusPotentialRisk,
					Severity:      "medium",
					Component:     component.Name,
					ComponentType: component.Type,
					Title:         component.Name + " chart kubeVersion excludes the target",
					Detail:        base + " " + note,
					Confidence:    "medium",
					Verification:  "chart-metadata",
					Evidence: []model.Evidence{{
						Kind:    "chart-metadata",
						Summary: "Helm chart kubeVersion " + component.KubeVersion,
					}},
				})
				continue
			}
			base += " " + note
		}
		findings = append(findings, model.Finding{
			ID:            "unknown-compatibility/" + component.ID,
			Status:        model.StatusUnknown,
			Severity:      "medium",
			Component:     component.Name,
			ComponentType: component.Type,
			Title:         component.Name + " compatibility with the target is unknown",
			Detail:        base + " Missing evidence was not treated as compatibility.",
			Confidence:    "unknown",
			Verification:  "insufficient-evidence",
		})
	}
	return findings
}

func handledType(componentType string) bool {
	switch componentType {
	case inventory.TypeKubernetes, inventory.TypeControlPlane, inventory.TypeAPIServer, inventory.TypeControllerManager, inventory.TypeScheduler, inventory.TypeCloudControllerManager, inventory.TypeKubelet, inventory.TypeKubeProxy:
		return true
	default:
		return false
	}
}

func evaluateWebhooks(inv *inventory.Inventory, rules *knowledge.Base) []model.Finding {
	if len(inv.Webhooks) == 0 {
		return nil
	}
	rule, ok := rules.ByID("apiserver-admission-webhook-readiness")
	if !ok {
		return []model.Finding{{
			ID:           "webhooks-unverified",
			Status:       model.StatusUnknown,
			Severity:     "medium",
			Title:        "Admission webhooks were found and no webhook rule is loaded",
			Detail:       fmt.Sprintf("%d webhook handlers were discovered. Their compatibility with the target was not inferred.", len(inv.Webhooks)),
			Confidence:   "unknown",
			Verification: "insufficient-evidence",
		}}
	}
	fail := 0
	for _, webhook := range inv.Webhooks {
		if webhook.FailurePolicy == "Fail" || webhook.FailurePolicy == "" {
			fail++
		}
	}
	return []model.Finding{{
		ID:            rule.ID,
		RuleID:        rule.ID,
		RuleType:      rule.RuleType,
		Status:        model.StatusPotentialRisk,
		Severity:      rule.Severity,
		Component:     "admission-webhooks",
		ComponentType: "admission-webhook",
		Title:         "Admission webhooks must be checked before kube-apiserver upgrades",
		Detail:        fmt.Sprintf("%d webhook handlers were discovered (%d with failurePolicy Fail or unset). The skew policy requires them to accept the new apiserver's objects. This POC did not verify that they do.", len(inv.Webhooks), fail),
		Remediation:   rule.Remediation,
		Evidence:      evidence(rule),
		Confidence:    rule.Confidence,
		Verification:  rule.Verification,
	}}
}

func evaluatePDBs(inv *inventory.Inventory, rules *knowledge.Base) []model.Finding {
	var names []string
	for _, pdb := range inv.PodDisruptionBudgets {
		if pdb.DisruptionsAllowed != nil && *pdb.DisruptionsAllowed == 0 {
			names = append(names, pdb.Namespace+"/"+pdb.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	rule, ok := rules.ByID("pdb-zero-disruptions-blocks-drain")
	if !ok {
		return []model.Finding{{
			ID:           "pdb-zero",
			Status:       model.StatusPotentialRisk,
			Severity:     "medium",
			Title:        "PodDisruptionBudgets allow zero disruptions",
			Detail:       "Budgets: " + strings.Join(names, ", "),
			Confidence:   "high",
			Verification: "observed",
		}}
	}
	return []model.Finding{{
		ID:           rule.ID,
		RuleID:       rule.ID,
		RuleType:     rule.RuleType,
		Status:       model.StatusPotentialRisk,
		Severity:     rule.Severity,
		Component:    "pod-disruption-budget",
		Title:        "PodDisruptionBudgets currently allow zero disruptions",
		Detail:       strings.TrimSpace(rule.Description) + " Budgets: " + strings.Join(names, ", ") + ".",
		Remediation:  rule.Remediation,
		Evidence:     evidence(rule),
		Confidence:   rule.Confidence,
		Verification: rule.Verification,
	}}
}

func evaluateHealth(inv *inventory.Inventory) []model.Finding {
	if !inv.Pods.Collected || (inv.Pods.NotReady == 0 && inv.Pods.Unschedulable == 0) {
		return nil
	}
	return []model.Finding{{
		ID:           "pod-health",
		Status:       model.StatusPotentialRisk,
		Severity:     "medium",
		Component:    "pods",
		Title:        "Some pods are not ready",
		Detail:       fmt.Sprintf("%d pods are not Ready and %d are unschedulable out of %d. This is observed health, not a version compatibility result. Examples: %s.", inv.Pods.NotReady, inv.Pods.Unschedulable, inv.Pods.Total, strings.Join(inv.Pods.UnhealthyExamples, ", ")),
		Confidence:   "high",
		Verification: "observed",
		Evidence: []model.Evidence{{
			Kind:    "observation",
			Summary: "pod status conditions collected by discovery",
		}},
	}}
}

func apiName(params knowledge.RemovedAPIParams) string {
	group := params.Group
	if group == "" {
		group = "core"
	}
	return group + "/" + params.Version
}

func display(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func evidenceNotes(items []inventory.Evidence, n int) []string {
	var notes []string
	for _, item := range items {
		if item.Summary == "" {
			continue
		}
		notes = append(notes, item.Summary)
		if len(notes) == n {
			break
		}
	}
	return notes
}

func limit(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}
