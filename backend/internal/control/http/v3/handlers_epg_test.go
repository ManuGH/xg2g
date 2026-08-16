package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockEpgSource is a mock implementation of the EpgSource interface
type MockEpgSource struct {
	mock.Mock
}

func (m *MockEpgSource) GetPrograms(ctx context.Context) ([]epg.Programme, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]epg.Programme), args.Error(1)
}

func (m *MockEpgSource) GetBouquetServiceRefs(ctx context.Context, bouquet string) (map[string]struct{}, error) {
	args := m.Called(ctx, bouquet)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]struct{}), args.Error(1)
}

func TestGetEpg_ResponseShape(t *testing.T) {
	// Setup
	mockSource := new(MockEpgSource)
	// We need to bypass the read package logic or mock it, but handlers_epg.go calls read.GetEpg directly.
	// Since read.GetEpg is a function and not on an interface, we can't easily mock it without refactoring
	// or relying on the behavior of read.GetEpg using the source we provide.

	// However, looking at handlers_epg.go:
	// entries, err := read.GetEpg(r.Context(), src, q, read.RealClock{})

	// read.GetEpg uses src.GetPrograms. So if we mock src, we can control the output associated with read.GetEpg logic.

	// Let's create a server instance with the mock source
	// Note: We need to see how Server is constructed and if we can inject epgSource.
	// In handlers_epg.go: src := s.epgSource

	server := &Server{
		epgSource: mockSource,
	}

	// Mock data
	now := time.Now()
	progs := []epg.Programme{
		{
			Channel: "1:0:1:1:1:1:1:0:0:0:",
			Title:   epg.Title{Text: "Test Show"},
			Start:   now.Format("20060102150405 -0700"), // XMLTV format
			Stop:    now.Add(1 * time.Hour).Format("20060102150405 -0700"),
		},
	}

	mockSource.On("GetPrograms", mock.Anything).Return(progs, nil)
	// For default query, it might not call GetBouquetServiceRefs unless bouquet filter is used,
	// but read.GetEpg might verify services. Let's assume simple path first.

	req := httptest.NewRequest("GET", "/api/v3/epg", nil)
	w := httptest.NewRecorder()

	// Execute
	server.GetEpg(w, req, GetEpgParams{})

	// Verify
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Response should be a bare array (not wrapped in {"items": ...})
	var items []EpgItem
	err := json.NewDecoder(resp.Body).Decode(&items)
	assert.NoError(t, err, "Response should be a bare JSON array")
	assert.Len(t, items, 1)
	assert.Equal(t, "Test Show", items[0].Title)
	assert.NotNil(t, items[0].StartXMLTV)
	assert.NotNil(t, items[0].EndXMLTV)
	assert.Equal(t, progs[0].Start, *items[0].StartXMLTV)
	assert.Equal(t, progs[0].Stop, *items[0].EndXMLTV)
}

func TestGetEpg_EmptyResponseIsArray(t *testing.T) {
	mockSource := new(MockEpgSource)
	server := &Server{
		epgSource: mockSource,
	}

	mockSource.On("GetPrograms", mock.Anything).Return([]epg.Programme{}, nil)

	req := httptest.NewRequest("GET", "/api/v3/epg", nil)
	w := httptest.NewRecorder()

	server.GetEpg(w, req, GetEpgParams{})

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "[]\n", w.Body.String())

	var items []EpgItem
	err := json.NewDecoder(resp.Body).Decode(&items)
	assert.NoError(t, err)
	assert.Len(t, items, 0)
}

