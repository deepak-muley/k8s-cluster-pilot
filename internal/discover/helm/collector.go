// Package helm lists Helm releases with the Helm SDK.
// Release values and secret payloads are not copied into the inventory.
package helm

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/releaseutil"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/inventory"
)

const resourceCap = 400

// Collect lists releases across namespaces. A failure is returned to the
// caller so discovery can keep the Kubernetes inventory and record a warning.
func Collect(kubeconfig, contextName string, inv *inventory.Inventory) error {
	settings := cli.New()
	if kubeconfig != "" {
		settings.KubeConfig = kubeconfig
	}
	if contextName != "" {
		settings.KubeContext = contextName
	}
	cfg := new(action.Configuration)
	if err := cfg.Init(settings.RESTClientGetter(), "", os.Getenv("HELM_DRIVER"), func(string, ...interface{}) {}); err != nil {
		return err
	}
	list := action.NewList(cfg)
	list.AllNamespaces = true
	list.StateMask = action.ListDeployed | action.ListFailed | action.ListPendingInstall | action.ListPendingUpgrade | action.ListPendingRollback | action.ListUninstalling
	releases, err := list.Run()
	if err != nil {
		return err
	}
	for _, release := range releases {
		if release == nil {
			continue
		}
		item := inventory.HelmRelease{
			Name:          release.Name,
			Namespace:     release.Namespace,
			Revision:      release.Version,
			ValuesOmitted: true,
		}
		if release.Info != nil {
			item.Status = release.Info.Status.String()
		}
		if release.Chart != nil && release.Chart.Metadata != nil {
			item.Chart = release.Chart.Metadata.Name
			item.ChartVersion = release.Chart.Metadata.Version
			item.AppVersion = release.Chart.Metadata.AppVersion
			item.KubeVersion = release.Chart.Metadata.KubeVersion
		}
		refs := manifestRefs(release.Manifest, false)
		if release.Hooks != nil {
			for _, hook := range release.Hooks {
				if hook == nil {
					continue
				}
				refs = append(refs, manifestRefs(hook.Manifest, true)...)
			}
		}
		if len(refs) > resourceCap {
			item.ResourcesTruncated = true
			refs = refs[:resourceCap]
		}
		item.Resources = refs
		for _, ref := range item.Resources {
			group, version := inventory.ParseAPIVersion(ref.APIVersion)
			source := "helm-manifest"
			if ref.Hook {
				source = "helm-hook"
			}
			example := item.Namespace + "/" + item.Name
			if ref.Name != "" {
				example = example + " " + ref.Kind + "/" + ref.Name
			}
			inv.AddAPIUsage(group, version, ref.Kind, source, example)
		}
		inv.HelmReleases = append(inv.HelmReleases, item)
	}
	sort.Slice(inv.HelmReleases, func(i, j int) bool {
		if inv.HelmReleases[i].Namespace != inv.HelmReleases[j].Namespace {
			return inv.HelmReleases[i].Namespace < inv.HelmReleases[j].Namespace
		}
		return inv.HelmReleases[i].Name < inv.HelmReleases[j].Name
	})
	return nil
}

func manifestRefs(manifest string, hook bool) []inventory.HelmResourceRef {
	if strings.TrimSpace(manifest) == "" {
		return nil
	}
	var refs []inventory.HelmResourceRef
	for _, doc := range releaseutil.SplitManifests(manifest) {
		var head struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
			Metadata   struct {
				Name      string `yaml:"name"`
				Namespace string `yaml:"namespace"`
			} `yaml:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(doc), &head); err != nil || head.APIVersion == "" || head.Kind == "" {
			continue
		}
		refs = append(refs, inventory.HelmResourceRef{
			APIVersion: head.APIVersion,
			Kind:       head.Kind,
			Namespace:  head.Metadata.Namespace,
			Name:       head.Metadata.Name,
			Hook:       hook,
		})
	}
	return refs
}

// FormatReleaseError makes an empty helm failure explicit.
func FormatReleaseError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
