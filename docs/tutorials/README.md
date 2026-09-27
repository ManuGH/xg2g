# Tutorials (Diátaxis: Learning-Oriented)

Tutorials guide newcomers step-by-step through a concrete learning journey to achieve their first successful result with **xg2g**.

---

## What is a Tutorial?

According to the [Diátaxis framework](https://diataxis.fr/tutorials/), a tutorial is:
- **Learning-oriented**: Focuses on helping a beginner gain competence and confidence.
- **Hands-on & executable**: Provides a clear sequence of steps from zero to a working system.
- **Minimal friction**: Minimizes conceptual deep-dives; focuses on immediate, visible success.

---

## Available Tutorials

| Tutorial | Focus | Outcome |
| :--- | :--- | :--- |
| [**Getting Started with xg2g**](../guides/GETTING_STARTED.md) | Initial setup, receiver connection, and playback verification | Play your first live DVB stream in under 5 minutes |
| [**Local Development Environment**](../guides/DEVELOPMENT.md) | Toolchain setup, building backend, running WebUI, mock tuner twin | Fully functional local dev environment with hot-reloading |

---

## Recommended Learning Path

```
  1. Getting Started
     ├── Configure OpenWebIf connection or digital twin
     ├── Launch xg2g daemon
     └── Open WebUI and verify active playback
         │
         ▼
  2. Local Development
     ├── Install Go 1.26+ and Node 22+
     ├── Run `make dev` for backend & WebUI
     └── Run `make test` for hermetic suite verification
```

---

## Where to Go Next

- Need practical recipes for server deployment? See [**How-To Guides**](../how-to/README.md).
- Looking for configuration parameters or reason codes? See [**Reference**](../reference/README.md).
- Want to understand composite lease arbitration or TS demuxing? See [**Explanation**](../explanation/README.md).
