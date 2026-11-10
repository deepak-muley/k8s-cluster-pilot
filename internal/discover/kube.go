package discover

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/version"
)

// RESTConfig loads a kubeconfig context and returns the context and cluster names.
func RESTConfig(kubeconfig, contextName string) (*rest.Config, string, string, error) {
	loading := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loading.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, overrides)
	raw, err := clientConfig.RawConfig()
	if err != nil {
		return nil, "", "", err
	}
	resolved := raw.CurrentContext
	if contextName != "" {
		resolved = contextName
		if _, ok := raw.Contexts[resolved]; !ok {
			return nil, "", "", fmt.Errorf("kubeconfig context %q was not found", resolved)
		}
	}
	cfg, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, "", "", err
	}
	clusterName := ""
	if ctx, ok := raw.Contexts[resolved]; ok && ctx != nil {
		clusterName = ctx.Cluster
	}
	cfg.QPS = 50
	cfg.Burst = 100
	cfg.UserAgent = "clusterpilot/" + version.Version
	return cfg, resolved, clusterName, nil
}
