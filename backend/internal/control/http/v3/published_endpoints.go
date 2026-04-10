package v3

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
)

type publishedEndpointResponse struct {
	URL             string `json:"url"`
	Kind            string `json:"kind"`
	Priority        int    `json:"priority"`
	TLSMode         string `json:"tlsMode"`
	AllowPairing    bool   `json:"allowPairing"`
	AllowStreaming  bool   `json:"allowStreaming"`
	AllowWeb        bool   `json:"allowWeb"`
	AllowNative     bool   `json:"allowNative"`
	AdvertiseReason string `json:"advertiseReason"`
	Source          string `json:"source"`
}

type serverPublishedEndpointProvider struct {
	s *Server
}

func (p serverPublishedEndpointProvider) PublishedEndpoints(_ context.Context) ([]connectivitydomain.PublishedEndpoint, error) {
	return p.s.publishedEndpoints()
}

func (s *Server) publishedEndpoints() ([]connectivitydomain.PublishedEndpoint, error) {
	endpoints, err := config.BuildPublishedEndpoints(s.GetConfig())
	if err != nil {
		return nil, err
	}
	return connectivitydomain.ClonePublishedEndpoints(endpoints), nil
}

func mapPublishedEndpointResponses(values []connectivitydomain.PublishedEndpoint) []publishedEndpointResponse {
	if len(values) == 0 {
		return []publishedEndpointResponse{}
	}

	out := make([]publishedEndpointResponse, 0, len(values))
	for _, value := range values {
		out = append(out, publishedEndpointResponse{
			URL:             value.URL,
			Kind:            string(value.Kind),
			Priority:        value.Priority,
			TLSMode:         string(value.TLSMode),
			AllowPairing:    value.AllowPairing,
			AllowStreaming:  value.AllowStreaming,
			AllowWeb:        value.AllowWeb,
			AllowNative:     value.AllowNative,
			AdvertiseReason: value.AdvertiseReason,
			Source:          string(value.Source),
		})
	}

	return out
}

func buildConnectivityConfigResponse(cfg config.AppConfig) (*ConnectivityConfig, error) {
	report, err := config.BuildConnectivityContract(cfg)
	if err != nil {
		return nil, err
	}
	return &ConnectivityConfig{
		Profile:            ConnectivityDeploymentProfile(report.Profile),
		AllowLocalHTTP:     report.AllowLocalHTTP,
		PublishedEndpoints: mapPublishedEndpointContracts(report.PublishedEndpoints),
	}, nil
}

func mapPublishedEndpointContracts(values []connectivitydomain.PublishedEndpoint) []PublishedEndpoint {
	if len(values) == 0 {
		return []PublishedEndpoint{}
	}

	out := make([]PublishedEndpoint, 0, len(values))
	for _, value := range values {
		kind := PublishedEndpointKind(value.Kind)
		tlsMode := PublishedEndpointTLSMode(value.TLSMode)
		source := PublishedEndpointSource(value.Source)
		out = append(out, PublishedEndpoint{
			Url:             value.URL,
			Kind:            kind,
			Priority:        int32(value.Priority),
			TlsMode:         tlsMode,
			AllowPairing:    value.AllowPairing,
			AllowStreaming:  value.AllowStreaming,
			AllowWeb:        value.AllowWeb,
			AllowNative:     value.AllowNative,
			AdvertiseReason: value.AdvertiseReason,
			Source:          source,
		})
	}

	return out
}
