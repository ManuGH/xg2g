# Dockerfile security policies using Open Policy Agent (OPA)
package main

# Deny if no USER directive is present (must run as non-root)
deny[msg] {
    not has_user
    msg = "Dockerfile must include USER directive to run as non-root"
}

has_user {
    input[_].Cmd == "user"
}

# Warn if HEALTHCHECK is missing
warn[msg] {
    not has_healthcheck
    msg = "Consider adding HEALTHCHECK directive for better container monitoring"
}

has_healthcheck {
    input[_].Cmd == "healthcheck"
}

# Deny if using latest tag
deny[msg] {
    input[_].Cmd == "from"
    val := input[_].Value
    contains(val[_], "latest")
    msg = sprintf("Avoid using 'latest' tag in FROM: %s", [val])
}

# Warn if no LABEL for maintainer
warn[msg] {
    not has_maintainer_label
    msg = "Consider adding LABEL with maintainer information"
}

has_maintainer_label {
    input[_].Cmd == "label"
    val := input[_].Value
    contains(lower(val[_]), "maintainer")
}

# Deny if running apt-get without -y flag
deny[msg] {
    input[_].Cmd == "run"
    val := concat(" ", input[_].Value)
    contains(val, "apt-get")
    not contains(val, "-y")
    msg = "apt-get commands must use -y flag for non-interactive installation"
}

# Deny if running apt-get without cleaning cache
deny[msg] {
    input[_].Cmd == "run"
    val := concat(" ", input[_].Value)
    contains(val, "apt-get install")
    not contains(val, "rm -rf /var/lib/apt/lists")
    msg = "apt-get install must clean cache with: && rm -rf /var/lib/apt/lists/*"
}

# Warn if EXPOSE uses privileged ports (< 1024)
warn[msg] {
    input[_].Cmd == "expose"
    port := input[_].Value[_]
    to_number(port) < 1024
    msg = sprintf("Consider using non-privileged port instead of %s", [port])
}

# Deny if ADD is used instead of COPY
deny[msg] {
    input[_].Cmd == "add"
    msg = "Use COPY instead of ADD unless you need tar extraction or URL support"
}
