// Package model holds analysis and plan documents shared by the CLI and report.
package model

import "github.com/deepak-muley/k8s-cluster-pilot/internal/graph"

// SchemaVersion is the analysis and plan document contract.
const SchemaVersion = "v1alpha1"

// Document kinds.
const (
	KindAnalysis = "clusterpilot.analysis"
	KindPlan     = "clusterpilot.plan"
)

// Finding statuses. Missing evidence is never recorded as compatible.
const (
	StatusCompatible    = "compatible"
	StatusIncompatible  = "incompatible"
	StatusPotentialRisk = "potential-risk"
	StatusUnknown       = "unknown"
)

// Overall statuses.
const (
	OutcomeBlocked              = "blocked"
	OutcomeRisksPresent         = "risks-present"
	OutcomeInsufficientEvidence = "insufficient-evidence"
	OutcomePlanAvailable        = "plan-available"
	OutcomeCompatible           = "compatible"
)

// Step requirements.
const (
	RequirementRequired    = "required"
	RequirementRecommended = "recommended"
	RequirementVerify      = "verify"
)

// Document is either an analysis or a plan.
type Document struct {
	SchemaVersion     string      `json:"schemaVersion"`
	Kind              string      `json:"kind"`
	GeneratedAt       string      `json:"generatedAt"`
	ReadOnly          bool        `json:"readOnly"`
	ClusterName       string      `json:"clusterName,omitempty"`
	CurrentKubernetes string      `json:"currentKubernetes,omitempty"`
	TargetKubernetes  string      `json:"targetKubernetes"`
	Status            string      `json:"status"`
	Summary           Summary     `json:"summary"`
	Findings          []Finding   `json:"findings"`
	Graph             graph.Graph `json:"graph"`
	Steps             []Step      `json:"steps"`
}

// Summary counts findings that affect the overall status.
// Info findings are advisories and are listed, but they do not change Status.
type Summary struct {
	Compatible       int `json:"compatible"`
	Incompatible     int `json:"incompatible"`
	PotentialRisk    int `json:"potentialRisk"`
	Unknown          int `json:"unknown"`
	Advisories       int `json:"advisories"`
	RequiredSteps    int `json:"requiredSteps,omitempty"`
	RecommendedSteps int `json:"recommendedSteps,omitempty"`
	VerifySteps      int `json:"verifySteps,omitempty"`
}

// Finding is one compatibility or health result.
type Finding struct {
	ID             string     `json:"id"`
	RuleID         string     `json:"ruleId,omitempty"`
	RuleType       string     `json:"ruleType,omitempty"`
	Status         string     `json:"status"`
	Severity       string     `json:"severity"`
	Component      string     `json:"component,omitempty"`
	ComponentType  string     `json:"componentType,omitempty"`
	Title          string     `json:"title"`
	Detail         string     `json:"detail"`
	Remediation    string     `json:"remediation,omitempty"`
	Evidence       []Evidence `json:"evidence,omitempty"`
	Confidence     string     `json:"confidence"`
	Verification   string     `json:"verification"`
	BlocksPlan     bool       `json:"blocksPlan,omitempty"`
	ResolvedByPlan bool       `json:"resolvedByPlan,omitempty"`
}

// Evidence cites a rule source or an observation.
type Evidence struct {
	Kind        string `json:"kind,omitempty"`
	Summary     string `json:"summary,omitempty"`
	URL         string `json:"url,omitempty"`
	Title       string `json:"title,omitempty"`
	RetrievedAt string `json:"retrievedAt,omitempty"`
	RuleID      string `json:"ruleId,omitempty"`
}

// Step is one ordered action in an upgrade plan.
type Step struct {
	ID          string     `json:"id"`
	Order       int        `json:"order"`
	Requirement string     `json:"requirement"`
	Action      string     `json:"action"`
	Component   string     `json:"component"`
	From        string     `json:"from,omitempty"`
	To          string     `json:"to,omitempty"`
	Rationale   string     `json:"rationale"`
	RuleIDs     []string   `json:"ruleIds,omitempty"`
	Validations []string   `json:"validations,omitempty"`
	Evidence    []Evidence `json:"evidence,omitempty"`
}

// Add records one finding into the summary.
func (s *Summary) Add(f Finding) {
	if f.Severity == "info" {
		s.Advisories++
		return
	}
	switch f.Status {
	case StatusCompatible:
		s.Compatible++
	case StatusIncompatible:
		s.Incompatible++
	case StatusPotentialRisk:
		s.PotentialRisk++
	default:
		s.Unknown++
	}
}

// DeriveStatus chooses the overall status from finding flags.
// A minor-skip incompatibility does not block when the plan can sequence it.
func DeriveStatus(findings []Finding, sequencedMinorJump bool) string {
	blocked := false
	risk := false
	unknown := false
	for _, f := range findings {
		if f.Severity == "info" {
			continue
		}
		if f.BlocksPlan {
			blocked = true
		}
		switch f.Status {
		case StatusPotentialRisk:
			risk = true
		case StatusUnknown:
			unknown = true
		}
	}
	switch {
	case blocked:
		return OutcomeBlocked
	case risk:
		return OutcomeRisksPresent
	case unknown:
		return OutcomeInsufficientEvidence
	case sequencedMinorJump:
		return OutcomePlanAvailable
	default:
		return OutcomeCompatible
	}
}
