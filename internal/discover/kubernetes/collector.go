// Package kubernetes collects read-only API facts.
// It does not create, update, patch, or delete objects.
package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/discover/ref"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
)

// Collect fills cluster, node, workload, API, storage, and webhook facts.
func Collect(ctx context.Context, cfg *rest.Config, inv *inventory.Inventory) error {
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}
	info, err := client.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("read server version: %w", err)
	}
	inv.Cluster.KubernetesVersion = info.GitVersion
	inv.Cluster.Platform = info.Platform

	groups, resources, err := client.Discovery().ServerGroupsAndResources()
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		inv.Discovery.Warnings = append(inv.Discovery.Warnings, "api discovery: "+err.Error())
	} else if err != nil {
		inv.Discovery.Warnings = append(inv.Discovery.Warnings, "api discovery was partial: "+err.Error())
	}
	preferred := map[string]struct{}{}
	for _, group := range groups {
		if group.PreferredVersion.GroupVersion != "" {
			preferred[group.PreferredVersion.GroupVersion] = struct{}{}
		}
	}
	for _, list := range resources {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		_, isPreferred := preferred[list.GroupVersion]
		for _, resource := range list.APIResources {
			if strings.Contains(resource.Name, "/") {
				continue
			}
			inv.APIResources = append(inv.APIResources, inventory.APIResource{
				Group:      gv.Group,
				Version:    gv.Version,
				Kind:       resource.Kind,
				Resource:   resource.Name,
				Namespaced: resource.Namespaced,
			})
			if !isPreferred || !interesting(resource.Kind) {
				continue
			}
			if err := listResource(ctx, dyn, inv, gv, resource.Name, resource.Kind); err != nil {
				inv.Discovery.Warnings = append(inv.Discovery.Warnings, fmt.Sprintf("list %s %s: %s", list.GroupVersion, resource.Name, err.Error()))
			}
		}
	}
	if err := collectNodes(ctx, client, inv); err != nil {
		inv.Discovery.Warnings = append(inv.Discovery.Warnings, "nodes: "+err.Error())
	}
	if err := collectPods(ctx, client, inv); err != nil {
		inv.Discovery.Warnings = append(inv.Discovery.Warnings, "pods: "+err.Error())
	}
	if err := collectEvents(ctx, client, inv); err != nil {
		inv.Discovery.Warnings = append(inv.Discovery.Warnings, "events: "+err.Error())
	}
	sort.Slice(inv.APIResources, func(i, j int) bool {
		if inv.APIResources[i].Group != inv.APIResources[j].Group {
			return inv.APIResources[i].Group < inv.APIResources[j].Group
		}
		if inv.APIResources[i].Version != inv.APIResources[j].Version {
			return inv.APIResources[i].Version < inv.APIResources[j].Version
		}
		return inv.APIResources[i].Kind < inv.APIResources[j].Kind
	})
	sort.Slice(inv.Workloads, func(i, j int) bool {
		if inv.Workloads[i].Kind != inv.Workloads[j].Kind {
			return inv.Workloads[i].Kind < inv.Workloads[j].Kind
		}
		if inv.Workloads[i].Namespace != inv.Workloads[j].Namespace {
			return inv.Workloads[i].Namespace < inv.Workloads[j].Namespace
		}
		return inv.Workloads[i].Name < inv.Workloads[j].Name
	})
	return nil
}

func interesting(kind string) bool {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "Ingress",
		"HorizontalPodAutoscaler", "PodDisruptionBudget", "NetworkPolicy",
		"ValidatingWebhookConfiguration", "MutatingWebhookConfiguration",
		"CustomResourceDefinition", "StorageClass", "CSIDriver",
		"PersistentVolume", "PersistentVolumeClaim":
		return true
	default:
		return false
	}
}

