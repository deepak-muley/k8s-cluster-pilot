package analyze

import (
	"fmt"
	"strings"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/model"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/semver"
)

func evaluateSkew(inv *inventory.Inventory, current semver.Version, rules *knowledge.Base) []model.Finding {
	var findings []model.Finding
	for _, rule := range rules.ByType(knowledge.RuleVersionSkew) {
		params, err := rule.Skew()
		if err != nil {
			findings = append(findings, unreadable(rule, err))
			continue
		}
		switch {
		case params.Subject == inventory.TypeAPIServer && params.RelativeTo == inventory.TypeAPIServer:
			findings = append(findings, evaluateHA(inv, rule, params)...)
		case params.Subject == inventory.TypeKubelet && params.RelativeTo == inventory.TypeAPIServer:
			findings = append(findings, evaluateKubelets(inv, current, rule, params)...)
		case params.MaxMinorDifference > 0 && params.RelativeTo == inventory.TypeKubelet:
			findings = append(findings, evaluateRelative(inv, rule, params)...)
		case params.NotNewerMinor && params.RelativeTo == inventory.TypeAPIServer:
			findings = append(findings, evaluateAgainstAPIServer(inv, current, rule, params)...)
		default:
			findings = append(findings, model.Finding{
				ID:           rule.ID + "/unimplemented",
				RuleID:       rule.ID,
				RuleType:     rule.RuleType,
				Status:       model.StatusUnknown,
				Severity:     "medium",
				Title:        "Skew rule shape is not evaluated by this POC",
				Detail:       fmt.Sprintf("Rule %s was not treated as compatible.", rule.ID),
				Confidence:   "unknown",
				Verification: "insufficient-evidence",
			})
		}
	}
	return findings
}

func evaluateKubelets(inv *inventory.Inventory, apiserver semver.Version, rule knowledge.Rule, params knowledge.SkewParams) []model.Finding {
	if len(inv.Nodes) == 0 {
		return []model.Finding{info(rule, "kubelet skew was not evaluated because the inventory has no nodes")}
	}
	var bad, missing []model.Finding
	minMinor, maxMinor := -1, -1
	for _, node := range inv.Nodes {
		if node.KubeletVersion == "" {
			missing = append(missing, model.Finding{
				ID:            rule.ID + "/" + node.Name + "/missing",
				RuleID:        rule.ID,
				RuleType:      rule.RuleType,
				Status:        model.StatusUnknown,
				Severity:      "high",
				Component:     node.Name,
				ComponentType: inventory.TypeKubelet,
				Title:         "Kubelet version was not reported",
				Detail:        fmt.Sprintf("Node %s has no kubelet version. Skew against kube-apiserver %s was not verified and was not treated as compatible.", node.Name, apiserver.Original),
				Evidence:      evidence(rule),
				Confidence:    "unknown",
				Verification:  "insufficient-evidence",
			})
			continue
		}
		kubelet, err := semver.Parse(node.KubeletVersion)
		if err != nil {
			missing = append(missing, model.Finding{
				ID:            rule.ID + "/" + node.Name + "/unparsed",
				RuleID:        rule.ID,
				RuleType:      rule.RuleType,
				Status:        model.StatusUnknown,
				Severity:      "high",
				Component:     node.Name,
				ComponentType: inventory.TypeKubelet,
				Title:         "Kubelet version could not be parsed",
				Detail:        fmt.Sprintf("Node %s reports kubelet version %q.", node.Name, node.KubeletVersion),
				Evidence:      evidence(rule),
				Confidence:    "unknown",
				Verification:  "insufficient-evidence",
			})
			continue
		}
		if minMinor < 0 || kubelet.Minor < minMinor {
			minMinor = kubelet.Minor
		}
		if kubelet.Minor > maxMinor {
			maxMinor = kubelet.Minor
		}
		if finding, ok := skewFinding(rule, params, "kubelet "+node.Name, inventory.TypeKubelet, kubelet, apiserver); ok {
			finding.ID = rule.ID + "/" + node.Name
			finding.Component = node.Name
			bad = append(bad, finding)
		}
	}
	if len(bad) == 0 && len(missing) == 0 {
		return []model.Finding{{
			ID:            rule.ID,
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusCompatible,
			Severity:      "low",
			Component:     "kubelet",
			ComponentType: inventory.TypeKubelet,
			Title:         "Kubelets are within the supported kube-apiserver skew",
			Detail:        fmt.Sprintf("%d kubelets are not newer than kube-apiserver %s and are within the rule's older-minor limit. Observed kubelet minors run from %d.%d through %d.%d.", len(inv.Nodes), apiserver.Original, apiserver.Major, minMinor, apiserver.Major, maxMinor),
			Evidence:      evidence(rule),
			Confidence:    rule.Confidence,
			Verification:  rule.Verification,
		}}
	}
	return append(bad, missing...)
}

