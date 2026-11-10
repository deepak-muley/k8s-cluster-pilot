// Package graph builds a directed component graph.
//
// Upgrade-before edges exist only when a compatibility rule requires that
// order. Structural edges record observations and do not imply an upgrade
// sequence. CNI, CSI, Helm, and application nodes are not ordered ahead of
// Kubernetes unless a rule says so.
package graph

import (
	"sort"
	"strings"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/semver"
)

// Edge kinds.
const (
	EdgeUpgradeBefore = "upgrade-before"
	EdgeRunsOn        = "runs-on"
	EdgePartOf        = "part-of"
	EdgeManages       = "manages"
	EdgeReports       = "reports"
)

// Graph is the serializable dependency graph.
type Graph struct {
	Nodes              []Node     `json:"nodes"`
	Edges              []Edge     `json:"edges"`
	UpgradeOrderCycles [][]string `json:"upgradeOrderCycles,omitempty"`
}

// Node is one component or cluster object.
type Node struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Version    string `json:"version,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Observed   bool   `json:"observed"`
}

// Edge connects two nodes.
// For upgrade-before, From must be upgraded before To when both change.
type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	RuleID     string `json:"ruleId,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
	Constraint string `json:"constraint,omitempty"`
}

// Build constructs the graph for an inventory and a target Kubernetes version.
func Build(inv *inventory.Inventory, target semver.Version, rules *knowledge.Base) Graph {
	g := Graph{}
	if inv == nil {
		return g
	}
	g.add(Node{ID: "cluster", Name: inv.Cluster.Name, Type: "cluster", Version: inv.Cluster.KubernetesVersion, Observed: true, Confidence: "high"})

	observed := map[string]bool{}
	versions := map[string]string{}
	for _, component := range inv.Components {
		observed[component.Type] = true
		versions[component.Type] = component.Version
		g.add(Node{
			ID:         componentID(component.ID),
			Name:       component.Name,
			Type:       component.Type,
			Version:    component.Version,
			Confidence: component.Confidence,
			Observed:   true,
		})
	}

	ensure := func(componentType, name string, always bool) {
		id := componentID("kubernetes/" + componentType)
		if _, ok := g.nodeIndex(id); ok {
			return
		}
		if !always && !observed[componentType] {
			return
		}
		g.add(Node{
			ID:         id,
			Name:       name,
			Type:       componentType,
			Version:    versions[componentType],
			Confidence: confidenceFor(observed[componentType]),
			Observed:   observed[componentType],
		})
	}
	ensure(inventory.TypeControlPlane, "control-plane", true)
	ensure(inventory.TypeAPIServer, "kube-apiserver", true)
	ensure(inventory.TypeControllerManager, "kube-controller-manager", true)
	ensure(inventory.TypeScheduler, "kube-scheduler", true)
	ensure(inventory.TypeKubelet, "kubelet", true)
	if observed[inventory.TypeCloudControllerManager] {
		ensure(inventory.TypeCloudControllerManager, "cloud-controller-manager", false)
	}
	if observed[inventory.TypeKubeProxy] {
		ensure(inventory.TypeKubeProxy, "kube-proxy", false)
	}

	g.edge(Edge{From: componentID("kubernetes/" + inventory.TypeAPIServer), To: componentID("kubernetes/" + inventory.TypeControlPlane), Kind: EdgePartOf, Confidence: "high", Evidence: "kube-apiserver is a control-plane component"})
	g.edge(Edge{From: componentID("kubernetes/" + inventory.TypeControllerManager), To: componentID("kubernetes/" + inventory.TypeControlPlane), Kind: EdgePartOf, Confidence: "high", Evidence: "kube-controller-manager is a control-plane component"})
	g.edge(Edge{From: componentID("kubernetes/" + inventory.TypeScheduler), To: componentID("kubernetes/" + inventory.TypeControlPlane), Kind: EdgePartOf, Confidence: "high", Evidence: "kube-scheduler is a control-plane component"})

	for _, node := range inv.Nodes {
		nodeID := "node/" + node.Name
		g.add(Node{ID: nodeID, Name: node.Name, Type: "node", Observed: true, Confidence: "high"})
		kubeletID := "kubelet/" + node.Name
		g.add(Node{ID: kubeletID, Name: node.Name, Type: inventory.TypeKubelet, Version: node.KubeletVersion, Observed: node.KubeletVersion != "", Confidence: confidenceFor(node.KubeletVersion != "")})
		g.edge(Edge{From: kubeletID, To: nodeID, Kind: EdgeRunsOn, Confidence: "high", Evidence: "node status reports this kubelet"})
		g.edge(Edge{From: kubeletID, To: componentID("kubernetes/" + inventory.TypeKubelet), Kind: EdgePartOf, Confidence: "high", Evidence: "node kubelet is part of the cluster kubelet set"})
		if node.ContainerRuntime != "" {
			runtimeID := "runtime/" + sanitize(node.ContainerRuntime)
			g.add(Node{ID: runtimeID, Name: node.ContainerRuntime, Type: inventory.TypeRuntime, Version: node.ContainerRuntimeVersion, Observed: true, Confidence: "medium"})
			g.edge(Edge{From: nodeID, To: runtimeID, Kind: EdgeReports, Confidence: "medium", Evidence: "node.status.nodeInfo.containerRuntimeVersion is kubelet-reported"})
		}
	}

	for _, component := range inv.Components {
		switch component.Type {
		case inventory.TypeCNI, inventory.TypeCSI, inventory.TypeStorageProvisioner, inventory.TypeCoreDNS, inventory.TypeHelm, inventory.TypeOperator, inventory.TypeEtcd, inventory.TypeRuntime:
			g.edge(Edge{
				From:       componentID(component.ID),
				To:         "cluster",
				Kind:       EdgePartOf,
				Confidence: component.Confidence,
				Evidence:   "component was detected in this cluster; no upgrade order is implied",
			})
		}
	}
	for _, release := range inv.HelmReleases {
		if release.ClassifiedAs == "" {
			continue
		}
		from := componentID("helm/" + release.Namespace + "/" + release.Name)
		to := componentID(release.ClassifiedAs)
		if _, ok := g.nodeIndex(from); !ok {
			continue
		}
		if _, ok := g.nodeIndex(to); !ok {
			continue
		}
		g.edge(Edge{
			From:       from,
			To:         to,
			Kind:       EdgeManages,
			Confidence: "high",
			Evidence:   "Helm release metadata matched this component",
		})
	}

	if rules != nil {
		for _, rule := range rules.ByType(knowledge.RuleVersionSkew) {
			params, err := rule.Skew()
			if err != nil || !params.NotNewerMinor || params.Subject == "" || params.Subject == params.RelativeTo {
				continue
			}
			from := componentID("kubernetes/" + params.RelativeTo)
			to := componentID("kubernetes/" + params.Subject)
			if _, ok := g.nodeIndex(to); !ok {
				continue
			}
			g.edge(Edge{
				From:       from,
				To:         to,
				Kind:       EdgeUpgradeBefore,
				RuleID:     rule.ID,
				Confidence: rule.Confidence,
				Constraint: "must-not-be-newer",
				Evidence:   rule.Source.URL,
			})
		}
		for _, rule := range rules.ByType(knowledge.RuleRemovedAPI) {
			params, err := rule.RemovedAPI()
			if err != nil {
				continue
			}
			applies, err := params.AppliesAtTarget(target)
			if err != nil || !applies {
				continue
			}
			if !usageMatches(inv, params) {
				continue
			}
			id := "migration/" + rule.ID
			g.add(Node{ID: id, Name: params.Replacement, Type: "api-migration", Observed: true, Confidence: "high"})
			g.edge(Edge{
				From:       id,
				To:         componentID("kubernetes/" + inventory.TypeAPIServer),
				Kind:       EdgeUpgradeBefore,
				RuleID:     rule.ID,
				Confidence: rule.Confidence,
				Constraint: "migrate-before-removal",
				Evidence:   rule.Source.URL,
			})
		}
	}

	g.UpgradeOrderCycles = cycles(g)
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].Kind != g.Edges[j].Kind {
			return g.Edges[i].Kind < g.Edges[j].Kind
		}
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		return g.Edges[i].To < g.Edges[j].To
	})
	return g
}

