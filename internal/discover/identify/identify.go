// Package identify derives component records from collected facts.
// A name match alone does not become a version.
package identify

import (
	"fmt"
	"sort"
	"strings"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/discover/ref"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/semver"
)

type signature struct {
	name         string
	component    string
	workloads    []string
	images       []string
	crdGroups    []string
	charts       []string
	drivers      []string
	provisioners []string
}

// Annotate replaces components with records derived from the inventory facts
// and fills distribution when the nodes agree.
func Annotate(inv *inventory.Inventory) {
	if inv == nil {
		return
	}
	var components []inventory.Component
	components = append(components, platform(inv)...)
	matchedDrivers := map[string]struct{}{}
	matchedProvisioners := map[string]struct{}{}
	for _, sig := range cniSignatures() {
		if component, ok := match(inv, sig); ok {
			components = append(components, component)
		}
	}
	for _, sig := range csiSignatures() {
		component, ok := match(inv, sig)
		if !ok {
			continue
		}
		components = append(components, component)
		for _, driver := range sig.drivers {
			matchedDrivers[driver] = struct{}{}
		}
		for _, provisioner := range sig.provisioners {
			matchedProvisioners[provisioner] = struct{}{}
		}
	}
	for _, driver := range inv.Storage.Drivers {
		if _, ok := matchedDrivers[driver.Name]; ok || driver.Name == "" {
			continue
		}
		components = append(components, inventory.Component{
			ID:              "csi/" + sanitize(driver.Name),
			Name:            driver.Name,
			Type:            inventory.TypeCSI,
			DetectionMethod: "csidriver",
			Confidence:      "medium",
			Evidence: []inventory.Evidence{{
				Kind:    "observation",
				Summary: "CSIDriver " + driver.Name + " exists. No version was inferred from the driver name.",
			}},
		})
	}
	for _, class := range inv.Storage.Classes {
		if class.Provisioner == "" || class.Provisioner == "kubernetes.io/no-provisioner" {
			continue
		}
		if _, ok := matchedProvisioners[class.Provisioner]; ok {
			continue
		}
		if _, ok := matchedDrivers[class.Provisioner]; ok {
			continue
		}
		components = append(components, inventory.Component{
			ID:              "storage/" + sanitize(class.Provisioner),
			Name:            class.Provisioner,
			Type:            inventory.TypeStorageProvisioner,
			DetectionMethod: "storage-class",
			Confidence:      "medium",
			Evidence: []inventory.Evidence{{
				Kind:    "observation",
				Summary: "StorageClass " + class.Name + " uses provisioner " + class.Provisioner + ". No version was inferred.",
			}},
		})
		matchedProvisioners[class.Provisioner] = struct{}{}
	}
	components = append(components, operators(inv)...)
	components = append(components, remainingHelm(inv)...)
	sort.Slice(components, func(i, j int) bool { return components[i].ID < components[j].ID })
	inv.Components = dedupe(components)
	detectDistribution(inv)
}

func platform(inv *inventory.Inventory) []inventory.Component {
	var out []inventory.Component
	if inv.Cluster.KubernetesVersion != "" {
		out = append(out, inventory.Component{
			ID:              "kubernetes/control-plane",
			Name:            "kubernetes",
			Type:            inventory.TypeControlPlane,
			Version:         inv.Cluster.KubernetesVersion,
			VersionSource:   "server-version",
			DetectionMethod: "discovery",
			Confidence:      "high",
			Evidence: []inventory.Evidence{{
				Kind:    "observation",
				Summary: "discovery.ServerVersion reports the kube-apiserver that answered",
			}},
		}, inventory.Component{
			ID:              "kubernetes/kube-apiserver",
			Name:            "kube-apiserver",
			Type:            inventory.TypeAPIServer,
			Version:         inv.Cluster.KubernetesVersion,
			VersionSource:   "server-version",
			DetectionMethod: "discovery",
			Confidence:      "high",
			Evidence:        append(observedEvidence(inv, inventory.TypeAPIServer), inventory.Evidence{Kind: "observation", Summary: "discovery server version is the contacted kube-apiserver"}),
		})
	}
	if component, ok := aggregate(inv, "kubernetes/kubelet", "kubelet", inventory.TypeKubelet, kubeletVersions(inv), "node-status"); ok {
		out = append(out, component)
	}
	for _, item := range []struct {
		id, name, typ string
	}{
		{"kubernetes/kube-proxy", "kube-proxy", inventory.TypeKubeProxy},
		{"kubernetes/kube-controller-manager", "kube-controller-manager", inventory.TypeControllerManager},
		{"kubernetes/kube-scheduler", "kube-scheduler", inventory.TypeScheduler},
		{"kubernetes/cloud-controller-manager", "cloud-controller-manager", inventory.TypeCloudControllerManager},
		{"kubernetes/coredns", "coredns", inventory.TypeCoreDNS},
		{"kubernetes/etcd", "etcd", inventory.TypeEtcd},
	} {
		versions := observedVersions(inv, item.typ)
		if len(versions) == 0 && !workloadNamed(inv, item.name) {
			continue
		}
		if component, ok := aggregate(inv, item.id, item.name, item.typ, versions, "pod-or-template"); ok || len(versions) == 0 {
			if len(versions) == 0 {
				component = inventory.Component{
					ID:              item.id,
					Name:            item.name,
					Type:            item.typ,
					DetectionMethod: "workload-name",
					Confidence:      "medium",
					Evidence: []inventory.Evidence{{
						Kind:    "observation",
						Summary: item.name + " was detected by name. No semantic version was found.",
					}},
				}
			}
			out = append(out, component)
		}
	}
	if runtime, ok := runtimeComponent(inv); ok {
		out = append(out, runtime)
	}
	return out
}

