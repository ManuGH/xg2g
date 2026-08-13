// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"time"
)

// Notification represents a per-user persistent in-app or push alert.
type Notification struct {
	ID             string     `json:"id"`
	HouseholdID    string     `json:"householdId"`
	UserID         string     `json:"userId"`
	Type           string     `json:"type"` // "approval_request", "recording_failed", "device_registered", "stream_preempted", "storage_low", "invite_redeemed"
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	ResourceID     string     `json:"resourceId,omitempty"`
	ActionRequired string     `json:"actionRequired,omitempty"` // e.g. "approve_content"
	CreatedAt      time.Time  `json:"createdAt"`
	ReadAt         *time.Time `json:"readAt,omitempty"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
}

// NotificationDelivery tracks push channel delivery attempts (WebPush, FCM) per notification.
type NotificationDelivery struct {
	ID             string     `json:"id"`
	NotificationID string     `json:"notificationId"`
	Channel        string     `json:"channel"` // "webpush" | "fcm"
	EndpointID     string     `json:"endpointId"`
	Status         string     `json:"status"` // "queued" | "sent" | "failed"
	AttemptCount   int        `json:"attemptCount"`
	LastError      string     `json:"lastError,omitempty"`
	SentAt         *time.Time `json:"sentAt,omitempty"`
}

// PushSubscription represents a WebPush or FCM device subscription token.
type PushSubscription struct {
	ID          string    `json:"id"`
	HouseholdID string    `json:"householdId"`
	UserID      string    `json:"userId"`
	Endpoint    string    `json:"endpoint"`
	P256dh      string    `json:"p256dh,omitempty"`
	Auth        string    `json:"auth,omitempty"`
	UserAgent   string    `json:"userAgent,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}
