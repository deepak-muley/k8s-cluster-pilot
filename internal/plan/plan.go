// Package plan turns an analysis into a dependency-aware upgrade sequence.
// Ordering comes from loaded rules. Components without a rule get a verify
// step and are not placed in a guessed order.
package plan

import (
	"fmt"
	"strings"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/analyze"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/model"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/semver"
)

// Build analyzes the inventory and appends upgrade steps.
func Build(inv *inventory.Inventory, targetKubernetes string, rules *knowledge.Base) (*model.Document, error) {
	doc, err := analyze.Analyze(inv, targetKubernetes, rules)
	if err != nil {
		return nil, err
	}
	doc.Kind = model.KindPlan
	doc.Steps = sequence(inv, doc, rules)
	recompute(doc)
	return doc, nil
}

type sequencer struct {
	steps []model.Step
}

func (s *sequencer) add(step model.Step) {
	s.steps = append(s.steps, step)
}

func (s *sequencer) finish() []model.Step {
	if len(s.steps) == 0 {
		return []model.Step{}
	}
	for i := range s.steps {
		s.steps[i].Order = i + 1
	}
	return s.steps
}

func sequence(inv *inventory.Inventory, doc *model.Document, rules *knowledge.Base) []model.Step {
	seq := &sequencer{}
	for _, component := range inv.Components {
		if handled(component.Type) {
			continue
		}
		version := component.Version
		if version == "" {
			version = "unknown"
		}
		seq.add(model.Step{
			ID:          "verify-" + component.ID,
			Requirement: model.RequirementVerify,
			Action:      "verify-compatibility",
			Component:   component.Name,
			From:        version,
			To:          doc.TargetKubernetes,
			Rationale:   fmt.Sprintf("No loaded rule gives a compatibility result or an upgrade order for %s %s (%s). Verify it with the vendor before changing the control plane. This step is not an ordered dependency.", component.Type, component.Name, version),
			Validations: []string{"Record the vendor support statement, version range, and source.", "Re-run clusterpilot analyze after the check."},
		})
	}
	if len(inv.Webhooks) > 0 {
		seq.add(model.Step{
			ID:          "verify-admission-webhooks",
			Requirement: model.RequirementVerify,
			Action:      "verify-compatibility",
			Component:   "admission-webhooks",
			Rationale:   "The version skew policy requires admission webhooks to handle the objects the new kube-apiserver will send. Compatibility of the webhook implementations was not verified.",
			RuleIDs:     []string{"apiserver-admission-webhook-readiness"},
			Validations: []string{"Confirm each webhook accepts the target API versions or sets matchPolicy Equivalent.", "Confirm failurePolicy Fail webhooks stay available during the apiserver rollout."},
		})
	}

	current, err := semver.Parse(inv.Cluster.KubernetesVersion)
	target, targetErr := semver.Parse(doc.TargetKubernetes)
	if err != nil || targetErr != nil || !current.SameMajor(target) || target.CompareMinor(current) < 0 {
		return seq.finish()
	}
	if target.CompareMinor(current) == 0 && target.HasPatch && current.HasPatch && target.Patch < current.Patch {
		return seq.finish()
	}

	hopRule, hopOK := rules.ByID("kube-apiserver-no-minor-skip")
	if !hopOK {
		seq.add(model.Step{
			ID:          "minor-sequence-unavailable",
			Requirement: model.RequirementVerify,
			Action:      "verify-compatibility",
			Component:   "kube-apiserver",
			Rationale:   "The minor-upgrade-step rule is not loaded, so no intermediate Kubernetes sequence was generated.",
		})
		return seq.finish()
	}
	hopParams, err := hopRule.MinorStep()
	if err != nil || hopParams.MaxMinorJump < 1 {
		return seq.finish()
	}

	kubeletRule, kubeletOK := rules.ByID("kubelet-apiserver-skew")
	proxyRule, proxyOK := rules.ByID("kube-proxy-apiserver-skew")
	migrations := apiMigrations(inv, target, rules)
	emitted := map[string]bool{}
	flush := func(uptoMinor int) {
		for _, migration := range migrations {
			if migration.minor <= uptoMinor && !emitted[migration.step.ID] {
				seq.add(migration.step)
				emitted[migration.step.ID] = true
			}
		}
	}
	flush(current.Minor)

	if target.CompareMinor(current) == 0 {
		if target.HasPatch && (!current.HasPatch || target.Patch > current.Patch) {
			to := target.Original
			seq.add(apiserverStep(current.Original, to, hopRule))
			seq.add(controlPlaneComponentsStep(to))
		}
		return seq.finish()
	}

	assumedKubelet, kubeletKnown := minMinor(kubeletVersions(inv))
	proxyVersions := componentVersions(inv, inventory.TypeKubeProxy)
	assumedProxy, proxyKnown := minMinor(proxyVersions)
	missingKubelet := missingKubeletVersion(inv)

	if kubeletOK && kubeletKnown && assumedKubelet > current.Minor {
		seq.add(model.Step{
			ID:          "restore-kubelet-skew",
			Requirement: model.RequirementRequired,
			Action:      "restore-version-skew",
			Component:   "kubelet",
			From:        fmt.Sprintf("%d.%d", current.Major, assumedKubelet),
			To:          current.MinorString(),
			Rationale:   "At least one kubelet minor is newer than kube-apiserver. The skew policy forbids that. This knowledge base does not define a kubelet downgrade; restore a supported skew before the later steps.",
			RuleIDs:     []string{kubeletRule.ID},
			Evidence:    stepEvidence(kubeletRule),
		})
	}

	for next := current.Minor + 1; next <= target.Minor; next++ {
		flush(next)
		if missingKubelet {
			seq.add(model.Step{
				ID:          fmt.Sprintf("verify-kubelet-before-%d.%d", current.Major, next),
				Requirement: model.RequirementVerify,
				Action:      "verify-compatibility",
				Component:   "kubelet",
				Rationale:   "At least one node has no parseable kubelet version. The skew rule cannot be evaluated, so the control-plane hop is not certified.",
				RuleIDs:     ids(kubeletOK, kubeletRule.ID),
			})
		} else if kubeletOK && kubeletKnown {
			if floor, ok := floorFor(kubeletRule, assumedKubelet, current.Major, next); ok && assumedKubelet < floor {
				if floor > current.Minor {
					seq.add(blockedSkewStep("kubelet", floor, current))
				} else {
					seq.add(raiseStep("kubelet", kubeletRule, assumedKubelet, floor, current.Minor, current.Major))
					assumedKubelet = floor
				}
			}
		} else if !kubeletKnown && len(inv.Nodes) > 0 {
			seq.add(model.Step{
				ID:          "verify-kubelet-versions",
				Requirement: model.RequirementVerify,
				Action:      "verify-compatibility",
				Component:   "kubelet",
				Rationale:   "Kubelet versions were not available, so the rule's lag limit was not applied.",
				RuleIDs:     ids(kubeletOK, kubeletRule.ID),
			})
		}
		if proxyOK && proxyKnown {
			if floor, ok := floorFor(proxyRule, assumedProxy, current.Major, next); ok && assumedProxy < floor {
				if floor <= current.Minor {
					seq.add(raiseStep("kube-proxy", proxyRule, assumedProxy, floor, current.Minor, current.Major))
					assumedProxy = floor
				}
			}
		}

		to := fmt.Sprintf("%d.%d", current.Major, next)
		from := current.MinorString()
		if next == current.Minor+1 {
			from = current.Original
		}
		if next == target.Minor && target.HasPatch {
			to = target.Original
		}
		seq.add(apiserverStep(from, to, hopRule))
		seq.add(controlPlaneComponentsStep(to))
		current = current.WithMinor(next)
	}

	if kubeletKnown && assumedKubelet < target.Minor {
		seq.add(model.Step{
			ID:          "align-kubelets",
			Requirement: model.RequirementRecommended,
			Action:      "upgrade-kubelet",
			Component:   "kubelet",
			From:        fmt.Sprintf("%d.%d", target.Major, assumedKubelet),
			To:          target.MinorString(),
			Rationale:   "Kubelets may remain up to the supported lag behind kube-apiserver. Aligning them to the target minor is recommended and is required before a further control-plane upgrade that would exceed the lag. Drain each node before a kubelet minor upgrade.",
			RuleIDs:     []string{kubeletRule.ID},
			Validations: []string{"Drain the node before upgrading its kubelet.", "Confirm the node is Ready before the next drain."},
			Evidence:    stepEvidence(kubeletRule),
		})
	}
	if proxyKnown && assumedProxy < target.Minor {
		seq.add(model.Step{
			ID:          "align-kube-proxy",
			Requirement: model.RequirementRecommended,
			Action:      "upgrade-kube-proxy",
			Component:   "kube-proxy",
			From:        fmt.Sprintf("%d.%d", target.Major, assumedProxy),
			To:          target.MinorString(),
			Rationale:   "kube-proxy may lag kube-apiserver by the supported skew. Upgrading it to the target minor is recommended. Keep it within the relative skew of the kubelets it runs with.",
			RuleIDs:     []string{proxyRule.ID, "kube-proxy-kubelet-skew"},
			Evidence:    stepEvidence(proxyRule),
		})
	}
	return seq.finish()
}

