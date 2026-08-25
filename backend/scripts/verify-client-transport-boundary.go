//go:build ignore

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

// verify-client-transport-boundary holds the client platforms to one transport
// entry point each.
//
// The rule it enforces is the one the architecture rests on: a feature module
// cannot reach the network, cannot name an API path, cannot set an auth header,
// cannot decide what this client calls itself, and cannot declare a wire type.
// All five belong to that platform's transport zone, which is the only place
// the check permits them.
//
// # Why this is not grep
//
// Three things separate it from a pattern scan, and each of them is a class of
// false result the scan produces:
//
//  1. Comments are stripped before matching. A doc comment explaining why a
//     layer must not build URLs contains the very strings the rule forbids, and
//     a scanner that flags it teaches people to phrase comments defensively.
//
//  2. String literals are extracted and matched as values. `"/api/v3/..."` in
//     source is a violation; the same characters inside an identifier are not.
//
//  3. Wire type names come from the generator manifest, not from a name
//     pattern. A hand-written `StartPairingResponse` in feature code is a
//     duplicate of a generated contract type because the manifest says that
//     name is a wire schema — not because it happens to end in "Response".
//
// # Zones
//
// Each platform declares its transport zone and its generated zone. Everything
// else is feature code. The zones are paths rather than an allowlist of files
// on purpose: moving a file into the transport zone is a visible, reviewable
// act, whereas adding a line to an exception list is not.
package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const manifestPath = "api/contract.manifest.json"

// platform is one client codebase with its own transport boundary.
type platform struct {
	name string
	// enforced fails the build on this platform's findings. A platform that is
	// not yet enforced still reports, so the number is visible and has to come
	// down rather than being forgotten. It is not an allowlist: it cannot
	// exempt a file, only a whole platform, and it is one line to flip.
	enforced bool
	// roots are scanned for source files with one of exts.
	roots []string
	exts  []string
	// transportZones may do networking, name paths, set headers and declare
	// wire types. generatedZones are generator output and are never checked.
	transportZones []string
	generatedZones []string
	// skipDirs are never scanned at all: build output and dependency trees.
	skipDirs []string
	// testSuffixes mark test sources. Tests may name a URL — they stand up
	// fixtures and servers — but they may not declare wire types.
	testSuffixes []string
	// networkSymbols are the platform's networking APIs.
	networkSymbols []string
	// lineComment starts a comment that runs to end of line.
	lineComment string
}

var platforms = []platform{
	{
		name:           "ios",
		enforced:       true,
		roots:          []string{"../ios/Xg2g", "../ios/Xg2gTests"},
		exts:           []string{".swift"},
		transportZones: []string{"ios/Xg2g/Transport/"},
		generatedZones: []string{"ios/Xg2g/Generated/"},
		skipDirs:       []string{".build", "DerivedData", "Pods"},
		testSuffixes:   []string{"Tests.swift"},
		networkSymbols: []string{
			"URLSession", "URLRequest", "NWConnection", "NWBrowser",
			"CFHTTPMessage", "URLDownload",
		},
		lineComment: "//",
	},
	{
		// Enforced: the Android client reaches the network only through
		// android/transport, and decodes the wire only through the generated
		// contract. Both were true once before and were lost in a branch
		// rewrite, which is the argument for the gate rather than against it.
		name:           "android",
		enforced:       true,
		roots:          []string{"../android/app/src"},
		exts:           []string{".kt"},
		transportZones: []string{"android/app/src/main/java/io/github/manugh/xg2g/android/transport/"},
		generatedZones: []string{"android/app/src/main/java/io/github/manugh/xg2g/android/contract/"},
		skipDirs:       []string{"build", "generated"},
		testSuffixes:   []string{"Test.kt", "Tests.kt"},
		networkSymbols: []string{
			"okhttp3", "OkHttpClient", "HttpURLConnection", "java.net.URL",
			"Retrofit", "Ktor", "WebSocketListener",
		},
		lineComment: "//",
	},
}

// forbiddenLiteralSubstrings are values feature code must never carry. Matched
// against extracted string literals, so an identifier that happens to contain
// one of them is not a hit.
var forbiddenLiteralSubstrings = []struct {
	needle string
	reason string
}{
	{"/api/v3", "API path fragment"},
	{"api/v3/", "API path fragment"},
	{"http://", "absolute URL"},
	{"https://", "absolute URL"},
	{"Authorization", "auth header name"},
	{"DPoP ", "auth scheme literal"},
	{"client_family", "client identity key"},
	{"clientFamilyFallback", "client identity key"},
	{"ios_safari_native", "client identity value"},
	{"android_tv_native", "client identity value"},
	{"xg2g_session", "session cookie name"},
}

