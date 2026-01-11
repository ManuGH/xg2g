# System Operations: Component Wiring

## Overview

The `internal/app/bootstrap` package is the **Single Source of Truth** for application construction and lifecycle management. It enforces a strict separation between **Construction** (pure, side-effect free) and **Execution** (runtime, background processes).

## Lifecycle

### 1. Construction (`WireServices`)

* **Responsibility**: Dependency Injection graph resolution.
* **Input**: `context.Context`, Version Info, Config Path.
* **Output**: `*Container` (Fully wired service graph).
* **Constraints**:
  * **NO Side-Effects**: Must not spawn goroutines, open network listeners (except for health checks if critical), or block.
  * **Deterministic**: Same config + same inputs = same graph struct.

### 2. Execution (`Start`)

* **Responsibility**: Launching background processes required for the application to function.
* **Input**: `context.Context` (Lifecycle context).
* **Output**: `error`.
* **Actions**:
  * Starts Cache Evicter.
  * *(Optional)* Triggers Initial Data Refresh (via `XG2G_INITIAL_REFRESH` env).
* **Constraints**:
  * **Non-Blocking**: Must return immediately to allow the caller to control the release of the main loop.
  * **No Sleeps**: Background tasks must be robust against race conditions (e.g., using internal dispatch instead of self-network-requests).

## Testing

* **Mechanical Proof**: `TestWiring_BootsMinimalStack` asserts that the graph is constructed and `Start()` executes without panic.
* **Isolation**: Tests should generally disable `XG2G_INITIAL_REFRESH` (`t.Setenv("XG2G_INITIAL_REFRESH", "false")`) to avoid non-deterministic network interactions during unit tests.
