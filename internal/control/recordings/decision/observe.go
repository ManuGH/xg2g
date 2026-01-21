package decision

import (
	"context"

	"github.com/ManuGH/xg2g/internal/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Observability Keys (Frozen)
const (
	AttrMode          = "xg2g.decision.mode"
	AttrProtocol      = "xg2g.decision.protocol"
	AttrReasons       = "xg2g.decision.reasons"
	AttrReasonPrimary = "xg2g.decision.reason_primary"
	AttrRequestID     = "xg2g.requestId"
)

// Frozen Whitelist for Enforcement
var allowedAttributes = map[string]bool{
	AttrMode:          true,
	AttrProtocol:      true,
	AttrReasons:       true,
	AttrReasonPrimary: true,
	AttrRequestID:     true,
}

// EmitDecisionObs enforces the Observability Decision Contract.
// It sets attributes on the current span and records metrics.
// It performs STRICT Attribute Whitelisting.
// P8-5 Refactor: Consumes Single Mapping Truth (mapping.go).
func EmitDecisionObs(ctx context.Context, input Input, dec *Decision, prob *Problem) {
	span := trace.SpanFromContext(ctx)

	// Runtime Provider Lookup (No Init-Time Rebinding)
	meter := otel.GetMeterProvider().Meter("xg2g.decision")

	// 1. Determine Values via Single Mapping Truth
	mode := "deny" // Default for Problem fallback
	if dec != nil {
		mode = string(dec.Mode)
	}

	protocol := ProtocolFrom(dec)
	reasonPrimary := ReasonPrimaryFrom(dec, prob)
	reasons := ReasonsAsStrings(dec, prob)

	// Metric Emission
	if prob != nil {
		// Problem Path
		problemTotal, _ := meter.Int64Counter("xg2g_decision_problem_total", metric.WithDescription("Total decision problems"))
		problemTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("code", string(prob.Code)),
		))
	} else {
		// Decision Path
		decisionTotal, _ := meter.Int64Counter("xg2g_decision_total", metric.WithDescription("Total decisions made"))
		decisionTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("mode", mode),
			attribute.String("protocol", protocol),
			attribute.String("reason_primary", reasonPrimary),
		))
	}

	// Prepare final attributes list
	attrs := []attribute.KeyValue{
		attribute.String(AttrMode, mode),
		attribute.String(AttrProtocol, protocol),
		attribute.StringSlice(AttrReasons, reasons),
		attribute.String(AttrReasonPrimary, reasonPrimary),
		attribute.String(AttrRequestID, input.RequestID),
	}

	// STRICT Whitelist Enforcement
	for _, kv := range attrs {
		if !allowedAttributes[string(kv.Key)] {
			// Stop-the-line violation!
			// P0-Panic-Fix: Log instead of crashing
			// We can't use 'log' package here as it might cycle? No, log is imported as 'log "github.com/ManuGH/xg2g/internal/log"' usually?
			// Wait, check imports in observe.go.
			// It imports:
			// "context"
			// "fmt"
			// "go.opentelemetry.io/otel" ...
			// It DOES NOT import internal/log.
			// I need to add import "github.com/ManuGH/xg2g/internal/log"
			// And then use log.L().Error()...
			// Or just fmt.Printf if I want to avoid import cycles?
			// Ideally use structured log.
			// I will add the import.
			log.L().Error().Str("key", string(kv.Key)).Msg("CRITICAL: Observability Invariant Violation: Attribute not in whitelist")
			return
		}
	}

	// Apply to Span
	span.SetAttributes(attrs...)
}

// StartDecisionSpan wraps span creation logic.
func StartDecisionSpan(ctx context.Context) (context.Context, trace.Span) {
	// Runtime Tracer Lookup
	tracer := otel.GetTracerProvider().Tracer("xg2g.decision")
	return tracer.Start(ctx, "xg2g.decision")
}
