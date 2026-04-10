// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerPairingRoutes(register routeRegistrar, handler pairingRoutes) {
	register.add(http.MethodPost, "/pairing/start", "StartPairing", handler.StartPairing)
	register.add(http.MethodPost, "/pairing/{pairingId}/status", "GetPairingStatus", handler.GetPairingStatus)
	register.add(http.MethodPost, "/pairing/{pairingId}/approve", "ApprovePairing", handler.ApprovePairing)
	register.add(http.MethodPost, "/pairing/{pairingId}/exchange", "ExchangePairing", handler.ExchangePairing)
}
