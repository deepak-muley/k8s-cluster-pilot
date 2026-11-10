// Package inventory is the versioned discovery contract.
// Analyzers and planners consume this document and do not talk to the cluster.
package inventory

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SchemaVersion is the inventory document contract implemented by this POC.
const SchemaVersion = "v1alpha1"

// Component types stored in the inventory.
const (
	TypeKubernetes             = "kubernetes"
	TypeControlPlane           = "control-plane"
	TypeAPIServer              = "kube-apiserver"
	TypeControllerManager      = "kube-controller-manager"
	TypeScheduler              = "kube-scheduler"
	TypeCloudControllerManager = "cloud-controller-manager"
	TypeKubelet                = "kubelet"
	TypeKubeProxy              = "kube-proxy"
	TypeCoreDNS                = "coredns"
	TypeEtcd                   = "etcd"
	TypeRuntime                = "runtime"
	TypeCNI                    = "cni"
	TypeCSI                    = "csi"
	TypeStorageProvisioner     = "storage-provisioner"
	TypeHelm                   = "helm"
	TypeOperator               = "operator"
)

// Inventory is the normalized cluster snapshot.
type Inventory struct {
	SchemaVersion        string                `json:"schemaVersion"`
	Cluster              Cluster               `json:"cluster"`
	Nodes                []Node                `json:"nodes"`
	Components           []Component           `json:"components"`
	Workloads            []Workload            `json:"workloads"`
	APIResources         []APIResource         `json:"apiResources"`
	APIUsages            []APIUsage            `json:"apiUsages"`
	CRDs                 []CRD                 `json:"crds"`
	Webhooks             []Webhook             `json:"webhooks"`
	Storage              Storage               `json:"storage"`
	PodDisruptionBudgets []PodDisruptionBudget `json:"podDisruptionBudgets"`
	NetworkPolicies      []NetworkPolicy       `json:"networkPolicies"`
	HelmReleases         []HelmRelease         `json:"helmReleases"`
	Pods                 PodSummary            `json:"pods"`
	Events               []Event               `json:"events"`
	ObservedVersions     []ObservedVersion     `json:"observedVersions"`
	Discovery            Discovery             `json:"discovery"`
}

// Cluster is the identity and server version of the discovered cluster.
type Cluster struct {
	Name                   string     `json:"name,omitempty"`
	Context                string     `json:"context,omitempty"`
	KubernetesVersion      string     `json:"kubernetesVersion,omitempty"`
	Platform               string     `json:"platform,omitempty"`
	Distribution           string     `json:"distribution,omitempty"`
	DistributionConfidence string     `json:"distributionConfidence,omitempty"`
	DistributionEvidence   []Evidence `json:"distributionEvidence,omitempty"`
	DiscoveredAt           string     `json:"discoveredAt,omitempty"`
}

// Evidence is a source or observation attached to a fact.
type Evidence struct {
	Kind        string `json:"kind,omitempty"`
	Summary     string `json:"summary,omitempty"`
	URL         string `json:"url,omitempty"`
	Title       string `json:"title,omitempty"`
	RetrievedAt string `json:"retrievedAt,omitempty"`
	RuleID      string `json:"ruleId,omitempty"`
}

// Node is kubelet-reported node information. Kernel and OS image values are
// copied from Node status and are not independently verified.
type Node struct {
	Name                    string            `json:"name"`
	Role                    string            `json:"role,omitempty"`
	KubeletVersion          string            `json:"kubeletVersion,omitempty"`
	OperatingSystem         string            `json:"operatingSystem,omitempty"`
	OSImage                 string            `json:"osImage,omitempty"`
	ReportedKernelVersion   string            `json:"reportedKernelVersion,omitempty"`
	Architecture            string            `json:"architecture,omitempty"`
	ContainerRuntime        string            `json:"containerRuntime,omitempty"`
	ContainerRuntimeVersion string            `json:"containerRuntimeVersion,omitempty"`
	ProviderID              string            `json:"providerId,omitempty"`
	Unschedulable           bool              `json:"unschedulable,omitempty"`
	Ready                   *bool             `json:"ready,omitempty"`
	Labels                  map[string]string `json:"labels,omitempty"`
}