// hostLikeLiteralPrefixes catch a bare host or address written without a
// scheme, which the absolute-URL rule above would miss.
var hostLikeLiteralPrefixes = []string{
	"10.", "192.168.", "172.16.", "127.0.0.1",
}

type finding struct {
	platform string
	file     string
	line     int
	rule     string
	detail   string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "verify-client-transport-boundary:", err)
		os.Exit(1)
	}
}

func run() error {
	wireTypes, err := loadWireTypeNames()
	if err != nil {
		return err
	}

	var findings []finding
	scanned := 0

	for _, p := range platforms {
		files, err := collect(p)
		if err != nil {
			return err
		}
		scanned += len(files)
		for _, file := range files {
			rel := repoRelative(file)
			if inAnyZone(rel, p.generatedZones) {
				continue
			}
			source, err := os.ReadFile(file) // #nosec G304 -- paths come from walking the declared roots
			if err != nil {
				return fmt.Errorf("read %s: %w", rel, err)
			}
			findings = append(findings, inspect(p, rel, string(source), wireTypes)...)
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].file != findings[j].file {
			return findings[i].file < findings[j].file
		}
		return findings[i].line < findings[j].line
	})

	enforced := map[string]bool{}
	for _, p := range platforms {
		enforced[p.name] = p.enforced
	}

	var blocking []finding
	pending := map[string]int{}
	for _, f := range findings {
		if enforced[f.platform] {
			blocking = append(blocking, f)
			continue
		}
		pending[f.platform]++
	}

	for _, p := range platforms {
		if p.enforced || pending[p.name] == 0 {
			continue
		}
		fmt.Printf("⚠️  %s: %d boundary finding(s), not yet enforced\n", p.name, pending[p.name])
	}

	findings = blocking
	if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "❌ client transport boundary violated (%d):\n\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(os.Stderr, "  %s:%d [%s] %s\n", f.file, f.line, f.rule, f.detail)
		}
		fmt.Fprint(os.Stderr, `
Feature code reaches the backend through its platform's transport module and
nowhere else. Networking, URL construction, auth headers, client identity and
wire types live there; move the code rather than the rule.

Wire types are generated from backend/api/openapi.yaml — declare the schema and
regenerate instead of writing the type by hand.
`)
		return fmt.Errorf("%d violation(s)", len(findings))
	}

	for _, p := range platforms {
		if p.enforced {
			fmt.Printf("✅ %s: transport boundary enforced and clean\n", p.name)
		}
	}
	fmt.Printf("scanned %d source files\n", scanned)
	return nil
}

