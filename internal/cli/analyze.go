package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/analyze"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/inventory"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/knowledge"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/report"
)

func newAnalyze(stdout io.Writer) *cobra.Command {
	var inventoryPath, target, output, format string
	var rules []string
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Evaluate an inventory against a target Kubernetes version",
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
			doc, err := analyze.Analyze(inv, target, base)
			if err != nil {
				return err
			}
			if output != "" {
				if err := writeJSON(stdout, output, doc); err != nil {
					return err
				}
			}
			text := report.Text(doc)
			if format == "markdown" {
				text = report.Markdown(doc)
			}
			_, err = io.WriteString(stdout, text)
			return err
		},
	}
	cmd.Flags().StringVar(&inventoryPath, "inventory", "", "inventory JSON file")
	cmd.Flags().StringVar(&target, "target-kubernetes", "", "target Kubernetes version, such as 1.33 or 1.33.2")
	cmd.Flags().StringVar(&output, "output", "", "also write the analysis JSON to this file")
	cmd.Flags().StringVar(&format, "format", "text", "text or markdown")
	cmd.Flags().StringSliceVar(&rules, "rules", nil, "extra rule file or directory")
	_ = cmd.MarkFlagRequired("inventory")
	_ = cmd.MarkFlagRequired("target-kubernetes")
	return cmd
}

func loadRules(paths []string) (*knowledge.Base, error) {
	base, err := knowledge.LoadBuiltin()
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return base, nil
	}
	extra, err := knowledge.LoadPaths(paths)
	if err != nil {
		return nil, err
	}
	if err := base.Merge(extra); err != nil {
		return nil, err
	}
	return base, nil
}