// Component is one detected infrastructure component.
// Version is the component version only when evidence identifies it.
// ChartVersion and AppVersion stay separate because Helm does not define them as equal.
type Component struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	Namespace       string     `json:"namespace,omitempty"`
	Version         string     `json:"version,omitempty"`
	AppVersion      string     `json:"appVersion,omitempty"`
	ChartVersion    string     `json:"chartVersion,omitempty"`
	VersionSource   string     `json:"versionSource,omitempty"`
	KubeVersion     string     `json:"kubeVersion,omitempty"`
	DetectionMethod string     `json:"detectionMethod"`
	Confidence      string     `json:"confidence"`
	Evidence        []Evidence `json:"evidence,omitempty"`
}

// Workload is a summarized controller or job. Env, volumes, and secret data are omitted.
type Workload struct {
	APIVersion    string   `json:"apiVersion,omitempty"`
	Kind          string   `json:"kind"`
	Namespace     string   `json:"namespace,omitempty"`
	Name          string   `json:"name"`
	Replicas      *int     `json:"replicas,omitempty"`
	ReadyReplicas *int     `json:"readyReplicas,omitempty"`
	Images        []string `json:"images,omitempty"`
	HelmRelease   string   `json:"helmRelease,omitempty"`
}

// APIResource is an API the apiserver currently serves.
type APIResource struct {
	Group      string `json:"group,omitempty"`
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	Resource   string `json:"resource"`
	Namespaced bool   `json:"namespaced"`
}

// APIUsage is an observed reference to an API group/version/kind.
type APIUsage struct {
	Group    string   `json:"group,omitempty"`
	Version  string   `json:"version"`
	Kind     string   `json:"kind,omitempty"`
	Source   string   `json:"source"`
	Count    int      `json:"count"`
	Examples []string `json:"examples,omitempty"`
}

// CRD is a custom resource definition without its OpenAPI schema.
type CRD struct {
	Name     string       `json:"name"`
	Group    string       `json:"group,omitempty"`
	Kind     string       `json:"kind,omitempty"`
	Scope    string       `json:"scope,omitempty"`
	Versions []CRDVersion `json:"versions,omitempty"`
}

// CRDVersion is one served or stored CRD version.
type CRDVersion struct {
	Name    string `json:"name"`
	Served  bool   `json:"served"`
	Storage bool   `json:"storage"`
}

// Webhook is one admission webhook handler.
type Webhook struct {
	ConfigurationKind string   `json:"configurationKind"`
	Configuration     string   `json:"configuration"`
	Name              string   `json:"name"`
	FailurePolicy     string   `json:"failurePolicy,omitempty"`
	MatchPolicy       string   `json:"matchPolicy,omitempty"`
	SideEffects       string   `json:"sideEffects,omitempty"`
	APIGroups         []string `json:"apiGroups,omitempty"`
	APIVersions       []string `json:"apiVersions,omitempty"`
	Resources         []string `json:"resources,omitempty"`
	Operations        []string `json:"operations,omitempty"`
	ServiceNamespace  string   `json:"serviceNamespace,omitempty"`
	ServiceName       string   `json:"serviceName,omitempty"`
	TimeoutSeconds    *int     `json:"timeoutSeconds,omitempty"`
}

// Storage holds CSI drivers, classes, and volume summaries.
type Storage struct {
	Classes           []StorageClass `json:"classes"`
	Drivers           []CSIDriver    `json:"drivers"`
	PersistentVolumes []Volume       `json:"persistentVolumes"`
	PersistentClaims  []VolumeClaim  `json:"persistentVolumeClaims"`
}

// StorageClass is a storage class without secret parameters.
type StorageClass struct {
	Name              string `json:"name"`
	Provisioner       string `json:"provisioner,omitempty"`
	ReclaimPolicy     string `json:"reclaimPolicy,omitempty"`
	VolumeBindingMode string `json:"volumeBindingMode,omitempty"`
	AllowExpansion    *bool  `json:"allowVolumeExpansion,omitempty"`
}

