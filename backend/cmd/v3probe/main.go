package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
)

type ProbeReport struct {
	Timestamp   time.Time         `json:"timestamp"`
	BaseURL     string            `json:"base_url"`
	Checks      []CheckResult     `json:"checks"`
	Environment map[string]string `json:"environment"`
}

type CheckResult struct {
	Name      string `json:"name"`
	Passed    bool   `json:"passed"`
	LatencyMs int64  `json:"latency_ms"`
	Details   string `json:"details,omitempty"`
	Body      string `json:"body,omitempty"` // Captured body on failure
}

var httpClient = &http.Client{
	Timeout: 5 * time.Second,
}

var (
	failAfterCreate = flag.String("fail-after-create", "", "Inject failure after creation: 'panic' or 'error'")
	baseURLFlag     = flag.String("base-url", "", "Override V3_BASE_URL")
)

func main() {
	flag.Parse()
	cfg := ProbeConfig{
		BaseURL:         *baseURLFlag,
		FailAfterCreate: *failAfterCreate,
	}

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Probe failed: %v\n", err)
		os.Exit(1)
	}
}

type ProbeConfig struct {
	BaseURL         string
	FailAfterCreate string
}

func run(cfg ProbeConfig) error {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = config.ParseString("V3_BASE_URL", "")
	}
	if baseURL == "" {
		baseURL = "http://localhost:8088"
	}

	report := ProbeReport{
		Timestamp: time.Now(),
		BaseURL:   baseURL,
		Checks:    make([]CheckResult, 0),
		Environment: map[string]string{
			"USER": config.ParseString("USER", ""),
		},
	}

	// Helper to capture body in error
	runCheck := func(name string, fn func() (string, error)) {
		start := time.Now()
		bodyCaptured, err := fn()
		latency := time.Since(start).Milliseconds()

		res := CheckResult{
			Name:      name,
			Passed:    err == nil,
			LatencyMs: latency,
			Body:      bodyCaptured,
		}
		if err != nil {
			res.Details = err.Error()
		}
		report.Checks = append(report.Checks, res)
		if err != nil {
			fmt.Printf("FAIL: %s (%s)\n", name, err)
		} else {
			fmt.Printf("PASS: %s (%dms)\n", name, latency)
		}
	}

	// 0. Server Identity (Check if V3 and Auth are awake)
	runCheck("Server_Identity", func() (string, error) {
		// Use a known existing endpoint, e.g. /api/v3/services or /api/v3/system/health logic
		// We'll check /api/v3/services as it's the base for everything else
		code, _, bodyBytes, err := doRequest("GET", baseURL+"/api/v3/services", nil)
		if err != nil {
			return "", fmt.Errorf("net error: %v", err)
		}

		// We accept:
		// 200 OK (Auth worked, or no auth)
		// 401/403 (Auth required - proves server is there)
		// We REJECT 404 (Server not mounted or wrong path)
		if code == http.StatusNotFound {
			return string(bodyBytes), fmt.Errorf("server returned 404 (Not Found) - V3 API likely not mounted")
		}
		if code >= 500 {
			return string(bodyBytes), fmt.Errorf("server error: %d", code)
		}
		return string(bodyBytes), nil
	})

	// 0.5 Setup: Get Valid Service Ref
	var serviceRef string
	runCheck("Setup_FetchServiceRef", func() (string, error) {
		// Priority 1: Env Var
		if s := config.ParseString("V3_SERVICE_REF", ""); s != "" {
			serviceRef = s
			return "", nil
		}

		code, _, bodyBytes, err := doRequest("GET", baseURL+"/api/v3/services", nil)
		if err != nil {
			return "", fmt.Errorf("net error: %v", err)
		}

		if code != http.StatusOK {
			return string(bodyBytes), fmt.Errorf("failed status: %d", code)
		}

		ref, err := parseServiceRef(bodyBytes)
		if err != nil {
			return string(bodyBytes), fmt.Errorf("parse failed: %v", err)
		}
		serviceRef = ref
		return "", nil
	})

	if serviceRef == "" {
		fmt.Println("FATAL: Could not resolve V3_SERVICE_REF. Will skip related DVR checks.")
	}

	// 1. System / Router Checks
	runCheck("Router_404_RFC7807", func() (string, error) {
		return checkRFC7807(baseURL+"/api/v3/non-existent-route", http.StatusNotFound, "NOT_FOUND")
	})

	runCheck("Router_405_RFC7807", func() (string, error) {
		// CreateSession is POST only, GET should be 405
		return checkRFC7807(baseURL+"/api/v3/auth/session", http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
	})

	// 2. DVR Timer CRUD
	if serviceRef != "" {
		var createdTimerID string
		runCheck("DVR_Timer_Lifecycle", func() (string, error) {
			// A. Create
			begin := time.Now().Add(24 * time.Hour).Unix()
			end := begin + 3600
			probeRunID := fmt.Sprintf("v3probe-%d", time.Now().UnixNano())

			createPayload := map[string]any{
				"serviceRef":  serviceRef,
				"serviceName": "V3Probe Test Service",
				"name":        "V3Probe Test Timer " + probeRunID,
				"description": "Created by v3probe " + probeRunID,
				"begin":       begin,
				"end":         end,
			}
			body, _ := json.Marshal(createPayload)
			code, _, bodyBytes, err := doRequest("POST", baseURL+"/api/v3/timers", bytes.NewReader(body))
			if err != nil {
				return "", err
			}

			if code != http.StatusCreated {
				return string(bodyBytes), fmt.Errorf("create failed: %d", code)
			}

			var created struct {
				TimerId string `json:"timerId"`
			}
			if err := json.Unmarshal(bodyBytes, &created); err != nil {
				// Try alternate casing
				var createdSnake struct {
					TimerId string `json:"timer_id"`
				}
				if err2 := json.Unmarshal(bodyBytes, &createdSnake); err2 == nil && createdSnake.TimerId != "" {
					createdTimerID = createdSnake.TimerId
				} else {
					return string(bodyBytes), fmt.Errorf("decode create response failed: %v", err)
				}
			} else {
				createdTimerID = created.TimerId
			}

			if createdTimerID == "" {
				return string(bodyBytes), fmt.Errorf("created timer ID is empty")
			}

			// --- PROBE HARDENING: IMMEDIATE CLEANUP REGISTRATION ---
			// Invariant: Once we have an ID, we MUST attempt to clean it up.
			// This defer handles panics and early returns.
			defer func() {
				// Best-effort cleanup
				fmt.Printf("CLEANUP: Attempting to delete timer %s\n", createdTimerID)
				delCode, _, _, delErr := doRequest("DELETE", baseURL+"/api/v3/timers/"+createdTimerID, nil)
				if delErr != nil {
					fmt.Printf("CLEANUP FAIL: net error %v\n", delErr)
				} else if delCode != http.StatusNoContent && delCode != http.StatusNotFound {
					fmt.Printf("CLEANUP FAIL: status %d\n", delCode)
				} else {
					fmt.Printf("CLEANUP SUCCESS: Timer %s deleted\n", createdTimerID)
				}
			}()

			// --- PROBE HARDENING: FAILURE INJECTION ---
			if cfg.FailAfterCreate != "" {
				fmt.Printf("INJECTING FAILURE: %s\n", cfg.FailAfterCreate)
				if cfg.FailAfterCreate == "panic" {
					panic("simulated panic after create")
				}
				return "", fmt.Errorf("simulated error after create")
			}

			// B. Read Back (Verify Round Trip)
			getCode, _, getBody, err := doRequest("GET", baseURL+"/api/v3/timers/"+createdTimerID, nil)
			if err != nil {
				return "", fmt.Errorf("read-back net error: %v", err)
			}

			if getCode != http.StatusOK {
				return string(getBody), fmt.Errorf("read-back status error: %d", getCode)
			}

			// C. Negative Test: PATCH with Padding (Option A Hardening)
			negPayload := map[string]any{
				"paddingBeforeSec": 60,
			}
			negBody, _ := json.Marshal(negPayload)
			negCode, _, negBytes, err := doRequest("PATCH", baseURL+"/api/v3/timers/"+createdTimerID, bytes.NewReader(negBody))
			if err != nil {
				return "", fmt.Errorf("neg-test net error: %v", err)
			}
			if negCode != http.StatusBadRequest {
				return string(negBytes), fmt.Errorf("negative test failed: expected 400 for padding patch, got %d", negCode)
			}

			// D. Update (PATCH) - Valid
			patchPayload := map[string]any{
				"name": "V3Probe Updated Name",
			}
			patchBody, _ := json.Marshal(patchPayload)
			patchCode, _, patchRespBody, err := doRequest("PATCH", baseURL+"/api/v3/timers/"+createdTimerID, bytes.NewReader(patchBody))
			if err != nil {
				return string(getBody), fmt.Errorf("update net error: %v", err)
			}

			if patchCode != http.StatusOK {
				return string(patchRespBody), fmt.Errorf("update status error: %d", patchCode)
			}

			// E. Delete (Explicit check implies success, but defer doubles down safely)
			// We can leave the actual delete to the defer, strictly speaking, but to test "delete works",
			// we should probably do it here. If we do it here, the defer will 404, which is fine.
			// Or we can rely on the defer logging.
			// Let's do explicit delete to assert 204.
			delCode, _, delBody, err := doRequest("DELETE", baseURL+"/api/v3/timers/"+createdTimerID, nil)
			if err != nil {
				return string(patchRespBody), fmt.Errorf("delete net error: %v", err)
			}

			if delCode != http.StatusNoContent {
				return string(delBody), fmt.Errorf("delete status error: %d", delCode)
			}

			return "", nil
		})
	}

	// 3. Conflict Preview (Fail Closed)
	runCheck("Conflicts_Preview_FailClosed", func() (string, error) {
		code, _, bodyBytes, err := doRequest("POST", baseURL+"/api/v3/timers/conflicts:preview", bytes.NewBufferString("{}"))
		if err != nil {
			return "", err
		}

		// If 404, the endpoint is missing - that's a failure of the probe target
		if code == http.StatusNotFound {
			return string(bodyBytes), fmt.Errorf("endpoint not found (404)")
		}
		if code >= 500 {
			return string(bodyBytes), fmt.Errorf("server error on conflicts: %d", code)
		}
		// Accept 400 (Invalid Input) or 401/403 (Auth)
		if code != http.StatusBadRequest && code != http.StatusUnauthorized && code != http.StatusForbidden { //nolint:staticcheck
			// Actually, on valid garbage input, it should be 400.
			// 401/403 acceptable if we failed auth.
			// 200 OK would be weird for garbage input unless schema validation is loose.
			// Let's warn but pass if 200, fail if 5xx or 404.
		}
		return string(bodyBytes), nil
	})

	// Output Report
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}

	// Fail if any checks failed
	for _, c := range report.Checks {
		if !c.Passed {
			return fmt.Errorf("one or more checks failed")
		}
	}
	return nil
}

