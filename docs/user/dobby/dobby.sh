#!/usr/bin/env bash
# dobby.sh - Dobby environment manager for tell-me-go
#
# Usage:
#   source dobby.sh [-n] [-p] [-c] [--] [<tag> <provider>]
#
#   -n  create/overwrite the environment directory
#   -p  use priority config (shared/priority Vertex headers); must be used with -n
#   -c  block copying the docs folder; must be used with -n
#   --  end of options (subsequent args are positional)
#
# Effects (in the *current* shell):
#   - Exports DOBBY_BASE_DIR, DOBBY_HOME, DOBBY_TAG, DOBBY_PROVIDER
#   - Creates ait-<tag>/ from ait-base/ if -n given
#   - Defines helper functions: b, a, c, t, r
#   - Prepends "[dobby:<tag>|provider]" to PS1

# ---------------------------------------------------------------------------
# Guard: must be sourced and only once
# ---------------------------------------------------------------------------
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    echo "Error: dobby.sh must be sourced, not executed." >&2
    echo "Run:   source ${0##*/}   (or: . ${0##*/})" >&2
    exit 1
fi

if [[ -n "${DOBBY_SOURCED:-}" ]]; then
    echo "Notice: dobby.sh is already sourced for [$DOBBY_TAG|$DOBBY_PROVIDER]." >&2
    echo "Please exit (Ctrl+D) and use a clean shell to change settings." >&2
    return 0 2>/dev/null || exit 0
fi
export DOBBY_SOURCED=1

# Require bash >= 4
if (( BASH_VERSINFO[0] < 4 )); then
    echo "Error: dobby.sh requires bash >= 4 (current: $BASH_VERSION)" >&2
    return 1 2>/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
DOBBY_BASE_DIR="${DOBBY_BASE_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
DOBBY_TEMPLATE="${DOBBY_BASE_DIR}/ait-base"
DOBBY_PROVIDERS_FILE="${DOBBY_TEMPLATE}/configs/butler.yaml"

# ---------------------------------------------------------------------------
# Pre-flight: required tools
# ---------------------------------------------------------------------------
for _dobby_tool in yq go; do
    if ! command -v "$_dobby_tool" >/dev/null 2>&1; then
        echo "Error: required command not found: $_dobby_tool" >&2
        unset _dobby_tool
        return 1 2>/dev/null || exit 1
    fi
done
unset _dobby_tool

if [[ ! -f "$DOBBY_PROVIDERS_FILE" ]]; then
    echo "Error: providers file not found: $DOBBY_PROVIDERS_FILE" >&2
    return 1 2>/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Discovery helpers
# ---------------------------------------------------------------------------
_dobby_known_providers() {
    yq eval '.PROVIDERS | keys | .[]' "$DOBBY_PROVIDERS_FILE" 2>/dev/null
}

_dobby_existing_tags() {
    local dir dirname tag
    for dir in "$DOBBY_BASE_DIR"/ait-*; do
        [[ -d "$dir" ]] || continue
        dirname="${dir##*/}"
        [[ "$dirname" == "ait-base" ]] && continue
        tag="${dirname#ait-}"
        printf '%s\n' "$tag"
    done
}

# ---------------------------------------------------------------------------
# Parse args / interactive selection
# ---------------------------------------------------------------------------
DOBBY_CREATE=0
DOBBY_PRIORITY=0
DOBBY_CLEAN=0
while [[ "${1:-}" == -* ]]; do
    case "$1" in
        -n) DOBBY_CREATE=1; shift ;;
        -p) DOBBY_PRIORITY=1; shift ;;
        -c) DOBBY_CLEAN=1; shift ;;
        -h|--help)
            echo "Usage: source dobby.sh [-n] [-p] [-c] [--] [<tag> <provider>]"
            echo "  -n  create/overwrite environment directory"
            echo "  -p  use priority config (must be used with -n)"
            echo "  -c  skip copying docs folder (must be used with -n)"
            return 0 2>/dev/null || exit 0
            ;;
        --) shift; break ;;
        *)
            echo "Unknown option: $1" >&2
            return 1 2>/dev/null || exit 1
            ;;
    esac
done

if (( DOBBY_PRIORITY == 1 && DOBBY_CREATE == 0 )); then
    echo "Error: -p flag must be used with -n" >&2
    return 1 2>/dev/null || exit 1
fi

