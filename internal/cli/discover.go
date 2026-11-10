package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/discover"
)

func newDiscover(stdout, stderr io.Writer) *cobra.Command {
	var kubeconfig, contextName, output string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Collect a read-only inventory from the current kubeconfig context",
		Long: `Collect a read-only inventory from a kubeconfig context.

Lists Kubernetes objects and Helm release metadata. Does not store Secret data,
ConfigMap data, environment values, webhook CA bundles, or Helm values.

With --output, the inventory is written to that file. Stdout stays empty, and
stderr is used only for warnings.`,
		Example: `  clusterpilot discover -o inventory.json
  clusterpilot discover --context kind-dev --kubeconfig ~/.kube/config -o inventory.json
  clusterpilot discover`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			inv, err := discover.Discover(cmd.Context(), discover.Options{
				Context:    contextName,
				Kubeconfig: kubeconfig,
				Timeout:    timeout,
			})
			if err != nil {
				return err
			}
			for _, warning := range inv.Discovery.Warnings {
				fmt.Fprintf(stderr, "warning: %s\n", warning)
			}
			return writeJSON(stdout, output, inv)
		},
	}
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "kubeconfig file (default is the kubectl default)")
	cmd.Flags().StringVar(&contextName, "context", "", "kubeconfig context (default is the current context)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "inventory JSON file, or stdout when omitted")
	cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "discovery timeout")
	return cmd
}
