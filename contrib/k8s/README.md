# Kubernetes Manifests

> [!WARNING]
> **These manifests are unmaintained examples.**
> The project does not officially support Kubernetes deployment at this time.

## Usage

These files are provided as a starting point for users who wish to deploy xg2g on Kubernetes. They may require modification to work with current versions of the application.

- `k8s-alpine.yaml`: Example deployment using the Alpine-based image.
- `k8s-distroless.yaml`: Example deployment using the Distroless-based image.

## Known Issues

- Image digests may be outdated.
- Metrics port configuration might need adjustment (`XG2G_METRICS_ENABLED=true`).