func aggregate(inv *inventory.Inventory, id, name, typ string, versions []string, method string) (inventory.Component, bool) {
	if len(versions) == 0 {
		return inventory.Component{}, false
	}
	component := inventory.Component{
		ID:              id,
		Name:            name,
		Type:            typ,
		DetectionMethod: method,
		Confidence:      "high",
		Evidence:        observedEvidence(inv, typ),
	}
	if same(versions) {
		component.Version = versions[0]
		component.VersionSource = method
	} else {
		component.VersionSource = ""
		component.Evidence = append(component.Evidence, inventory.Evidence{
			Kind:    "observation",
			Summary: "mixed versions: " + strings.Join(unique(versions), ", "),
		})
	}
	return component, true
}

func runtimeComponent(inv *inventory.Inventory) (inventory.Component, bool) {
	var names, versions []string
	for _, node := range inv.Nodes {
		if node.ContainerRuntime == "" {
			continue
		}
		names = append(names, node.ContainerRuntime)
		if node.ContainerRuntimeVersion != "" {
			versions = append(versions, node.ContainerRuntimeVersion)
		}
	}
	if len(names) == 0 {
		return inventory.Component{}, false
	}
	name := names[0]
	if !same(names) {
		name = "mixed"
	}
	component := inventory.Component{
		ID:              "runtime/" + sanitize(name),
		Name:            name,
		Type:            inventory.TypeRuntime,
		DetectionMethod: "node-status",
		Confidence:      "medium",
		Evidence: []inventory.Evidence{{
			Kind:    "observation",
			Summary: "container runtime was copied from node status and was not verified by a node agent",
		}},
	}
	if name != "mixed" && same(versions) && len(versions) > 0 {
		component.Version = versions[0]
		component.VersionSource = "node-status"
	}
	return component, true
}

