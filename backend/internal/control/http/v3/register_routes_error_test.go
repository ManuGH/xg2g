package v3

import (
	"errors"
	"net/http"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/require"
)

type failingRouteRegistrar struct {
	err error
}

func (r failingRouteRegistrar) Register(string, string, http.Handler) error {
	return r.err
}

func TestRegisterRoutesPropagatesRegistrarErrorWithoutPanic(t *testing.T) {
	sentinel := errors.New("registration rejected")
	svc := NewServer(config.AppConfig{}, config.NewManager(""), nil)

	require.NotPanics(t, func() {
		err := RegisterRoutes(failingRouteRegistrar{err: sentinel}, svc)
		require.ErrorIs(t, err, sentinel)
		require.ErrorContains(t, err, "register operation")
	})
}
