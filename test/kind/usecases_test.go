//go:build kind

package kindtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/discover"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/graph"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/model"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/plan"
)

const (
	defaultCluster = "clusterpilot-usecases"
	defaultImage   = "kindest/node:v1.31.14"
	// Pluto v5.24.5 says this API is removed in Kubernetes 1.32.
	legacyFlow  = "removed-flowcontrol-apiserver-k8s-io-v1beta3-flowschema"
	hiddenToken = "not-for-inventory-9f3c"
)

var (
	clusterName string
	kubeconfig  string
	rules       *knowledge.Base
	inv         *inventory.Inventory
)

func TestMain(m *testing.M) {
	clusterName = env("KIND_CLUSTER_NAME", defaultCluster)
	image := env("KIND_NODE_IMAGE", defaultImage)
	keep := os.Getenv("KEEP_KIND") == "1"
	dir, err := os.MkdirTemp("", "clusterpilot-kind-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	kubeconfig = filepath.Join(dir, "kubeconfig")
	code := 1
	defer func() {
		if !keep {
			_ = command("kind", "delete", "cluster", "--name", clusterName)
		}
		os.RemoveAll(dir)
		os.Exit(code)
	}()
	if err := setup(image); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	rules, err = knowledge.LoadBuiltin()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	inv, err = discover.Discover(context.Background(), discover.Options{
		Kubeconfig: kubeconfig,
		Context:    "kind-" + clusterName,
		Timeout:    3 * time.Minute,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	code = m.Run()
}

func TestUseCases(t *testing.T) {
	if !strings.Contains(inv.Cluster.KubernetesVersion, "v1.31.") {
		t.Fatalf("cluster %s, want kindest/node v1.31.x so the legacy FlowSchema is still servable and removed in 1.32", inv.Cluster.KubernetesVersion)
	}
	raw, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), hiddenToken) {
		t.Fatal("discovery stored Helm values or ConfigMap data")
	}

	t.Run("control plane skew is compatible", func(t *testing.T) {
		doc := mustPlan(t, "1.31")
		if !hasStatus(doc, "kubelet-apiserver-skew", model.StatusCompatible) {
			t.Fatalf("findings %s", ids(doc))
		}
	})

	t.Run("same minor does not invent an upgrade", func(t *testing.T) {
		doc := mustPlan(t, "1.31")
		if !hasStatus(doc, "kube-apiserver-no-minor-skip/same-minor", model.StatusCompatible) {
			t.Fatal("expected the same-minor result")
		}
		if stepByAction(doc, "upgrade-kube-apiserver").ID != "" {
			t.Fatalf("same minor planned a control-plane upgrade: %s", stepIDs(doc))
		}
	})

	t.Run("removed API blocks the target that removes it", func(t *testing.T) {
		doc := mustPlan(t, "1.32")
		if doc.Status != model.OutcomeBlocked {
			t.Fatalf("status %s", doc.Status)
		}
		if !hasStatus(doc, legacyFlow+"/used", model.StatusIncompatible) {
			t.Fatalf("findings %s", ids(doc))
		}
	})

	t.Run("two minor jump is sequenced without a patch number", func(t *testing.T) {
		doc := mustPlan(t, "1.33")
		migrate := stepByPrefix(doc, "migrate-"+legacyFlow)
		first := stepByPrefix(doc, "upgrade-kube-apiserver-")
		second := stepTo(doc, "1.33")
		if migrate.Order == 0 || first.Order == 0 || second.Order == 0 {
			t.Fatalf("steps %s", stepIDs(doc))
		}
		if !(migrate.Order < first.Order && first.Order < second.Order) {
			t.Fatalf("order migrate %d first %d second %d", migrate.Order, first.Order, second.Order)
		}
		if strings.Contains(second.To, ".0") {
			t.Fatalf("plan invented a patch: %+v", second)
		}
	})

	t.Run("deprecation before removal does not block", func(t *testing.T) {
		doc := mustPlan(t, "1.31")
		finding, ok := findingByID(doc, legacyFlow+"/used")
		if !ok || finding.Status != model.StatusPotentialRisk || finding.BlocksPlan {
			t.Fatalf("finding %+v", finding)
		}
	})

	t.Run("chart kubeVersion that excludes the target is a risk", func(t *testing.T) {
		doc := mustPlan(t, "1.33")
		if !hasStatus(doc, "chart-kubeversion/helm/demo-platform", model.StatusPotentialRisk) {
			t.Fatalf("findings %s", ids(doc))
		}
		if hasStatus(doc, "unknown-compatibility/helm/demo-platform", model.StatusCompatible) {
			t.Fatal("chart constraint was recorded as verified compatible")
		}
		included := mustPlan(t, "1.31")
		if !hasStatus(included, "unknown-compatibility/helm/demo-platform", model.StatusUnknown) {
			t.Fatalf("a kubeVersion that includes 1.31 was not left unknown: %s", ids(included))
		}
	})

	t.Run("apps without a rule stay unknown and unordered", func(t *testing.T) {
		doc := mustPlan(t, "1.33")
		for _, id := range []string{
			"unknown-compatibility/helm/demo-nats",
			"unknown-compatibility/cni/kindnet",
			"unknown-compatibility/kubernetes/coredns",
			"unknown-compatibility/kubernetes/etcd",
			"unknown-compatibility/runtime/containerd",
		} {
			if !hasStatus(doc, id, model.StatusUnknown) {
				t.Fatalf("missing %s in %s", id, ids(doc))
			}
		}
		nats := stepByID(doc, "verify-helm/demo-nats")
		if nats.Requirement != model.RequirementVerify {
			t.Fatalf("nats step %+v", nats)
		}
		for _, edge := range doc.Graph.Edges {
			if edge.Kind != graph.EdgeUpgradeBefore {
				continue
			}
			blob := edge.From + " " + edge.To
			for _, name := range []string{"kindnet", "coredns", "etcd", "nats", "containerd"} {
				if strings.Contains(blob, name) {
					t.Fatalf("upgrade-before edge for %s: %+v", name, edge)
				}
			}
		}
	})

	t.Run("zero disruptions and unready pods are not version results", func(t *testing.T) {
		doc := mustPlan(t, "1.31")
		if !hasStatus(doc, "pdb-zero-disruptions-blocks-drain", model.StatusPotentialRisk) {
			t.Fatalf("findings %s", ids(doc))
		}
		health, ok := findingByID(doc, "pod-health")
		if !ok || health.Status != model.StatusPotentialRisk || !strings.Contains(health.Detail, "not a version compatibility result") {
			t.Fatalf("health %+v", health)
		}
	})

	t.Run("admission webhooks are not assumed compatible", func(t *testing.T) {
		doc := mustPlan(t, "1.31")
		if !hasStatus(doc, "apiserver-admission-webhook-readiness", model.StatusPotentialRisk) {
			t.Fatalf("findings %s", ids(doc))
		}
		step := stepByID(doc, "verify-admission-webhooks")
		if step.Requirement != model.RequirementVerify {
			t.Fatalf("webhook step %+v", step)
		}
	})

	t.Run("a downgrade is refused", func(t *testing.T) {
		doc := mustPlan(t, "1.30")
		if doc.Status != model.OutcomeBlocked {
			t.Fatalf("status %s", doc.Status)
		}
		if !hasStatus(doc, "kubernetes-downgrade", model.StatusUnknown) {
			t.Fatalf("findings %s", ids(doc))
		}
		if stepByAction(doc, "upgrade-kube-apiserver").ID != "" {
			t.Fatalf("downgrade invented an upgrade: %s", stepIDs(doc))
		}
	})
}

func setup(image string) error {
	for _, tool := range []string{"docker", "kind", "kubectl", "helm"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is required for make test-kind", tool)
		}
	}
	if os.Getenv("KIND_REUSE") == "1" {
		if err := command("kind", "export", "kubeconfig", "--name", clusterName, "--kubeconfig", kubeconfig); err != nil {
			if err := command("kind", "create", "cluster", "--name", clusterName, "--image", image, "--kubeconfig", kubeconfig, "--wait", "180s"); err != nil {
				return err
			}
		}
	} else if err := command("kind", "create", "cluster", "--name", clusterName, "--image", image, "--kubeconfig", kubeconfig, "--wait", "180s"); err != nil {
		_ = command("kind", "delete", "cluster", "--name", clusterName)
		if err := command("kind", "create", "cluster", "--name", clusterName, "--image", image, "--kubeconfig", kubeconfig, "--wait", "180s"); err != nil {
			return err
		}
	}
	root := filepath.Join("fixtures")
	charts := filepath.Join("charts")
	// Tests run with the package directory as the working directory.
	if err := command("kubectl", "apply", "-f", filepath.Join(root, "namespace.yaml")); err != nil {
		return err
	}
	if err := waitServiceAccount(); err != nil {
		return err
	}
	for _, manifest := range []string{"workload.yaml", "webhook.yaml"} {
		if err := command("kubectl", "apply", "-f", filepath.Join(root, manifest)); err != nil {
			return err
		}
	}
	if err := command("kubectl", "wait", "--for=condition=available", "deployment/pause", "-n", "demo", "--timeout=180s"); err != nil {
		return err
	}
	if err := waitPDB(); err != nil {
		return err
	}
	if err := waitUnready(); err != nil {
		return err
	}
	if err := command("helm", "upgrade", "--install", "nats", filepath.Join(charts, "nats"), "--namespace", "demo", "--set", "token="+hiddenToken); err != nil {
		return err
	}
	if err := command("helm", "upgrade", "--install", "platform", filepath.Join(charts, "platform"), "--namespace", "demo"); err != nil {
		return err
	}
	// Helm stores the rendered manifest unchanged. kubectl apply would rewrite
	// v1beta3 to v1, and discovery would no longer see the removed API.
	return command("helm", "upgrade", "--install", "legacy-api", filepath.Join(charts, "legacy-api"))
}

