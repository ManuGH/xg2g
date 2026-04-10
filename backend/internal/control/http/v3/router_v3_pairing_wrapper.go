// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

type pairingRoutesWrapper struct {
	Handler            pairingRoutes
	HandlerMiddlewares []MiddlewareFunc
}

func (w *pairingRoutesWrapper) StartPairing(rw http.ResponseWriter, r *http.Request) {
	handler := http.Handler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.Handler.StartPairing(rw, r)
	}))
	for _, middleware := range w.HandlerMiddlewares {
		handler = middleware(handler)
	}
	handler.ServeHTTP(rw, r)
}

func (w *pairingRoutesWrapper) GetPairingStatus(rw http.ResponseWriter, r *http.Request) {
	handler := http.Handler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.Handler.GetPairingStatus(rw, r)
	}))
	for _, middleware := range w.HandlerMiddlewares {
		handler = middleware(handler)
	}
	handler.ServeHTTP(rw, r)
}

func (w *pairingRoutesWrapper) ApprovePairing(rw http.ResponseWriter, r *http.Request) {
	handler := http.Handler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.Handler.ApprovePairing(rw, r)
	}))
	for _, middleware := range w.HandlerMiddlewares {
		handler = middleware(handler)
	}
	handler.ServeHTTP(rw, r)
}

func (w *pairingRoutesWrapper) ExchangePairing(rw http.ResponseWriter, r *http.Request) {
	handler := http.Handler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.Handler.ExchangePairing(rw, r)
	}))
	for _, middleware := range w.HandlerMiddlewares {
		handler = middleware(handler)
	}
	handler.ServeHTTP(rw, r)
}

type noopPairingRoutes struct{}

func (noopPairingRoutes) StartPairing(http.ResponseWriter, *http.Request)     {}
func (noopPairingRoutes) GetPairingStatus(http.ResponseWriter, *http.Request) {}
func (noopPairingRoutes) ApprovePairing(http.ResponseWriter, *http.Request)   {}
func (noopPairingRoutes) ExchangePairing(http.ResponseWriter, *http.Request)  {}
