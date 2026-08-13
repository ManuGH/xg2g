// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/SherClockHolmes/webpush-go"
)

type VAPIDKeys struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
}

// GetOrGenerateVAPIDKeys loads persistent VAPID keys or generates a new keypair with 0600 file permissions.
func GetOrGenerateVAPIDKeys(storageDir string) (*VAPIDKeys, error) {
	if storageDir == "" {
		storageDir = "."
	}
	keyPath := filepath.Join(storageDir, "vapid_keys.json")

	if data, err := os.ReadFile(keyPath); err == nil {
		var keys VAPIDKeys
		if err := json.Unmarshal(data, &keys); err == nil && keys.PrivateKey != "" && keys.PublicKey != "" {
			return &keys, nil
		}
	}

	privKey, pubKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to generate VAPID keys: %w", err)
	}

	keys := &VAPIDKeys{
		PrivateKey: privKey,
		PublicKey:  pubKey,
	}

	data, err := json.MarshalIndent(keys, "", "  ")
	if err == nil {
		_ = os.MkdirAll(storageDir, 0700)
		_ = os.WriteFile(keyPath, data, 0600)
	}

	return keys, nil
}

// BackoffDelays for retries: 1->5s, 2->30s, 3->2m, 4->10m, 5->30m
var retryBackoffs = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
}

// ProcessNotificationQueue processes pending push deliveries from the database queue.
func (s *Service) ProcessNotificationQueue(ctx context.Context) error {
	deliveries, err := s.store.GetPendingNotificationDeliveries(ctx, 20)
	if err != nil || len(deliveries) == 0 {
		return err
	}

	vapidKeys, err := GetOrGenerateVAPIDKeys(".")
	if err != nil {
		return err
	}

	for _, d := range deliveries {
		s.dispatchSingleDelivery(ctx, d, vapidKeys)
	}

	return nil
}

func (s *Service) dispatchSingleDelivery(ctx context.Context, d NotificationDelivery, vapidKeys *VAPIDKeys) {
	subs, err := s.store.ListPushSubscriptions(ctx, "default_household", "")
	if err != nil {
		return
	}

	var targetSub *PushSubscription
	for _, sub := range subs {
		if sub.Endpoint == d.EndpointID || sub.ID == d.EndpointID {
			targetSub = &sub
			break
		}
	}

	if targetSub == nil {
		d.Status = "failed_permanent"
		d.LastError = "Subscription endpoint not found"
		_ = s.store.UpdateNotificationDelivery(ctx, &d)
		return
	}

	payloadData, _ := json.Marshal(map[string]interface{}{
		"id":             d.NotificationID,
		"title":          "xg2g - Freigabe erforderlich",
		"body":           "Eine neue Freigabeanfrage wartet auf Entscheidung.",
		"resourceId":     d.NotificationID,
		"actionRequired": "approve_content",
	})

	sSubscription := &webpush.Subscription{
		Endpoint: targetSub.Endpoint,
		Keys: webpush.Keys{
			P256dh: targetSub.P256dh,
			Auth:   targetSub.Auth,
		},
	}

	resp, err := webpush.SendNotification(payloadData, sSubscription, &webpush.Options{
		Subscriber:      "mailto:admin@xg2g.local",
		VAPIDPublicKey:  vapidKeys.PublicKey,
		VAPIDPrivateKey: vapidKeys.PrivateKey,
		TTL:             86400,
	})

	now := s.now()

	if err == nil && resp != nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated) {
		d.Status = "sent"
		d.SentAt = &now
		d.LastError = ""
		_ = s.store.UpdateNotificationDelivery(ctx, &d)
		return
	}

	d.AttemptCount++

	if resp != nil {
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			// 404/410 Gone -> Prune strictly ONLY this endpoint
			_ = s.store.DeletePushSubscriptionByEndpoint(ctx, targetSub.Endpoint)
			d.Status = "failed_permanent"
			d.LastError = fmt.Sprintf("HTTP %d: Endpoint revoked", resp.StatusCode)
			_ = s.store.UpdateNotificationDelivery(ctx, &d)
			return
		}

		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			d.Status = "failed_permanent"
			d.LastError = fmt.Sprintf("HTTP %d permanent failure", resp.StatusCode)
			_ = s.store.UpdateNotificationDelivery(ctx, &d)
			return
		}
	}

	if d.AttemptCount >= 5 {
		d.Status = "failed_permanent"
		if err != nil {
			d.LastError = err.Error()
		}
		_ = s.store.UpdateNotificationDelivery(ctx, &d)
		return
	}

	// Calculate exponential backoff
	backoffIdx := d.AttemptCount - 1
	if backoffIdx < 0 {
		backoffIdx = 0
	}
	if backoffIdx >= len(retryBackoffs) {
		backoffIdx = len(retryBackoffs) - 1
	}

	nextAttempt := now.Add(retryBackoffs[backoffIdx])
	d.Status = "failed"
	d.NextAttemptAt = &nextAttempt
	if err != nil {
		d.LastError = err.Error()
	}
	_ = s.store.UpdateNotificationDelivery(ctx, &d)
}