func waitServiceAccount() error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := output("kubectl", "get", "serviceaccount", "default", "-n", "demo"); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("service account demo/default was not created")
}

func waitPDB() error {
	deadline := time.Now().Add(90 * time.Second)
	var last []byte
	for time.Now().Before(deadline) {
		out, err := output("kubectl", "get", "pdb", "pause", "-n", "demo", "-o", "jsonpath={.status.disruptionsAllowed}")
		last = out
		if err == nil && strings.TrimSpace(string(out)) == "0" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("pdb pause did not reach disruptionsAllowed=0 (last %q)", last)
}

func waitUnready() error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		out, err := output("kubectl", "get", "pod", "stuck", "-n", "demo", "-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`)
		if err == nil && strings.TrimSpace(string(out)) == "False" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("pod stuck did not become unready")
}

func command(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func output(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig)
	return cmd.Output()
}

func mustPlan(t *testing.T, target string) *model.Document {
	t.Helper()
	doc, err := plan.Build(inv, target, rules)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func hasStatus(doc *model.Document, id, status string) bool {
	finding, ok := findingByID(doc, id)
	return ok && finding.Status == status
}

func findingByID(doc *model.Document, id string) (model.Finding, bool) {
	for _, finding := range doc.Findings {
		if finding.ID == id {
			return finding, true
		}
	}
	return model.Finding{}, false
}

func stepByID(doc *model.Document, id string) model.Step {
	for _, step := range doc.Steps {
		if step.ID == id {
			return step
		}
	}
	return model.Step{}
}

func stepByPrefix(doc *model.Document, prefix string) model.Step {
	for _, step := range doc.Steps {
		if strings.HasPrefix(step.ID, prefix) {
			return step
		}
	}
	return model.Step{}
}

func stepByAction(doc *model.Document, action string) model.Step {
	for _, step := range doc.Steps {
		if step.Action == action {
			return step
		}
	}
	return model.Step{}
}

func stepTo(doc *model.Document, to string) model.Step {
	for _, step := range doc.Steps {
		if step.Action == "upgrade-kube-apiserver" && step.To == to {
			return step
		}
	}
	return model.Step{}
}

func ids(doc *model.Document) string {
	parts := make([]string, 0, len(doc.Findings))
	for _, finding := range doc.Findings {
		parts = append(parts, finding.ID+":"+finding.Status)
	}
	return strings.Join(parts, ", ")
}

func stepIDs(doc *model.Document) string {
	parts := make([]string, 0, len(doc.Steps))
	for _, step := range doc.Steps {
		parts = append(parts, fmt.Sprintf("%d:%s", step.Order, step.ID))
	}
	return strings.Join(parts, ", ")
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
