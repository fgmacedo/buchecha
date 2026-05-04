package services

// PromptService stub. The real implementation lands with T1.5.
type PromptService struct {
	deps Deps
}

func newPromptService(deps Deps) *PromptService {
	return &PromptService{deps: deps}
}
