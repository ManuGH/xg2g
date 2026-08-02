# Lease Engine Reason Code Matrix

This document provides an authoritative audit matrix of all `ReasonCode` and `ReconciliationReasonCode` constants within the `xg2g` Lease Engine system.

---

## 1. Core Lease Backend Reason Codes (`ReasonCode`)

| Reason Code | Erzeugt von | Terminal (`bool`) | Retrybar (`bool`) | Audit / Beschreibung |
| :--- | :--- | :---: | :---: | :--- |
| `LEASE_ACQUIRED` | `Manager.Acquire`, `Manager.AcquireWithLease` | `false` | `N/A` | Erzeugt bei erfolgreichem Erwerb einer neuen Lease. |
| `LEASE_RELEASED_BY_OWNER` | `Manager.Release` | `true` | `false` | Explizite Freigabe der Lease durch den autorisierten Owner. |
| `LEASE_EXPIRED` | `Manager.SweepExpired` | `true` | `false` | Automatische Beendigung nach Ablauf der vereinbarten TTL. |
| `LEASE_REVOKED_BY_ADMIN` | Admin / API (Phase E) | `true` | `false` | Manuelle oder administrative Annullierung einer Lease. |
| `LEASE_PREEMPTED` | Policy Engine (Phase E) | `true` | `true` | Verdrängung der Lease zugunsten einer höher priorisierten Session. |
| `LEASE_OWNER_MISMATCH` | `Manager.Release`, `Manager.Renew` | `false` | `false` | Abgelehnte Operation: Der Anrufer ist nicht der eingetragene Owner. |
| `LEASE_ALREADY_RELEASED` | `Manager.Release`, `Manager.Renew` | `true` | `false` | Abgelehnte Operation: Lease wurde bereits freigegeben. |
| `LEASE_ALREADY_EXPIRED` | `Manager.Release`, `Manager.Renew` | `true` | `false` | Abgelehnte Operation: Lease ist bereits abgelaufen. |
| `LEASE_SCOPE_CONFLICT` | `Manager.Acquire` | `false` | `true` | Abgelehnter Erwerb: Der angeforderte Scope ist bereits belegt. |
| `LEASE_INVALID_TTL` | `Manager.Acquire`, `Manager.Renew` | `false` | `false` | Ungültige TTL (<= 0 oder oberhalb des maximalen Limits). |
| `LEASE_INVALID_OWNER` | `Manager.Acquire` | `false` | `false` | Ungültige Owner-ID (leer). |
| `LEASE_INVALID_SCOPE` | `Manager.Acquire` | `false` | `false` | Ungültiger Scope (leer). |
| `LEASE_NOT_FOUND` | `Manager.Renew`, `Manager.Release`, `Manager.Get` | `true` | `false` | Gesuchte Lease-ID existiert nicht im Backend. |
| `LEASE_MANAGER_CLOSED` | `Manager.Acquire`, `Manager.Renew`, `Manager.Release` | `true` | `true` | Backend befindet sich im Shutdown. |
| `LEASE_ROLLBACK` | `CompositeManager.rollbackAcquired`, `IntentTrackedTunerLeaseController` | `true` | `false` | Automatische Kompensations-Freigabe nach einem partiellen Composite- oder Intent-Fehler. |
| `LEASE_RECONCILIATION_ORPHAN_CLEANUP` | `Reconciler.Reconcile` | `true` | `false` | Automatische Freigabe einer verwaisten Backend-Lease durch die Reconciliation Engine. |

---

## 2. Reconciliation Engine Reason Codes (`ReconciliationReasonCode`)

| Reason Code | Erzeugt von | Terminal (`bool`) | Retrybar (`bool`) | Audit / Beschreibung |
| :--- | :--- | :---: | :---: | :--- |
| `RECONCILIATION_MATCH_CONFIRMED` | `Reconciler.Reconcile` | `false` | `N/A` | `confirmed`: Aktiver Intent und Backend-Lease stimmen vollständig überein (ID, Owner, Scope). |
| `RECONCILIATION_RELEASED` | `Reconciler.Reconcile` | `true` | `N/A` | `released`: Intent und Backend stimmen überein, dass die Lease beendet/freigegeben ist. |
| `RECONCILIATION_LEASE_MISSING` | `Reconciler.Reconcile` | `false` | `true` | `missing`: Aktiver Intent existiert, aber im Backend existiert keine Lease für diesen Scope. |
| `RECONCILIATION_ORPHANED` | `Reconciler.Reconcile` | `false` | `true` | `orphaned`: Backend hält aktive Lease, für die kein aktiver Intent existiert. |
| `RECONCILIATION_OWNER_MISMATCH` | `Reconciler.Reconcile` | `false` | `false` | `manual-intervention-required`: Abweichender Owner zwischen Intent und Backend. |
| `RECONCILIATION_LEASE_ID_MISMATCH` | `Reconciler.Reconcile` | `false` | `false` | `manual-intervention-required`: Abweichende LeaseID zwischen Intent und Backend. |
| `RECONCILIATION_DUPLICATE_INTENTS` | `Reconciler.Reconcile` | `false` | `false` | `manual-intervention-required`: Mehrere aktive Intents für denselben Scope. |
| `RECONCILIATION_DUPLICATE_BACKEND_LEASES` | `Reconciler.Reconcile` | `false` | `false` | `manual-intervention-required`: Mehrere aktive Backend-Leases für denselben Scope. |
| `RECONCILIATION_ORPHAN_RELEASE_FAILED` | `Reconciler.Reconcile` | `false` | `true` | Automatische Freigabe der verwaisten Lease ist im Backend fehlgeschlagen. |
| `RECONCILIATION_REVALIDATION_FAILED` | `Reconciler.Reconcile` | `false` | `true` | Re-Validierung nach der Remediation schlug fehl. |
| `RECONCILIATION_PENDING_ACQUISITION` | `Reconciler.Reconcile` | `false` | `true` | `confirmed`: Intent befindet sich im Zustand PENDING während des Erwerbs. |
| `RECONCILIATION_RELEASE_PENDING` | `Reconciler.Reconcile` | `false` | `true` | `confirmed`: Intent befindet sich im Zustand RELEASING während der Freigabe. |
| `RECONCILIATION_BROKEN_COMPOSITE` | `Reconciler.Reconcile` | `true` | `false` | `broken`: Composite Aggregate besitzt unvollständige oder fehlerhafte Member-Leases. |