if (( DOBBY_CLEAN == 1 && DOBBY_CREATE == 0 )); then
    echo "Error: -c flag must be used with -n" >&2
    return 1 2>/dev/null || exit 1
fi

case $# in
    2)
        DOBBY_TAG="$1"
        DOBBY_PROVIDER="$2"
        ;;
    0)
        if ! command -v fzf >/dev/null 2>&1; then
            echo "Error: fzf is required for interactive selection" >&2
            echo "Usage: source dobby.sh <tag> <provider>" >&2
            return 1 2>/dev/null || exit 1
        fi

        _out=$(_dobby_existing_tags | sort -u | fzf --prompt="Tag (or type new): " --print-query --height=40% --reverse)
        [[ -z "$_out" ]] && { echo "Error: no tag selected" >&2; return 1 2>/dev/null || exit 1; }
        _query=$(printf '%s\n' "$_out" | head -n1)
        _selected=$(printf '%s\n' "$_out" | tail -n +2 | head -n1)
        DOBBY_TAG="${_selected:-$_query}"

        DOBBY_PROVIDER=$(_dobby_known_providers | sort -u | fzf --prompt="Provider for [$DOBBY_TAG]: " --height=40% --reverse)
        [[ -z "$DOBBY_PROVIDER" ]] && { echo "Error: no provider selected" >&2; return 1 2>/dev/null || exit 1; }
        ;;
    *)
        echo "Usage: source dobby.sh [-n] [-p] [-c] [<tag> <provider>]" >&2
        return 1 2>/dev/null || exit 1
        ;;
esac

[[ -z "$DOBBY_TAG"      ]] && { echo "Error: empty tag" >&2;      return 1 2>/dev/null || exit 1; }
[[ -z "$DOBBY_PROVIDER" ]] && { echo "Error: empty provider" >&2; return 1 2>/dev/null || exit 1; }

DOBBY_HOME="${DOBBY_BASE_DIR}/ait-${DOBBY_TAG}"

