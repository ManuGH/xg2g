# Lease Engine Reason Code Matrix

This document provides an authoritative audit matrix of all `ReasonCode` and `ReconciliationReasonCode` constants within the `xg2g` Lease Engine system.

---

## 1. Core Lease Backend Reason Codes (`ReasonCode`)

| Reason Code | Produced by | Terminal (`bool`) | Retryable (`bool`) | Audit Description |
| :--- | :--- | :---: | :---: | :--- |
| `LEASE_ACQUIRED` | `Manager.Acquire`, `Manager.AcquireWithLease` | `false` | `N/A` | Produced upon successful acquisition of a new resource lease. |
| `LEASE_RELEASED_BY_OWNER` | `Manager.Release` | `true` | `false` | Explicit release of the lease by the authorized owner. |
| `LEASE_EXPIRED` | `Manager.SweepExpired` | `true` | `false` | Automatic termination after expiration of the configured TTL. |
| `LEASE_REVOKED_BY_ADMIN` | Admin API (Phase E reserved) | `true` | `false` | Reserved for Phase E administrative revocation of a lease. |
| `LEASE_PREEMPTED` | Policy Engine (Phase E reserved) | `true` | `true` | Reserved for Phase E preemption in favor of a higher-priority session. |
| `LEASE_OWNER_MISMATCH` | `Manager.Release`, `Manager.Renew` | `false` | `false` | Rejected operation: caller is not the registered lease owner. |
| `LEASE_ALREADY_RELEASED` | `Manager.Release`, `Manager.Renew` | `true` | `false` | Rejected operation: lease has already been released. |
| `LEASE_ALREADY_EXPIRED` | `Manager.Release`, `Manager.Renew` | `true` | `false` | Rejected operation: lease has already expired. |
| `LEASE_SCOPE_CONFLICT` | `Manager.Acquire` | `false` | `true` | Rejected acquisition: requested resource scope is currently held. |
| `LEASE_INVALID_TTL` | `Manager.Acquire`, `Manager.Renew` | `false` | `false` | Invalid TTL (<= 0 or above maximum configured limit). |
| `LEASE_INVALID_OWNER` | `Manager.Acquire` | `false` | `false` | Invalid owner identifier (empty string). |
| `LEASE_INVALID_SCOPE` | `Manager.Acquire` | `false` | `false` | Invalid scope key (empty string). |
| `LEASE_NOT_FOUND` | `Manager.Renew`, `Manager.Release`, `Manager.Get` | `true` | `false` | Target lease ID does not exist in backend state. |
| `LEASE_MANAGER_CLOSED` | `Manager.Acquire`, `Manager.Renew`, `Manager.Release` | `true` | `true` | Backend manager is currently shutting down. |
| `LEASE_ROLLBACK` | `CompositeManager.rollbackAcquired`, `IntentTrackedTunerLeaseController` | `true` | `false` | Automatic compensating release following a partial composite or intent failure. |
| `LEASE_RECONCILIATION_ORPHAN_CLEANUP` | `Reconciler.Reconcile` | `true` | `false` | Automatic release of an orphaned backend lease by the Reconciliation Engine. |
| `LEASE_RECONCILIATION_RECOVERY_REQUIRED` | `IntentTrackedTunerLeaseController` | `false` | `true` | Produced when intent state persistence or release compensation fails, requiring recovery. |

---

## 2. Reconciliation Engine Reason Codes (`ReconciliationReasonCode`)

| Reason Code | Produced by | Terminal (`bool`) | Retryable (`bool`) | Audit Description |
| :--- | :--- | :---: | :---: | :--- |
| `RECONCILIATION_MATCH_CONFIRMED` | `Reconciler.Reconcile` | `false` | `N/A` | `confirmed`: Active intent and backend lease match fully (ID, Owner, Scope). |
| `RECONCILIATION_RELEASED` | `Reconciler.Reconcile` | `true` | `N/A` | `released`: Intent and backend state agree that the lease is terminated. |
| `RECONCILIATION_LEASE_MISSING` | `Reconciler.Reconcile` | `false` | `true` | `missing`: Active intent exists, but backend holds no lease for this scope. |
| `RECONCILIATION_ORPHANED` | `Reconciler.Reconcile` | `false` | `true` | `orphaned`: Backend holds active lease with no corresponding active intent. |
| `RECONCILIATION_OWNER_MISMATCH` | `Reconciler.Reconcile` | `false` | `false` | `manual-intervention-required`: Owner mismatch between intent and backend. |
| `RECONCILIATION_LEASE_ID_MISMATCH` | `Reconciler.Reconcile` | `false` | `false` | `manual-intervention-required`: LeaseID mismatch between intent and backend. |
| `RECONCILIATION_DUPLICATE_INTENTS` | `Reconciler.Reconcile` | `false` | `false` | `manual-intervention-required`: Multiple active intents exist for the same scope. |
| `RECONCILIATION_DUPLICATE_BACKEND_LEASES` | `Reconciler.Reconcile` | `false` | `false` | `manual-intervention-required`: Multiple active backend leases exist for the same scope. |
| `RECONCILIATION_ORPHAN_RELEASE_FAILED` | `Reconciler.Reconcile` | `false` | `true` | Automatic release of orphaned backend lease failed. |
| `RECONCILIATION_REVALIDATION_FAILED` | `Reconciler.Reconcile` | `false` | `true` | Post-remediation revalidation check failed. |
| `RECONCILIATION_PENDING_ACQUISITION` | `Reconciler.Reconcile` | `false` | `true` | `confirmed`: Intent is in PENDING state during acquisition. |
| `RECONCILIATION_RELEASE_PENDING` | `Reconciler.Reconcile` | `false` | `true` | `confirmed`: Intent is in RELEASING state during release. |
| `RECONCILIATION_BROKEN_COMPOSITE` | `Reconciler.Reconcile` | `true` | `false` | `broken`: Composite aggregate has incomplete or missing member leases. |
