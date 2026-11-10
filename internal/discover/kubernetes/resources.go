package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/discover/ref"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/inventory"
)

const listCap = 2000

func listResource(ctx context.Context, dyn dynamic.Interface, inv *inventory.Inventory, gv schema.GroupVersion, resource, kind string) error {
	gvr := schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: resource}
	list, err := dyn.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	items := list.Items
	if len(items) > listCap {
		inv.Discovery.Warnings = append(inv.Discovery.Warnings, fmt.Sprintf("%s %s truncated from %d to %d objects", gv.String(), resource, len(items), listCap))
		items = items[:listCap]
	}
	for _, item := range items {
		recordUsage(inv, item)
		switch kind {
		case "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob":
			inv.Workloads = append(inv.Workloads, workload(inv, item, gv))
		case "ValidatingWebhookConfiguration", "MutatingWebhookConfiguration":
			inv.Webhooks = append(inv.Webhooks, webhooks(item, kind)...)
		case "CustomResourceDefinition":
			if crd, ok := crd(item); ok {
				inv.CRDs = append(inv.CRDs, crd)
			}
		case "StorageClass":
			if class, ok := storageClass(item); ok {
				inv.Storage.Classes = append(inv.Storage.Classes, class)
			}
		case "CSIDriver":
			if name := item.GetName(); name != "" {
				inv.Storage.Drivers = append(inv.Storage.Drivers, inventory.CSIDriver{Name: name})
			}
		case "PersistentVolume":
			inv.Storage.PersistentVolumes = append(inv.Storage.PersistentVolumes, volume(item))
		case "PersistentVolumeClaim":
			inv.Storage.PersistentClaims = append(inv.Storage.PersistentClaims, claim(item))
		case "PodDisruptionBudget":
			inv.PodDisruptionBudgets = append(inv.PodDisruptionBudgets, pdb(item))
		case "NetworkPolicy":
			inv.NetworkPolicies = append(inv.NetworkPolicies, networkPolicy(item))
		case "Ingress", "HorizontalPodAutoscaler":
			inv.Workloads = append(inv.Workloads, workload(inv, item, gv))
		}
	}
	return nil
}

func recordUsage(inv *inventory.Inventory, item unstructured.Unstructured) {
	example := item.GetNamespace()
	if example != "" {
		example += "/"
	}
	example += item.GetName()
	for _, managed := range item.GetManagedFields() {
		group, version := inventory.ParseAPIVersion(managed.APIVersion)
		inv.AddAPIUsage(group, version, item.GetKind(), "managed-fields", example)
	}
	raw := item.GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]
	if raw == "" {
		return
	}
	var head struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(raw), &head); err != nil || head.APIVersion == "" {
		return
	}
	kind := head.Kind
	if kind == "" {
		kind = item.GetKind()
	}
	group, version := inventory.ParseAPIVersion(head.APIVersion)
	inv.AddAPIUsage(group, version, kind, "last-applied-configuration", example)
}

func workload(inv *inventory.Inventory, item unstructured.Unstructured, gv schema.GroupVersion) inventory.Workload {
	images := containerImages(item)
	example := item.GetNamespace() + "/" + item.GetName()
	for _, image := range images {
		component, version, ok := ref.KnownControlPlane(imageBase(image), image)
		if ok {
			inv.AddObservedVersion(component, version, image, "pod-template", example)
		}
	}
	replicas := intField(item, "spec", "replicas")
	ready := intField(item, "status", "readyReplicas")
	if item.GetKind() == "DaemonSet" {
		replicas = intField(item, "status", "desiredNumberScheduled")
		ready = intField(item, "status", "numberReady")
	}
	release := ""
	annotations := item.GetAnnotations()
	if name := annotations["meta.helm.sh/release-name"]; name != "" {
		release = annotations["meta.helm.sh/release-namespace"] + "/" + name
	}
	return inventory.Workload{
		APIVersion:    gv.String(),
		Kind:          item.GetKind(),
		Namespace:     item.GetNamespace(),
		Name:          item.GetName(),
		Replicas:      replicas,
		ReadyReplicas: ready,
		Images:        images,
		HelmRelease:   release,
	}
}