func evaluateAgainstAPIServer(inv *inventory.Inventory, apiserver semver.Version, rule knowledge.Rule, params knowledge.SkewParams) []model.Finding {
	versions := componentVersions(inv, params.Subject)
	if len(versions) == 0 {
		switch params.Subject {
		case inventory.TypeCloudControllerManager:
			return nil
		case inventory.TypeControllerManager, inventory.TypeScheduler:
			return []model.Finding{info(rule, params.Subject+" version was not observed. Skew was not verified. The upgrade plan still lists it in the documented order.")}
		case inventory.TypeKubeProxy:
			if !componentPresent(inv, params.Subject) {
				return []model.Finding{info(rule, "kube-proxy was not observed, so its skew was not evaluated")}
			}
		}
		return []model.Finding{{
			ID:            rule.ID + "/missing",
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusUnknown,
			Severity:      "high",
			Component:     params.Subject,
			ComponentType: params.Subject,
			Title:         params.Subject + " version was not observed",
			Detail:        "Skew against kube-apiserver was not verified and was not treated as compatible.",
			Evidence:      evidence(rule),
			Confidence:    "unknown",
			Verification:  "insufficient-evidence",
		}}
	}
	var findings []model.Finding
	okCount := 0
	for _, raw := range versions {
		parsed, err := semver.Parse(raw)
		if err != nil {
			findings = append(findings, model.Finding{
				ID:            rule.ID + "/unparsed",
				RuleID:        rule.ID,
				RuleType:      rule.RuleType,
				Status:        model.StatusUnknown,
				Severity:      "high",
				Component:     params.Subject,
				ComponentType: params.Subject,
				Title:         params.Subject + " version could not be parsed",
				Detail:        fmt.Sprintf("Observed version %q.", raw),
				Evidence:      evidence(rule),
				Confidence:    "unknown",
				Verification:  "insufficient-evidence",
			})
			continue
		}
		if finding, bad := skewFinding(rule, params, params.Subject+" "+raw, params.Subject, parsed, apiserver); bad {
			finding.ID = rule.ID + "/" + parsed.MinorString()
			findings = append(findings, finding)
			continue
		}
		okCount++
	}
	if len(findings) == 0 {
		findings = append(findings, model.Finding{
			ID:            rule.ID,
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusCompatible,
			Severity:      "low",
			Component:     params.Subject,
			ComponentType: params.Subject,
			Title:         params.Subject + " is within the supported kube-apiserver skew",
			Detail:        fmt.Sprintf("%d observed version(s) are not newer than kube-apiserver %s and are within %d older minor(s).", okCount, apiserver.Original, params.OlderLimit(semver.MustParse(versions[0]))),
			Evidence:      evidence(rule),
			Confidence:    rule.Confidence,
			Verification:  rule.Verification,
		})
	}
	return findings
}

