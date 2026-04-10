// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package health

import (
	"context"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
)

type ConnectivityContractChecker struct {
	name      string
	getConfig func() config.AppConfig
}

func NewConnectivityContractChecker(name string, getConfig func() config.AppConfig) *ConnectivityContractChecker {
	if strings.TrimSpace(name) == "" {
		name = "public_connectivity_contract"
	}
	return &ConnectivityContractChecker{
		name:      name,
		getConfig: getConfig,
	}
}

func (c *ConnectivityContractChecker) Name() string {
	return c.name
}

func (c *ConnectivityContractChecker) Type() CheckType {
	return CheckHealth | CheckReadiness
}

func (c *ConnectivityContractChecker) Check(ctx context.Context) CheckResult {
	if c == nil || c.getConfig == nil {
		return CheckResult{
			Status:  StatusHealthy,
			Message: "connectivity contract checker not configured",
		}
	}

	report, err := config.BuildConnectivityContract(c.getConfig())
	if err != nil {
		return CheckResult{
			Status:  StatusUnhealthy,
			Message: "connectivity contract evaluation failed",
			Error:   err.Error(),
		}
	}

	if report.ReadinessBlocked() {
		return CheckResult{
			Status:  StatusUnhealthy,
			Message: "public deployment contract blocked",
			Error:   summarizeConnectivityFindings(report, connectivitydomain.FindingScopeReadiness),
		}
	}

	if report.Severity == connectivitydomain.FindingSeverityWarn || report.Severity == connectivitydomain.FindingSeverityDegraded {
		return CheckResult{
			Status:  StatusDegraded,
			Message: "public deployment contract has non-blocking findings",
			Error:   summarizeConnectivityFindings(report, connectivitydomain.FindingScopeGeneral),
		}
	}

	return CheckResult{
		Status:  StatusHealthy,
		Message: "public deployment contract satisfied",
	}
}

func summarizeConnectivityFindings(report connectivitydomain.ContractReport, scopes ...connectivitydomain.FindingScope) string {
	parts := make([]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		if len(scopes) > 0 {
			matched := false
			for _, scope := range scopes {
				if hasConnectivityScope(finding.Scopes, scope) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		summary := strings.TrimSpace(finding.Summary)
		if summary == "" {
			summary = strings.TrimSpace(finding.Detail)
		}
		if summary == "" {
			summary = finding.Code
		}
		parts = append(parts, summary)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

func hasConnectivityScope(scopes []connectivitydomain.FindingScope, want connectivitydomain.FindingScope) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}
