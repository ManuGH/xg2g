// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"os"
	"sort"

	"github.com/ManuGH/xg2g/internal/log"
)

type RemovedEnvKey struct {
	Key     string
	Message string
}

var removedEnvKeys = []RemovedEnvKey{
	{
		Key:     "XG2G_HTTP_ENABLE_HTTP2",
		Message: "HTTP/2 toggle removed; HTTP/2 is always enabled. Setting is ignored.",
	},
	{
		Key:     "XG2G_RESUME_BACKEND",
		Message: "Resume backend selection removed; SQLite is the durable truth. Setting is ignored.",
	},
	{
		Key:     "XG2G_SESSION_BACKEND",
		Message: "Session backend selection removed; SQLite is the durable truth. Setting is ignored.",
	},
	{
		Key:     "XG2G_CAPABILITIES_BACKEND",
		Message: "Capabilities backend selection removed; SQLite is the durable truth. Setting is ignored.",
	},
}

func FindActiveRemovedEnvKeysWithLookup(lookup envLookupFunc) []RemovedEnvKey {
	if lookup == nil {
		lookup = os.LookupEnv
	}

	out := make([]RemovedEnvKey, 0, len(removedEnvKeys))
	for _, k := range removedEnvKeys {
		if _, ok := lookup(k.Key); ok {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func FindActiveRemovedEnvKeys() []RemovedEnvKey {
	return FindActiveRemovedEnvKeysWithLookup(os.LookupEnv)
}

func WarnRemovedEnvKeysWithLookup(lookup envLookupFunc) {
	active := FindActiveRemovedEnvKeysWithLookup(lookup)
	if len(active) == 0 {
		return
	}

	logger := log.WithComponent("config")
	for _, k := range active {
		logger.Warn().
			Str("key", k.Key).
			Msgf("REMOVED env var is set: %s", k.Message)
	}
}

func WarnRemovedEnvKeys() {
	WarnRemovedEnvKeysWithLookup(os.LookupEnv)
}