func match(inv *inventory.Inventory, sig signature) (inventory.Component, bool) {
	var evidence []inventory.Evidence
	confidence := ""
	var appVersion, chartVersion, kubeVersion, namespace, imageTag string
	imageMixed := false
	for i := range inv.HelmReleases {
		rel := &inv.HelmReleases[i]
		if !equalsAny(sig.charts, rel.Chart) && !equalsAny(sig.charts, rel.Name) {
			continue
		}
		appVersion = rel.AppVersion
		chartVersion = rel.ChartVersion
		kubeVersion = rel.KubeVersion
		namespace = rel.Namespace
		confidence = "high"
		evidence = append(evidence, inventory.Evidence{
			Kind:    "observation",
			Summary: fmt.Sprintf("Helm release %s/%s chart %s chartVersion %s appVersion %s", rel.Namespace, rel.Name, rel.Chart, rel.ChartVersion, rel.AppVersion),
		})
		rel.ClassifiedAs = sig.component + "/" + sig.name
	}
	for _, workload := range inv.Workloads {
		if equalsAny(sig.workloads, workload.Name) {
			confidence = prefer("high", confidence)
			evidence = append(evidence, inventory.Evidence{Kind: "observation", Summary: workload.Kind + " " + workload.Namespace + "/" + workload.Name})
		}
		tag, observedTag, mixed, found := imageMatch(workload.Images, sig.images)
		if found {
			if confidence == "" {
				confidence = "medium"
			}
			summary := "image match on " + workload.Kind + " " + workload.Name
			if observedTag != "" {
				summary += " tag " + observedTag
			}
			if observedTag != "" && tag == "" && !mixed {
				summary += " (not a semantic version, so no component version was selected)"
			}
			evidence = append(evidence, inventory.Evidence{Kind: "observation", Summary: summary})
		}
		if tag != "" {
			if imageTag != "" && imageTag != tag {
				imageMixed = true
			}
			imageTag = tag
		}
		if mixed {
			imageMixed = true
		}
	}
	for _, crd := range inv.CRDs {
		if equalsAny(sig.crdGroups, crd.Group) {
			if confidence == "" {
				confidence = "medium"
			}
			evidence = append(evidence, inventory.Evidence{Kind: "observation", Summary: "CRD group " + crd.Group})
		}
	}
	for _, driver := range inv.Storage.Drivers {
		if equalsAny(sig.drivers, driver.Name) {
			confidence = prefer("high", confidence)
			evidence = append(evidence, inventory.Evidence{Kind: "observation", Summary: "CSIDriver " + driver.Name})
		}
	}
	for _, class := range inv.Storage.Classes {
		if equalsAny(sig.provisioners, class.Provisioner) {
			if confidence == "" {
				confidence = "medium"
			}
			evidence = append(evidence, inventory.Evidence{Kind: "observation", Summary: "StorageClass " + class.Name + " provisioner " + class.Provisioner})
		}
	}
	if confidence == "" {
		return inventory.Component{}, false
	}
	component := inventory.Component{
		ID:              sig.component + "/" + sig.name,
		Name:            sig.name,
		Type:            sig.component,
		Namespace:       namespace,
		AppVersion:      appVersion,
		ChartVersion:    chartVersion,
		KubeVersion:     kubeVersion,
		DetectionMethod: methodFrom(evidence),
		Confidence:      confidence,
		Evidence:        evidence,
	}
	if parsed, ok := semverTag(appVersion); ok {
		component.Version = parsed
		component.VersionSource = "helm-appVersion"
	} else if !imageMixed {
		if parsed, ok := semverTag(imageTag); ok {
			component.Version = parsed
			component.VersionSource = "image-tag"
			if confidence == "high" && appVersion == "" {
				component.Confidence = "medium"
			}
		}
	}
	if imageMixed {
		component.Evidence = append(component.Evidence, inventory.Evidence{Kind: "observation", Summary: "image tags disagreed, so no image version was selected"})
	}
	return component, true
}

func operators(inv *inventory.Inventory) []inventory.Component {
	var out []inventory.Component
	seen := map[string]struct{}{}
	for i := range inv.HelmReleases {
		rel := &inv.HelmReleases[i]
		if rel.ClassifiedAs != "" || !strings.HasSuffix(rel.Chart, "-operator") && !strings.HasSuffix(rel.Name, "-operator") {
			continue
		}
		id := "operator/" + sanitize(rel.Namespace+"-"+rel.Name)
		rel.ClassifiedAs = id
		component := inventory.Component{
			ID:              id,
			Name:            rel.Name,
			Type:            inventory.TypeOperator,
			Namespace:       rel.Namespace,
			AppVersion:      rel.AppVersion,
			ChartVersion:    rel.ChartVersion,
			KubeVersion:     rel.KubeVersion,
			DetectionMethod: "helm-name",
			Confidence:      "medium",
			Evidence: []inventory.Evidence{{
				Kind:    "observation",
				Summary: "Name suffix -operator is a heuristic. Chart version and app version are kept separate.",
			}},
		}
		if parsed, ok := semverTag(rel.AppVersion); ok {
			component.Version = parsed
			component.VersionSource = "helm-appVersion"
		}
		out = append(out, component)
		seen[id] = struct{}{}
	}
	return out
}

