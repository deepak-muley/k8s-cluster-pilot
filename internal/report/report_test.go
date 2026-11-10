package report

import (
	"strings"
	"testing"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/plan"
)

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