// CSIDriver is a CSIDriver name. Volume attributes are not collected.
type CSIDriver struct {
	Name string `json:"name"`
}

// Volume is a persistent volume summary.
type Volume struct {
	Name          string `json:"name"`
	Phase         string `json:"phase,omitempty"`
	StorageClass  string `json:"storageClass,omitempty"`
	Driver        string `json:"driver,omitempty"`
	Capacity      string `json:"capacity,omitempty"`
	ReclaimPolicy string `json:"reclaimPolicy,omitempty"`
}

// VolumeClaim is a persistent volume claim summary.
type VolumeClaim struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace,omitempty"`
	Phase        string `json:"phase,omitempty"`
	StorageClass string `json:"storageClass,omitempty"`
	Capacity     string `json:"capacity,omitempty"`
}

// PodDisruptionBudget records drain-relevant PDB status.
type PodDisruptionBudget struct {
	Name               string `json:"name"`
	Namespace          string `json:"namespace,omitempty"`
	MinAvailable       string `json:"minAvailable,omitempty"`
	MaxUnavailable     string `json:"maxUnavailable,omitempty"`
	CurrentHealthy     *int   `json:"currentHealthy,omitempty"`
	DesiredHealthy     *int   `json:"desiredHealthy,omitempty"`
	DisruptionsAllowed *int   `json:"disruptionsAllowed,omitempty"`
	ExpectedPods       *int   `json:"expectedPods,omitempty"`
}

// NetworkPolicy is a network policy summary without the full rule bodies.
type NetworkPolicy struct {
	Name             string   `json:"name"`
	Namespace        string   `json:"namespace,omitempty"`
	PolicyTypes      []string `json:"policyTypes,omitempty"`
	IngressRuleCount int      `json:"ingressRuleCount"`
	EgressRuleCount  int      `json:"egressRuleCount"`
}

// HelmRelease is Helm release metadata. Values and rendered secret data are omitted.
type HelmRelease struct {
	Name               string            `json:"name"`
	Namespace          string            `json:"namespace,omitempty"`
	Chart              string            `json:"chart,omitempty"`
	ChartVersion       string            `json:"chartVersion,omitempty"`
	AppVersion         string            `json:"appVersion,omitempty"`
	KubeVersion        string            `json:"kubeVersion,omitempty"`
	Status             string            `json:"status,omitempty"`
	Revision           int               `json:"revision,omitempty"`
	ValuesOmitted      bool              `json:"valuesOmitted"`
	Resources          []HelmResourceRef `json:"resources,omitempty"`
	ResourcesTruncated bool              `json:"resourcesTruncated,omitempty"`
	ClassifiedAs       string            `json:"classifiedAs,omitempty"`
}

// HelmResourceRef identifies an object rendered by a release.
type HelmResourceRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
	Hook       bool   `json:"hook,omitempty"`
}

// PodSummary is aggregate pod health. Individual pod specs are not stored.
type PodSummary struct {
	Collected         bool           `json:"collected"`
	Total             int            `json:"total"`
	ByPhase           map[string]int `json:"byPhase,omitempty"`
	NotReady          int            `json:"notReady"`
	Unschedulable     int            `json:"unschedulable"`
	UnhealthyExamples []string       `json:"unhealthyExamples,omitempty"`
}

// Event is a warning event with a truncated message.
type Event struct {
	Type         string `json:"type,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
	Count        int    `json:"count,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	InvolvedKind string `json:"involvedKind,omitempty"`
	InvolvedName string `json:"involvedName,omitempty"`
	LastSeen     string `json:"lastSeen,omitempty"`
}

// ObservedVersion is a version seen on a pod or pod template.
type ObservedVersion struct {
	Component string `json:"component"`
	Version   string `json:"version,omitempty"`
	Image     string `json:"image,omitempty"`
	Source    string `json:"source"`
	Example   string `json:"example,omitempty"`
	Count     int    `json:"count"`
}

