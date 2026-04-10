// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

type deviceAuthRoutesWrapper struct {
	Handler            deviceAuthRoutes
	HandlerMiddlewares []MiddlewareFunc
}

func (w *deviceAuthRoutesWrapper) CreateDeviceSession(rw http.ResponseWriter, r *http.Request) {
	handler := http.Handler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.Handler.CreateDeviceSession(rw, r)
	}))
	for _, middleware := range w.HandlerMiddlewares {
		handler = middleware(handler)
	}
	handler.ServeHTTP(rw, r)
}

func (w *deviceAuthRoutesWrapper) CreateWebBootstrap(rw http.ResponseWriter, r *http.Request) {
	handler := http.Handler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.Handler.CreateWebBootstrap(rw, r)
	}))
	for _, middleware := range w.HandlerMiddlewares {
		handler = middleware(handler)
	}
	handler.ServeHTTP(rw, r)
}

func (w *deviceAuthRoutesWrapper) CompleteWebBootstrap(rw http.ResponseWriter, r *http.Request) {
	handler := http.Handler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.Handler.CompleteWebBootstrap(rw, r)
	}))
	for _, middleware := range w.HandlerMiddlewares {
		handler = middleware(handler)
	}
	handler.ServeHTTP(rw, r)
}

type noopDeviceAuthRoutes struct{}

func (noopDeviceAuthRoutes) CreateDeviceSession(http.ResponseWriter, *http.Request)  {}
func (noopDeviceAuthRoutes) CreateWebBootstrap(http.ResponseWriter, *http.Request)   {}
func (noopDeviceAuthRoutes) CompleteWebBootstrap(http.ResponseWriter, *http.Request) {}
