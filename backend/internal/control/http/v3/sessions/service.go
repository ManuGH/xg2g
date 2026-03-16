package sessions

// Service resolves transport-neutral session read use cases for the v3 HTTP layer.
type Service struct {
	deps Deps
}

// NewService constructs a v3 sessions service.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}
