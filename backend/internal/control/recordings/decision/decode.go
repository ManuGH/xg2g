package decision

import (
	"encoding/json"
	"fmt"
)

// SchemaKey defines a mapping between compact and legacy root keys.
type SchemaKey struct {
	Compact string
	Legacy  string
}

// rootKeys is the single source of truth for top-level schema tags.
var rootKeys = []SchemaKey{
	{"source", "Source"},
	{"caps", "Capabilities"},
	{"policy", "Policy"},
	{"api", "APIVersion"},
	{"rid", "RequestID"},
}

// KeyRules defines schema separation rules for a nested object.
type KeyRules struct {
	// Pairs define compact->legacy synonyms that must never coexist in the same object.
	Pairs map[string]string
	// Shared are keys allowed in both schemas without triggering overlap checks.
	Shared map[string]struct{}
}

func mustNoSharedInPairs(r KeyRules) {
	// Guardrail: prevent shared keys from drifting into the synonym matrix.
	for comp, leg := range r.Pairs {
		if comp == leg {
			panic("invalid KeyRules: pair has identical keys: " + comp)
		}
		if _, ok := r.Shared[comp]; ok {
			panic("invalid KeyRules: compact key is marked shared: " + comp)
		}
		if _, ok := r.Shared[leg]; ok {
			panic("invalid KeyRules: legacy key is marked shared: " + leg)
		}
	}
}

func rejectUnknownRootKeys(raw map[string]json.RawMessage) *Problem {
	allowed := make(map[string]struct{}, len(rootKeys)*2)
	for _, pk := range rootKeys {
		allowed[pk.Compact] = struct{}{}
		allowed[pk.Legacy] = struct{}{}
	}

	for k := range raw {
		if _, ok := allowed[k]; !ok {
			return &Problem{
				Type:   "recordings/capabilities-invalid",
				Title:  "Unknown Root Key",
				Status: 400,
				Code:   string(ProblemCapabilitiesInvalid),
				Detail: fmt.Sprintf("Fail-Closed: Unknown root key %q", k),
			}
		}
	}
	return nil
}

func invalidStructureProblem(key string) *Problem {
	return &Problem{
		Type:   "recordings/capabilities-invalid",
		Title:  "Invalid Structure",
		Status: 400,
		Code:   string(ProblemCapabilitiesInvalid),
		Detail: fmt.Sprintf("Fail-Closed: %s must be an object", key),
	}
}

func parseRootObject(raw map[string]json.RawMessage, key string) (map[string]json.RawMessage, *Problem) {
	data, ok := raw[key]
	if !ok {
		return nil, nil
	}
	if !isJSONObject(data) {
		return nil, invalidStructureProblem(key)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, invalidStructureProblem(key)
	}

	return m, nil
}

