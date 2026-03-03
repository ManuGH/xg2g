// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"time"
)

// enigma2FilePatch is a normalized update set for AppConfig.Enigma2.
// It is intentionally pointer-based so adapters can express "field not set".
type enigma2FilePatch struct {
	BaseURL               *string
	Username              *string
	Password              *string
	Timeout               *time.Duration
	ResponseHeaderTimeout *time.Duration
	Retries               *int
	Backoff               *time.Duration
	MaxBackoff            *time.Duration
	RateLimit             *int
	RateBurst             *int
	UserAgent             *string
	AnalyzeDuration       *string
	ProbeSize             *string
	StreamPort            *int
	UseWebIFStreams       *bool
	FallbackTo8001        *bool
}

func applyEnigma2FilePatch(dst *AppConfig, patch enigma2FilePatch) {
	if patch.BaseURL != nil {
		dst.Enigma2.BaseURL = *patch.BaseURL
	}
	if patch.Username != nil {
		dst.Enigma2.Username = *patch.Username
	}
	if patch.Password != nil {
		dst.Enigma2.Password = *patch.Password
	}
	if patch.Timeout != nil {
		dst.Enigma2.Timeout = *patch.Timeout
	}
	if patch.ResponseHeaderTimeout != nil {
		dst.Enigma2.ResponseHeaderTimeout = *patch.ResponseHeaderTimeout
	}
	if patch.Retries != nil {
		dst.Enigma2.Retries = *patch.Retries
	}
	if patch.Backoff != nil {
		dst.Enigma2.Backoff = *patch.Backoff
	}
	if patch.MaxBackoff != nil {
		dst.Enigma2.MaxBackoff = *patch.MaxBackoff
	}
	if patch.RateLimit != nil {
		dst.Enigma2.RateLimit = *patch.RateLimit
	}
	if patch.RateBurst != nil {
		dst.Enigma2.RateBurst = *patch.RateBurst
	}
	if patch.UserAgent != nil {
		dst.Enigma2.UserAgent = *patch.UserAgent
	}
	if patch.AnalyzeDuration != nil {
		dst.Enigma2.AnalyzeDuration = *patch.AnalyzeDuration
	}
	if patch.ProbeSize != nil {
		dst.Enigma2.ProbeSize = *patch.ProbeSize
	}
	if patch.StreamPort != nil {
		dst.Enigma2.StreamPort = *patch.StreamPort
	}
	if patch.UseWebIFStreams != nil {
		dst.Enigma2.UseWebIFStreams = *patch.UseWebIFStreams
	}
	if patch.FallbackTo8001 != nil {
		dst.Enigma2.FallbackTo8001 = *patch.FallbackTo8001
	}
}

func enigma2FilePatchFromOpenWebIF(src OpenWebIFConfig) (enigma2FilePatch, error) {
	var patch enigma2FilePatch

	if src.BaseURL != "" {
		v := expandEnv(src.BaseURL)
		patch.BaseURL = &v
	}
	if src.Username != "" {
		v := expandEnv(src.Username)
		patch.Username = &v
	}
	if src.Password != "" {
		v := expandEnv(src.Password)
		patch.Password = &v
	}
	if src.StreamPort > 0 {
		v := src.StreamPort
		patch.StreamPort = &v
	}
	if src.UseWebIF != nil {
		v := *src.UseWebIF
		patch.UseWebIFStreams = &v
	}
	if src.Timeout != "" {
		d, err := time.ParseDuration(src.Timeout)
		if err != nil {
			return enigma2FilePatch{}, fmt.Errorf("invalid openWebIF.timeout: %w", err)
		}
		patch.Timeout = &d
	}
	if src.Backoff != "" {
		d, err := time.ParseDuration(src.Backoff)
		if err != nil {
			return enigma2FilePatch{}, fmt.Errorf("invalid openWebIF.backoff: %w", err)
		}
		patch.Backoff = &d
	}
	if src.MaxBackoff != "" {
		d, err := time.ParseDuration(src.MaxBackoff)
		if err != nil {
			return enigma2FilePatch{}, fmt.Errorf("invalid openWebIF.maxBackoff: %w", err)
		}
		patch.MaxBackoff = &d
	}
	if src.Retries > 0 {
		v := src.Retries
		patch.Retries = &v
	}

	return patch, nil
}

func enigma2FilePatchFromEnigma2(src Enigma2Config) (enigma2FilePatch, error) {
	var patch enigma2FilePatch

	if src.BaseURL != "" {
		v := expandEnv(src.BaseURL)
		patch.BaseURL = &v
	}
	if src.Username != "" {
		v := expandEnv(src.Username)
		patch.Username = &v
	}
	if src.Password != "" {
		v := expandEnv(src.Password)
		patch.Password = &v
	}
	if src.UseWebIF != nil {
		v := *src.UseWebIF
		patch.UseWebIFStreams = &v
	}
	if src.StreamPort != nil {
		v := *src.StreamPort
		patch.StreamPort = &v
	}
	if src.Timeout != "" {
		d, err := time.ParseDuration(src.Timeout)
		if err != nil {
			return enigma2FilePatch{}, fmt.Errorf("invalid enigma2.timeout: %w", err)
		}
		patch.Timeout = &d
	}
	if src.ResponseHeaderTimeout != "" {
		d, err := time.ParseDuration(src.ResponseHeaderTimeout)
		if err != nil {
			return enigma2FilePatch{}, fmt.Errorf("invalid enigma2.responseHeaderTimeout: %w", err)
		}
		patch.ResponseHeaderTimeout = &d
	}
	if src.Backoff != "" {
		d, err := time.ParseDuration(src.Backoff)
		if err != nil {
			return enigma2FilePatch{}, fmt.Errorf("invalid enigma2.backoff: %w", err)
		}
		patch.Backoff = &d
	}
	if src.MaxBackoff != "" {
		d, err := time.ParseDuration(src.MaxBackoff)
		if err != nil {
			return enigma2FilePatch{}, fmt.Errorf("invalid enigma2.maxBackoff: %w", err)
		}
		patch.MaxBackoff = &d
	}
	if src.Retries > 0 {
		v := src.Retries
		patch.Retries = &v
	}
	if src.RateLimit > 0 {
		v := src.RateLimit
		patch.RateLimit = &v
	}
	if src.RateBurst > 0 {
		v := src.RateBurst
		patch.RateBurst = &v
	}
	if src.UserAgent != "" {
		v := src.UserAgent
		patch.UserAgent = &v
	}
	if src.AnalyzeDuration != "" {
		v := src.AnalyzeDuration
		patch.AnalyzeDuration = &v
	}
	if src.ProbeSize != "" {
		v := src.ProbeSize
		patch.ProbeSize = &v
	}
	if src.FallbackTo8001 != nil {
		v := *src.FallbackTo8001
		patch.FallbackTo8001 = &v
	}

	return patch, nil
}