func usageMatches(inv *inventory.Inventory, params knowledge.RemovedAPIParams) bool {
	for _, usage := range inv.APIUsages {
		if params.MatchesUsage(usage.Group, usage.Version, usage.Kind) {
			return true
		}
	}
	return false
}

func (g *Graph) add(node Node) {
	if _, ok := g.nodeIndex(node.ID); ok {
		return
	}
	g.Nodes = append(g.Nodes, node)
}

func (g *Graph) nodeIndex(id string) (Node, bool) {
	for _, node := range g.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return Node{}, false
}

func (g *Graph) edge(edge Edge) {
	for _, existing := range g.Edges {
		if existing == edge {
			return
		}
	}
	g.Edges = append(g.Edges, edge)
}

func componentID(id string) string {
	if strings.HasPrefix(id, "component/") || id == "cluster" {
		return id
	}
	return "component/" + id
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, s)
}

func confidenceFor(ok bool) string {
	if ok {
		return "high"
	}
	return "unknown"
}

func cycles(g Graph) [][]string {
	next := map[string][]string{}
	nodes := map[string]struct{}{}
	for _, edge := range g.Edges {
		if edge.Kind != EdgeUpgradeBefore {
			continue
		}
		next[edge.From] = append(next[edge.From], edge.To)
		nodes[edge.From] = struct{}{}
		nodes[edge.To] = struct{}{}
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var found [][]string
	var visit func(string)
	visit = func(id string) {
		color[id] = gray
		stack = append(stack, id)
		for _, to := range next[id] {
			switch color[to] {
			case white:
				visit(to)
			case gray:
				cycle := []string{to}
				for i := len(stack) - 1; i >= 0; i-- {
					cycle = append(cycle, stack[i])
					if stack[i] == to {
						break
					}
				}
				found = append(found, cycle)
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
	}
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if color[id] == white {
			visit(id)
		}
	}
	return found
}
