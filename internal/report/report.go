// Package report renders an analysis or plan for a person.
package report

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/graph"
	"github.com/deepak-muley/k8s-cluster-pilot/internal/model"
)

const lineWidth = 108

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
	heading(&b, title, markdown, 1)

	pairs := [][2]string{}
	if doc.ClusterName != "" {
		pairs = append(pairs, [2]string{"Cluster", doc.ClusterName})
	}
	current := empty(doc.CurrentKubernetes)
	target := empty(doc.TargetKubernetes)
	pairs = append(pairs, [2]string{"Kubernetes", current + " -> " + target})
	pairs = append(pairs, [2]string{"Status", empty(doc.Status)})
	readOnly := "no"
	if doc.ReadOnly {
		readOnly = "yes"
	}
	pairs = append(pairs, [2]string{"Read-only", readOnly})
	writePairs(&b, pairs, markdown)

	writeSummary(&b, doc, markdown)
	writeFindings(&b, doc.Findings, markdown)
	if len(doc.Steps) > 0 {
		writeSteps(&b, doc.Steps, markdown)
	}
	writeGraph(&b, doc.Graph, markdown)
	writeCaveats(&b, markdown)
	return b.String()
}

func writeSummary(b *strings.Builder, doc *model.Document, markdown bool) {
	heading(b, "Summary", markdown, 2)
	rows := [][]string{
		{"compatible", strconv.Itoa(doc.Summary.Compatible)},
		{"incompatible", strconv.Itoa(doc.Summary.Incompatible)},
		{"potential-risk", strconv.Itoa(doc.Summary.PotentialRisk)},
		{"unknown", strconv.Itoa(doc.Summary.Unknown)},
		{"advisories", strconv.Itoa(doc.Summary.Advisories)},
	}
	if doc.Kind == model.KindPlan {
		rows = append(rows,
			[]string{"required steps", strconv.Itoa(doc.Summary.RequiredSteps)},
			[]string{"recommended steps", strconv.Itoa(doc.Summary.RecommendedSteps)},
			[]string{"verify steps", strconv.Itoa(doc.Summary.VerifySteps)},
		)
	}
	writeTable(b, []string{"Status", "Count"}, rows, tableOpts{markdown: markdown, right: map[int]bool{1: true}})
	b.WriteString("\n")
}

func writeFindings(b *strings.Builder, findings []model.Finding, markdown bool) {
	heading(b, "Findings", markdown, 2)
	if len(findings) == 0 {
		b.WriteString("None.\n\n")
		return
	}
	groups := map[string][]model.Finding{}
	for _, finding := range findings {
		key := findingGroup(finding)
		groups[key] = append(groups[key], finding)
	}
	for _, key := range []string{
		model.StatusIncompatible,
		model.StatusPotentialRisk,
		model.StatusUnknown,
		model.StatusCompatible,
		"advisory",
	} {
		items := groups[key]
		if len(items) == 0 {
			continue
		}
		writeFindingGroup(b, key, items, markdown)
	}
}

func writeFindingGroup(b *strings.Builder, key string, items []model.Finding, markdown bool) {
	heading(b, fmt.Sprintf("%s (%d)", groupTitle(key), len(items)), markdown, 3)
	type field struct {
		name string
		get  func(model.Finding) string
	}
	meta := []field{
		{"Status", func(f model.Finding) string { return dash(f.Status) }},
		{"Severity", func(f model.Finding) string { return dash(f.Severity) }},
		{"Confidence", func(f model.Finding) string { return dash(f.Confidence) }},
		{"Verification", func(f model.Finding) string { return dash(f.Verification) }},
		{"Rule", func(f model.Finding) string { return dash(f.RuleID) }},
	}
	var subtitle []string
	var columns []field
	for _, spec := range meta {
		value, same := uniform(items, spec.get)
		switch {
		case same && (value == "-" || (spec.name == "Status" && value == key)):
			continue
		case same:
			subtitle = append(subtitle, strings.ToLower(spec.name)+" "+value)
		default:
			columns = append(columns, spec)
		}
	}
	if len(subtitle) > 0 {
		fmt.Fprintf(b, "%s\n\n", strings.Join(subtitle, ", "))
	}
	headers := []string{"Component"}
	wrap := map[int]int{}
	rows := make([][]string, len(items))
	for i, item := range items {
		rows[i] = []string{componentLabel(item)}
	}
	for _, spec := range columns {
		headers = append(headers, spec.name)
		for i, item := range items {
			rows[i] = append(rows[i], spec.get(item))
		}
	}
	detailIdx := len(headers)
	headers = append(headers, "Detail")
	includeTitle := key != model.StatusUnknown
	for i, item := range items {
		rows[i] = append(rows[i], findingDetail(item, includeTitle))
	}
	wrap[detailIdx] = detailWidth(headers, rows, detailIdx)
	writeTable(b, headers, rows, tableOpts{markdown: markdown, wrap: wrap, gapRows: !markdown})
	b.WriteString("\n")
}