func imageBase(image string) string {
	name, _ := ref.ImageNameAndTag(image)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func containerImages(item unstructured.Unstructured) []string {
	var images []string
	paths := [][]string{
		{"spec", "template", "spec", "containers"},
		{"spec", "template", "spec", "initContainers"},
		{"spec", "jobTemplate", "spec", "template", "spec", "containers"},
		{"spec", "jobTemplate", "spec", "template", "spec", "initContainers"},
	}
	seen := map[string]struct{}{}
	for _, path := range paths {
		arr, found, err := unstructured.NestedSlice(item.Object, path...)
		if err != nil || !found {
			continue
		}
		for _, entry := range arr {
			container, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			image, _, _ := unstructured.NestedString(container, "image")
			if image == "" {
				continue
			}
			if _, ok := seen[image]; ok {
				continue
			}
			seen[image] = struct{}{}
			images = append(images, image)
		}
	}
	return images
}

func webhooks(item unstructured.Unstructured, kind string) []inventory.Webhook {
	arr, found, err := unstructured.NestedSlice(item.Object, "webhooks")
	if err != nil || !found {
		return nil
	}
	var out []inventory.Webhook
	for _, entry := range arr {
		hook, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		wh := inventory.Webhook{
			ConfigurationKind: kind,
			Configuration:     item.GetName(),
			Name:              nestedString(hook, "name"),
			FailurePolicy:     nestedString(hook, "failurePolicy"),
			MatchPolicy:       nestedString(hook, "matchPolicy"),
			SideEffects:       nestedString(hook, "sideEffects"),
			ServiceName:       nestedString(hook, "clientConfig", "service", "name"),
			ServiceNamespace:  nestedString(hook, "clientConfig", "service", "namespace"),
			TimeoutSeconds:    intPtr(hook, "timeoutSeconds"),
		}
		rules, _, _ := unstructured.NestedSlice(hook, "rules")
		for _, ruleEntry := range rules {
			rule, ok := ruleEntry.(map[string]any)
			if !ok {
				continue
			}
			wh.APIGroups = appendUnique(wh.APIGroups, stringSlice(rule, "apiGroups")...)
			wh.APIVersions = appendUnique(wh.APIVersions, stringSlice(rule, "apiVersions")...)
			wh.Resources = appendUnique(wh.Resources, stringSlice(rule, "resources")...)
			wh.Operations = appendUnique(wh.Operations, stringSlice(rule, "operations")...)
		}
		out = append(out, wh)
	}
	return out
}

func crd(item unstructured.Unstructured) (inventory.CRD, bool) {
	out := inventory.CRD{
		Name:  item.GetName(),
		Group: nestedString(item.Object, "spec", "group"),
		Kind:  nestedString(item.Object, "spec", "names", "kind"),
		Scope: nestedString(item.Object, "spec", "scope"),
	}
	versions, found, _ := unstructured.NestedSlice(item.Object, "spec", "versions")
	if found {
		for _, entry := range versions {
			version, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			out.Versions = append(out.Versions, inventory.CRDVersion{
				Name:    nestedString(version, "name"),
				Served:  nestedBool(version, "served"),
				Storage: nestedBool(version, "storage"),
			})
		}
	}
	return out, out.Name != ""
}

func storageClass(item unstructured.Unstructured) (inventory.StorageClass, bool) {
	class := inventory.StorageClass{
		Name:              item.GetName(),
		Provisioner:       nestedString(item.Object, "provisioner"),
		ReclaimPolicy:     nestedString(item.Object, "reclaimPolicy"),
		VolumeBindingMode: nestedString(item.Object, "volumeBindingMode"),
	}
	if value, found, err := unstructured.NestedBool(item.Object, "allowVolumeExpansion"); err == nil && found {
		class.AllowExpansion = &value
	}
	return class, class.Name != ""
}

func volume(item unstructured.Unstructured) inventory.Volume {
	return inventory.Volume{
		Name:          item.GetName(),
		Phase:         nestedString(item.Object, "status", "phase"),
		StorageClass:  nestedString(item.Object, "spec", "storageClassName"),
		Driver:        nestedString(item.Object, "spec", "csi", "driver"),
		Capacity:      nestedString(item.Object, "spec", "capacity", "storage"),
		ReclaimPolicy: nestedString(item.Object, "spec", "persistentVolumeReclaimPolicy"),
	}
}

func claim(item unstructured.Unstructured) inventory.VolumeClaim {
	return inventory.VolumeClaim{
		Name:         item.GetName(),
		Namespace:    item.GetNamespace(),
		Phase:        nestedString(item.Object, "status", "phase"),
		StorageClass: nestedString(item.Object, "spec", "storageClassName"),
		Capacity:     nestedString(item.Object, "spec", "resources", "requests", "storage"),
	}
}

func pdb(item unstructured.Unstructured) inventory.PodDisruptionBudget {
	minAvailable, _, _ := unstructured.NestedFieldNoCopy(item.Object, "spec", "minAvailable")
	maxUnavailable, _, _ := unstructured.NestedFieldNoCopy(item.Object, "spec", "maxUnavailable")
	return inventory.PodDisruptionBudget{
		Name:               item.GetName(),
		Namespace:          item.GetNamespace(),
		MinAvailable:       scalarString(minAvailable),
		MaxUnavailable:     scalarString(maxUnavailable),
		CurrentHealthy:     intField(item, "status", "currentHealthy"),
		DesiredHealthy:     intField(item, "status", "desiredHealthy"),
		DisruptionsAllowed: intField(item, "status", "disruptionsAllowed"),
		ExpectedPods:       intField(item, "status", "expectedPods"),
	}
}

func networkPolicy(item unstructured.Unstructured) inventory.NetworkPolicy {
	ingress, _, _ := unstructured.NestedSlice(item.Object, "spec", "ingress")
	egress, _, _ := unstructured.NestedSlice(item.Object, "spec", "egress")
	return inventory.NetworkPolicy{
		Name:             item.GetName(),
		Namespace:        item.GetNamespace(),
		PolicyTypes:      stringSlice(item.Object, "spec", "policyTypes"),
		IngressRuleCount: len(ingress),
		EgressRuleCount:  len(egress),
	}
}

func intField(item unstructured.Unstructured, fields ...string) *int {
	value, found, err := unstructured.NestedFieldNoCopy(item.Object, fields...)
	if err != nil || !found {
		return nil
	}
	return intPtrValue(value)
}

func intPtr(obj map[string]any, fields ...string) *int {
	value, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil || !found {
		return nil
	}
	return intPtrValue(value)
}

func intPtrValue(value any) *int {
	switch n := value.(type) {
	case int64:
		v := int(n)
		return &v
	case int32:
		v := int(n)
		return &v
	case int:
		v := n
		return &v
	case float64:
		v := int(n)
		return &v
	default:
		return nil
	}
}

func nestedString(obj any, fields ...string) string {
	switch typed := obj.(type) {
	case unstructured.Unstructured:
		value, _, _ := unstructured.NestedString(typed.Object, fields...)
		return value
	case map[string]any:
		value, _, _ := unstructured.NestedString(typed, fields...)
		return value
	default:
		return ""
	}
}

func nestedBool(obj map[string]any, fields ...string) bool {
	value, _, _ := unstructured.NestedBool(obj, fields...)
	return value
}

func stringSlice(obj any, fields ...string) []string {
	var arr []any
	var found bool
	switch typed := obj.(type) {
	case map[string]any:
		arr, found, _ = unstructured.NestedSlice(typed, fields...)
	case unstructured.Unstructured:
		arr, found, _ = unstructured.NestedSlice(typed.Object, fields...)
	}
	if !found {
		return nil
	}
	var out []string
	for _, entry := range arr {
		if text, ok := entry.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return fmt.Sprint(typed)
	}
}

func appendUnique(dst []string, values ...string) []string {
	for _, value := range values {
		found := false
		for _, existing := range dst {
			if existing == value {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, value)
		}
	}
	return dst
}
