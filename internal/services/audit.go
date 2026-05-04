package services

// Audit stub. The real implementation lands with T1.7.
type Audit struct {
	deps Deps
}

func newAudit(deps Deps) *Audit {
	return &Audit{deps: deps}
}
