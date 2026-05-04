package services

// BriefingService stub. The real implementation lands with T1.4.
type BriefingService struct {
	deps Deps
}

func newBriefingService(deps Deps) *BriefingService {
	return &BriefingService{deps: deps}
}
