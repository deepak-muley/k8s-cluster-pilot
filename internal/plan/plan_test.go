package plan

import (
	"strings"
	"testing"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/graph"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/model"
)

func TestMinorSequenceAndAPIMigration(t *testing.T) {
	rules := mustRules(t)
	inv := &inventory.Inventory{
		SchemaVersion: inventory.SchemaVersion,
		Cluster:       inventory.Cluster{Name: "fixture", KubernetesVersion: "1.30.4"},
		Nodes: []inventory.Node{
			{Name: "cp", Role: "control-plane", KubeletVersion: "1.30.4"},
			{Name: "w", Role: "worker", KubeletVersion: "1.27.1"},
		},
	}
	inv.AddAPIUsage("autoscaling", "v2beta2", "HorizontalPodAutoscaler", "helm-manifest", "web/frontend")
	inv.Normalize()

	doc, err := Build(inv, "1.32", rules)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != model.OutcomeBlocked {
		t.Fatalf("status %s", doc.Status)
	}
	migrate := findStep(doc, "migrate-removed-autoscaling-v2beta2-hpa")
	hop31 := findStep(doc, "upgrade-kube-apiserver-1-30-4-to-1-31")
	hop32 := findStep(doc, "upgrade-kube-apiserver-1-31-to-1-32")
	if migrate.Order == 0 || hop31.Order == 0 || hop32.Order == 0 {
		t.Fatalf("missing steps: %+v", stepIDs(doc))
	}
	if !(migrate.Order < hop32.Order && hop31.Order < hop32.Order) {
		t.Fatalf("migration and hops out of order: migrate %d hop31 %d hop32 %d", migrate.Order, hop31.Order, hop32.Order)
	}
	if migrate.Order > hop31.Order {
		t.Fatal("v2beta2 is removed in 1.26, which is already behind 1.30, so migration comes before the first hop")
	}
	raise := findStep(doc, "raise-kubelet-before-1-31")
	if raise.Order == 0 || raise.Order > hop31.Order {
		t.Fatalf("kubelet 1.27 must be raised before apiserver 1.31, got %+v", raise)
	}
	if raise.Requirement != model.RequirementRequired {
		t.Fatalf("requirement %s", raise.Requirement)
	}
	assertUpgradeBefore(t, doc, "component/kubernetes/kube-apiserver", "component/kubernetes/kubelet")
	for _, edge := range doc.Graph.Edges {
		if edge.Kind == graph.EdgeUpgradeBefore && strings.Contains(edge.To, "cni") {
			t.Fatalf("cni must not get an upgrade-before edge: %+v", edge)
		}
	}
}

func TestUnknownComponentIsVerifyNotOrdered(t *testing.T) {
	rules := mustRules(t)
	inv := &inventory.Inventory{
		SchemaVersion: inventory.SchemaVersion,
		Cluster:       inventory.Cluster{Name: "fixture", KubernetesVersion: "1.32.2"},
		Nodes:         []inventory.Node{{Name: "cp", Role: "control-plane", KubeletVersion: "1.32.2"}},
		Components: []inventory.Component{{
			ID:              "cni/kindnet",
			Name:            "kindnet",
			Type:            inventory.TypeCNI,
			DetectionMethod: "daemonset",
			Confidence:      "high",
		}},
	}
	inv.Normalize()
	doc, err := Build(inv, "1.33", rules)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != model.OutcomeInsufficientEvidence {
		t.Fatalf("status %s", doc.Status)
	}
	verify := findStep(doc, "verify-cni/kindnet")
	apiserver := findStep(doc, "upgrade-kube-apiserver-1-32-2-to-1-33")
	if verify.Order == 0 || apiserver.Order == 0 {
		t.Fatalf("steps %+v", stepIDs(doc))
	}
	if verify.Requirement != model.RequirementVerify {
		t.Fatalf("verify requirement %s", verify.Requirement)
	}
	if verify.Order > apiserver.Order {
		t.Fatal("verification should be visible before the control-plane hop")
	}
}

func TestNoDowngradePlan(t *testing.T) {
	rules := mustRules(t)
	inv := &inventory.Inventory{
		SchemaVersion: inventory.SchemaVersion,
		Cluster:       inventory.Cluster{Name: "fixture", KubernetesVersion: "1.32.2"},
		Nodes:         []inventory.Node{{Name: "cp", Role: "control-plane", KubeletVersion: "1.32.2"}},
	}
	inv.Normalize()
	doc, err := Build(inv, "1.31", rules)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != model.OutcomeBlocked {
		t.Fatalf("status %s", doc.Status)
	}
	for _, step := range doc.Steps {
		if step.Action == "upgrade-kube-apiserver" {
			t.Fatalf("downgrade invented a control-plane upgrade: %+v", step)
		}
	}
}

func assertUpgradeBefore(t *testing.T, doc *model.Document, from, to string) {
	t.Helper()
	for _, edge := range doc.Graph.Edges {
		if edge.Kind == graph.EdgeUpgradeBefore && edge.From == from && edge.To == to && edge.RuleID != "" {
			return
		}
	}
	t.Fatalf("missing upgrade-before %s -> %s", from, to)
}

func findStep(doc *model.Document, id string) model.Step {
	for _, step := range doc.Steps {
		if step.ID == id {
			return step
		}
	}
	return model.Step{}
}

func stepIDs(doc *model.Document) []string {
	out := make([]string, len(doc.Steps))
	for i, step := range doc.Steps {
		out[i] = step.ID
	}
	return out
}

func mustRules(t *testing.T) *knowledge.Base {
	t.Helper()
	rules, err := knowledge.LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	return rules
}
