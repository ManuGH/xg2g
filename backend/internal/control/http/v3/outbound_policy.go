// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"github.com/ManuGH/xg2g/internal/config"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

func outboundPolicyFromConfig(cfg config.AppConfig) platformnet.OutboundPolicy {
	allow := cfg.Network.Outbound.Allow
	return platformnet.OutboundPolicy{
		Enabled: cfg.Network.Outbound.Enabled,
		Allow: platformnet.OutboundAllowlist{
			Hosts:   append([]string(nil), allow.Hosts...),
			CIDRs:   append([]string(nil), allow.CIDRs...),
			Ports:   append([]int(nil), allow.Ports...),
			Schemes: append([]string(nil), allow.Schemes...),
		},
	}
}
