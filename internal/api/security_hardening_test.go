package api

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestReloadRequiresRestart_Hardening(t *testing.T) {
	baseCfg := config.AppConfig{
		APIToken:       "A",
		APITokenScopes: []string{"v3:write"},
		APITokens: []config.ScopedToken{
			{Token: "T1", Scopes: []string{"v3:read"}},
		},
	}

	tests := []struct {
		name     string
		mod      func(*config.AppConfig)
		mustRest bool
	}{
		{
			name:     "NoChange",
			mod:      func(c *config.AppConfig) {},
			mustRest: false,
		},
		{
			name: "TokenRotation_SameLen",
			mod: func(c *config.AppConfig) {
				c.APITokens[0].Token = "T2" // content changed
			},
			mustRest: true,
		},
		{
			name: "ScopeRotation_SameLen",
			mod: func(c *config.AppConfig) {
				c.APITokens[0].Scopes[0] = "v3:admin" // content changed
			},
			mustRest: true,
		},
		{
			name: "TopLevelScopeRotation_SameLen",
			mod: func(c *config.AppConfig) {
				c.APITokenScopes[0] = "v3:admin" // content changed
			},
			mustRest: true,
		},
		{
			name: "NewToken",
			mod: func(c *config.AppConfig) {
				c.APITokens = append(c.APITokens, config.ScopedToken{Token: "T2", Scopes: []string{"v3:read"}})
			},
			mustRest: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newCfg := baseCfg
			// We need deep copy for base config slices to simulate real world,
			// otherwise we mutate baseCfg across tests in memory.
			// Helper to deep copy struct manually for test
			newCfg.APITokenScopes = append([]string(nil), baseCfg.APITokenScopes...)
			newCfg.APITokens = make([]config.ScopedToken, len(baseCfg.APITokens))
			for i, v := range baseCfg.APITokens {
				newCfg.APITokens[i] = v
				newCfg.APITokens[i].Scopes = append([]string(nil), v.Scopes...)
			}

			tt.mod(&newCfg)

			// reloadRequiresRestart is unexported. We are in package api so we can call it.
			got := reloadRequiresRestart(baseCfg, newCfg)
			assert.Equal(t, tt.mustRest, got)
		})
	}
}