// Discovery records which collectors ran.
type Discovery struct {
	Collectors []CollectorResult `json:"collectors"`
	Warnings   []string          `json:"warnings,omitempty"`
}

// CollectorResult is the outcome of one collector.
type CollectorResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

// Normalize fills nil slices so the JSON contract stays stable.
func (inv *Inventory) Normalize() {
	if inv.SchemaVersion == "" {
		inv.SchemaVersion = SchemaVersion
	}
	inv.Nodes = empty(inv.Nodes)
	inv.Components = empty(inv.Components)
	inv.Workloads = empty(inv.Workloads)
	inv.APIResources = empty(inv.APIResources)
	inv.APIUsages = empty(inv.APIUsages)
	inv.CRDs = empty(inv.CRDs)
	inv.Webhooks = empty(inv.Webhooks)
	inv.Storage.Classes = empty(inv.Storage.Classes)
	inv.Storage.Drivers = empty(inv.Storage.Drivers)
	inv.Storage.PersistentVolumes = empty(inv.Storage.PersistentVolumes)
	inv.Storage.PersistentClaims = empty(inv.Storage.PersistentClaims)
	inv.PodDisruptionBudgets = empty(inv.PodDisruptionBudgets)
	inv.NetworkPolicies = empty(inv.NetworkPolicies)
	inv.HelmReleases = empty(inv.HelmReleases)
	inv.Events = empty(inv.Events)
	inv.ObservedVersions = empty(inv.ObservedVersions)
	inv.Discovery.Collectors = empty(inv.Discovery.Collectors)
	if inv.Pods.ByPhase == nil && inv.Pods.Collected {
		inv.Pods.ByPhase = map[string]int{}
	}
}

func empty[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}

// AddAPIUsage aggregates usage by group, version, kind, and source.
func (inv *Inventory) AddAPIUsage(group, version, kind, source, example string) {
	if version == "" {
		return
	}
	for i := range inv.APIUsages {
		u := &inv.APIUsages[i]
		if u.Group == group && u.Version == version && u.Kind == kind && u.Source == source {
			u.Count++
			if example != "" && len(u.Examples) < 5 && !contains(u.Examples, example) {
				u.Examples = append(u.Examples, example)
			}
			return
		}
	}
	u := APIUsage{Group: group, Version: version, Kind: kind, Source: source, Count: 1}
	if example != "" {
		u.Examples = []string{example}
	}
	inv.APIUsages = append(inv.APIUsages, u)
}

func contains(ss []string, s string) bool {
	for _, item := range ss {
		if item == s {
			return true
		}
	}
	return false
}

// AddObservedVersion aggregates a seen component version.
func (inv *Inventory) AddObservedVersion(component, ver, image, source, example string) {
	for i := range inv.ObservedVersions {
		o := &inv.ObservedVersions[i]
		if o.Component == component && o.Version == ver && o.Source == source && o.Image == image {
			o.Count++
			return
		}
	}
	inv.ObservedVersions = append(inv.ObservedVersions, ObservedVersion{
		Component: component,
		Version:   ver,
		Image:     image,
		Source:    source,
		Example:   example,
		Count:     1,
	})
}

// ParseAPIVersion splits "apps/v1" or "v1" into group and version.
func ParseAPIVersion(apiVersion string) (group, version string) {
	apiVersion = strings.TrimSpace(apiVersion)
	if apiVersion == "" {
		return "", ""
	}
	if i := strings.LastIndex(apiVersion, "/"); i >= 0 {
		return apiVersion[:i], apiVersion[i+1:]
	}
	return "", apiVersion
}

// ReadFile loads an inventory JSON document.
func ReadFile(path string) (*Inventory, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inv Inventory
	if err := json.Unmarshal(b, &inv); err != nil {
		return nil, fmt.Errorf("parse inventory: %w", err)
	}
	if inv.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported inventory schema %q (this build reads %s)", inv.SchemaVersion, SchemaVersion)
	}
	inv.Normalize()
	return &inv, nil
}

// WriteJSON writes indented JSON to path, or stdout when path is empty or "-".
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(b)
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
