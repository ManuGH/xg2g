package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	ctrlauth "github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/playback"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/epg"
	householddomain "github.com/ManuGH/xg2g/internal/household"
)

func TestHouseholdMiddlewareUsesDefaultProfileWhenHeaderMissing(t *testing.T) {
	svc := householddomain.NewService(householddomain.NewMemoryStore())
	srv := NewServer(config.AppConfig{TrustedProxies: "0.0.0.0/0,::/0"}, nil, nil)
	srv.SetDependencies(Dependencies{Households: svc})
	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:read"})

	var resolvedProfileID string
	handler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), []MiddlewareFunc{
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				profile := householddomain.ProfileFromContext(r.Context())
				if profile != nil {
					resolvedProfileID = profile.ID
				}
				w.WriteHeader(http.StatusNoContent)
			})
		},
	})
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, V3BaseURL+"/services/bouquets", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if resolvedProfileID != householddomain.DefaultProfileID {
		t.Fatalf("expected default household profile, got %q", resolvedProfileID)
	}
}

func TestHouseholdMiddlewareRejectsUnknownProfileHeader(t *testing.T) {
	svc := householddomain.NewService(householddomain.NewMemoryStore())
	srv := NewServer(config.AppConfig{TrustedProxies: "0.0.0.0/0,::/0"}, nil, nil)
	srv.SetDependencies(Dependencies{Households: svc})
	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:read"})

	handler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, V3BaseURL+"/services/bouquets", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set(householddomain.ProfileHeader, "does-not-exist")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetServicesFiltersByHouseholdProfile(t *testing.T) {
	tmpDir := t.TempDir()
	playlistPath := filepath.Join(tmpDir, "household.m3u")
	content := `#EXTM3U
#EXTINF:-1 tvg-id="kids-1" group-title="Kids",Kids 1
http://example.com/live?k=1&ref=1:0:1:AAAA:
#EXTINF:-1 tvg-id="news-1" group-title="News",News 1
http://example.com/live?k=2&ref=1:0:1:BBBB:
`
	if err := os.WriteFile(playlistPath, []byte(content), 0600); err != nil {
		t.Fatalf("write playlist: %v", err)
	}

	cfg := config.AppConfig{DataDir: tmpDir}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: "household.m3u"}}

	srv := NewServer(cfg, nil, nil)
	srv.UpdateConfig(cfg, snap)

	profile := householddomain.Profile{
		ID:              "kids",
		Name:            "Kinder",
		Kind:            householddomain.ProfileKindChild,
		AllowedBouquets: []string{"kids"},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v3/services", nil)
	req = req.WithContext(householddomain.WithProfile(req.Context(), &profile))
	rr := httptest.NewRecorder()

	srv.GetServices(rr, req, GetServicesParams{})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var services []Service
	if err := json.NewDecoder(rr.Body).Decode(&services); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one visible service, got %d", len(services))
	}
	if services[0].Name == nil || *services[0].Name != "Kids 1" {
		t.Fatalf("unexpected visible service: %#v", services[0])
	}
}