type migration struct {
	minor int
	step  model.Step
}

func apiMigrations(inv *inventory.Inventory, target semver.Version, rules *knowledge.Base) []migration {
	var out []migration
	for _, rule := range rules.ByType(knowledge.RuleRemovedAPI) {
		params, err := rule.RemovedAPI()
		if err != nil {
			continue
		}
		applies, err := params.AppliesAtTarget(target)
		if err != nil || !applies {
			continue
		}
		count := 0
		var examples []string
		for _, usage := range inv.APIUsages {
			if params.MatchesUsage(usage.Group, usage.Version, usage.Kind) {
				count += usage.Count
				examples = append(examples, usage.Examples...)
			}
		}
		if count == 0 {
			continue
		}
		removed, err := semver.Parse(params.RemovedIn)
		if err != nil {
			continue
		}
		detail := fmt.Sprintf("Migrate %s/%s (%s) to %s before kube-apiserver reaches %s. %d observed reference(s).", params.Group, params.Version, strings.Join(params.Kinds, ", "), params.Replacement, params.RemovedIn, count)
		if len(examples) > 0 {
			detail += " Examples: " + strings.Join(examples, ", ") + "."
		}
		out = append(out, migration{
			minor: removed.Minor,
			step: model.Step{
				ID:          "migrate-" + rule.ID,
				Requirement: model.RequirementRequired,
				Action:      "migrate-api",
				Component:   params.Group + "/" + params.Version,
				To:          params.Replacement,
				Rationale:   detail,
				RuleIDs:     []string{rule.ID},
				Validations: []string{"Confirm no remaining manifests, Helm releases, or clients use the removed API.", "Re-run clusterpilot discover and analyze."},
				Evidence:    stepEvidence(rule),
			},
		})
	}
	return out
}

