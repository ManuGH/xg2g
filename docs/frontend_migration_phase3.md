# Frontend Migration Phase 3: State Components

**Date:** 2025-12-26
**Status:** Complete
**Goal:** Full migration of `EditTimerDialog` and `SeriesManager` to TypeScript
(`.tsx`) and strict type safety.

## 1. Migrated Components

### EditTimerDialog (`EditTimerDialog.tsx`)

* **Status**: Migrated from `.jsx`.

* **Key Changes**:
  * **Strict Typing**: Implemented interfaces for `Props`, `FormData`, and
        mapped SDK types (`Timer`, `DvrCapabilities`).
  * **SDK Fix (Critical)**:
    * Replaced nonexistent `TimersService` / `DvrService` imports with
            functional endpoints.
    * Corrected `previewConflicts` signature to
            `{ body: { proposed, mode } }`.
    * Corrected `updateTimer` signature to
            `{ path: { timerId }, body: { ... } }`.
  * **Form State**: Typed `useState` generic to ensure correct field usage.

### SeriesManager (`SeriesManager.tsx`)

* **Status**: Migrated from `.jsx`.

* **Key Changes**:
  * **Strict Typing**: Implemented `SeriesRule`, `Service`,
        `SeriesRuleWritable`.
  * **SDK Fix (Critical)**:
    * `runSeriesRule` updated to use `query` parameters instead of `body`.
    * `deleteSeriesRule` updated to use correctly named path parameter
            `id`.
  * **Logic Fixes**:
    * Enforced `service_ref` usage for Channel mapping (fixing potential
            `undefined` usage on `ref`).

## 2. Runtime Smoke Test Checklist (DoD)

Please perform the following manual verification steps to ensure the mutations
and Receiver interactions work as expected throughout the new TypeScript
components.

### 2.1 EditTimerDialog

* [ ] **Edit & Save**: Open an existing timer, modify fields (Name,
    Description), and Save. Verify changes persist.

* [ ] **Conflict Detection**:
  * [ ] Change `Begin` / `End` time.
  * [ ] Verify "Prüfe auf Konflikte..." appears.
  * [ ] Create a deliberate conflict (overlap with existing timer). Verify
        conflict list is shown and "Save" is disabled.

* [ ] **Error Handling**:
  * [ ] Simulate or force a duplicate/conflict error from the backend.
        Verify the error message is displayed correctly in the dialog.

* [ ] **Lifecycle**:
  * [ ] Close the dialog appropriately. Verify no "setState on unmounted
        component" warnings in the console.

### 2.2 SeriesManager

* [ ] **List View**: Verify all Series Rules load and display correctly.

* [ ] **Create Rule**:
  * [ ] Click "+ New Rule".
  * [ ] Fill in details (Keyword, Channel, Days, etc.).
  * [ ] Save. Verify the new rule appears in the list.

* [ ] **Channel Mapping**: Verify the Channel dropdown works and selected
    channels display their **Name** in the list (not just the reference ID).

* [ ] **Run Rule**:
  * [ ] Click "Run Now" on a rule.
  * [ ] Verify the "Running..." state.
  * [ ] Verify the completion Alert shows a summary
        (Matched/Created/Errors).

* [ ] **Delete Rule**: Delete a rule and verify it is removed from the list.

### 2.3 Auth & General

* [ ] **Token Expiry**: If the auth token expires, ensure the UI redirects
    or shows a clean error rather than failing silently.

## 3. Build Verification

* [x] `npm run build` completes successfully.

* [x] No `.jsx` files remain in `src/components`.