func remainingHelm(inv *inventory.Inventory) []inventory.Component {
	var out []inventory.Component
	for _, rel := range inv.HelmReleases {
		if rel.ClassifiedAs != "" {
			continue
		}
		component := inventory.Component{
			ID:              "helm/" + sanitize(rel.Namespace+"-"+rel.Name),
			Name:            rel.Name,
			Type:            inventory.TypeHelm,
			Namespace:       rel.Namespace,
			AppVersion:      rel.AppVersion,
			ChartVersion:    rel.ChartVersion,
			KubeVersion:     rel.KubeVersion,
			DetectionMethod: "helm",
			Confidence:      "high",
			Evidence: []inventory.Evidence{{
				Kind:    "observation",
				Summary: fmt.Sprintf("Helm release %s/%s chart %s chartVersion %s appVersion %s. Values were omitted.", rel.Namespace, rel.Name, rel.Chart, rel.ChartVersion, rel.AppVersion),
			}},
		}
		if parsed, ok := semverTag(rel.AppVersion); ok {
			component.Version = parsed
			component.VersionSource = "helm-appVersion"
		}
		out = append(out, component)
	}
	return out
}

func detectDistribution(inv *inventory.Inventory) {
	if inv.Cluster.Distribution != "" {
		return
	}
	found := map[string]string{}
	var evidence []inventory.Evidence
	for _, node := range inv.Nodes {
		name, confidence, summary := nodeDistribution(node)
		if name == "" {
			continue
		}
		found[name] = confidence
		evidence = append(evidence, inventory.Evidence{Kind: "observation", Summary: node.Name + ": " + summary})
	}
	if len(found) == 1 {
		for name, confidence := range found {
			inv.Cluster.Distribution = name
			inv.Cluster.DistributionConfidence = confidence
		}
		inv.Cluster.DistributionEvidence = evidence
		return
	}
	if len(found) > 1 {
		inv.Cluster.DistributionEvidence = append(evidence, inventory.Evidence{Kind: "observation", Summary: "nodes disagreed, so no distribution was selected"})
		return
	}
	version := inv.Cluster.KubernetesVersion
	switch {
	case strings.Contains(version, "+k3s"):
		inv.Cluster.Distribution = "k3s"
		inv.Cluster.DistributionConfidence = "medium"
		inv.Cluster.DistributionEvidence = []inventory.Evidence{{Kind: "observation", Summary: "server version contains +k3s"}}
	case strings.Contains(version, "+rke2"):
		inv.Cluster.Distribution = "rke2"
		inv.Cluster.DistributionConfidence = "medium"
		inv.Cluster.DistributionEvidence = []inventory.Evidence{{Kind: "observation", Summary: "server version contains +rke2"}}
	}
}

func nodeDistribution(node inventory.Node) (string, string, string) {
	if strings.HasPrefix(node.ProviderID, "kind://") {
		return "kind", "high", "providerID " + node.ProviderID
	}
	if _, ok := node.Labels["eks.amazonaws.com/nodegroup"]; ok {
		return "eks", "high", "label eks.amazonaws.com/nodegroup"
	}
	if _, ok := node.Labels["cloud.google.com/gke-nodepool"]; ok {
		return "gke", "high", "label cloud.google.com/gke-nodepool"
	}
	if _, ok := node.Labels["kubernetes.azure.com/cluster"]; ok {
		return "aks", "high", "label kubernetes.azure.com/cluster"
	}
	if _, ok := node.Labels["node.openshift.io/os_id"]; ok {
		return "openshift", "high", "label node.openshift.io/os_id"
	}
	return "", "", ""
}

func cniSignatures() []signature {
	return []signature{
		{name: "cilium", component: inventory.TypeCNI, workloads: []string{"cilium", "cilium-agent"}, images: []string{"cilium/cilium"}, crdGroups: []string{"cilium.io"}, charts: []string{"cilium"}},
		{name: "calico", component: inventory.TypeCNI, workloads: []string{"calico-node"}, images: []string{"calico/node"}, crdGroups: []string{"crd.projectcalico.org", "projectcalico.org"}, charts: []string{"calico", "tigera-operator"}},
		{name: "flannel", component: inventory.TypeCNI, workloads: []string{"kube-flannel-ds", "kube-flannel"}, images: []string{"flannel"}, charts: []string{"flannel"}},
		{name: "kindnet", component: inventory.TypeCNI, workloads: []string{"kindnet"}, images: []string{"kindnetd"}},
		{name: "aws-vpc-cni", component: inventory.TypeCNI, workloads: []string{"aws-node"}, images: []string{"amazon-k8s-cni"}},
		{name: "antrea", component: inventory.TypeCNI, workloads: []string{"antrea-agent"}, images: []string{"antrea/antrea"}, crdGroups: []string{"crd.antrea.io"}, charts: []string{"antrea"}},
		{name: "weave", component: inventory.TypeCNI, workloads: []string{"weave-net"}, images: []string{"weaveworks/weave-kube"}},
	}
}

