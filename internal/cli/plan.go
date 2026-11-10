package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/plan"
)

func newPlan(stdout, stderr io.Writer) *cobra.Command {
	var inventoryPath, target, output string
	var rules []string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Build a dependency-aware upgrade plan from an inventory",
		Long: `Build a dependency-aware upgrade plan from an inventory file.

Does not connect to a cluster. Prints the overall status to stderr and writes
plan JSON to --output, or to stdout when --output is omitted. --rules merges
extra YAML rule files with the builtin knowledge base.`,
		Example: `  clusterpilot plan --inventory inventory.json --target-kubernetes 1.33 -o plan.json
  clusterpilot plan --inventory inventory.json --target-kubernetes 1.33 --rules ./extra-rules`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			inv, err := inventory.ReadFile(inventoryPath)
			if err != nil {
				return err
			}
			base, err := loadRules(rules)
			if err != nil {
				return err
			}
			doc, err := plan.Build(inv, target, base)
			if err != nil {
				return err
			}
			fmt.Fprintf(stderr, "status: %s\n", doc.Status)
			return writeJSON(stdout, output, doc)
		},
	}
	cmd.Flags().StringVar(&inventoryPath, "inventory", "", "inventory JSON file")
	cmd.Flags().StringVar(&target, "target-kubernetes", "", "target Kubernetes version")
	cmd.Flags().StringVarP(&output, "output", "o", "", "plan JSON file, or stdout when omitted")
	cmd.Flags().StringSliceVar(&rules, "rules", nil, "extra rule file or directory")
	_ = cmd.MarkFlagRequired("inventory")
	_ = cmd.MarkFlagRequired("target-kubernetes")
	return cmd
}
