// Package report renders an analysis or plan for a person.
package report

import (
	"fmt"
	"strings"

	"github.com/deepak-muley/k8s-cluster-explorer/internal/graph"
	"github.com/deepak-muley/k8s-cluster-explorer/internal/model"
)

// Markdown renders a plan or analysis as Markdown.
func Markdown(doc *model.Document) string {
	return render(doc, true)
}

// Text renders a plan or analysis as plain text.
func Text(doc *model.Document) string {
	return render(doc, false)
}

func render(doc *model.Document, markdown bool) string {
	var b strings.Builder
	title := "ClusterPilot analysis"
	if doc.Kind == model.KindPlan {
		title = "ClusterPilot upgrade plan"
	}
	if markdown {
		fmt.Fprintf(&b, "# %s\n\n", title)
	} else {
		fmt.Fprintf(&b, "%s\n\n", title)
	}
	fmt.Fprintf(&b, "Status: %s\n", doc.Status)
	if doc.ClusterName != "" {
		fmt.Fprintf(&b, "Cluster: %s\n", doc.ClusterName)
	}
	fmt.Fprintf(&b, "Current Kubernetes: %s\n", empty(doc.CurrentKubernetes))
	fmt.Fprintf(&b, "Target Kubernetes: %s\n", doc.TargetKubernetes)
	fmt.Fprintf(&b, "Read-only: %t\n", doc.ReadOnly)
	fmt.Fprintf(&b, "Findings: %d compatible, %d incompatible, %d potential risk, %d unknown, %d advisories\n",
		doc.Summary.Compatible, doc.Summary.Incompatible, doc.Summary.PotentialRisk, doc.Summary.Unknown, doc.Summary.Advisories)
	if doc.Kind == model.KindPlan {
		fmt.Fprintf(&b, "Steps: %d required, %d recommended, %d verify\n", doc.Summary.RequiredSteps, doc.Summary.RecommendedSteps, doc.Summary.VerifySteps)
	}
	b.WriteString("\n")
	writeFindings(&b, doc.Findings, markdown)
	if len(doc.Steps) > 0 {
		writeSteps(&b, doc.Steps, markdown)
	}
	writeGraph(&b, doc.Graph, markdown)
	b.WriteString("Caveats\n")
	if markdown {
		b.WriteString("\n")
	}
	for _, line := range []string{
		"This tool does not change the cluster.",
		"A missing rule is unknown, not compatible.",
		"Removed-API coverage is the Kubernetes Deprecated API Migration Guide snapshot retrieved on 2026-11-09. Later removals are not assumed to be absent.",
		"Node kernel and OS image values are copied from kubelet status and were not verified by a node agent.",
		"Helm values and Secret data are omitted.",
		"Local kubectl skew is not evaluated.",
		"CNI, CSI, runtime, and Helm compatibility is unknown unless a loaded rule or a chart kubeVersion constraint says otherwise. A chart constraint is a publisher claim.",
	} {
		if markdown {
			fmt.Fprintf(&b, "- %s\n", line)
		} else {
			fmt.Fprintf(&b, "  - %s\n", line)
		}
	}
	return b.String()
}

func writeFindings(b *strings.Builder, findings []model.Finding, markdown bool) {
	if markdown {
		b.WriteString("## Findings\n\n")
	} else {
		b.WriteString("Findings\n")
	}
	if len(findings) == 0 {
		b.WriteString("None.\n\n")
		return
	}
	for _, finding := range findings {
		line := fmt.Sprintf("[%s] %s", finding.Status, finding.Title)
		if markdown {
			fmt.Fprintf(b, "### %s\n\n", line)
		} else {
			fmt.Fprintf(b, "- %s\n", line)
		}
		if finding.Severity != "" {
			fmt.Fprintf(b, "Severity: %s\n", finding.Severity)
		}
		if finding.Component != "" {
			fmt.Fprintf(b, "Component: %s\n", finding.Component)
		}
		if finding.RuleID != "" {
			fmt.Fprintf(b, "Rule: %s\n", finding.RuleID)
		}
		fmt.Fprintf(b, "Confidence: %s\n", empty(finding.Confidence))
		fmt.Fprintf(b, "Verification: %s\n", empty(finding.Verification))
		if finding.BlocksPlan {
			b.WriteString("Blocks the upgrade until resolved.\n")
		}
		if finding.ResolvedByPlan {
			b.WriteString("The direct path is unsupported. The plan sequences around it.\n")
		}
		fmt.Fprintf(b, "%s\n", strings.TrimSpace(finding.Detail))
		if finding.Remediation != "" {
			fmt.Fprintf(b, "Remediation: %s\n", strings.TrimSpace(finding.Remediation))
		}
		for _, item := range finding.Evidence {
			if item.URL != "" {
				fmt.Fprintf(b, "Evidence: %s", item.URL)
				if item.RetrievedAt != "" {
					fmt.Fprintf(b, " (retrieved %s)", item.RetrievedAt)
				}
				b.WriteString("\n")
			} else if item.Summary != "" {
				fmt.Fprintf(b, "Evidence: %s\n", item.Summary)
			}
		}
		b.WriteString("\n")
	}
}

func writeSteps(b *strings.Builder, steps []model.Step, markdown bool) {
	if markdown {
		b.WriteString("## Steps\n\n")
	} else {
		b.WriteString("Steps\n")
	}
	for _, step := range steps {
		fmt.Fprintf(b, "%d. [%s] %s", step.Order, step.Requirement, step.Component)
		if step.From != "" || step.To != "" {
			fmt.Fprintf(b, " (%s -> %s)", empty(step.From), empty(step.To))
		}
		b.WriteString("\n")
		fmt.Fprintf(b, "   Action: %s\n", step.Action)
		fmt.Fprintf(b, "   %s\n", strings.TrimSpace(step.Rationale))
		if len(step.RuleIDs) > 0 {
			fmt.Fprintf(b, "   Rules: %s\n", strings.Join(step.RuleIDs, ", "))
		}
		for _, validation := range step.Validations {
			fmt.Fprintf(b, "   Validate: %s\n", validation)
		}
		for _, item := range step.Evidence {
			if item.URL != "" {
				fmt.Fprintf(b, "   Evidence: %s\n", item.URL)
			}
		}
	}
	b.WriteString("\n")
}

func writeGraph(b *strings.Builder, g graph.Graph, markdown bool) {
	if markdown {
		b.WriteString("## Upgrade-order edges\n\n")
	} else {
		b.WriteString("Upgrade-order edges\n")
	}
	count := 0
	for _, edge := range g.Edges {
		if edge.Kind != graph.EdgeUpgradeBefore {
			continue
		}
		count++
		fmt.Fprintf(b, "- %s before %s", edge.From, edge.To)
		if edge.RuleID != "" {
			fmt.Fprintf(b, " (%s)", edge.RuleID)
		}
		b.WriteString("\n")
	}
	if count == 0 {
		b.WriteString("None. No verified upgrade order was derived.\n")
	}
	if len(g.UpgradeOrderCycles) > 0 {
		fmt.Fprintf(b, "Cycles: %v\n", g.UpgradeOrderCycles)
	}
	b.WriteString("\n")
}

func empty(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
