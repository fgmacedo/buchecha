package services

// SessionService stub. The real implementation lands with T1.2.
type SessionService struct {
	deps Deps
}

func newSessionService(deps Deps) *SessionService {
	return &SessionService{deps: deps}
}
