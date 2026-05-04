package services

// EventService stub. The real implementation lands with T1.3.
type EventService struct {
	deps Deps
}

func newEventService(deps Deps) *EventService {
	return &EventService{deps: deps}
}