func writeSteps(b *strings.Builder, steps []model.Step, markdown bool) {
	heading(b, "Steps", markdown, 2)
	headers := []string{"#", "Requirement", "Component", "Change", "Action"}
	rows := make([][]string, len(steps))
	notes := make([]string, len(steps))
	for i, step := range steps {
		rows[i] = []string{
			strconv.Itoa(step.Order),
			dash(step.Requirement),
			dash(step.Component),
			change(step.From, step.To),
			dash(step.Action),
		}
		notes[i] = stepDetail(step)
	}
	keep := visibleColumns(headers, rows)
	headers, rows = project(headers, rows, keep)
	opts := tableOpts{markdown: markdown}
	if len(headers) > 0 && headers[0] == "#" {
		opts.right = map[int]bool{0: true}
	}
	writeTable(b, headers, rows, opts)
	b.WriteString("\n")
	for i, note := range notes {
		if strings.TrimSpace(note) == "" {
			continue
		}
		label := rows[i][0]
		component := ""
		for c, header := range headers {
			if strings.EqualFold(header, "Component") && c < len(rows[i]) {
				component = rows[i][c]
			}
		}
		if component != "" && component != "-" {
			label += "  " + component
		}
		fmt.Fprintf(b, "%s\n", label)
		for _, paragraph := range strings.Split(note, "\n") {
			paragraph = strings.TrimSpace(paragraph)
			if paragraph == "" {
				continue
			}
			if markdown {
				fmt.Fprintf(b, "\n%s\n\n", paragraph)
				continue
			}
			for _, line := range wrap(paragraph, lineWidth-4) {
				fmt.Fprintf(b, "    %s\n", line)
			}
		}
		b.WriteString("\n")
	}
}

func writeGraph(b *strings.Builder, g graph.Graph, markdown bool) {
	heading(b, "Upgrade order", markdown, 2)
	labels := nodeLabels(g)
	var rows [][]string
	for _, edge := range g.Edges {
		if edge.Kind != graph.EdgeUpgradeBefore {
			continue
		}
		rows = append(rows, []string{labelFor(labels, edge.From), labelFor(labels, edge.To), dash(edge.RuleID)})
	}
	if len(rows) == 0 {
		b.WriteString("None. No verified upgrade order was derived.\n\n")
	} else {
		writeTable(b, []string{"Upgrade first", "Before", "Rule"}, rows, tableOpts{markdown: markdown})
		b.WriteString("\n")
	}
	if len(g.UpgradeOrderCycles) > 0 {
		b.WriteString("Cycles\n")
		for _, cycle := range g.UpgradeOrderCycles {
			parts := make([]string, len(cycle))
			for i, id := range cycle {
				parts[i] = labelFor(labels, id)
			}
			fmt.Fprintf(b, "- %s\n", strings.Join(parts, " -> "))
		}
		b.WriteString("\n")
	}
}

func writeCaveats(b *strings.Builder, markdown bool) {
	heading(b, "Caveats", markdown, 2)
	for _, line := range []string{
		"This tool does not change the cluster.",
		"A missing rule is unknown, not compatible.",
		"Removed Kubernetes APIs come from the Pluto versions.yaml pinned in knowledge/pluto/source.yaml. A deprecation with no removal version is a potential risk. An API absent from that file was not treated as compatible.",
		"Node kernel and OS image values are copied from kubelet status and were not verified by a node agent.",
		"Helm values and Secret data are omitted.",
		"Local kubectl skew is not evaluated.",
		"CNI, CSI, runtime, and Helm compatibility is unknown unless a loaded rule or a chart kubeVersion constraint says otherwise. A chart constraint is a publisher claim.",
	} {
		fmt.Fprintf(b, "- %s\n", line)
	}
}

func findingGroup(f model.Finding) string {
	if f.Severity == "info" {
		return "advisory"
	}
	switch f.Status {
	case model.StatusIncompatible, model.StatusPotentialRisk, model.StatusCompatible, model.StatusUnknown:
		return f.Status
	default:
		return model.StatusUnknown
	}
}

func groupTitle(key string) string {
	switch key {
	case model.StatusIncompatible:
		return "Incompatible"
	case model.StatusPotentialRisk:
		return "Potential risk"
	case model.StatusUnknown:
		return "Unknown"
	case model.StatusCompatible:
		return "Compatible"
	case "advisory":
		return "Advisories"
	default:
		return key
	}
}

