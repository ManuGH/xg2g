package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
)

func runEntitlementsCLI(args []string) int {
	return runEntitlementsCLIWithIO(args, os.Stdout, os.Stderr)
}

func runEntitlementsCLIWithIO(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printEntitlementsUsage(stdout)
		return 0
	}

	switch args[0] {
	case "list":
		return runEntitlementsList(args[1:], stdout, stderr)
	case "grant":
		return runEntitlementsGrant(args[1:], stdout, stderr)
	case "revoke":
		return runEntitlementsRevoke(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Unknown subcommand: %s\n\n", args[0])
		printEntitlementsUsage(stderr)
		return 2
	}
}

func printEntitlementsUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  xg2g entitlements list [--token TOKEN] [--base-url URL | --port PORT] [--principal-id ID] [--json]")
	_, _ = fmt.Fprintln(w, "  xg2g entitlements grant --principal-id ID --scope SCOPE [--scope SCOPE] [--expires 24h] [--token TOKEN] [--base-url URL | --port PORT]")
	_, _ = fmt.Fprintln(w, "  xg2g entitlements revoke --principal-id ID --scope SCOPE [--scope SCOPE] [--token TOKEN] [--base-url URL | --port PORT]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Notes:")
	_, _ = fmt.Fprintln(w, "  grant/revoke go through the authenticated API so the running daemon invalidates its entitlement cache immediately.")
	_, _ = fmt.Fprintln(w, "  list reads /system/entitlements and can inspect another principal with an admin token plus --principal-id.")
}

func runEntitlementsList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("xg2g entitlements list", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var token string
	var baseURL string
	var principalID string
	var port int
	var timeout time.Duration
	var jsonOutput bool

	fs.StringVar(&token, "token", "", "API token (defaults to $XG2G_API_TOKEN)")
	fs.StringVar(&baseURL, "base-url", "", "Base daemon URL, with or without /api/v3")
	fs.StringVar(&principalID, "principal-id", "", "Inspect a specific principal (admin scope required)")
	fs.IntVar(&port, "port", 8088, "Local daemon API port")
	fs.DurationVar(&timeout, "timeout", 5*time.Second, "HTTP timeout")
	fs.BoolVar(&jsonOutput, "json", false, "Print raw JSON response")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	apiBaseURL, resolvedToken, ok := resolveEntitlementsCLICommon(baseURL, port, token, stderr)
	if !ok {
		return 2
	}

	status, rawBody, err := getEntitlementStatusFromAPI(apiBaseURL, resolvedToken, strings.TrimSpace(principalID), timeout)
	if err != nil {
		fmt.Fprintf(stderr, "FAILED: %v\n", err)
		return 1
	}

	if jsonOutput {
		_, _ = stdout.Write(rawBody)
		if len(rawBody) == 0 || rawBody[len(rawBody)-1] != '\n' {
			_, _ = fmt.Fprintln(stdout)
		}
		return 0
	}

	targetPrincipalID := derefStringOr(status.PrincipalId, "(current principal)")
	fmt.Fprintf(stdout, "Principal: %s\n", targetPrincipalID)
	fmt.Fprintf(stdout, "Unlocked: %t\n", derefBoolOr(status.Unlocked, false))
	fmt.Fprintf(stdout, "Required: %s\n", formatCLIStringSlice(status.RequiredScopes))
	fmt.Fprintf(stdout, "Granted:  %s\n", formatCLIStringSlice(status.GrantedScopes))
	fmt.Fprintf(stdout, "Missing:  %s\n", formatCLIStringSlice(status.MissingScopes))
	if grants := status.Grants; grants != nil && len(*grants) > 0 {
		_, _ = fmt.Fprintln(stdout, "Grants:")
		for _, grant := range *grants {
			line := fmt.Sprintf("  - %s via %s", derefStringOr(grant.Scope, "?"), derefStringOr(grant.Source, "?"))
			if grant.ExpiresAt != nil {
				line += " expires " + grant.ExpiresAt.UTC().Format(time.RFC3339)
			}
			if grant.Active != nil {
				if *grant.Active {
					line += " [active]"
				} else {
					line += " [expired]"
				}
			}
			_, _ = fmt.Fprintln(stdout, line)
		}
	}

	return 0
}

