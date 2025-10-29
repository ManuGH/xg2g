# Kubernetes security policies
package main

# Deny if container runs as root
deny[msg] {
    input.kind == "Deployment"
    not input.spec.template.spec.securityContext.runAsNonRoot
    msg = "Containers must not run as root (set runAsNonRoot: true)"
}

# Deny if no resource limits are set
deny[msg] {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.resources.limits
    msg = sprintf("Container '%s' must have resource limits defined", [container.name])
}

# Deny if no resource requests are set
deny[msg] {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.resources.requests
    msg = sprintf("Container '%s' must have resource requests defined", [container.name])
}

# Deny if privileged mode is enabled
deny[msg] {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    container.securityContext.privileged
    msg = sprintf("Container '%s' must not run in privileged mode", [container.name])
}

# Warn if readOnlyRootFilesystem is not set
warn[msg] {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.securityContext.readOnlyRootFilesystem
    msg = sprintf("Consider setting readOnlyRootFilesystem for container '%s'", [container.name])
}

# Deny if allowPrivilegeEscalation is not explicitly set to false
deny[msg] {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.securityContext.allowPrivilegeEscalation == false
    msg = sprintf("Container '%s' must set allowPrivilegeEscalation: false", [container.name])
}

# Warn if no liveness probe
warn[msg] {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.livenessProbe
    msg = sprintf("Consider adding livenessProbe to container '%s'", [container.name])
}

# Warn if no readiness probe
warn[msg] {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.readinessProbe
    msg = sprintf("Consider adding readinessProbe to container '%s'", [container.name])
}

# Deny if using latest tag
deny[msg] {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    endswith(container.image, ":latest")
    msg = sprintf("Container '%s' must not use 'latest' tag", [container.name])
}

# Deny if no image pull policy is set or is not Always
deny[msg] {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.imagePullPolicy == "Always"
    msg = sprintf("Container '%s' should use imagePullPolicy: Always", [container.name])
}