func componentLabel(f model.Finding) string {
	if f.Component != "" {
		return f.Component
	}
	if f.Title != "" {
		return f.Title
	}
	if f.ID != "" {
		return f.ID
	}
	return "-"
}

func findingDetail(f model.Finding, includeTitle bool) string {
	var parts []string
	if f.BlocksPlan {
		parts = append(parts, "Blocks the upgrade until resolved.")
	}
	if f.ResolvedByPlan {
		parts = append(parts, "The direct path is unsupported. The plan sequences around it.")
	}
	title := strings.TrimSpace(f.Title)
	detail := strings.TrimSpace(f.Detail)
	if includeTitle && title != "" && title != detail {
		parts = append(parts, title)
	}
	if detail != "" {
		parts = append(parts, detail)
	} else if title != "" && !includeTitle {
		parts = append(parts, title)
	}
	if remediation := strings.TrimSpace(f.Remediation); remediation != "" {
		parts = append(parts, "Remediation: "+remediation)
	}
	for _, item := range f.Evidence {
		switch {
		case item.URL != "":
			parts = append(parts, item.URL)
			if item.RetrievedAt != "" {
				parts = append(parts, "retrieved "+item.RetrievedAt)
			}
		case item.Summary != "":
			parts = append(parts, item.Summary)
		}
	}
	return strings.Join(parts, "\n")
}

func stepDetail(step model.Step) string {
	var parts []string
	if rationale := strings.TrimSpace(step.Rationale); rationale != "" {
		parts = append(parts, rationale)
	}
	for _, validation := range step.Validations {
		if strings.TrimSpace(validation) != "" {
			parts = append(parts, "Validate: "+strings.TrimSpace(validation))
		}
	}
	if len(step.RuleIDs) > 0 {
		parts = append(parts, "Rules: "+strings.Join(step.RuleIDs, ", "))
	}
	for _, item := range step.Evidence {
		if item.URL != "" {
			parts = append(parts, item.URL)
		} else if item.Summary != "" {
			parts = append(parts, item.Summary)
		}
	}
	return strings.Join(parts, "\n")
}

func change(from, to string) string {
	switch {
	case from == "" && to == "":
		return "-"
	case from == "":
		return "-> " + to
	case to == "":
		return from + " ->"
	default:
		return from + " -> " + to
	}
}

func uniform(items []model.Finding, get func(model.Finding) string) (string, bool) {
	if len(items) == 0 {
		return "", true
	}
	first := get(items[0])
	for _, item := range items[1:] {
		if get(item) != first {
			return "", false
		}
	}
	return first, true
}

func detailWidth(headers []string, rows [][]string, detailIdx int) int {
	used := 0
	for i, header := range headers {
		if i == detailIdx {
			continue
		}
		w := len(header)
		for _, row := range rows {
			if i < len(row) {
				for _, line := range strings.Split(row[i], "\n") {
					if len(line) > w {
						w = len(line)
					}
				}
			}
		}
		used += w + 2
	}
	width := lineWidth - used
	if width < 40 {
		width = 40
	}
	if width > 72 {
		width = 72
	}
	return width
}

func visibleColumns(headers []string, rows [][]string) []int {
	var keep []int
	for c := range headers {
		show := false
		for _, row := range rows {
			cell := ""
			if c < len(row) {
				cell = strings.TrimSpace(row[c])
			}
			if cell != "" && cell != "-" {
				show = true
				break
			}
		}
		if show {
			keep = append(keep, c)
		}
	}
	return keep
}

func project(headers []string, rows [][]string, keep []int) ([]string, [][]string) {
	outHeaders := make([]string, len(keep))
	outRows := make([][]string, len(rows))
	for i, c := range keep {
		outHeaders[i] = headers[c]
	}
	for r, row := range rows {
		outRows[r] = make([]string, len(keep))
		for i, c := range keep {
			if c < len(row) {
				outRows[r][i] = row[c]
			}
		}
	}
	return outHeaders, outRows
}

type tableOpts struct {
	markdown bool
	wrap     map[int]int
	right    map[int]bool
	gapRows  bool
}

