package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/model"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/report"
)

func newReport(stdout io.Writer) *cobra.Command {
	var input, format, output string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Render an analysis or plan",
		Long: `Render an analysis or plan JSON file as Markdown or text.

Does not connect to a cluster. Writes to stdout unless --output is set.`,
		Example: `  clusterpilot report --input plan.json --format markdown
  clusterpilot report --input analysis.json --format text -o report.txt`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			var doc model.Document
			if err := readJSON(input, &doc); err != nil {
				return err
			}
			if doc.SchemaVersion != model.SchemaVersion {
				return fmt.Errorf("unsupported document schema %q", doc.SchemaVersion)
			}
			var text string
			switch format {
			case "markdown", "md":
				text = report.Markdown(&doc)
			case "text":
				text = report.Text(&doc)
			default:
				return fmt.Errorf("unsupported format %q", format)
			}
			if output == "" || output == "-" {
				_, err := io.WriteString(stdout, text)
				return err
			}
			return os.WriteFile(output, []byte(text), 0o644)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "analysis or plan JSON file")
	cmd.Flags().StringVar(&format, "format", "markdown", "markdown or text")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write the report to this file instead of stdout")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}