func isJSONObject(data json.RawMessage) bool {
	for _, b := range data {
		switch b {
		case ' ', '\n', '\t', '\r':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}

// DecodeDecisionInput decodes raw bytes into DecisionInput with strict schema hardening.
// ADR-009.2: Enforces "Mix is 400" rule - rejects inputs containing both legacy and compact tags.
func DecodeDecisionInput(data []byte) (DecisionInput, *Problem) {
	// 1. Raw key presence scan (Condition 1: Pre-marshal detection)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return DecisionInput{}, &Problem{
			Type:   "recordings/capabilities-invalid",
			Title:  "Invalid JSON",
			Status: 400,
			Code:   string(ProblemCapabilitiesInvalid),
			Detail: err.Error(),
		}
	}

	if len(raw) == 0 {
		return DecisionInput{}, &Problem{
			Type:   "recordings/capabilities-invalid",
			Title:  "Empty Request",
			Status: 400,
			Code:   string(ProblemCapabilitiesInvalid),
			Detail: "Fail-Closed: Request object cannot be empty",
		}
	}

	// 1a. Global Schema Rejection (INV-006)
	// A request must be either entirely legacy or entirely compact at the root level.
	var hasCompact, hasLegacy bool
	for _, pk := range rootKeys {
		if hasKey(raw, pk.Compact) {
			hasCompact = true
		}
		if hasKey(raw, pk.Legacy) {
			hasLegacy = true
		}
		// Direct collision rejection within the same key pair
		if hasKey(raw, pk.Compact) && hasKey(raw, pk.Legacy) {
			return DecisionInput{}, mixedSchemaProblem(fmt.Sprintf("top-level %s vs %s", pk.Compact, pk.Legacy))
		}
	}

	if prob := rejectUnknownRootKeys(raw); prob != nil {
		return DecisionInput{}, prob
	}

	if hasCompact && hasLegacy {
		return DecisionInput{}, mixedSchemaProblem("mixture of legacy and compact root keys")
	}

	// Reject schema-less payloads (e.g. {"foo": "bar"}) to prevent silent fail-through.
	if !hasCompact && !hasLegacy {
		return DecisionInput{}, &Problem{
			Type:   "recordings/capabilities-invalid",
			Title:  "Schema Undetermined",
			Status: 400,
			Code:   string(ProblemCapabilitiesInvalid),
			Detail: "Fail-Closed: Unknown schema root keys. Request must use legacy v3.0 or compact v3.1 terminology.",
		}
	}

	// 1b. Structural Validation (root objects must be objects)
	var (
		sourceObj       map[string]json.RawMessage
		legacySourceObj map[string]json.RawMessage
		capsObj         map[string]json.RawMessage
		legacyCapsObj   map[string]json.RawMessage
		policyObj       map[string]json.RawMessage
		legacyPolicyObj map[string]json.RawMessage
		prob            *Problem
	)
	sourceObj, prob = parseRootObject(raw, "source")
	if prob != nil {
		return DecisionInput{}, prob
	}
	legacySourceObj, prob = parseRootObject(raw, "Source")
	if prob != nil {
		return DecisionInput{}, prob
	}
	capsObj, prob = parseRootObject(raw, "caps")
	if prob != nil {
		return DecisionInput{}, prob
	}
	legacyCapsObj, prob = parseRootObject(raw, "Capabilities")
	if prob != nil {
		return DecisionInput{}, prob
	}
	policyObj, prob = parseRootObject(raw, "policy")
	if prob != nil {
		return DecisionInput{}, prob
	}
	legacyPolicyObj, prob = parseRootObject(raw, "Policy")
	if prob != nil {
		return DecisionInput{}, prob
	}

	// 1c. Symmetric Nested Overlap Detection
	sourceRules := KeyRules{
		Pairs: map[string]string{
			"c":  "container",
			"v":  "videoCodec",
			"a":  "audioCodec",
			"br": "bitrateKbps",
			"w":  "width",
			"h":  "height",
		},
		Shared: map[string]struct{}{
			"fps": {},
		},
	}
	if err := checkSymmetricOverlapRules("source", sourceObj, "Source", legacySourceObj, sourceRules); err != nil {
		return DecisionInput{}, mixedSchemaProblem(err.Error())
	}

	capsRules := KeyRules{
		Pairs: map[string]string{
			"v":   "version",
			"c":   "containers",
			"vc":  "videoCodecs",
			"ac":  "audioCodecs",
			"hls": "supportsHls",
			"rng": "supportsRange",
			"dev": "deviceType",
			"mv":  "maxVideo",
		},
		Shared: map[string]struct{}{},
	}
	if err := checkSymmetricOverlapRules("caps", capsObj, "Capabilities", legacyCapsObj, capsRules); err != nil {
		return DecisionInput{}, mixedSchemaProblem(err.Error())
	}

	policyRules := KeyRules{
		Pairs: map[string]string{
			"tx": "allowTranscode",
		},
		Shared: map[string]struct{}{},
	}
	if err := checkSymmetricOverlapRules("policy", policyObj, "Policy", legacyPolicyObj, policyRules); err != nil {
		return DecisionInput{}, mixedSchemaProblem(err.Error())
	}

	// 2. Perform Presence-aware Unmarshal
	type legacySource struct {
		Container   string  `json:"container"`
		VideoCodec  string  `json:"videoCodec"`
		AudioCodec  string  `json:"audioCodec"`
		BitrateKbps int     `json:"bitrateKbps"`
		Width       int     `json:"width"`
		Height      int     `json:"height"`
		FPS         float64 `json:"fps"`
	}
	type legacyCaps struct {
		Version       int                 `json:"version"`
		Containers    []string            `json:"containers"`
		VideoCodecs   []string            `json:"videoCodecs"`
		AudioCodecs   []string            `json:"audioCodecs"`
		SupportsHLS   bool                `json:"supportsHls"`
		SupportsRange *bool               `json:"supportsRange"`
		MaxVideo      *MaxVideoDimensions `json:"maxVideo"`
		DeviceType    string              `json:"deviceType"`
	}
	type legacyPolicy struct {
		AllowTranscode bool `json:"allowTranscode"`
	}

	var aux struct {
		Source       Source       `json:"source"`
		Capabilities Capabilities `json:"caps"`
		Policy       Policy       `json:"policy"`
		APIVersion   string       `json:"api"`
		RequestID    string       `json:"rid"`

		LegacySource       legacySource `json:"Source"`
		LegacyCapabilities legacyCaps   `json:"Capabilities"`
		LegacyPolicy       legacyPolicy `json:"Policy"`
		LegacyAPIVersion   string       `json:"APIVersion"`
		LegacyRequestID    string       `json:"RequestID"`
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return DecisionInput{}, &Problem{
			Type:   "recordings/capabilities-invalid",
			Title:  "Decode Failure",
			Status: 400,
			Code:   string(ProblemCapabilitiesInvalid),
			Detail: err.Error(),
		}
	}

	// 3. Presence-aware Selection Logic
	input := DecisionInput{}

	if hasKey(raw, "source") {
		input.Source = aux.Source
	} else if hasKey(raw, "Source") {
		input.Source = Source{
			Container:   aux.LegacySource.Container,
			VideoCodec:  aux.LegacySource.VideoCodec,
			AudioCodec:  aux.LegacySource.AudioCodec,
			BitrateKbps: aux.LegacySource.BitrateKbps,
			Width:       aux.LegacySource.Width,
			Height:      aux.LegacySource.Height,
			FPS:         aux.LegacySource.FPS,
		}
	}

	if hasKey(raw, "caps") {
		input.Capabilities = aux.Capabilities
	} else if hasKey(raw, "Capabilities") {
		input.Capabilities = Capabilities{
			Version:       aux.LegacyCapabilities.Version,
			Containers:    aux.LegacyCapabilities.Containers,
			VideoCodecs:   aux.LegacyCapabilities.VideoCodecs,
			AudioCodecs:   aux.LegacyCapabilities.AudioCodecs,
			SupportsHLS:   aux.LegacyCapabilities.SupportsHLS,
			SupportsRange: aux.LegacyCapabilities.SupportsRange,
			MaxVideo:      aux.LegacyCapabilities.MaxVideo,
			DeviceType:    aux.LegacyCapabilities.DeviceType,
		}
	}

	if hasKey(raw, "policy") {
		input.Policy = aux.Policy
	} else if hasKey(raw, "Policy") {
		input.Policy = Policy{AllowTranscode: aux.LegacyPolicy.AllowTranscode}
	}

	if hasKey(raw, "api") {
		input.APIVersion = aux.APIVersion
	} else if hasKey(raw, "APIVersion") {
		input.APIVersion = aux.LegacyAPIVersion
	}

	if hasKey(raw, "rid") {
		input.RequestID = aux.RequestID
	} else if hasKey(raw, "RequestID") {
		input.RequestID = aux.LegacyRequestID
	}

	// 4. Traceability & Semantic Validation Boundary
	input = NormalizeInput(input)
	if prob := validateInput(input); prob != nil {
		return input, prob
	}

	return input, nil
}

