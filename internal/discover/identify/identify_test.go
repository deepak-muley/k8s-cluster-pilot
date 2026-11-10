package identify

import (
	"strings"
	"testing"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
)

func TestCiliumKeepsChartAndAppVersionsSeparate(t *testing.T) {
	inv := &inventory.Inventory{
		SchemaVersion: inventory.SchemaVersion,
		Cluster:       inventory.Cluster{KubernetesVersion: "v1.32.2"},
		HelmReleases: []inventory.HelmRelease{{
			Name:          "cilium",
			Namespace:     "kube-system",
			Chart:         "cilium",
			ChartVersion:  "1.16.5",
			AppVersion:    "1.16.3",
			ValuesOmitted: true,
		}},
		Workloads: []inventory.Workload{{
			Kind:      "DaemonSet",
			Namespace: "kube-system",
			Name:      "cilium",
			Images:    []string{"quay.io/cilium/cilium:v1.16.3"},
		}},
		Nodes: []inventory.Node{{
			Name:                    "kind-control-plane",
			Role:                    "control-plane",
			KubeletVersion:          "v1.32.2",
			ProviderID:              "kind://docker/kind-dev/kind-control-plane",
			ContainerRuntime:        "containerd",
			ContainerRuntimeVersion: "1.7.22",
		}},
	}
	Annotate(inv)
	component := find(inv, "cni/cilium")
	if component.Version != "1.16.3" || component.VersionSource != "helm-appVersion" {
		t.Fatalf("version %+v", component)
	}
	if component.ChartVersion != "1.16.5" || component.AppVersion != "1.16.3" {
		t.Fatalf("helm versions were collapsed: %+v", component)
	}
	if component.Confidence != "high" {
		t.Fatalf("confidence %s", component.Confidence)
	}
	if inv.HelmReleases[0].ClassifiedAs != "cni/cilium" {
		t.Fatalf("classified as %s", inv.HelmReleases[0].ClassifiedAs)
	}
	if inv.Cluster.Distribution != "kind" || inv.Cluster.DistributionConfidence != "high" {
		t.Fatalf("distribution %+v", inv.Cluster)
	}
	runtime := find(inv, "runtime/containerd")
	if runtime.Version != "1.7.22" || runtime.Confidence != "medium" {
		t.Fatalf("runtime %+v", runtime)
	}
}

func TestKindnetWithoutVersionIsNotGuessed(t *testing.T) {
	inv := &inventory.Inventory{
		Workloads: []inventory.Workload{{
			Kind:   "DaemonSet",
			Name:   "kindnet",
			Images: []string{"docker.io/kindest/kindnetd:v20251212-v0.29.0-alpha-105-g20ccfc88"},
		}},
	}
	Annotate(inv)
	component := find(inv, "cni/kindnet")
	if component.Version != "" {
		t.Fatalf("version guessed from kind build tag: %+v", component)
	}
	if component.Confidence != "high" {
		t.Fatalf("daemonset name should be high confidence, got %s", component.Confidence)
	}
	joined := ""
	for _, item := range component.Evidence {
		joined += item.Summary + "\n"
	}
	if !strings.Contains(joined, "v20251212-v0.29.0-alpha-105-g20ccfc88") || !strings.Contains(joined, "not a semantic version") {
		t.Fatalf("image tag was not kept as evidence: %s", joined)
	}
}

func find(inv *inventory.Inventory, id string) inventory.Component {
	for _, component := range inv.Components {
		if component.ID == id {
			return component
		}
	}
	return inventory.Component{}
}