func collectNodes(ctx context.Context, client kubernetes.Interface, inv *inventory.Inventory) error {
	list, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, node := range list.Items {
		runtimeName, runtimeVersion := ref.ParseRuntime(node.Status.NodeInfo.ContainerRuntimeVersion)
		ready := nodeReady(node)
		role := "worker"
		if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
			role = "control-plane"
		}
		if _, ok := node.Labels["node-role.kubernetes.io/master"]; ok {
			role = "control-plane"
		}
		inv.Nodes = append(inv.Nodes, inventory.Node{
			Name:                    node.Name,
			Role:                    role,
			KubeletVersion:          node.Status.NodeInfo.KubeletVersion,
			OperatingSystem:         node.Status.NodeInfo.OperatingSystem,
			OSImage:                 node.Status.NodeInfo.OSImage,
			ReportedKernelVersion:   node.Status.NodeInfo.KernelVersion,
			Architecture:            node.Status.NodeInfo.Architecture,
			ContainerRuntime:        runtimeName,
			ContainerRuntimeVersion: runtimeVersion,
			ProviderID:              node.Spec.ProviderID,
			Unschedulable:           node.Spec.Unschedulable,
			Ready:                   ready,
			Labels:                  node.Labels,
		})
	}
	return nil
}

func nodeReady(node corev1.Node) *bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			ready := condition.Status == corev1.ConditionTrue
			return &ready
		}
	}
	return nil
}

func collectPods(ctx context.Context, client kubernetes.Interface, inv *inventory.Inventory) error {
	list, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	summary := inventory.PodSummary{Collected: true, ByPhase: map[string]int{}}
	for _, pod := range list.Items {
		summary.Total++
		summary.ByPhase[string(pod.Status.Phase)]++
		ready := false
		sawReady := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady {
				sawReady = true
				ready = condition.Status == corev1.ConditionTrue
			}
			if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse && condition.Reason == corev1.PodReasonUnschedulable {
				summary.Unschedulable++
				addExample(&summary, pod.Namespace+"/"+pod.Name+" unschedulable")
			}
		}
		if (pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning) && (!sawReady || !ready) {
			summary.NotReady++
			addExample(&summary, pod.Namespace+"/"+pod.Name+" not ready")
		}
		observePodContainers(inv, pod.Namespace+"/"+pod.Name, pod.Spec.Containers)
		observePodContainers(inv, pod.Namespace+"/"+pod.Name, pod.Spec.InitContainers)
	}
	inv.Pods = summary
	return nil
}

func observePodContainers(inv *inventory.Inventory, example string, containers []corev1.Container) {
	for _, container := range containers {
		component, version, ok := ref.KnownControlPlane(container.Name, container.Image)
		if !ok {
			continue
		}
		inv.AddObservedVersion(component, version, container.Image, "pod", example)
	}
}

func addExample(summary *inventory.PodSummary, example string) {
	if len(summary.UnhealthyExamples) < 10 {
		summary.UnhealthyExamples = append(summary.UnhealthyExamples, example)
	}
}

func collectEvents(ctx context.Context, client kubernetes.Interface, inv *inventory.Inventory) error {
	var events []corev1.Event
	token := ""
	for len(events) < 500 {
		list, err := client.CoreV1().Events("").List(ctx, metav1.ListOptions{
			FieldSelector: "type=Warning",
			Limit:         200,
			Continue:      token,
		})
		if err != nil {
			return err
		}
		events = append(events, list.Items...)
		if list.Continue == "" {
			break
		}
		token = list.Continue
	}
	sort.Slice(events, func(i, j int) bool {
		return eventTime(events[i]).After(eventTime(events[j]))
	})
	if len(events) > 50 {
		inv.Discovery.Warnings = append(inv.Discovery.Warnings, "warning events were truncated to the 50 most recent of those listed")
		events = events[:50]
	}
	for _, event := range events {
		message := event.Message
		if len(message) > 300 {
			message = message[:300]
		}
		message = strings.ReplaceAll(message, "\n", " ")
		inv.Events = append(inv.Events, inventory.Event{
			Type:         event.Type,
			Reason:       event.Reason,
			Message:      message,
			Count:        int(event.Count),
			Namespace:    event.Namespace,
			InvolvedKind: event.InvolvedObject.Kind,
			InvolvedName: event.InvolvedObject.Name,
			LastSeen:     eventTime(event).UTC().Format(time.RFC3339),
		})
	}
	return nil
}

func eventTime(event corev1.Event) time.Time {
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	return event.CreationTimestamp.Time
}