func hasKey(m map[string]json.RawMessage, key string) bool {
	_, ok := m[key]
	return ok
}

// checkSymmetricOverlapRules ensures strict separation between compact and legacy sub-field terminology.
func checkSymmetricOverlapRules(compactRoot string, compact map[string]json.RawMessage, legacyRoot string, legacy map[string]json.RawMessage, rules KeyRules) error {
	mustNoSharedInPairs(rules)

	// Compact root must not contain legacy-only keys.
	if compact != nil {
		for _, legacyKey := range rules.Pairs {
			if _, shared := rules.Shared[legacyKey]; shared {
				continue
			}
			if hasKey(compact, legacyKey) {
				return fmt.Errorf("nested %s: contains legacy key %s", compactRoot, legacyKey)
			}
		}
	}

	// Legacy root must not contain compact-only keys.
	if legacy != nil {
		for compactKey := range rules.Pairs {
			if _, shared := rules.Shared[compactKey]; shared {
				continue
			}
			if hasKey(legacy, compactKey) {
				return fmt.Errorf("nested %s: contains compact key %s", legacyRoot, compactKey)
			}
		}
	}

	return nil
}

func mixedSchemaProblem(detail string) *Problem {
	return &Problem{
		Type:   "recordings/capabilities-invalid",
		Title:  "Mixed Schema Detected",
		Status: 400,
		Code:   string(ProblemCapabilitiesInvalid),
		Detail: fmt.Sprintf("Fail-Closed: Mixed schema detected. %s", detail),
	}
}

// GetSchemaType returns the detected schema flavor for telemetry.
// Mechanically linked to rootKeys structure to prevent long-term telemetry drift.
func GetSchemaType(data []byte) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "unknown"
	}

	var hasCompact, hasLegacy bool
	for _, pk := range rootKeys {
		if hasKey(raw, pk.Compact) {
			hasCompact = true
		}
		if hasKey(raw, pk.Legacy) {
			hasLegacy = true
		}
	}

	if hasCompact && hasLegacy {
		return "mixed"
	}
	if hasCompact {
		return "compact"
	}
	if hasLegacy {
		return "legacy"
	}
	return "unknown"
}
