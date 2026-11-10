// Package cli is the clusterpilot command line.
package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/version"
)

// Execute runs clusterpilot with args. Writers receive normal output and errors.
func Execute(args []string, stdout, stderr io.Writer) error {
	root := newRoot(stdout, stderr)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	return root.Execute()
}

func newRoot(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "clusterpilot",
		Short: "Read-only Kubernetes upgrade intelligence",
		Long: `ClusterPilot discovers a Kubernetes cluster, writes a versioned inventory,
and evaluates that inventory offline against a target Kubernetes version.

discover is the only command that connects to a cluster. analyze, plan, and
report read files and do not change the cluster.`,
		Example: `  clusterpilot discover -o inventory.json
  clusterpilot analyze --inventory inventory.json --target-kubernetes 1.33
  clusterpilot plan --inventory inventory.json --target-kubernetes 1.33 -o plan.json
  clusterpilot report --input plan.json --format markdown`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Version,
	}
	root.SetVersionTemplate("clusterpilot {{.Version}}\n")
	root.AddCommand(newDiscover(stdout, stderr), newAnalyze(stdout), newPlan(stdout, stderr), newReport(stdout))
	return root
}