func evaluateRelative(inv *inventory.Inventory, rule knowledge.Rule, params knowledge.SkewParams) []model.Finding {
	subjects := componentVersions(inv, params.Subject)
	relatives := kubeletVersions(inv)
	if len(subjects) == 0 || len(relatives) == 0 {
		return []model.Finding{info(rule, "kube-proxy and kubelet relative skew was not evaluated because one side was not observed")}
	}
	var findings []model.Finding
	for _, subjectRaw := range subjects {
		subject, err := semver.Parse(subjectRaw)
		if err != nil {
			continue
		}
		limit := params.DifferenceLimit(subject)
		for _, relativeRaw := range relatives {
			relative, err := semver.Parse(relativeRaw)
			if err != nil {
				continue
			}
			if !subject.SameMajor(relative) {
				findings = append(findings, model.Finding{
					ID:            rule.ID + "/major",
					RuleID:        rule.ID,
					RuleType:      rule.RuleType,
					Status:        model.StatusIncompatible,
					Severity:      rule.Severity,
					Component:     params.Subject,
					ComponentType: params.Subject,
					Title:         "kube-proxy and kubelet majors differ",
					Detail:        fmt.Sprintf("kube-proxy %s and kubelet %s do not share a major version. The relative skew rule was not treated as satisfied.", subjectRaw, relativeRaw),
					Evidence:      evidence(rule),
					Confidence:    rule.Confidence,
					Verification:  rule.Verification,
					BlocksPlan:    true,
				})
				continue
			}
			delta := subject.Minor - relative.Minor
			if delta < 0 {
				delta = -delta
			}
			if delta > limit {
				findings = append(findings, model.Finding{
					ID:            fmt.Sprintf("%s/%s/%s", rule.ID, subject.MinorString(), relative.MinorString()),
					RuleID:        rule.ID,
					RuleType:      rule.RuleType,
					Status:        model.StatusIncompatible,
					Severity:      rule.Severity,
					Component:     params.Subject,
					ComponentType: params.Subject,
					Title:         "kube-proxy is outside the supported kubelet skew",
					Detail:        fmt.Sprintf("kube-proxy %s and kubelet %s differ by %d minors. The limit for this kube-proxy is %d.", subjectRaw, relativeRaw, delta, limit),
					Remediation:   rule.Remediation,
					Evidence:      evidence(rule),
					Confidence:    rule.Confidence,
					Verification:  rule.Verification,
					BlocksPlan:    true,
				})
			}
		}
	}
	if len(findings) == 0 {
		findings = append(findings, model.Finding{
			ID:            rule.ID,
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusCompatible,
			Severity:      "low",
			Component:     params.Subject,
			ComponentType: params.Subject,
			Title:         "kube-proxy is within the supported kubelet skew",
			Detail:        "Observed kube-proxy and kubelet minors are inside the relative skew limit.",
			Evidence:      evidence(rule),
			Confidence:    rule.Confidence,
			Verification:  rule.Verification,
		})
	}
	return findings
}

func evaluateHA(inv *inventory.Inventory, rule knowledge.Rule, params knowledge.SkewParams) []model.Finding {
	controlPlanes := 0
	for _, node := range inv.Nodes {
		if node.Role == "control-plane" {
			controlPlanes++
		}
	}
	versions := map[string]semver.Version{}
	observations := 0
	for _, observed := range inv.ObservedVersions {
		if observed.Component != inventory.TypeAPIServer || observed.Source != "pod" || observed.Version == "" {
			continue
		}
		parsed, err := semver.Parse(observed.Version)
		if err != nil {
			continue
		}
		versions[parsed.MinorString()] = parsed
		observations += observed.Count
	}
	if len(versions) >= 2 {
		minV, maxV := pickMinMax(versions)
		delta := maxV.Minor - minV.Minor
		if !minV.SameMajor(maxV) || delta > params.MaxMinorDifference {
			return []model.Finding{{
				ID:            rule.ID,
				RuleID:        rule.ID,
				RuleType:      rule.RuleType,
				Status:        model.StatusIncompatible,
				Severity:      rule.Severity,
				Component:     "kube-apiserver",
				ComponentType: inventory.TypeAPIServer,
				Title:         "kube-apiserver instances exceed the HA skew limit",
				Detail:        fmt.Sprintf("Observed instance minors %s and %s differ by more than %d.", minV.MinorString(), maxV.MinorString(), params.MaxMinorDifference),
				Remediation:   rule.Remediation,
				Evidence:      evidence(rule),
				Confidence:    rule.Confidence,
				Verification:  rule.Verification,
				BlocksPlan:    true,
			}}
		}
		return []model.Finding{{
			ID:            rule.ID,
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusCompatible,
			Severity:      "low",
			Component:     "kube-apiserver",
			ComponentType: inventory.TypeAPIServer,
			Title:         "Observed kube-apiserver instances are within one minor",
			Detail:        fmt.Sprintf("Observed instance versions stay within %d minor.", params.MaxMinorDifference),
			Evidence:      evidence(rule),
			Confidence:    rule.Confidence,
			Verification:  rule.Verification,
		}}
	}
	if controlPlanes >= 2 && observations < controlPlanes {
		return []model.Finding{{
			ID:            rule.ID + "/unverified",
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusUnknown,
			Severity:      "medium",
			Component:     "kube-apiserver",
			ComponentType: inventory.TypeAPIServer,
			Title:         "HA kube-apiserver skew was not verified",
			Detail:        fmt.Sprintf("%d control-plane nodes were found, but per-instance kube-apiserver versions were not all observed. The discovery server version is only the apiserver that answered. HA skew was not treated as compatible.", controlPlanes),
			Evidence:      evidence(rule),
			Confidence:    "unknown",
			Verification:  "insufficient-evidence",
		}}
	}
	return nil
}

