package analyze

import (
	"testing"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/model"
)

func TestKubeletSkewAndUnknownComponent(t *testing.T) {
	rules := mustRules(t)
	inv := baseInventory("1.32.2", "1.32.2")
	inv.Components = []inventory.Component{{
		ID:              "cni/cilium",
		Name:            "cilium",
		Type:            inventory.TypeCNI,
		Version:         "1.16.5",
		DetectionMethod: "helm",
		Confidence:      "high",
	}}
	doc, err := Analyze(inv, "1.33", rules)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != model.OutcomeInsufficientEvidence {
		t.Fatalf("status %s, want unknown components to prevent a compatible verdict", doc.Status)
	}
	if !hasStatus(doc, "kubelet-apiserver-skew", model.StatusCompatible) {
		t.Fatal("expected compatible kubelet skew")
	}
	if !hasStatus(doc, "unknown-compatibility/cni/cilium", model.StatusUnknown) {
		t.Fatal("expected unknown cilium finding")
	}
}

func TestKubeletNewerBlocks(t *testing.T) {
	rules := mustRules(t)
	doc, err := Analyze(baseInventory("1.31.0", "1.32.0"), "1.32", rules)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != model.OutcomeBlocked {
		t.Fatalf("status %s", doc.Status)
	}
	if !hasStatus(doc, "kubelet-apiserver-skew/n0", model.StatusIncompatible) {
		t.Fatalf("findings %+v", titles(doc))
	}
}

func TestOldKubeletLimit(t *testing.T) {
	rules := mustRules(t)
	// kubelet 1.24 is below 1.25, so the limit is two minors. 1.27 is three newer.
	doc, err := Analyze(baseInventory("1.27.0", "1.24.0"), "1.28", rules)
	if err != nil {
		t.Fatal(err)
	}
	if !hasStatus(doc, "kubelet-apiserver-skew/n0", model.StatusIncompatible) {
		t.Fatal("expected the pre-1.25 two-minor limit to reject a three-minor lag")
	}
	ok, err := Analyze(baseInventory("1.26.0", "1.24.0"), "1.27", rules)
	if err != nil {
		t.Fatal(err)
	}
	if !hasStatus(ok, "kubelet-apiserver-skew", model.StatusCompatible) {
		t.Fatalf("two-minor lag should be compatible, got %+v", titles(ok))
	}
}

func TestRemovedAPIUsageBlocks(t *testing.T) {
	rules := mustRules(t)
	inv := baseInventory("1.24.0", "1.24.0")
	inv.AddAPIUsage("batch", "v1beta1", "CronJob", "helm-manifest", "jobs/nightly")
	doc, err := Analyze(inv, "1.25", rules)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != model.OutcomeBlocked {
		t.Fatalf("status %s", doc.Status)
	}
	if !hasStatus(doc, "removed-batch-v1beta1-cronjob/used", model.StatusIncompatible) {
		t.Fatal("expected cronjob removal finding")
	}
}

func TestChartConstraintIsNotVerifiedCompatibility(t *testing.T) {
	rules := mustRules(t)
	inv := baseInventory("1.32.2", "1.32.2")
	inv.Components = []inventory.Component{{
		ID:              "helm/platform",
		Name:            "platform",
		Type:            inventory.TypeHelm,
		Version:         "2.0.0",
		KubeVersion:     ">=1.25.0-0 <1.33.0-0",
		DetectionMethod: "helm",
		Confidence:      "high",
	}}
	doc, err := Analyze(inv, "1.33", rules)
	if err != nil {
		t.Fatal(err)
	}
	if !hasStatus(doc, "chart-kubeversion/helm/platform", model.StatusPotentialRisk) {
		t.Fatalf("findings %+v", titles(doc))
	}
	if hasStatus(doc, "unknown-compatibility/helm/platform", model.StatusCompatible) {
		t.Fatal("chart constraint must not be recorded as verified compatible")
	}
}

func baseInventory(server, kubelet string) *inventory.Inventory {
	inv := &inventory.Inventory{
		SchemaVersion: inventory.SchemaVersion,
		Cluster:       inventory.Cluster{Name: "fixture", KubernetesVersion: server},
		Nodes: []inventory.Node{{
			Name:           "n0",
			Role:           "control-plane",
			KubeletVersion: kubelet,
		}},
	}
	inv.Normalize()
	return inv
}

func mustRules(t *testing.T) *knowledge.Base {
	t.Helper()
	rules, err := knowledge.LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

func hasStatus(doc *model.Document, id, status string) bool {
	for _, finding := range doc.Findings {
		if finding.ID == id && finding.Status == status {
			return true
		}
	}
	return false
}

func titles(doc *model.Document) []string {
	out := make([]string, 0, len(doc.Findings))
	for _, finding := range doc.Findings {
		out = append(out, finding.ID+":"+finding.Status)
	}
	return out
}
