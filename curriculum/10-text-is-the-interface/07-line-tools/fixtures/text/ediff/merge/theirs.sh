#!/bin/bash
attempts=5
# retry COMMAND...: run COMMAND until it succeeds, at most $attempts times.
retry() {
    local i=0
    while [ "$i" -lt "$attempts" ]; do
        "$@" && return 0
        i=$((i + 1))
        sleep 2
    done
    echo "gave up after $attempts attempts" >&2
    return 1
}