// loadWireTypeNames reads the generated contract's schema closure. These are the
// names that exist as generated types on the native platforms, so a hand-written
// declaration of one is a duplicate by definition.
func loadWireTypeNames() (map[string]bool, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s (run: make generate-contract-manifest): %w", manifestPath, err)
	}
	var manifest struct {
		NativeContract struct {
			Closure []string `json:"closure"`
		} `json:"nativeContract"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("decode %s: %w", manifestPath, err)
	}
	if len(manifest.NativeContract.Closure) == 0 {
		return nil, fmt.Errorf("%s declares no native contract schemas", manifestPath)
	}
	names := make(map[string]bool, len(manifest.NativeContract.Closure))
	for _, name := range manifest.NativeContract.Closure {
		names[name] = true
	}
	return names, nil
}

func collect(p platform) ([]string, error) {
	var files []string
	for _, root := range p.roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				for _, skip := range p.skipDirs {
					if d.Name() == skip {
						return fs.SkipDir
					}
				}
				return nil
			}
			for _, ext := range p.exts {
				if strings.HasSuffix(d.Name(), ext) {
					files = append(files, path)
					return nil
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

// repoRelative turns a backend-relative scan path into a repository-relative one
// so zones and messages read the way the repository is laid out.
func repoRelative(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.TrimPrefix(clean, "../")
}

func inAnyZone(rel string, zones []string) bool {
	for _, zone := range zones {
		if strings.HasPrefix(rel, zone) {
			return true
		}
	}
	return false
}

func isTest(p platform, rel string) bool {
	for _, suffix := range p.testSuffixes {
		if strings.HasSuffix(rel, suffix) {
			return true
		}
	}
	return strings.Contains(rel, "/test/") || strings.Contains(rel, "/androidTest/")
}

func inspect(p platform, rel, source string, wireTypes map[string]bool) []finding {
	transport := inAnyZone(rel, p.transportZones)
	test := isTest(p, rel)

	var out []finding
	for i, line := range strings.Split(source, "\n") {
		lineNo := i + 1
		code, literals := splitCodeAndLiterals(line, p.lineComment)
		if strings.TrimSpace(code) == "" && len(literals) == 0 {
			continue
		}

		// Tests are allowed to reach the network: they exercise the transport
		// and stand up fixture servers. Whether a given test *should* be
		// hermetic is a different question, answered by the test gates rather
		// than by an architecture boundary.
		if !transport && !test {
			for _, symbol := range p.networkSymbols {
				if strings.Contains(code, symbol) {
					out = append(out, finding{p.name, rel, lineNo, "network-api",
						fmt.Sprintf("%q outside the transport zone", symbol)})
					break
				}
			}
		}

		// Wire types are generated. Declaring one by hand anywhere outside the
		// generated zone recreates the drift the generator exists to prevent,
		// so this rule holds in the transport zone and in tests too.
		if name, ok := declaredTypeName(code); ok && wireTypes[name] {
			out = append(out, finding{p.name, rel, lineNo, "wire-type",
				fmt.Sprintf("%q is a generated contract type; use the generated one", name)})
		}

		if transport || test {
			continue
		}
		for _, literal := range literals {
			for _, forbidden := range forbiddenLiteralSubstrings {
				if strings.Contains(literal, forbidden.needle) {
					out = append(out, finding{p.name, rel, lineNo, "literal",
						fmt.Sprintf("%s in %q", forbidden.reason, truncate(literal))})
				}
			}
			for _, prefix := range hostLikeLiteralPrefixes {
				if strings.HasPrefix(literal, prefix) {
					out = append(out, finding{p.name, rel, lineNo, "literal",
						fmt.Sprintf("host address in %q", truncate(literal))})
				}
			}
		}
	}
	return out
}

// declaredTypeName returns the name a line declares, if it declares one.
// Covers the Swift and Kotlin spellings the native clients use.
func declaredTypeName(code string) (string, bool) {
	trimmed := strings.TrimSpace(code)
	for _, keyword := range []string{
		"struct ", "class ", "enum ", "data class ", "interface ", "typealias ",
	} {
		idx := strings.Index(trimmed, keyword)
		if idx < 0 {
			continue
		}
		// Only a declaration, not a mention: everything before the keyword must
		// be declaration modifiers.
		if !onlyModifiers(trimmed[:idx]) {
			continue
		}
		rest := trimmed[idx+len(keyword):]
		name := strings.TrimSpace(rest)
		if cut := strings.IndexAny(name, " :<({\t"); cut >= 0 {
			name = name[:cut]
		}
		if name == "" {
			continue
		}
		return name, true
	}
	return "", false
}

var declarationModifiers = map[string]bool{
	"public": true, "private": true, "internal": true, "fileprivate": true,
	"open": true, "final": true, "sealed": true, "abstract": true,
	"static": true, "data": true, "value": true, "actor": true,
	"@objc": true, "override": true, "protected": true, "inner": true,
}

func onlyModifiers(prefix string) bool {
	for _, word := range strings.Fields(prefix) {
		if !declarationModifiers[strings.TrimPrefix(word, "@")] && !declarationModifiers[word] {
			return false
		}
	}
	return true
}

// splitCodeAndLiterals removes comments and separates string literals from the
// rest of the line. Both Swift and Kotlin use `"` and `//`, and both are scanned
// with escape handling so a quote inside a literal does not end it early.
func splitCodeAndLiterals(line, lineComment string) (string, []string) {
	var code strings.Builder
	var literals []string
	var current strings.Builder

	inString := false
	escaped := false
	runes := []rune(line)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if inString {
			if escaped {
				escaped = false
				current.WriteRune(ch)
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
				literals = append(literals, current.String())
				current.Reset()
			default:
				current.WriteRune(ch)
			}
			continue
		}

		if ch == '"' {
			inString = true
			continue
		}
		if strings.HasPrefix(string(runes[i:]), lineComment) {
			break
		}
		code.WriteRune(ch)
	}

	// An unterminated literal means a multi-line string; treat what we have as
	// a literal rather than dropping it.
	if inString && current.Len() > 0 {
		literals = append(literals, current.String())
	}
	return code.String(), literals
}

func truncate(value string) string {
	const limit = 60
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