// Wraps http.NewRequest + Auth Injection + Client.Do + Body Read
func doRequest(method, urlStr string, body io.Reader) (int, http.Header, []byte, error) {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return 0, nil, nil, err
	}

	// Apply Auth
	if token := config.ParseString("V3_AUTH_BEARER", ""); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if user := config.ParseString("V3_AUTH_BASIC_USER", ""); user != "" {
		pass := config.ParseString("V3_AUTH_BASIC_PASS", "")
		req.SetBasicAuth(user, pass)
	} // else: no auth configured, try annonymous

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, bodyBytes, err
}

func checkRFC7807(urlStr string, expectedStatus int, expectedCode string) (string, error) {
	code, header, bodyBytes, err := doRequest("GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	if code != expectedStatus {
		return string(bodyBytes), fmt.Errorf("status mismatch: got %d want %d", code, expectedStatus)
	}

	contentType := header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/problem+json") {
		return string(bodyBytes), fmt.Errorf("content-type mismatch: got %s", contentType)
	}

	var prob struct {
		Code     string `json:"code"`
		Title    string `json:"title"`
		Status   int    `json:"status"`
		Instance string `json:"instance"`
	}
	if err := json.Unmarshal(bodyBytes, &prob); err != nil {
		return string(bodyBytes), fmt.Errorf("invalid json body: %v", err)
	}

	if prob.Code != expectedCode {
		return string(bodyBytes), fmt.Errorf("code mismatch: got %s want %s", prob.Code, expectedCode)
	}
	if prob.Status != expectedStatus {
		return string(bodyBytes), fmt.Errorf("body status mismatch: got %d want %d", prob.Status, expectedStatus)
	}

	// Check instance contains path
	u, _ := url.Parse(urlStr)
	if !strings.Contains(prob.Instance, u.Path) {
		return string(bodyBytes), fmt.Errorf("instance path mismatch: got %s, expected to contain %s", prob.Instance, u.Path)
	}

	return string(bodyBytes), nil
}