func writeTable(b *strings.Builder, headers []string, rows [][]string, opts tableOpts) {
	if opts.markdown {
		writeMarkdownTable(b, headers, rows, opts.right)
		return
	}
	wrapped := make([][][]string, len(rows))
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = len(headerLabel(header, false))
	}
	for r, row := range rows {
		wrapped[r] = make([][]string, len(headers))
		for c := range headers {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			var lines []string
			if width, ok := opts.wrap[c]; ok {
				for _, paragraph := range strings.Split(cell, "\n") {
					lines = append(lines, wrap(paragraph, width)...)
				}
			} else {
				lines = []string{oneLine(cell)}
			}
			if len(lines) == 0 {
				lines = []string{""}
			}
			wrapped[r][c] = lines
			for _, line := range lines {
				if len(line) > widths[c] {
					widths[c] = len(line)
				}
			}
		}
	}
	writeCells := func(cells []string, right map[int]bool) {
		for c, width := range widths {
			cell := ""
			if c < len(cells) {
				cell = cells[c]
			}
			if c > 0 {
				b.WriteString("  ")
			}
			if c == len(widths)-1 && !right[c] {
				b.WriteString(cell)
				continue
			}
			if right[c] {
				fmt.Fprintf(b, "%*s", width, cell)
			} else {
				fmt.Fprintf(b, "%-*s", width, cell)
			}
		}
		b.WriteString("\n")
	}
	headerCells := make([]string, len(headers))
	for i, header := range headers {
		headerCells[i] = headerLabel(header, false)
	}
	writeCells(headerCells, nil)
	for c, width := range widths {
		if c > 0 {
			b.WriteString("  ")
		}
		b.WriteString(strings.Repeat("-", width))
	}
	b.WriteString("\n")
	for r := range rows {
		height := 1
		for c := range headers {
			if n := len(wrapped[r][c]); n > height {
				height = n
			}
		}
		for line := 0; line < height; line++ {
			cells := make([]string, len(headers))
			for c := range headers {
				if line < len(wrapped[r][c]) {
					cells[c] = wrapped[r][c][line]
				}
			}
			writeCells(cells, opts.right)
		}
		if opts.gapRows && r < len(rows)-1 {
			b.WriteString("\n")
		}
	}
}

func writeMarkdownTable(b *strings.Builder, headers []string, rows [][]string, right map[int]bool) {
	b.WriteString("|")
	for _, header := range headers {
		fmt.Fprintf(b, " %s |", escapeMD(headerLabel(header, true)))
	}
	b.WriteString("\n|")
	for i := range headers {
		if right[i] {
			b.WriteString(" ---: |")
			continue
		}
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, row := range rows {
		b.WriteString("|")
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			fmt.Fprintf(b, " %s |", escapeMD(cell))
		}
		b.WriteString("\n")
	}
}

func writePairs(b *strings.Builder, pairs [][2]string, markdown bool) {
	if markdown {
		for _, pair := range pairs {
			fmt.Fprintf(b, "- **%s:** %s\n", pair[0], pair[1])
		}
		b.WriteString("\n")
		return
	}
	width := 0
	for _, pair := range pairs {
		if len(pair[0]) > width {
			width = len(pair[0])
		}
	}
	for _, pair := range pairs {
		fmt.Fprintf(b, "%-*s  %s\n", width, pair[0], pair[1])
	}
	b.WriteString("\n")
}

func heading(b *strings.Builder, text string, markdown bool, level int) {
	if markdown {
		fmt.Fprintf(b, "%s %s\n\n", strings.Repeat("#", level), text)
		return
	}
	if level == 1 {
		fmt.Fprintf(b, "%s\n\n", text)
		return
	}
	fmt.Fprintf(b, "%s\n", text)
}

func headerLabel(header string, markdown bool) string {
	if markdown || header == "#" {
		return header
	}
	return strings.ToUpper(header)
}

func nodeLabels(g graph.Graph) map[string]string {
	byName := map[string][]string{}
	raw := map[string]string{}
	for _, node := range g.Nodes {
		label := node.Name
		if label == "" {
			label = pathTail(node.ID)
		}
		raw[node.ID] = label
		byName[label] = append(byName[label], node.ID)
	}
	out := make(map[string]string, len(raw))
	for id, label := range raw {
		if len(byName[label]) > 1 {
			out[id] = id
		} else {
			out[id] = label
		}
	}
	return out
}

func labelFor(labels map[string]string, id string) string {
	if label, ok := labels[id]; ok && label != "" {
		return label
	}
	return pathTail(id)
}

func pathTail(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 && i < len(id)-1 {
		return id[i+1:]
	}
	if id == "" {
		return "-"
	}
	return id
}

func wrap(s string, width int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if width < 1 {
		return []string{s}
	}
	var lines []string
	var current string
	for _, word := range strings.Fields(s) {
		if current == "" {
			current = word
			continue
		}
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = word
			continue
		}
		current += " " + word
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func escapeMD(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "<br>")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func empty(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
