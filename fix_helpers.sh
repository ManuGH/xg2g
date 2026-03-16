#!/bin/bash
awk '
BEGIN {
    in_func = 0
}
/^func \(o \*Orchestrator\) runExecutionLoop\(/ {
    in_func = 1
    exit
}
{
    print
}
' backend/internal/domain/session/manager/orchestrator_helpers.go > .dev/orchestrator_helpers.go.temp

awk '
BEGIN {
    in_func = 0
    done = 0
}
/^func \(o \*Orchestrator\) runExecutionLoop\(/ {
    if (!done) {
        in_func = 1
    }
}
{
    if (in_func) {
        print
        if (/^}/) {
            in_func = 0
            done = 1
        }
    }
}
' .dev/orchestrator_helpers.head.go >> .dev/orchestrator_helpers.go.temp

awk '
BEGIN {
    in_func = 0
    done_func = 0
}
/^func \(o \*Orchestrator\) runExecutionLoop\(/ {
    in_func = 1
}
/^func \(o \*Orchestrator\) finalizeDeferred\(/ {
    done_func = 1
}
{
    if (done_func) {
        print
    }
}
' backend/internal/domain/session/manager/orchestrator_helpers.go >> .dev/orchestrator_helpers.go.temp

mv .dev/orchestrator_helpers.go.temp backend/internal/domain/session/manager/orchestrator_helpers.go