func runEntitlementsGrant(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("xg2g entitlements grant", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var token string
	var baseURL string
	var principalID string
	var port int
	var timeout time.Duration
	var expires time.Duration
	var scopes stringSliceFlag

	fs.StringVar(&token, "token", "", "API token (defaults to $XG2G_API_TOKEN)")
	fs.StringVar(&baseURL, "base-url", "", "Base daemon URL, with or without /api/v3")
	fs.StringVar(&principalID, "principal-id", "", "Principal to grant")
	fs.IntVar(&port, "port", 8088, "Local daemon API port")
	fs.DurationVar(&timeout, "timeout", 5*time.Second, "HTTP timeout")
	fs.DurationVar(&expires, "expires", 0, "Relative expiry for the override, for example 24h")
	fs.Var(&scopes, "scope", "Scope to grant; repeat the flag or pass comma-separated values")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	apiBaseURL, resolvedToken, ok := resolveEntitlementsCLICommon(baseURL, port, token, stderr)
	if !ok {
		return 2
	}

	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		fmt.Fprintln(stderr, "Error: --principal-id is required")
		return 2
	}
	normalizedScopes := scopes.Normalized()
	if len(normalizedScopes) == 0 {
		fmt.Fprintln(stderr, "Error: at least one --scope is required")
		return 2
	}
	if expires < 0 {
		fmt.Fprintln(stderr, "Error: --expires must not be negative")
		return 2
	}

	var expiresAt *time.Time
	if expires > 0 {
		ts := time.Now().UTC().Add(expires)
		expiresAt = &ts
	}

	if err := postEntitlementOverrideToAPI(apiBaseURL, resolvedToken, principalID, normalizedScopes, expiresAt, timeout); err != nil {
		fmt.Fprintf(stderr, "FAILED: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Granted %s to %s\n", strings.Join(normalizedScopes, ", "), principalID)
	return 0
}

func runEntitlementsRevoke(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("xg2g entitlements revoke", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var token string
	var baseURL string
	var principalID string
	var port int
	var timeout time.Duration
	var scopes stringSliceFlag

	fs.StringVar(&token, "token", "", "API token (defaults to $XG2G_API_TOKEN)")
	fs.StringVar(&baseURL, "base-url", "", "Base daemon URL, with or without /api/v3")
	fs.StringVar(&principalID, "principal-id", "", "Principal to revoke from")
	fs.IntVar(&port, "port", 8088, "Local daemon API port")
	fs.DurationVar(&timeout, "timeout", 5*time.Second, "HTTP timeout")
	fs.Var(&scopes, "scope", "Scope to revoke; repeat the flag or pass comma-separated values")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	apiBaseURL, resolvedToken, ok := resolveEntitlementsCLICommon(baseURL, port, token, stderr)
	if !ok {
		return 2
	}

	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		fmt.Fprintln(stderr, "Error: --principal-id is required")
		return 2
	}
	normalizedScopes := scopes.Normalized()
	if len(normalizedScopes) == 0 {
		fmt.Fprintln(stderr, "Error: at least one --scope is required")
		return 2
	}

	for _, scope := range normalizedScopes {
		if err := deleteEntitlementOverrideFromAPI(apiBaseURL, resolvedToken, principalID, scope, timeout); err != nil {
			fmt.Fprintf(stderr, "FAILED: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(stdout, "Revoked %s from %s\n", strings.Join(normalizedScopes, ", "), principalID)
	return 0
}

func resolveEntitlementsCLICommon(baseURL string, port int, token string, stderr io.Writer) (string, string, bool) {
	apiBaseURL, err := resolveV3APIBaseURL(baseURL, port)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return "", "", false
	}

	resolvedToken := strings.TrimSpace(token)
	if resolvedToken == "" {
		resolvedToken = strings.TrimSpace(config.ParseString("XG2G_API_TOKEN", ""))
	}
	if resolvedToken == "" {
		fmt.Fprintln(stderr, "Error: authentication required: set --token or XG2G_API_TOKEN")
		return "", "", false
	}

	return apiBaseURL, resolvedToken, true
}

func resolveV3APIBaseURL(rawBaseURL string, port int) (string, error) {
	if strings.TrimSpace(rawBaseURL) == "" {
		return fmt.Sprintf("http://localhost:%d/api/v3", port), nil
	}

	parsedURL, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil {
		return "", fmt.Errorf("invalid --base-url: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", fmt.Errorf("invalid --base-url: expected absolute http(s) URL")
	}

	trimmedPath := strings.TrimRight(parsedURL.Path, "/")
	switch {
	case trimmedPath == "":
		parsedURL.Path = "/api/v3"
	case trimmedPath == "/api/v3":
		parsedURL.Path = trimmedPath
	default:
		parsedURL.Path = trimmedPath + "/api/v3"
	}
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""

	return strings.TrimRight(parsedURL.String(), "/"), nil
}

func getEntitlementStatusFromAPI(apiBaseURL, token, principalID string, timeout time.Duration) (v3.EntitlementStatus, []byte, error) {
	endpoint, err := url.Parse(apiBaseURL + "/system/entitlements")
	if err != nil {
		return v3.EntitlementStatus{}, nil, err
	}
	if strings.TrimSpace(principalID) != "" {
		query := endpoint.Query()
		query.Set("principalId", strings.TrimSpace(principalID))
		endpoint.RawQuery = query.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return v3.EntitlementStatus{}, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	body, err := doEntitlementsRequest(req, timeout, http.StatusOK)
	if err != nil {
		return v3.EntitlementStatus{}, nil, err
	}

	var status v3.EntitlementStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return v3.EntitlementStatus{}, nil, fmt.Errorf("decode entitlement status: %w", err)
	}
	return status, body, nil
}

func postEntitlementOverrideToAPI(apiBaseURL, token, principalID string, scopes []string, expiresAt *time.Time, timeout time.Duration) error {
	principalIDCopy := principalID
	reqBody, err := json.Marshal(v3.PostSystemEntitlementOverrideJSONRequestBody{
		PrincipalId: &principalIDCopy,
		Scopes:      scopes,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return fmt.Errorf("encode override request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, apiBaseURL+"/system/entitlements/overrides", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	_, err = doEntitlementsRequest(req, timeout, http.StatusNoContent)
	return err
}

func deleteEntitlementOverrideFromAPI(apiBaseURL, token, principalID, scope string, timeout time.Duration) error {
	path := apiBaseURL + "/system/entitlements/overrides/" + url.PathEscape(principalID) + "/" + url.PathEscape(scope)
	req, err := http.NewRequest(http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	_, err = doEntitlementsRequest(req, timeout, http.StatusNoContent)
	return err
}

func doEntitlementsRequest(req *http.Request, timeout time.Duration, expectedStatus int) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read response body: %w", readErr)
	}
	if resp.StatusCode != expectedStatus {
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			detail = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, detail)
	}
	return body, nil
}

func formatCLIStringSlice(values *[]string) string {
	if values == nil || len(*values) == 0 {
		return "-"
	}
	return strings.Join(*values, ", ")
}

func derefStringOr(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return *value
}

func derefBoolOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

type stringSliceFlag []string

func (f *stringSliceFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *stringSliceFlag) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		trimmed := strings.ToLower(strings.TrimSpace(part))
		if trimmed == "" {
			continue
		}
		*f = append(*f, trimmed)
	}
	return nil
}

func (f stringSliceFlag) Normalized() []string {
	seen := make(map[string]struct{}, len(f))
	out := make([]string, 0, len(f))
	for _, item := range f {
		normalized := strings.ToLower(strings.TrimSpace(item))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}