func csiSignatures() []signature {
	return []signature{
		{name: "aws-ebs-csi-driver", component: inventory.TypeCSI, drivers: []string{"ebs.csi.aws.com"}, images: []string{"aws-ebs-csi-driver"}, charts: []string{"aws-ebs-csi-driver"}, provisioners: []string{"ebs.csi.aws.com"}},
		{name: "gcp-pd-csi-driver", component: inventory.TypeCSI, drivers: []string{"pd.csi.storage.gke.io"}, images: []string{"gcp-compute-persistent-disk-csi-driver"}, provisioners: []string{"pd.csi.storage.gke.io"}},
		{name: "azuredisk-csi-driver", component: inventory.TypeCSI, drivers: []string{"disk.csi.azure.com"}, images: []string{"azuredisk-csi"}, provisioners: []string{"disk.csi.azure.com"}},
		{name: "longhorn", component: inventory.TypeCSI, drivers: []string{"driver.longhorn.io"}, images: []string{"longhornio/longhorn-manager"}, crdGroups: []string{"longhorn.io"}, charts: []string{"longhorn"}, provisioners: []string{"driver.longhorn.io"}},
		{name: "local-path-provisioner", component: inventory.TypeStorageProvisioner, workloads: []string{"local-path-provisioner"}, images: []string{"local-path-provisioner"}, charts: []string{"local-path-provisioner"}, provisioners: []string{"rancher.io/local-path"}},
	}
}

func observedVersions(inv *inventory.Inventory, component string) []string {
	var out []string
	for _, observed := range inv.ObservedVersions {
		if observed.Component == component && observed.Version != "" {
			out = append(out, observed.Version)
		}
	}
	return out
}

func observedEvidence(inv *inventory.Inventory, component string) []inventory.Evidence {
	var out []inventory.Evidence
	for _, observed := range inv.ObservedVersions {
		if observed.Component != component {
			continue
		}
		out = append(out, inventory.Evidence{Kind: "observation", Summary: fmt.Sprintf("%s %s from %s (%s)", component, observed.Version, observed.Source, observed.Example)})
		if len(out) == 5 {
			break
		}
	}
	return out
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

func workloadNamed(inv *inventory.Inventory, name string) bool {
	for _, workload := range inv.Workloads {
		if workload.Name == name {
			return true
		}
	}
	return false
}

func imageMatch(images, needles []string) (tag, observedTag string, mixed, found bool) {
	var raw []string
	for _, image := range images {
		repo, imageTag := ref.ImageNameAndTag(image)
		for _, needle := range needles {
			if !strings.Contains(repo, needle) && !strings.Contains(image, needle) {
				continue
			}
			found = true
			if imageTag != "" && !contains(raw, imageTag) {
				raw = append(raw, imageTag)
			}
			if parsed, ok := semverTag(imageTag); ok {
				if tag != "" && tag != parsed {
					mixed = true
				}
				tag = parsed
			}
		}
	}
	if len(raw) == 1 {
		observedTag = raw[0]
	} else if len(raw) > 1 {
		mixed = true
		observedTag = strings.Join(raw, ", ")
	}
	return tag, observedTag, mixed, found
}

func semverTag(tag string) (string, bool) {
	if _, err := semver.Parse(tag); err != nil {
		return "", false
	}
	return tag, true
}

func methodFrom(evidence []inventory.Evidence) string {
	for _, item := range evidence {
		if strings.Contains(item.Summary, "Helm release") {
			return "helm"
		}
	}
	for _, item := range evidence {
		if strings.HasPrefix(item.Summary, "DaemonSet") || strings.HasPrefix(item.Summary, "Deployment") {
			return "workload"
		}
	}
	if len(evidence) > 0 {
		return "evidence"
	}
	return "unknown"
}

func contains(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func equalsAny(options []string, value string) bool {
	for _, option := range options {
		if option == value {
			return true
		}
	}
	return false
}

func prefer(next, current string) string {
	if current == "" || current == "medium" {
		return next
	}
	return current
}

func same(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values[1:] {
		if value != values[0] {
			return false
		}
	}
	return true
}

func unique(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func dedupe(components []inventory.Component) []inventory.Component {
	if len(components) == 0 {
		return []inventory.Component{}
	}
	out := []inventory.Component{components[0]}
	for _, component := range components[1:] {
		if component.ID == out[len(out)-1].ID {
			continue
		}
		out = append(out, component)
	}
	return out
}

func sanitize(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, value)
}
