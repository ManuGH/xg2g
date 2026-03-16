// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/stretchr/testify/require"
)

func TestGetErrors_ReturnsPublicRegistryEntries(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, V3BaseURL+"/errors", nil)
	rr := httptest.NewRecorder()

	s.GetErrors(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var body ErrorCatalogResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))

	expected := make([]ErrorCatalogEntry, 0, len(problemcode.PublicEntries()))
	for _, entry := range problemcode.PublicEntries() {
		expected = append(expected, ErrorCatalogEntry{
			Code:         ProblemCode(entry.Code),
			Description:  entry.Description,
			OperatorHint: entry.OperatorHint,
			ProblemType:  entry.ProblemType,
			Retryable:    entry.Retryable,
			RunbookUrl:   stringPtrOrNil(entry.RunbookURL),
			Severity:     ErrorSeverity(entry.Severity),
			Title:        entry.DefaultTitle,
		})
	}

	require.Equal(t, expected, body.Items)
	require.NotEmpty(t, body.Items[0].Description)
	require.NotEmpty(t, body.Items[0].OperatorHint)
	require.NotEmpty(t, body.Items[0].Severity)
}

func TestV3Contract_GetErrors_OpenAPI(t *testing.T) {
	s, _ := newV3TestServer(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, V3BaseURL+"/errors", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	rr := httptest.NewRecorder()
	NewRouter(s, RouterOptions{BaseURL: V3BaseURL}).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	validateOpenAPIResponse(t, loadOpenAPIDoc(t), req, rr, nil)
}