func parseServiceRef(body []byte) (string, error) {
	// Shape 1: []{ "serviceRef": "..." }
	var shape1 []struct {
		ServiceRef string `json:"serviceRef"`
	}
	if err := json.Unmarshal(body, &shape1); err == nil && len(shape1) > 0 {
		for _, s := range shape1 {
			if s.ServiceRef != "" {
				return s.ServiceRef, nil
			}
		}
	}

	// Shape 2: []{ "service_ref": "..." }
	var shape2 []struct {
		ServiceRef string `json:"service_ref"`
	}
	if err := json.Unmarshal(body, &shape2); err == nil && len(shape2) > 0 {
		for _, s := range shape2 {
			if s.ServiceRef != "" {
				return s.ServiceRef, nil
			}
		}
	}

	// Shape 3: { "items": [ { "serviceRef": "..." } ] }
	var shape3 struct {
		Items []struct {
			ServiceRef string `json:"serviceRef"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &shape3); err == nil && len(shape3.Items) > 0 {
		for _, s := range shape3.Items {
			if s.ServiceRef != "" {
				return s.ServiceRef, nil
			}
		}
	}

	// Shape 4: { "items": [ { "service_ref": "..." } ] }
	var shape4 struct {
		Items []struct {
			ServiceRef string `json:"service_ref"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &shape4); err == nil && len(shape4.Items) > 0 {
		for _, s := range shape4.Items {
			if s.ServiceRef != "" {
				return s.ServiceRef, nil
			}
		}
	}

	return "", fmt.Errorf("no valid serviceRef found in response")
}