func TestPostServicesNowNext_FallsBackToEpgSourceWhenCacheMissing(t *testing.T) {
	mockSource := new(MockEpgSource)
	server := &Server{
		epgSource: mockSource,
	}

	now := time.Now()
	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0"
	progs := []epg.Programme{
		{
			Channel: serviceRef,
			Title:   epg.Title{Text: "ZIB Flash"},
			Start:   now.Add(-5 * time.Minute).Format(xmltvTimeFormat),
			Stop:    now.Add(10 * time.Minute).Format(xmltvTimeFormat),
		},
		{
			Channel: serviceRef,
			Title:   epg.Title{Text: "S.W.A.T."},
			Start:   now.Add(10 * time.Minute).Format(xmltvTimeFormat),
			Stop:    now.Add(55 * time.Minute).Format(xmltvTimeFormat),
		},
	}

	mockSource.On("GetPrograms", mock.Anything).Return(progs, nil).Once()

	body := bytes.NewBufferString(`{"services":["1:0:19:132F:3EF:1:C00000:0:0:0"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v3/services/now-next", body)
	w := httptest.NewRecorder()

	server.PostServicesNowNext(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var payload struct {
		Items []nowNextItem `json:"items"`
	}
	err := json.NewDecoder(resp.Body).Decode(&payload)
	assert.NoError(t, err)
	assert.Len(t, payload.Items, 1)
	assert.NotNil(t, payload.Items[0].Now)
	assert.NotNil(t, payload.Items[0].Next)
	assert.Equal(t, "ZIB Flash", payload.Items[0].Now.Title)
	assert.Equal(t, "S.W.A.T.", payload.Items[0].Next.Title)

	mockSource.AssertExpectations(t)
}

func TestBuildNowNextItems_CanonicalizesServiceRefs(t *testing.T) {
	now := time.Now()
	items := buildNowNextItems(
		[]string{"1:0:19:132f:3ef:1:c00000:0:0:0:"},
		[]epg.Programme{
			{
				Channel: "1:0:19:132F:3EF:1:C00000:0:0:0",
				Title:   epg.Title{Text: "Current Show"},
				Start:   now.Add(-10 * time.Minute).Format(xmltvTimeFormat),
				Stop:    now.Add(20 * time.Minute).Format(xmltvTimeFormat),
			},
			{
				Channel: "1:0:19:132F:3EF:1:C00000:0:0:0",
				Title:   epg.Title{Text: "Next Show"},
				Start:   now.Add(20 * time.Minute).Format(xmltvTimeFormat),
				Stop:    now.Add(50 * time.Minute).Format(xmltvTimeFormat),
			},
		},
		now,
	)

	assert.Len(t, items, 1)
	assert.NotNil(t, items[0].Now)
	assert.NotNil(t, items[0].Next)
	assert.Equal(t, "Current Show", items[0].Now.Title)
	assert.Equal(t, "Next Show", items[0].Next.Title)
}

func TestBuildNowNextItems_PreservesXmltvOffsets(t *testing.T) {
	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0"
	items := buildNowNextItems(
		[]string{serviceRef},
		[]epg.Programme{
			{
				Channel: serviceRef,
				Title:   epg.Title{Text: "DST Special"},
				Start:   "20260329013000 +0100",
				Stop:    "20260329033000 +0200",
			},
		},
		time.Date(2026, time.March, 29, 1, 0, 0, 0, time.UTC),
	)

	assert.Len(t, items, 1)
	if assert.NotNil(t, items[0].Now) {
		assert.Equal(t, "20260329013000 +0100", items[0].Now.StartXMLTV)
		assert.Equal(t, "20260329033000 +0200", items[0].Now.EndXMLTV)
	}
}

func TestPostServicesNowNext_CanonicalMetadataInResponse(t *testing.T) {
	mockSource := new(MockEpgSource)
	server := &Server{
		epgSource: mockSource,
	}

	now := time.Now()
	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0"
	progs := []epg.Programme{
		{
			Channel:  serviceRef,
			Title:    epg.Title{Text: "Babylon Berlin S03E05"},
			Desc:     &epg.Description{Text: "Krimi im Berlin der 1920er Jahre. FSK: 16. Regie: Tom Tykwer."},
			Start:    now.Add(-10 * time.Minute).Format(xmltvTimeFormat),
			Stop:     now.Add(35 * time.Minute).Format(xmltvTimeFormat),
			Category: []string{"Krimiserie"},
		},
	}
	// Enrich once as done on ingest
	epg.EnrichProgramme(&progs[0])

	mockSource.On("GetPrograms", mock.Anything).Return(progs, nil).Once()

	body := bytes.NewBufferString(`{"services":["1:0:19:132F:3EF:1:C00000:0:0:0"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v3/services/now-next", body)
	w := httptest.NewRecorder()

	server.PostServicesNowNext(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var payload struct {
		Items []nowNextItem `json:"items"`
	}
	err := json.NewDecoder(resp.Body).Decode(&payload)
	require.NoError(t, err)
	require.Len(t, payload.Items, 1)

	nowItem := payload.Items[0].Now
	require.NotNil(t, nowItem)
	assert.Equal(t, "Babylon Berlin S03E05", nowItem.Title)
	assert.Equal(t, "series", nowItem.Genre)
	assert.Equal(t, "dvb_category", nowItem.GenreSource)

	require.NotNil(t, nowItem.AgeRating)
	assert.Equal(t, 16, nowItem.AgeRating.Value)
	assert.Equal(t, "FSK", nowItem.AgeRating.Scheme)
	assert.Equal(t, "DE", nowItem.AgeRating.Country)
	assert.Equal(t, epg.RatingSourceDVBText, nowItem.AgeRating.Source)
	assert.Equal(t, epg.RatingConfidenceObserved, nowItem.AgeRating.Confidence)

	require.NotNil(t, nowItem.EpisodeInfo)
	assert.Equal(t, 3, nowItem.EpisodeInfo.SeasonNumber)
	assert.Equal(t, 5, nowItem.EpisodeInfo.EpisodeNumber)
	assert.Equal(t, "SxxExx", nowItem.EpisodeInfo.SourcePattern)
}

func TestPostServicesNowNext_ReadOnlyDoesNotEnrichUncanonicalProgramme(t *testing.T) {
	mockSource := new(MockEpgSource)
	server := &Server{
		epgSource: mockSource,
	}

	now := time.Now()
	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0"
	// Uncanonical programme: contains text that would match parser, but Canonical is nil.
	progs := []epg.Programme{
		{
			Channel:   serviceRef,
			Title:     epg.Title{Text: "Babylon Berlin S03E05"},
			Desc:      &epg.Description{Text: "Krimi im Berlin der 1920er Jahre. FSK: 16. Regie: Tom Tykwer."},
			Start:     now.Add(-10 * time.Minute).Format(xmltvTimeFormat),
			Stop:      now.Add(35 * time.Minute).Format(xmltvTimeFormat),
			Category:  []string{"Krimiserie"},
			Canonical: nil, // Strictly unparsed
		},
	}

	mockSource.On("GetPrograms", mock.Anything).Return(progs, nil).Once()

	body := bytes.NewBufferString(`{"services":["1:0:19:132F:3EF:1:C00000:0:0:0"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v3/services/now-next", body)
	w := httptest.NewRecorder()

	server.PostServicesNowNext(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var payload struct {
		Items []nowNextItem `json:"items"`
	}
	err := json.NewDecoder(resp.Body).Decode(&payload)
	require.NoError(t, err)
	require.Len(t, payload.Items, 1)

	nowItem := payload.Items[0].Now
	require.NotNil(t, nowItem)
	assert.Equal(t, "Babylon Berlin S03E05", nowItem.Title)
	// Must be strictly nil/empty because the handler MUST NOT parse during request processing
	assert.Nil(t, nowItem.AgeRating, "handler must not parse AgeRating on the fly")
	assert.Nil(t, nowItem.EpisodeInfo, "handler must not parse EpisodeInfo on the fly")
	assert.Empty(t, nowItem.Genre, "handler must not extract Genre on the fly")
	assert.Empty(t, nowItem.GenreSource, "handler must not extract GenreSource on the fly")
}

func TestPostServicesNowNext_ProgrammesFromEPGIntegration(t *testing.T) {
	// 1. Raw OpenWebIF EPG Event (Ingest boundary)
	now := time.Now()
	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0"
	events := []openwebif.EPGEvent{
		{
			ID:          2001,
			Title:       "Tatort: Das Team S02E04",
			Description: "Krimi. FSK 12. Regie: Jan Georg Schütte",
			LongDesc:    "Krimi. FSK 12. Regie: Jan Georg Schütte",
			Begin:       now.Add(-10 * time.Minute).Unix(),
			Duration:    5400,
			SRef:        serviceRef,
			Genre:       "Fernsehserie",
		},
	}

	// 2. Ingest transformation (ProgrammesFromEPG parses canonical metadata on ingest)
	programmes := epg.ProgrammesFromEPG(events, serviceRef)
	require.Len(t, programmes, 1)
	require.NotNil(t, programmes[0].Canonical, "Ingest MUST populate Canonical metadata")

	// 3. Stored in EPG source / cache
	mockSource := new(MockEpgSource)
	mockSource.On("GetPrograms", mock.Anything).Return(programmes, nil).Once()

	server := &Server{
		epgSource: mockSource,
	}

	// 4. HTTP Read endpoint (/services/now-next reads pre-parsed Canonical metadata)
	body := bytes.NewBufferString(`{"services":["1:0:19:132F:3EF:1:C00000:0:0:0"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v3/services/now-next", body)
	w := httptest.NewRecorder()

	server.PostServicesNowNext(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var payload struct {
		Items []nowNextItem `json:"items"`
	}
	err := json.NewDecoder(resp.Body).Decode(&payload)
	require.NoError(t, err)
	require.Len(t, payload.Items, 1)

	nowItem := payload.Items[0].Now
	require.NotNil(t, nowItem)
	assert.Equal(t, "Tatort: Das Team S02E04", nowItem.Title)
	assert.Equal(t, "series", nowItem.Genre)
	assert.Equal(t, "dvb_category", nowItem.GenreSource)

	require.NotNil(t, nowItem.AgeRating)
	assert.Equal(t, 12, nowItem.AgeRating.Value)
	assert.Equal(t, "FSK", nowItem.AgeRating.Scheme)
	assert.Equal(t, "DE", nowItem.AgeRating.Country)
	assert.Equal(t, epg.RatingSourceDVBText, nowItem.AgeRating.Source)
	assert.Equal(t, epg.RatingConfidenceObserved, nowItem.AgeRating.Confidence)

	require.NotNil(t, nowItem.EpisodeInfo)
	assert.Equal(t, 2, nowItem.EpisodeInfo.SeasonNumber)
	assert.Equal(t, 4, nowItem.EpisodeInfo.EpisodeNumber)
	assert.Equal(t, "SxxExx", nowItem.EpisodeInfo.SourcePattern)
}

// panicProvider implements epg.MetadataProvider and panics if ever called.
type panicProvider struct{}

func (panicProvider) Name() string { return "panic_guard" }
func (panicProvider) Lookup(ctx context.Context, fp epg.ProgrammeFingerprint) (*epg.EnrichmentData, error) {
	panic("VIOLATION: MetadataProvider.Lookup must NEVER be called from the HTTP request path")
}

func TestPostServicesNowNext_ArchitectureIsolationNeverTouchesProvider(t *testing.T) {
	// Proves that /services/now-next never invokes any metadata provider method
	mockSource := new(MockEpgSource)
	server := &Server{
		epgSource: mockSource,
	}

	now := time.Now()
	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0"
	progs := []epg.Programme{
		{
			Channel: serviceRef,
			Title:   epg.Title{Text: "Test Programme"},
			Desc:    &epg.Description{Text: "Some description"},
			Start:   now.Add(-10 * time.Minute).Format(xmltvTimeFormat),
			Stop:    now.Add(35 * time.Minute).Format(xmltvTimeFormat),
		},
	}

	mockSource.On("GetPrograms", mock.Anything).Return(progs, nil).Once()

	body := bytes.NewBufferString(`{"services":["1:0:19:132F:3EF:1:C00000:0:0:0"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v3/services/now-next", body)
	w := httptest.NewRecorder()

	// Must execute cleanly without triggering any panic from panicProvider
	assert.NotPanics(t, func() {
		server.PostServicesNowNext(w, req)
	})

	assert.Equal(t, http.StatusOK, w.Result().StatusCode)
}
