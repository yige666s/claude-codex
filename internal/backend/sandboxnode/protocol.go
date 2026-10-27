package sandboxnode

import "context"

const ProtocolVersion = "v1"

type Origin string

const (
	OriginRepository Origin = "repo_release"
	OriginExternal   Origin = "external"
	OriginGenerated  Origin = "generated"
	OriginUnknown    Origin = "unknown"
)

// InvocationRef identifies code without claiming that it is trusted.
// Callers must not derive trust from a Skill name, path, frontmatter or request body.
type InvocationRef struct {
	SkillID string
	EntryID string
	CodeRef string
}

// OriginDecision is produced by a trusted, server-side resolver. The specific
// provenance mechanism is intentionally separate from this transport contract.
type OriginDecision struct {
	Origin     Origin
	EvidenceID string
	EntryID    string
	Verified   bool
}

type OriginResolver interface {
	ResolveOrigin(context.Context, InvocationRef) (OriginDecision, error)
}

type ExecutionClass string

const (
	ClassIsolated ExecutionClass = "isolated"
	ClassNative   ExecutionClass = "native"
)

// ClassForOrigin is a fail-closed planning helper, not a node authorization
// check. The node must independently verify evidence before native execution.
func ClassForOrigin(decision OriginDecision) ExecutionClass {
	if decision.Origin == OriginRepository && decision.Verified && decision.EvidenceID != "" && decision.EntryID != "" {
		return ClassNative
	}
	return ClassIsolated
}

type Capabilities struct {
	ProtocolVersion string   `json:"protocol_version"`
	AcceptingJobs   bool     `json:"accepting_jobs"`
	Drivers         []string `json:"drivers"`
}
