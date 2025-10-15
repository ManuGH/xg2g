package metrics_test

import (
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestPromhttpExposure(t *testing.T) {
	srv := httptest.NewServer(promhttp.Handler())
	defer srv.Close()

	if _, err := srv.Client().Get(srv.URL); err != nil {
		t.Fatal(err)
	}
}
