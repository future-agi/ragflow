#!/usr/bin/env bash
# Run in a clean build environment: bash test/build_dependencies.sh
# No network or native binaries are needed for these recovery-path tests.
set -eu
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${REPO_ROOT}/build.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Test the real version gate while replacing only the external downloader.
_seed_from_system() { :; }
uv() {
    printf '%s\n' "$*" > "${PROJECT_ROOT}/uv.args"
    test "$*" = 'run --script ragflow_deps/download_deps.py' || return 41
    test ! -e "${PROJECT_ROOT}/ragflow_deps/office_oxide-linux-x86_64.tar.gz" || return 42
    test "${DOWNLOAD_RESULT}" != failure || return 43
    mkdir -p "${OFFICE_OXIDE_PREFIX}/lib" "${OFFICE_OXIDE_PREFIX}/include/office_oxide_c"
    printf '%s\n' "${DOWNLOAD_VERSION}" > "${OFFICE_OXIDE_PREFIX}/lib/liboffice_oxide.a"
    if [ "${DOWNLOAD_RESULT}" != missing-header ]; then
        touch "${OFFICE_OXIDE_PREFIX}/include/office_oxide_c/office_oxide.h"
    fi
}
fixture() {
    PROJECT_ROOT="${tmp}/$1"
    OFFICE_OXIDE_PREFIX="${PROJECT_ROOT}/cache/office_oxide"
    mkdir -p "${OFFICE_OXIDE_PREFIX}/lib" "${OFFICE_OXIDE_PREFIX}/include/office_oxide_c" "${PROJECT_ROOT}/ragflow_deps"
    printf '%s\n' "$2" > "${OFFICE_OXIDE_PREFIX}/lib/liboffice_oxide.a"
    touch "${OFFICE_OXIDE_PREFIX}/include/office_oxide_c/office_oxide.h"
    printf 'stale archive\n' > "${PROJECT_ROOT}/ragflow_deps/office_oxide-linux-x86_64.tar.gz"
    DOWNLOAD_RESULT=success
    DOWNLOAD_VERSION="${OFFICE_OXIDE_VERSION}"
}
expect_failure() {
    if (check_office_oxide_deps) > "${PROJECT_ROOT}/check.log" 2>&1; then
        echo "FAIL: $1 was accepted" >&2
        exit 1
    fi
    echo "PASS: $1 rejected"
}

fixture matching "$OFFICE_OXIDE_VERSION"
check_office_oxide_deps
test ! -e "${PROJECT_ROOT}/uv.args"
echo 'PASS: matching native cache does not download'

fixture stale '0.1.16'
check_office_oxide_deps
test -f "${PROJECT_ROOT}/uv.args"
grep -Fx "$OFFICE_OXIDE_VERSION" "${OFFICE_OXIDE_PREFIX}/lib/liboffice_oxide.a"
echo 'PASS: stale native cache refresh uses script metadata and clears the correct archive'

fixture wrong-download '0.1.16'
DOWNLOAD_VERSION=0.1.16
expect_failure 'wrong downloaded native version'

fixture broken-download '0.1.16'
DOWNLOAD_RESULT=failure
expect_failure 'failed downloader'

fixture missing-header '0.1.16'
DOWNLOAD_RESULT=missing-header
expect_failure 'download without required header'

# Pin Docker's acquisition contract as well as the shell recovery behavior.
# In particular, inline assignments before `git config && build.sh` do not
# propagate to build.sh; use ENV for the cache and proxy across both commands.
grep -F 'ARG GOPROXY="https://proxy.golang.org|https://goproxy.cn|direct"' "${REPO_ROOT}/Dockerfile"
grep -F 'ENV GOPROXY=${GOPROXY}' "${REPO_ROOT}/Dockerfile"
grep -F '    GOMODCACHE=/root/.cache/gomod \' "${REPO_ROOT}/Dockerfile"
grep -Fx '    GOCACHE=/root/.cache/gobuild' "${REPO_ROOT}/Dockerfile"
if grep -Eq 'github_action_runner:latest|ragflow_deps:latest|GOPROXY=.*goproxy.cn,' "${REPO_ROOT}/Dockerfile"; then
    echo 'FAIL: Docker dependencies must be pinned and proxy errors must fall back' >&2
    exit 1
fi
echo 'PASS: pinned Docker inputs, error fallback and shared Go cache'
echo 'ALL BUILD DEPENDENCY TESTS PASSED'
