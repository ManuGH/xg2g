package openwebif

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	timerUpdateTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_openwebif_timer_update_total",
		Help: "Outcome of timer update attempts (native, fallback, partial, terminal)",
	}, []string{
		"result",        // success|fallback_success|partial_failure|terminal_failure
		"reason",        // none|identity_mismatch|param_rejection|unsupported
		"native_flavor", // none|A|B
		"cap_supported", // true|false
		"cap_flavor",    // unknown|A|B
	})
)

func observeTimerUpdate(result, reason string, nativeFlavor TimerChangeFlavor, cap TimerChangeCap) {
	nf := "none"
	if nativeFlavor == TimerChangeFlavorA {
		nf = "A"
	} else if nativeFlavor == TimerChangeFlavorB {
		nf = "B"
	}

	cf := "unknown"
	switch cap.Flavor {
	case TimerChangeFlavorA:
		cf = "A"
	case TimerChangeFlavorB:
		cf = "B"
	}

	timerUpdateTotal.WithLabelValues(
		result,
		reason,
		nf,
		strconv.FormatBool(cap.Supported),
		cf,
	).Inc()
}