func apiserverStep(from, to string, rule knowledge.Rule) model.Step {
	return model.Step{
		ID:          "upgrade-kube-apiserver-" + sanitize(from) + "-to-" + sanitize(to),
		Requirement: model.RequirementRequired,
		Action:      "upgrade-kube-apiserver",
		Component:   "kube-apiserver",
		From:        from,
		To:          to,
		Rationale:   "Upgrade kube-apiserver by one minor. The version skew policy forbids skipping minors, including on single-instance clusters. The patch number is included only when the target specified one; otherwise the latest patch must be chosen outside this knowledge base.",
		RuleIDs:     []string{rule.ID},
		Validations: []string{
			"Back up the cluster and confirm etcd health with a procedure this knowledge base does not certify.",
			"Re-run clusterpilot discover and analyze against this minor before the next hop.",
			"Confirm removed APIs for this minor are unused.",
			"Confirm admission webhooks accept the new apiserver.",
		},
		Evidence: stepEvidence(rule),
	}
}

func controlPlaneComponentsStep(to string) model.Step {
	return model.Step{
		ID:          "upgrade-control-plane-components-to-" + sanitize(to),
		Requirement: model.RequirementRequired,
		Action:      "upgrade-control-plane-components",
		Component:   "kube-controller-manager, kube-scheduler, cloud-controller-manager",
		To:          to,
		Rationale:   "After kube-apiserver is upgraded, upgrade kube-controller-manager, kube-scheduler, and cloud-controller-manager when it is installed. They must not be newer than kube-apiserver and may be one minor older during the live upgrade. The policy does not require an order among these three. Versions that discovery did not observe still need to be checked on the host or static pod.",
		RuleIDs:     []string{"kube-controller-manager-apiserver-skew", "kube-scheduler-apiserver-skew", "cloud-controller-manager-apiserver-skew"},
		Validations: []string{"Confirm each component reports the intended version after the rollout."},
	}
}

