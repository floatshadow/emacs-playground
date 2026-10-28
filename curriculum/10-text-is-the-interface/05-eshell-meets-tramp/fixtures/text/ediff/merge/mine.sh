#!/bin/bash
attempts=3
# retry COMMAND...: run COMMAND until it succeeds.
retry() {
    local i=0
    while [ "$i" -lt "$attempts" ]; do
        "$@" && return 0
        i=$((i + 1))
        sleep $((2 ** i))
    done
    echo "gave up" >&2
    return 1
}