func TestGetEpgFiltersByHouseholdProfile(t *testing.T) {
	tmpDir := t.TempDir()
	playlistPath := filepath.Join(tmpDir, "household-epg.m3u")
	content := `#EXTM3U
#EXTINF:-1 tvg-id="kids-1" group-title="Kids",Kids 1
http://example.com/live?k=1&ref=1:0:1:AAAA:
#EXTINF:-1 tvg-id="news-1" group-title="News",News 1
http://example.com/live?k=2&ref=1:0:1:BBBB:
#EXTINF:-1 tvg-id="sports-1" group-title="Sports",Sports 1
http://example.com/live?k=3&ref=1:0:1:CCCC:
`
	if err := os.WriteFile(playlistPath, []byte(content), 0600); err != nil {
		t.Fatalf("write playlist: %v", err)
	}

	cfg := config.AppConfig{DataDir: tmpDir}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: "household-epg.m3u"}}

	now := time.Now()
	srv := NewServer(cfg, nil, nil)
	srv.UpdateConfig(cfg, snap)
	srv.epgSource = stubHouseholdEpgSource{
		programs: []epg.Programme{
			{
				Channel: "1:0:1:AAAA:",
				Title:   epg.Title{Text: "Kids Show"},
				Start:   now.Add(-15 * time.Minute).Format("20060102150405 -0700"),
				Stop:    now.Add(15 * time.Minute).Format("20060102150405 -0700"),
			},
			{
				Channel: "1:0:1:BBBB:",
				Title:   epg.Title{Text: "News Update"},
				Start:   now.Add(-15 * time.Minute).Format("20060102150405 -0700"),
				Stop:    now.Add(15 * time.Minute).Format("20060102150405 -0700"),
			},
			{
				Channel: "1:0:1:CCCC:",
				Title:   epg.Title{Text: "Sports Live"},
				Start:   now.Add(-15 * time.Minute).Format("20060102150405 -0700"),
				Stop:    now.Add(15 * time.Minute).Format("20060102150405 -0700"),
			},
		},
	}

	profile := householddomain.Profile{
		ID:                 "kids",
		Name:               "Kinder",
		Kind:               householddomain.ProfileKindChild,
		AllowedBouquets:    []string{"kids"},
		AllowedServiceRefs: []string{"1:0:1:BBBB"},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v3/epg", nil)
	req = req.WithContext(householddomain.WithProfile(req.Context(), &profile))
	rr := httptest.NewRecorder()

	srv.GetEpg(rr, req, GetEpgParams{})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var items []EpgItem
	if err := json.NewDecoder(rr.Body).Decode(&items); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two visible EPG entries, got %d", len(items))
	}
	if items[0].Title != "Kids Show" || items[1].Title != "News Update" {
		t.Fatalf("unexpected epg items: %#v", items)
	}
}