func raiseStep(component string, rule knowledge.Rule, fromMinor, floor, maxMinor, major int) model.Step {
	return model.Step{
		ID:          fmt.Sprintf("raise-%s-before-%d-%d", component, major, maxMinor+1),
		Requirement: model.RequirementRequired,
		Action:      "upgrade-" + component,
		Component:   component,
		From:        fmt.Sprintf("%d.%d", major, fromMinor),
		To:          fmt.Sprintf(">=%d.%d and <=%d.%d", major, floor, major, maxMinor),
		Rationale:   fmt.Sprintf("The next kube-apiserver minor would leave %s older than the skew rule allows. Raise it to at least %d.%d without making it newer than the current kube-apiserver %d.%d. The exact patch was not selected.", component, major, floor, major, maxMinor),
		RuleIDs:     []string{rule.ID},
		Validations: []string{"Drain a node before a kubelet minor upgrade.", "Re-check skew before the control-plane hop."},
		Evidence:    stepEvidence(rule),
	}
}

func blockedSkewStep(component string, floor int, current semver.Version) model.Step {
	return model.Step{
		ID:          "skew-blocked-" + component,
		Requirement: model.RequirementRequired,
		Action:      "restore-version-skew",
		Component:   component,
		To:          fmt.Sprintf("at least %d.%d without exceeding kube-apiserver %s", current.Major, floor, current.Original),
		Rationale:   "The skew rule requires a newer component version than kube-apiserver currently allows. No in-policy version satisfies both constraints. Do not continue the control-plane upgrade until this is resolved.",
	}
}

func floorFor(rule knowledge.Rule, subjectMinor, major, nextMinor int) (int, bool) {
	params, err := rule.Skew()
	if err != nil || !params.NotNewerMinor {
		return 0, false
	}
	subject := semver.Version{Major: major, Minor: subjectMinor}
	limit := params.OlderLimit(subject)
	if limit < 0 {
		return 0, false
	}
	return nextMinor - limit, true
}

func stepEvidence(rule knowledge.Rule) []model.Evidence {
	return []model.Evidence{{
		Kind:        "url",
		URL:         rule.Source.URL,
		Title:       rule.Source.Title,
		RetrievedAt: rule.Source.RetrievedAt,
		RuleID:      rule.ID,
	}}
}

func handled(componentType string) bool {
	switch componentType {
	case inventory.TypeKubernetes, inventory.TypeControlPlane, inventory.TypeAPIServer, inventory.TypeControllerManager, inventory.TypeScheduler, inventory.TypeCloudControllerManager, inventory.TypeKubelet, inventory.TypeKubeProxy:
		return true
	default:
		return false
	}
}

func kubeletVersions(inv *inventory.Inventory) []string {
	var out []string
	for _, node := range inv.Nodes {
		if node.KubeletVersion != "" {
			out = append(out, node.KubeletVersion)
		}
	}
	return out
}

func componentVersions(inv *inventory.Inventory, componentType string) []string {
	var out []string
	for _, component := range inv.Components {
		if component.Type == componentType && component.Version != "" {
			out = append(out, component.Version)
		}
	}
	for _, observed := range inv.ObservedVersions {
		if observed.Component == componentType && observed.Version != "" {
			out = append(out, observed.Version)
		}
	}
	return out
}

func minMinor(versions []string) (int, bool) {
	min := 0
	ok := false
	for _, raw := range versions {
		parsed, err := semver.Parse(raw)
		if err != nil {
			continue
		}
		if !ok || parsed.Minor < min {
			min = parsed.Minor
			ok = true
		}
	}
	return min, ok
}

func missingKubeletVersion(inv *inventory.Inventory) bool {
	if len(inv.Nodes) == 0 {
		return false
	}
	for _, node := range inv.Nodes {
		if node.KubeletVersion == "" {
			return true
		}
		if _, err := semver.Parse(node.KubeletVersion); err != nil {
			return true
		}
	}
	return false
}

func ids(ok bool, id string) []string {
	if !ok || id == "" {
		return nil
	}
	return []string{id}
}

func sanitize(s string) string {
	return strings.NewReplacer(".", "-", " ", "", ">", "", "=", "", "+", "").Replace(s)
}

func recompute(doc *model.Document) {
	sequenced := false
	doc.Summary = model.Summary{}
	for _, finding := range doc.Findings {
		if finding.ResolvedByPlan {
			sequenced = true
		}
		doc.Summary.Add(finding)
	}
	for _, step := range doc.Steps {
		switch step.Requirement {
		case model.RequirementRequired:
			doc.Summary.RequiredSteps++
		case model.RequirementRecommended:
			doc.Summary.RecommendedSteps++
		case model.RequirementVerify:
			doc.Summary.VerifySteps++
		}
	}
	doc.Status = model.DeriveStatus(doc.Findings, sequenced)
}