func skewFinding(rule knowledge.Rule, params knowledge.SkewParams, name, componentType string, subject, relative semver.Version) (model.Finding, bool) {
	limit := params.OlderLimit(subject)
	if !subject.SameMajor(relative) {
		return model.Finding{
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusIncompatible,
			Severity:      rule.Severity,
			Component:     name,
			ComponentType: componentType,
			Title:         name + " major version differs from kube-apiserver",
			Detail:        fmt.Sprintf("%s is %s and kube-apiserver is %s. The minor skew limits apply inside one major version, so this pair was not treated as supported.", name, subject.Original, relative.Original),
			Remediation:   rule.Remediation,
			Evidence:      evidence(rule),
			Confidence:    rule.Confidence,
			Verification:  rule.Verification,
			BlocksPlan:    true,
		}, true
	}
	lag, _ := subject.MinorLag(relative)
	if lag < 0 {
		return model.Finding{
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusIncompatible,
			Severity:      rule.Severity,
			Component:     name,
			ComponentType: componentType,
			Title:         name + " is newer than kube-apiserver",
			Detail:        fmt.Sprintf("%s %s is a newer minor than kube-apiserver %s. The version skew policy does not allow that.", name, subject.Original, relative.Original),
			Remediation:   rule.Remediation,
			Evidence:      evidence(rule),
			Confidence:    rule.Confidence,
			Verification:  rule.Verification,
			BlocksPlan:    true,
		}, true
	}
	if lag > limit {
		return model.Finding{
			RuleID:        rule.ID,
			RuleType:      rule.RuleType,
			Status:        model.StatusIncompatible,
			Severity:      rule.Severity,
			Component:     name,
			ComponentType: componentType,
			Title:         name + " is older than the supported skew",
			Detail:        fmt.Sprintf("%s %s is %d minors older than kube-apiserver %s. The limit for this version is %d.", name, subject.Original, lag, relative.Original, limit),
			Remediation:   rule.Remediation,
			Evidence:      evidence(rule),
			Confidence:    rule.Confidence,
			Verification:  rule.Verification,
			BlocksPlan:    true,
		}, true
	}
	return model.Finding{}, false
}

func componentVersions(inv *inventory.Inventory, componentType string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if _, ok := seen[raw]; ok {
			return
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	for _, component := range inv.Components {
		if component.Type == componentType {
			add(component.Version)
		}
	}
	for _, observed := range inv.ObservedVersions {
		if observed.Component == componentType {
			add(observed.Version)
		}
	}
	return out
}

func kubeletVersions(inv *inventory.Inventory) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, node := range inv.Nodes {
		if node.KubeletVersion == "" {
			continue
		}
		if _, ok := seen[node.KubeletVersion]; ok {
			continue
		}
		seen[node.KubeletVersion] = struct{}{}
		out = append(out, node.KubeletVersion)
	}
	return out
}

func componentPresent(inv *inventory.Inventory, componentType string) bool {
	for _, component := range inv.Components {
		if component.Type == componentType {
			return true
		}
	}
	for _, observed := range inv.ObservedVersions {
		if observed.Component == componentType {
			return true
		}
	}
	return false
}

func pickMinMax(versions map[string]semver.Version) (semver.Version, semver.Version) {
	var minV, maxV semver.Version
	first := true
	for _, version := range versions {
		if first || version.CompareMinor(minV) < 0 {
			minV = version
		}
		if first || version.CompareMinor(maxV) > 0 {
			maxV = version
		}
		first = false
	}
	return minV, maxV
}

func info(rule knowledge.Rule, detail string) model.Finding {
	return model.Finding{
		ID:           rule.ID + "/not-observed",
		RuleID:       rule.ID,
		RuleType:     rule.RuleType,
		Status:       model.StatusUnknown,
		Severity:     "info",
		Title:        detail,
		Detail:       detail,
		Evidence:     evidence(rule),
		Confidence:   "unknown",
		Verification: "insufficient-evidence",
	}
}

func unreadable(rule knowledge.Rule, err error) model.Finding {
	return model.Finding{
		ID:           rule.ID + "/unreadable",
		RuleID:       rule.ID,
		RuleType:     rule.RuleType,
		Status:       model.StatusUnknown,
		Severity:     "high",
		Title:        "Rule parameters could not be read",
		Detail:       err.Error() + ". The rule was not treated as compatible.",
		Confidence:   "unknown",
		Verification: "insufficient-evidence",
	}
}
