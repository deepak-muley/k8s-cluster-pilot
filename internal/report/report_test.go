package report

import (
	"strings"
	"testing"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/graph"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/model"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/plan"
)

func TestTextRendersGroupedTables(t *testing.T) {
	doc := &model.Document{
		Kind:              model.KindAnalysis,
		Status:            model.OutcomeInsufficientEvidence,
		ReadOnly:          true,
		ClusterName:       "kind-global-chat",
		CurrentKubernetes: "v1.35.0",
		TargetKubernetes:  "1.36",
		Summary:           model.Summary{Compatible: 1, Unknown: 1, Advisories: 1},
		Findings: []model.Finding{
			{
				Status: model.StatusUnknown, Severity: "medium", Component: "kindnet",
				Confidence: "unknown", Verification: "insufficient-evidence",
				Title:  "kindnet compatibility with the target is unknown",
				Detail: "kindnet version unknown (cni) has no compatibility rule for Kubernetes 1.36.",
			},
			{
				Status: model.StatusCompatible, Severity: "low", Component: "kube-apiserver",
				RuleID: "kube-apiserver-no-minor-skip", Confidence: "high", Verification: "official-documentation",
				Title:  "Direct upgrade stays within the minor-hop rule",
				Detail: "Upgrading from 1.35 to 1.36 crosses 1 minor version, and the rule allows 1.",
				Evidence: []model.Evidence{{
					URL: "https://kubernetes.io/releases/version-skew-policy/", RetrievedAt: "2026-11-09",
				}},
			},
			{
				Status: model.StatusUnknown, Severity: "info", Component: "kubernetes",
				RuleID: "prefer-latest-patch", Confidence: "high", Verification: "official-documentation",
				Title: "Latest patch release was not selected", Detail: "No patch number is selected.",
			},
		},
		Graph: graph.Graph{
			Nodes: []graph.Node{
				{ID: "component/kubernetes/kube-apiserver", Name: "kube-apiserver"},
				{ID: "component/kubernetes/kubelet", Name: "kubelet"},
			},
			Edges: []graph.Edge{{
				From: "component/kubernetes/kube-apiserver", To: "component/kubernetes/kubelet",
				Kind: graph.EdgeUpgradeBefore, RuleID: "kubelet-apiserver-skew",
			}},
		},
		Steps: []model.Step{{
			Order: 1, Requirement: model.RequirementRequired, Action: "upgrade-kube-apiserver",
			Component: "kube-apiserver", From: "1.35", To: "1.36",
			Rationale:   "Upgrade kube-apiserver by one minor.",
			Validations: []string{"Confirm removed APIs for this minor are unused."},
		}},
	}
	text := Text(doc)
	for _, want := range []string{
		"v1.35.0 -> 1.36",
		"Read-only   yes",
		"Unknown (1)",
		"Compatible (1)",
		"Advisories (1)",
		"severity medium, confidence unknown, verification insufficient-evidence",
		"COMPONENT",
		"DETAIL",
		"kindnet",
		"kube-apiserver-no-minor-skip",
		"https://kubernetes.io/releases/version-skew-policy/",
		"UPGRADE FIRST",
		"kubelet-apiserver-skew",
		"1.35 -> 1.36",
		"Confirm removed APIs for this minor are unused.",
		"does not change the cluster",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "Severity:") || strings.Contains(text, "component/kubernetes/kube-apiserver before") {
		t.Fatalf("old field dump still present\n%s", text)
	}
	markdown := Markdown(doc)
	if !strings.Contains(markdown, "| Component |") || !strings.Contains(markdown, "| Count |") {
		t.Fatalf("markdown tables missing\n%s", markdown)
	}
}

func TestMarkdownIncludesEvidenceAndUnknown(t *testing.T) {
	rules, err := knowledge.LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	inv := &inventory.Inventory{
		SchemaVersion: inventory.SchemaVersion,
		Cluster:       inventory.Cluster{Name: "fixture", KubernetesVersion: "1.32.2"},
		Nodes:         []inventory.Node{{Name: "cp", Role: "control-plane", KubeletVersion: "1.32.2"}},
		Components: []inventory.Component{{
			ID: "cni/cilium", Name: "cilium", Type: inventory.TypeCNI, Version: "1.16.5",
			DetectionMethod: "helm", Confidence: "high",
		}},
	}
	inv.Normalize()
	doc, err := plan.Build(inv, "1.33", rules)
	if err != nil {
		t.Fatal(err)
	}
	text := Markdown(doc)
	for _, want := range []string{
		"https://kubernetes.io/releases/version-skew-policy/",
		"unknown",
		"cilium",
		"does not change the cluster",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q\n%s", want, text)
		}
	}
}