# Validate provider
if [[ "$(yq eval ".PROVIDERS[\"$DOBBY_PROVIDER\"]" "$DOBBY_PROVIDERS_FILE")" == "null" ]]; then
    echo "Error: provider '$DOBBY_PROVIDER' not in butler.yaml" >&2
    echo "Available: $(yq eval '.PROVIDERS | keys | join(", ")' "$DOBBY_PROVIDERS_FILE")" >&2
    return 1 2>/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Provision directory from template
# ---------------------------------------------------------------------------
if (( DOBBY_CREATE == 1 )); then
    if [[ -d "$DOBBY_HOME" ]]; then
        echo "Overwriting $DOBBY_HOME from ait-base template..."
        rm -rf -- "$DOBBY_HOME"
    else
        echo "Creating $DOBBY_HOME from ait-base template..."
    fi

    if ! cp -r "$DOBBY_TEMPLATE" "$DOBBY_HOME"; then
        rm -rf -- "$DOBBY_HOME"
        echo "Error: failed to copy template into $DOBBY_HOME" >&2
        return 1 2>/dev/null || exit 1
    fi

    # Pick the source YAML for role config generation
    _dobby_source_yaml="${DOBBY_HOME}/configs/butler.yaml"
    if (( DOBBY_PRIORITY == 1 )); then
        _dobby_source_yaml="${DOBBY_HOME}/configs/butler-priority.yaml"
        echo "Using priority config source"
    fi

    # Generate full role configs from source, preserving each role's PERSON
    echo "Generating role configs..."
    for _role in architect coder reviewer tester; do
        _role_file="${DOBBY_HOME}/configs/${_role}.yaml"
        if [[ -f "$_role_file" ]]; then
            _person=$(yq eval '.PERSON' "$_role_file")
            yq eval ".MODE = \"${_role}\" | .PERSON = \"${_person}\"" \
                "$_dobby_source_yaml" > "$_role_file" \
                || echo "Warning: failed to generate ${_role}.yaml" >&2
        fi
    done

    # If non-priority, remove the unused priority config
    if (( DOBBY_PRIORITY == 0 )); then
        rm -f "${DOBBY_HOME}/configs/butler-priority.yaml"
    fi

    # .gitignore
    cat > "${DOBBY_HOME}/.gitignore" <<'GITIGNORE'
output/
secrets/
GITIGNORE

    if (( DOBBY_CLEAN == 1 )); then
        rm -rf -- "${DOBBY_HOME}/docs"
    fi
elif [[ ! -d "$DOBBY_HOME" ]]; then
    echo "Error: $DOBBY_HOME does not exist (use -n to create)" >&2
    return 1 2>/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Source secrets
# ---------------------------------------------------------------------------
_dobby_keys_file="${DOBBY_HOME}/secrets/keys"
if [[ -f "$_dobby_keys_file" ]]; then
    echo "Sourcing $_dobby_keys_file"
    source "$_dobby_keys_file"
else
    echo "Warning: keys file not found at $_dobby_keys_file" >&2
fi

# ---------------------------------------------------------------------------
# Exports
# ---------------------------------------------------------------------------
export DOBBY_BASE_DIR DOBBY_HOME DOBBY_TAG DOBBY_PROVIDER

# ---------------------------------------------------------------------------
# Bash completion
# ---------------------------------------------------------------------------
DOBBY_GOBIN="$(go env GOPATH)/bin"
export DOBBY_GOBIN

if [[ -x "$DOBBY_GOBIN/tell-me-go" ]]; then
    eval "$("$DOBBY_GOBIN/tell-me-go" completion bash)"
fi

# ---------------------------------------------------------------------------
# Install alias (single-shell scope)
# ---------------------------------------------------------------------------
alias c-install='(GOPROXY=direct go install github.com/gosharplite/tell-me-go/cmd/tell-me-go@latest)'

# ---------------------------------------------------------------------------
# Helper functions
# ---------------------------------------------------------------------------

_dobby_run() {
    local role="$1" config="$2" provider="$3"
    shift 3

    if [[ "$(yq eval ".PROVIDERS[\"$provider\"]" "$DOBBY_PROVIDERS_FILE")" == "null" ]]; then
        echo "Error: unknown provider '$provider'" >&2
        return 1
    fi

    TELL_ME_MODE="${role}-${DOBBY_TAG}" \
    TELL_ME_SELECTED_PROVIDER="$provider" \
    TELL_ME_HOME="$DOBBY_HOME" \
    "$DOBBY_GOBIN/tell-me-go" -c "$config" "$@"
}

b() { _dobby_run butler    "${DOBBY_HOME}/configs/butler.yaml"    "$DOBBY_PROVIDER" "$@"; }
a() { _dobby_run architect "${DOBBY_HOME}/configs/architect.yaml" "$DOBBY_PROVIDER" "$@"; }
c() { _dobby_run coder     "${DOBBY_HOME}/configs/coder.yaml"     "$DOBBY_PROVIDER" "$@"; }
t() { _dobby_run tester    "${DOBBY_HOME}/configs/tester.yaml"    "$DOBBY_PROVIDER" "$@"; }
r() { _dobby_run reviewer  "${DOBBY_HOME}/configs/reviewer.yaml"  "$DOBBY_PROVIDER" "$@"; }

complete -F __start_tell-me-go b 2>/dev/null || true
complete -F __start_tell-me-go a 2>/dev/null || true
complete -F __start_tell-me-go c 2>/dev/null || true
complete -F __start_tell-me-go t 2>/dev/null || true
complete -F __start_tell-me-go r 2>/dev/null || true

# ---------------------------------------------------------------------------
# Prompt
# ---------------------------------------------------------------------------
_dobby_green="\[\e[0;32m\]"
_dobby_cyan="\[\e[0;36m\]"
_dobby_reset="\[\e[0m\]"

PS1="[${_dobby_green}${DOBBY_TAG}${_dobby_reset}|${_dobby_cyan}${DOBBY_PROVIDER}${_dobby_reset}] $PS1"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
cat <<EOF
Tag          : $DOBBY_TAG
Provider     : $DOBBY_PROVIDER
DOBBY_HOME   : $DOBBY_HOME

Commands:
  b "prompt"        butler (assistant)
  a "prompt"        architect
  c "prompt"        coder
  t "prompt"        tester
  r "prompt"        reviewer
EOF

# ---------------------------------------------------------------------------
# Cleanup internal helpers
# ---------------------------------------------------------------------------
unset -f _dobby_known_providers _dobby_existing_tags 2>/dev/null
unset _dobby_green _dobby_cyan _dobby_reset _dobby_tool _person _role _role_file _dobby_keys_file _dobby_source_yaml