func TestGetSystemConfigRequiresHouseholdSettingsAccess(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)
	profile := householddomain.Profile{
		ID:   "child",
		Name: "Kind",
		Kind: householddomain.ProfileKindChild,
		Permissions: householddomain.Permissions{
			DVRPlayback: true,
			DVRManage:   false,
			Settings:    false,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/config", nil)
	req = req.WithContext(householddomain.WithProfile(req.Context(), &profile))
	rr := httptest.NewRecorder()

	srv.GetSystemConfig(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestGetHouseholdProfilesAllowsReadWithoutSettingsAccess(t *testing.T) {
	svc := householddomain.NewService(householddomain.NewMemoryStore())
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.SetDependencies(Dependencies{Households: svc})
	profile := householddomain.Profile{
		ID:   "child",
		Name: "Kind",
		Kind: householddomain.ProfileKindChild,
		Permissions: householddomain.Permissions{
			DVRPlayback: true,
			DVRManage:   false,
			Settings:    false,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v3/household/profiles", nil)
	req = req.WithContext(householddomain.WithProfile(req.Context(), &profile))
	rr := httptest.NewRecorder()

	srv.GetHouseholdProfiles(rr, req, GetHouseholdProfilesParams{})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestGetRecordingsFiltersByHouseholdProfile(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.SetDependencies(Dependencies{
		RecordingsService: stubHouseholdRecordingsService{
			listResult: recservice.ListResult{
				Roots: []recservice.RecordingRoot{
					{ID: "hdd", Name: "movie"},
				},
				CurrentRoot: "hdd",
				Recordings: []recservice.RecordingItem{
					{
						ServiceRef:  "1:0:1:AAAA",
						RecordingID: "allowed-recording",
						Title:       "Allowed Recording",
					},
					{
						ServiceRef:  "1:0:1:BBBB",
						RecordingID: "blocked-recording",
						Title:       "Blocked Recording",
					},
				},
			},
		},
	})

	profile := householddomain.Profile{
		ID:   "child",
		Name: "Kind",
		Kind: householddomain.ProfileKindChild,
		Permissions: householddomain.Permissions{
			DVRPlayback: true,
			DVRManage:   false,
			Settings:    false,
		},
		AllowedServiceRefs: []string{"1:0:1:AAAA"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings", nil)
	req = req.WithContext(householddomain.WithProfile(req.Context(), &profile))
	rr := httptest.NewRecorder()

	srv.GetRecordings(rr, req, GetRecordingsParams{})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var response RecordingResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Recordings == nil || len(*response.Recordings) != 1 {
		t.Fatalf("expected one visible recording, got %#v", response.Recordings)
	}
	if title := (*response.Recordings)[0].Title; title == nil || *title != "Allowed Recording" {
		t.Fatalf("unexpected visible recording: %#v", (*response.Recordings)[0])
	}
}

func TestAddTimerRejectsForbiddenHouseholdServiceRef(t *testing.T) {
	profile := householddomain.Profile{
		ID:   "child",
		Name: "Kind",
		Kind: householddomain.ProfileKindChild,
		Permissions: householddomain.Permissions{
			DVRPlayback: true,
			DVRManage:   true,
			Settings:    false,
		},
		AllowedServiceRefs: []string{"1:0:1:AAAA"},
	}

	body := TimerCreateRequest{
		ServiceRef: "1:0:1:BBBB",
		Name:       "Forbidden Timer",
		Begin:      time.Now().Add(5 * time.Minute).Unix(),
		End:        time.Now().Add(65 * time.Minute).Unix(),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	srv := NewServer(config.AppConfig{}, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v3/timers", bytes.NewReader(payload))
	req = req.WithContext(householddomain.WithProfile(req.Context(), &profile))
	rr := httptest.NewRecorder()

	srv.AddTimer(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestHouseholdProfilesCRUDRoundTrip(t *testing.T) {
	svc := householddomain.NewService(householddomain.NewMemoryStore())
	if _, err := svc.Save(context.Background(), householddomain.CreateDefaultProfile()); err != nil {
		t.Fatalf("seed default profile: %v", err)
	}

	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.SetDependencies(Dependencies{Households: svc})

	createBody := HouseholdProfile{
		Id:   "child-room",
		Name: "Kinderzimmer",
		Kind: HouseholdProfileKind("child"),
		Permissions: HouseholdProfilePermissions{
			DvrPlayback: true,
			DvrManage:   false,
			Settings:    false,
		},
	}
	createJSON, err := json.Marshal(createBody)
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v3/household/profiles", bytes.NewReader(createJSON))
	createRes := httptest.NewRecorder()
	srv.PostHouseholdProfiles(createRes, createReq, PostHouseholdProfilesParams{})
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createRes.Code)
	}

	updateBody := createBody
	updateBody.Name = "Schlafzimmer"
	updateJSON, err := json.Marshal(updateBody)
	if err != nil {
		t.Fatalf("marshal update body: %v", err)
	}
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v3/household/profiles/child-room", bytes.NewReader(updateJSON))
	updateRes := httptest.NewRecorder()
	srv.PutHouseholdProfile(updateRes, updateReq, "child-room", PutHouseholdProfileParams{})
	if updateRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", updateRes.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v3/household/profiles", nil)
	listRes := httptest.NewRecorder()
	srv.GetHouseholdProfiles(listRes, listReq, GetHouseholdProfilesParams{})
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRes.Code)
	}

	var profiles []HouseholdProfile
	if err := json.NewDecoder(listRes.Body).Decode(&profiles); err != nil {
		t.Fatalf("decode profile list: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected two profiles, got %d", len(profiles))
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v3/household/profiles/child-room", nil)
	deleteRes := httptest.NewRecorder()
	srv.DeleteHouseholdProfile(deleteRes, deleteReq, "child-room", DeleteHouseholdProfileParams{})
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", deleteRes.Code)
	}
}

func TestHouseholdUnlockAllowsExplicitAdultProfile(t *testing.T) {
	pinHash, err := householddomain.HashPIN("1234")
	if err != nil {
		t.Fatalf("hash pin: %v", err)
	}

	svc := householddomain.NewService(householddomain.NewMemoryStore())
	srv := NewServer(config.AppConfig{
		TrustedProxies: "0.0.0.0/0,::/0",
		Household: config.HouseholdConfig{
			PinHash: pinHash,
		},
	}, nil, nil)
	srv.SetDependencies(Dependencies{Households: svc})
	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:read"})

	protectedHandler := srv.householdMiddleware(srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	blockedReq := httptest.NewRequest(http.MethodGet, V3BaseURL+"/services/bouquets", nil)
	blockedReq.RemoteAddr = "127.0.0.1:1234"
	blockedReq.Header.Set(householddomain.ProfileHeader, householddomain.DefaultProfileID)
	blockedRes := httptest.NewRecorder()
	protectedHandler.ServeHTTP(blockedRes, blockedReq)

	if blockedRes.Code != http.StatusForbidden {
		t.Fatalf("expected 403 before unlock, got %d", blockedRes.Code)
	}

	unlockBody, err := json.Marshal(HouseholdUnlockRequest{Pin: "1234"})
	if err != nil {
		t.Fatalf("marshal unlock request: %v", err)
	}
	unlockReq := httptest.NewRequest(http.MethodPost, V3BaseURL+"/household/unlock", bytes.NewReader(unlockBody))
	unlockReq.RemoteAddr = "127.0.0.1:1234"
	unlockRes := httptest.NewRecorder()
	srv.PostHouseholdUnlock(unlockRes, unlockReq, PostHouseholdUnlockParams{})

	if unlockRes.Code != http.StatusOK {
		t.Fatalf("expected 200 unlock response, got %d", unlockRes.Code)
	}

	cookies := unlockRes.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected household unlock cookie")
	}

	allowedReq := httptest.NewRequest(http.MethodGet, V3BaseURL+"/services/bouquets", nil)
	allowedReq.RemoteAddr = "127.0.0.1:1234"
	allowedReq.Header.Set(householddomain.ProfileHeader, householddomain.DefaultProfileID)
	allowedReq.AddCookie(cookies[0])
	allowedRes := httptest.NewRecorder()
	protectedHandler.ServeHTTP(allowedRes, allowedReq)

	if allowedRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204 after unlock, got %d", allowedRes.Code)
	}

	lockReq := httptest.NewRequest(http.MethodDelete, V3BaseURL+"/household/unlock", nil)
	lockReq.RemoteAddr = "127.0.0.1:1234"
	lockReq.AddCookie(cookies[0])
	lockRes := httptest.NewRecorder()
	srv.DeleteHouseholdUnlock(lockRes, lockReq, DeleteHouseholdUnlockParams{})
	if lockRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204 lock response, got %d", lockRes.Code)
	}

	relockedReq := httptest.NewRequest(http.MethodGet, V3BaseURL+"/services/bouquets", nil)
	relockedReq.RemoteAddr = "127.0.0.1:1234"
	relockedReq.Header.Set(householddomain.ProfileHeader, householddomain.DefaultProfileID)
	relockedReq.AddCookie(cookies[0])
	relockedRes := httptest.NewRecorder()
	protectedHandler.ServeHTTP(relockedRes, relockedReq)

	if relockedRes.Code != http.StatusForbidden {
		t.Fatalf("expected 403 after relock, got %d", relockedRes.Code)
	}
}

func TestDeleteSessionRejectsLockedChildLogoutWhenPinConfigured(t *testing.T) {
	pinHash, err := householddomain.HashPIN("1234")
	if err != nil {
		t.Fatalf("hash pin: %v", err)
	}

	srv := NewServer(config.AppConfig{
		Household: config.HouseholdConfig{
			PinHash: pinHash,
		},
	}, nil, nil)
	profile := householddomain.Profile{
		ID:   "child",
		Name: "Kind",
		Kind: householddomain.ProfileKindChild,
	}
	req := httptest.NewRequest(http.MethodDelete, V3BaseURL+"/auth/session", nil)
	req = req.WithContext(householddomain.WithProfile(req.Context(), &profile))
	req = req.WithContext(householddomain.WithAccessState(req.Context(), householddomain.AccessState{
		PinConfigured: true,
		Unlocked:      false,
	}))
	rr := httptest.NewRecorder()

	srv.DeleteSession(rr, req, DeleteSessionParams{})

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestGetSystemConfigIncludesHouseholdPinConfigured(t *testing.T) {
	pinHash, err := householddomain.HashPIN("1234")
	if err != nil {
		t.Fatalf("hash pin: %v", err)
	}

	srv := NewServer(config.AppConfig{
		Household: config.HouseholdConfig{
			PinHash: pinHash,
		},
	}, nil, nil)
	profile := householddomain.Profile{
		ID:   householddomain.DefaultProfileID,
		Name: "Haushalt",
		Kind: householddomain.ProfileKindAdult,
		Permissions: householddomain.Permissions{
			DVRPlayback: true,
			DVRManage:   true,
			Settings:    true,
		},
	}

	req := httptest.NewRequest(http.MethodGet, V3BaseURL+"/system/config", nil)
	req = req.WithContext(householddomain.WithProfile(req.Context(), &profile))
	req = req.WithContext(householddomain.WithAccessState(req.Context(), householddomain.AccessState{
		PinConfigured: true,
		Unlocked:      true,
	}))
	rr := httptest.NewRecorder()

	srv.GetSystemConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var response AppConfig
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Household == nil || response.Household.PinConfigured == nil || !*response.Household.PinConfigured {
		t.Fatalf("expected household pinConfigured=true, got %#v", response.Household)
	}
}

func TestHouseholdUnlockTTLDefaultsToFourHours(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)
	if got := srv.householdUnlockTTLOrDefault(); got != 4*time.Hour {
		t.Fatalf("expected default household unlock ttl to be 4h, got %v", got)
	}
}

func TestHouseholdUnlockTTLRespectsConfigAndAuthSessionCap(t *testing.T) {
	srv := NewServer(config.AppConfig{
		Household: config.HouseholdConfig{
			UnlockTTL: 6 * time.Hour,
		},
	}, nil, nil)
	srv.authSessionTTL = 2 * time.Hour

	if got := srv.householdUnlockTTLOrDefault(); got != 2*time.Hour {
		t.Fatalf("expected household unlock ttl to be capped by auth session ttl, got %v", got)
	}

	srv.authSessionTTL = 12 * time.Hour
	if got := srv.householdUnlockTTLOrDefault(); got != 6*time.Hour {
		t.Fatalf("expected configured household unlock ttl to be used, got %v", got)
	}
}

func testPrincipalAuthMiddleware(t *testing.T, scopes []string) func(http.Handler) http.Handler {
	t.Helper()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal := ctrlauth.NewPrincipal("test-token", "viewer", scopes)
			next.ServeHTTP(w, r.WithContext(ctrlauth.WithPrincipal(r.Context(), principal)))
		})
	}
}

type stubHouseholdEpgSource struct {
	programs           []epg.Programme
	bouquetServiceRefs map[string]map[string]struct{}
}

func (s stubHouseholdEpgSource) GetPrograms(context.Context) ([]epg.Programme, error) {
	return append([]epg.Programme(nil), s.programs...), nil
}

func (s stubHouseholdEpgSource) GetBouquetServiceRefs(_ context.Context, bouquet string) (map[string]struct{}, error) {
	if bouquet == "" {
		return nil, nil
	}
	if s.bouquetServiceRefs == nil {
		return map[string]struct{}{}, nil
	}
	refs := s.bouquetServiceRefs[bouquet]
	if refs == nil {
		return map[string]struct{}{}, nil
	}
	cloned := make(map[string]struct{}, len(refs))
	for key := range refs {
		cloned[key] = struct{}{}
	}
	return cloned, nil
}

type stubHouseholdRecordingsService struct {
	listResult recservice.ListResult
}

func (s stubHouseholdRecordingsService) ResolvePlayback(context.Context, string, string) (recservice.PlaybackResolution, error) {
	return recservice.PlaybackResolution{}, nil
}

func (s stubHouseholdRecordingsService) List(context.Context, recservice.ListInput) (recservice.ListResult, error) {
	return s.listResult, nil
}

func (s stubHouseholdRecordingsService) GetPlaybackInfo(context.Context, recservice.PlaybackInfoInput) (recservice.PlaybackInfoResult, error) {
	return recservice.PlaybackInfoResult{}, nil
}

func (s stubHouseholdRecordingsService) GetStatus(context.Context, recservice.StatusInput) (recservice.StatusResult, error) {
	return recservice.StatusResult{}, nil
}

func (s stubHouseholdRecordingsService) GetMediaTruth(context.Context, string) (playback.MediaTruth, error) {
	return playback.MediaTruth{}, nil
}

func (s stubHouseholdRecordingsService) Stream(context.Context, recservice.StreamInput) (recservice.StreamResult, error) {
	return recservice.StreamResult{}, nil
}

func (s stubHouseholdRecordingsService) Delete(context.Context, recservice.DeleteInput) (recservice.DeleteResult, error) {
	return recservice.DeleteResult{}, nil
}
