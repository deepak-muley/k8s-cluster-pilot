// Package discover collects a read-only inventory from a kubeconfig context.
package discover

import (
	"context"
	"time"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/discover/helm"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/discover/identify"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/discover/kubernetes"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
)

// Options selects the cluster and how long discovery may run.
type Options struct {
	Context    string
	Kubeconfig string
	Timeout    time.Duration
}

// Discover connects to the cluster, collects facts, and identifies components.
// It lists and gets objects only. Helm release values are not stored.
func Discover(ctx context.Context, opts Options) (*inventory.Inventory, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cfg, contextName, clusterName, err := RESTConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return nil, err
	}
	inv := &inventory.Inventory{SchemaVersion: inventory.SchemaVersion}
	inv.Cluster.Name = clusterName
	inv.Cluster.Context = contextName
	inv.Cluster.DiscoveredAt = time.Now().UTC().Format(time.RFC3339)

	start := time.Now()
	err = kubernetes.Collect(ctx, cfg, inv)
	inv.Discovery.Collectors = append(inv.Discovery.Collectors, inventory.CollectorResult{
		Name:       "kubernetes",
		Status:     statusOf(err),
		Message:    messageOf(err),
		DurationMs: time.Since(start).Milliseconds(),
	})
	if err != nil {
		return nil, err
	}

	start = time.Now()
	err = helm.Collect(opts.Kubeconfig, opts.Context, inv)
	inv.Discovery.Collectors = append(inv.Discovery.Collectors, inventory.CollectorResult{
		Name:       "helm",
		Status:     statusOf(err),
		Message:    messageOf(err),
		DurationMs: time.Since(start).Milliseconds(),
	})
	if err != nil {
		inv.Discovery.Warnings = append(inv.Discovery.Warnings, "helm: "+err.Error())
	}

	identify.Annotate(inv)
	inv.Discovery.Collectors = append(inv.Discovery.Collectors, inventory.CollectorResult{
		Name:    "identify",
		Status:  "ok",
		Message: "CNI, CSI, and platform components were derived from collected facts",
	})
	inv.Normalize()
	return inv, nil
}

func statusOf(err error) string {
	if err != nil {
		return "failed"
	}
	return "ok"
}

func messageOf(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
