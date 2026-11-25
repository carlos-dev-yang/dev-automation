package core

// JSON Schemas enforced on structured stage verdicts.
const (
	Gate0Schema = `{
  "type": "object",
  "required": ["result", "summary", "open_decisions", "roadmap", "acceptance_tcs"],
  "properties": {
    "result": {"type": "string", "enum": ["PASS", "BLOCKED"]},
    "summary": {"type": "string"},
    "open_decisions": {"type": "array", "items": {"type": "string"}},
    "roadmap": {"type": "array", "items": {"type": "string"}},
    "acceptance_tcs": {"type": "array", "items": {"type": "string"}}
  }
}`

	ImplSchema = `{
  "type": "object",
  "required": ["status", "summary", "changed_files", "compliance_evidence"],
  "properties": {
    "status": {"type": "string", "enum": ["READY_FOR_STAGE_3", "ABORTED"]},
    "summary": {"type": "string"},
    "changed_files": {"type": "array", "items": {"type": "string"}},
    "compliance_evidence": {"type": "array", "items": {"type": "string"}},
    "stop_reason": {"type": "string"}
  }
}`

	GoldenASchema = `{
  "type": "object",
  "required": ["result", "summary", "failures", "unverified_scope"],
  "properties": {
    "result": {"type": "string", "enum": ["PASS", "FAIL"]},
    "summary": {"type": "string"},
    "failures": {"type": "array", "items": {"type": "string"}},
    "unverified_scope": {"type": "array", "items": {"type": "string"}}
  }
}`

	GoldenBSchema = `{
  "type": "object",
  "required": ["result", "critical_count", "medium_count", "low_count", "findings", "skill_compliance_notes"],
  "properties": {
    "result": {"type": "string", "enum": ["PASS", "PASS_WITH_ADVICE", "FAIL"]},
    "critical_count": {"type": "integer", "minimum": 0},
    "medium_count": {"type": "integer", "minimum": 0},
    "low_count": {"type": "integer", "minimum": 0},
    "findings": {"type": "array", "items": {"type": "string"}},
    "skill_compliance_notes": {"type": "array", "items": {"type": "string"}}
  }
}`

	RemediationSchema = `{
  "type": "object",
  "required": ["sequence", "summary", "root_causes", "code_directive", "anti_hacking_constraints"],
  "properties": {
    "sequence": {"type": "string", "enum": ["SKILL_CORRECTION_FIRST", "CODE_CORRECTION_ONLY"]},
    "summary": {"type": "string"},
    "root_causes": {"type": "array", "items": {"type": "string"}},
    "skill_correction_plan": {"type": "string"},
    "code_directive": {"type": "string"},
    "anti_hacking_constraints": {"type": "array", "items": {"type": "string"}}
  }
}`

	SkillEvolutionSchema = `{
  "type": "object",
  "required": ["action", "summary", "target_skills"],
  "properties": {
    "action": {"type": "string", "enum": ["NEW_DRAFT", "CLEAN_REWRITE", "PROMOTE", "RETIRE"]},
    "summary": {"type": "string"},
    "target_skills": {"type": "array", "items": {"type": "string"}}
  }
}`
)

type Gate0Verdict struct {
	Result        string   `json:"result"` // PASS | BLOCKED
	Summary       string   `json:"summary"`
	OpenDecisions []string `json:"open_decisions"`
	Roadmap       []string `json:"roadmap"`
	AcceptanceTCs []string `json:"acceptance_tcs"`
}

type ImplVerdict struct {
	Status             string   `json:"status"` // READY_FOR_STAGE_3 | ABORTED
	Summary            string   `json:"summary"`
	ChangedFiles       []string `json:"changed_files"`
	ComplianceEvidence []string `json:"compliance_evidence"`
	StopReason         string   `json:"stop_reason,omitempty"`
}

type GoldenAVerdict struct {
	Result          string   `json:"result"` // PASS | FAIL
	Summary         string   `json:"summary"`
	Failures        []string `json:"failures"`
	UnverifiedScope []string `json:"unverified_scope"`
}

type GoldenBVerdict struct {
	Result               string   `json:"result"` // PASS | PASS_WITH_ADVICE | FAIL
	CriticalCount        int      `json:"critical_count"`
	MediumCount          int      `json:"medium_count"`
	LowCount             int      `json:"low_count"`
	Findings             []string `json:"findings"`
	SkillComplianceNotes []string `json:"skill_compliance_notes"`
}

type RemediationVerdict struct {
	Sequence               string   `json:"sequence"` // SKILL_CORRECTION_FIRST | CODE_CORRECTION_ONLY
	Summary                string   `json:"summary"`
	RootCauses             []string `json:"root_causes"`
	SkillCorrectionPlan    string   `json:"skill_correction_plan,omitempty"`
	CodeDirective          string   `json:"code_directive"`
	AntiHackingConstraints []string `json:"anti_hacking_constraints"`
}

type SkillEvolutionVerdict struct {
	Action       string   `json:"action"`
	Summary      string   `json:"summary"`
	TargetSkills []string `json:"target_skills"`
}
