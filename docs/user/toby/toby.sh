#!/usr/bin/env bash
# toby.sh - set shell prompt tag and TELL_ME_HOME for current shell
#
# Usage:
#   source toby.sh [-n] [--] [<tag> <provider>]
#
#   With no arguments, fzf-based interactive selection is used.
#   Press Ctrl+C / Ctrl+D in fzf to abort.
#   -n  create missing directory (default: select only, fail if missing)
#   --  end of options (subsequent args are positional)
#
# Environment overrides (read at source-time):
#   TOBY_BASE_DIR        Root directory containing ait-base/ and ait-<tag>-<provider>/
#                        Default: directory containing this script. If that cannot
#                        be determined, falls back to TOBY_DEFAULT_BASE_DIR below.
#   TOBY_BASE_TEMPLATE   Override the template directory.
#                        Default: $TOBY_BASE_DIR/ait-base
#   TOBY_SHARED_AIT_DIR  Override the source-of-truth ait/ dir whose API_KEY
#                        paths get rewritten into TELL_ME_HOME.
#                        Default: $TOBY_BASE_DIR/ait
#   TOBY_DEV_GO_SRC      Override the local source path used by `c-install-dev`.
#                        Default: hard-coded developer path (see below).
#
# Effects (in the *current* shell):
#   - Exports TOBY_BASE_DIR, TOBY_TAG, TOBY_PROVIDER, TELL_ME_HOME, TELL_ME_GOBIN
#   - Creates ait-<tag>-<provider>/ from ait-base/ if missing
#   - Updates configs/*.yaml with selected provider & MODE
#   - Defines helper functions: c, t, r, a, b
#   - Prepends "[<tag>] " to PS1

# ---------------------------------------------------------------------------
# Guard: must be sourced
# ---------------------------------------------------------------------------
# When sourced in bash, ${BASH_SOURCE[0]} != $0
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    echo "Error: toby.sh must be sourced, not executed." >&2
    echo "Run:   source ${0##*/}   (or: . ${0##*/})" >&2
    exit 1
fi

# Require bash >= 4 (mapfile, associative arrays).
if (( BASH_VERSINFO[0] < 4 )); then
    echo "Error: toby.sh requires bash >= 4 (current: $BASH_VERSION)" >&2
    return 1 2>/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
# Last-resort fallback if neither $TOBY_BASE_DIR nor script auto-detection
# yields a usable directory. Override at source-time via the env var.
TOBY_DEFAULT_BASE_DIR="${HOME}/tell-me-go/docs/user/toby"

# Resolve a path to its absolute, canonical form. Falls back to the input
# unchanged if `realpath` is unavailable.
_toby_realpath() {
    local p="$1"
    if command -v realpath >/dev/null 2>&1; then
        realpath -m -- "$p" 2>/dev/null || printf '%s\n' "$p"
    elif command -v readlink >/dev/null 2>&1 && readlink -f / >/dev/null 2>&1; then
        readlink -f -- "$p" 2>/dev/null || printf '%s\n' "$p"
    else
        # Best-effort: prepend $PWD if path is relative
        case "$p" in
            /*) printf '%s\n' "$p" ;;
            *)  printf '%s/%s\n' "$PWD" "$p" ;;
        esac
    fi
}

# Returns 0 if $1 looks like a valid TOBY_BASE_DIR (contains the template).
_toby_is_valid_base() {
    [[ -d "$1" && -f "$1/ait-base/configs/assistant.yaml" ]]
}

# Resolution order for the base directory. Each candidate is validated
# (must contain ait-base/configs/assistant.yaml); the first valid one wins.
#   1. caller-provided $TOBY_BASE_DIR              (hard error if invalid)
#   2. directory containing this script            (skipped if invalid)
#   3. TOBY_DEFAULT_BASE_DIR fallback              (skipped if invalid)
_toby_resolved=""
_toby_tried=()

if [[ -n "${TOBY_BASE_DIR:-}" ]]; then
    _toby_candidate=$(_toby_realpath "$TOBY_BASE_DIR")
    _toby_tried+=("env TOBY_BASE_DIR=$_toby_candidate")
    if _toby_is_valid_base "$_toby_candidate"; then
        _toby_resolved="$_toby_candidate"
    else
        # Caller asked for this explicitly -- don't silently fall through.
        echo "Error: TOBY_BASE_DIR does not contain ait-base/configs/assistant.yaml" >&2
        echo "       (looked in: $_toby_candidate)" >&2
        unset -f _toby_realpath _toby_is_valid_base
        unset _toby_candidate _toby_tried _toby_resolved
        return 1 2>/dev/null || exit 1
    fi
fi

if [[ -z "$_toby_resolved" && -n "${BASH_SOURCE[0]:-}" ]]; then
    _toby_candidate=$(_toby_realpath "$(dirname -- "${BASH_SOURCE[0]}")")
    _toby_tried+=("script dir=$_toby_candidate")
    _toby_is_valid_base "$_toby_candidate" && _toby_resolved="$_toby_candidate"
fi

if [[ -z "$_toby_resolved" ]]; then
    _toby_candidate=$(_toby_realpath "$TOBY_DEFAULT_BASE_DIR")
    _toby_tried+=("default=$_toby_candidate")
    _toby_is_valid_base "$_toby_candidate" && _toby_resolved="$_toby_candidate"
fi

if [[ -z "$_toby_resolved" ]]; then
    echo "Error: could not locate a valid TOBY_BASE_DIR." >&2
    echo "A valid base must contain: ait-base/configs/assistant.yaml" >&2
    echo "Tried (in order):" >&2
    for _toby_c in "${_toby_tried[@]}"; do
        echo "  - $_toby_c" >&2
    done
    echo "Hint: set TOBY_BASE_DIR explicitly, e.g.:" >&2
    echo "      TOBY_BASE_DIR=/path/to/beta-booth source ${BASH_SOURCE[0]:-toby.sh}" >&2
    unset -f _toby_realpath _toby_is_valid_base
    unset _toby_candidate _toby_tried _toby_resolved _toby_c
    return 1 2>/dev/null || exit 1
fi

TOBY_BASE_DIR="$_toby_resolved"
unset _toby_candidate _toby_tried _toby_resolved _toby_c
unset -f _toby_is_valid_base

# Derived paths (also overridable via env).
TOBY_BASE_TEMPLATE="${TOBY_BASE_TEMPLATE:-${TOBY_BASE_DIR}/ait-base}"
TOBY_SHARED_AIT_DIR="${TOBY_SHARED_AIT_DIR:-${TOBY_BASE_DIR}/ait}"
TOBY_PROVIDERS_FILE="${TOBY_BASE_TEMPLATE}/configs/assistant.yaml"

# ---------------------------------------------------------------------------
# Helper: bail out from a sourced script without killing the shell
# ---------------------------------------------------------------------------
_toby_fail() {
    echo "Error: $*" >&2
    # `return` works because we're sourced; fall back to `exit` defensively.
    return 1 2>/dev/null || exit 1
}

# ---------------------------------------------------------------------------
# Pre-flight: required tools
# ---------------------------------------------------------------------------
for _toby_tool in yq go; do
    if ! command -v "$_toby_tool" >/dev/null 2>&1; then
        echo "Error: required command not found: $_toby_tool" >&2
        unset _toby_tool
        return 1 2>/dev/null || exit 1
    fi
done
unset _toby_tool

if [[ ! -f "$TOBY_PROVIDERS_FILE" ]]; then
    _toby_fail "providers file not found: $TOBY_PROVIDERS_FILE"
    return 1 2>/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Discovery helpers
# ---------------------------------------------------------------------------

# Echo known providers (one per line) from the base assistant.yaml
_toby_known_providers() {
    yq eval '.PROVIDERS | keys | .[]' "$TOBY_PROVIDERS_FILE" 2>/dev/null
}

# Echo "tag:provider" pairs derived from existing ait-* directories.
# Strategy: match the longest known-provider suffix.
_toby_existing_pairs() {
    local -a known
    mapfile -t known < <(_toby_known_providers \
        | awk '{print length, $0}' | sort -rn | cut -d' ' -f2-)

    local dir dirname suffix provider tag
    # nullglob: the loop body is skipped (instead of running once on the
    # literal pattern) when no ait-* directories exist yet.
    local _restore_nullglob=0
    shopt -q nullglob || { _restore_nullglob=1; shopt -s nullglob; }
    for dir in "$TOBY_BASE_DIR"/ait-*; do
        [[ -d "$dir" ]] || continue
        dirname="${dir##*/}"
        [[ "$dirname" == "ait-base" ]] && continue
        suffix="${dirname#ait-}"
        for provider in "${known[@]}"; do
            if [[ "$suffix" == *"-${provider}" ]]; then
                tag="${suffix%-${provider}}"
                printf '%s:%s\n' "$tag" "$provider"
                break
            fi
        done
    done
    (( _restore_nullglob )) && shopt -u nullglob
}

# Pick one item from a newline-separated list using fzf (or `select` fallback).
# $1 = prompt, stdin = candidates. Echoes selection (possibly a new value).
_toby_pick() {
    local prompt="$1"
    local -a candidates=()
    mapfile -t candidates

    if command -v fzf >/dev/null 2>&1; then
        local out query selected
        out=$(printf '%s\n' "${candidates[@]}" \
            | fzf --prompt="$prompt" --print-query --height=40% --reverse)
        [[ -z "$out" ]] && return 1
        query=$(printf '%s\n' "$out" | head -n1)
        selected=$(printf '%s\n' "$out" | tail -n +2 | head -n1)
        
        if (( TOBY_CREATE == 0 )); then
            [[ -n "$selected" ]] || return 1
            printf '%s\n' "$selected"
        else
            printf '%s\n' "${selected:-$query}"
        fi
        return 0
    fi

    # Fallback: numbered menu
    echo "$prompt" >&2
    local choice
    local -a menu_items=("${candidates[@]}")
    (( TOBY_CREATE != 0 )) && menu_items+=("<enter new>")

    select choice in "${menu_items[@]}"; do
        if [[ "$choice" == "<enter new>" ]]; then
            local new
            read -r -p "Enter new value: " new
            [[ -n "$new" ]] && { printf '%s\n' "$new"; return 0; }
        elif [[ -n "$choice" ]]; then
            printf '%s\n' "$choice"
            return 0
        fi
    done
    return 1
}

# ---------------------------------------------------------------------------
# Argument / interactive selection
# ---------------------------------------------------------------------------
TOBY_CREATE=0
while [[ "${1:-}" == -* ]]; do
    case "$1" in
        -n)
            TOBY_CREATE=1
            shift
            ;;
        -h|--help)
            echo "Usage: source toby.sh [-n] [--] [<tag> <provider>]"
            echo "  -n  create missing directory (default: select only, fail if missing)"
            return 0 2>/dev/null || exit 0
            ;;
        --)
            shift
            break
            ;;
        *)
            echo "Unknown option: $1" >&2
            return 1 2>/dev/null || exit 1
            ;;
    esac
done

case $# in
    2)
        TOBY_TAG="$1"
        TOBY_PROVIDER="$2"
        ;;
    0)
        echo "Select tag..."
        _toby_tag_prompt="Tag (or type new): "
        (( TOBY_CREATE == 0 )) && _toby_tag_prompt="Tag: "
        TOBY_TAG=$(_toby_existing_pairs | cut -d: -f1 | sort -u \
            | _toby_pick "$_toby_tag_prompt") \
            || { _toby_fail "no tag selected"; return 1 2>/dev/null || exit 1; }

        echo "Select provider..."
        if (( TOBY_CREATE == 0 )); then
            TOBY_PROVIDER=$(_toby_existing_pairs | awk -F: -v t="$TOBY_TAG" '$1 == t { print $2 }' | sort -u \
                | _toby_pick "Provider for [$TOBY_TAG]: ") \
                || { _toby_fail "no provider selected"; return 1 2>/dev/null || exit 1; }
        else
            _toby_existing_for_tag=$(_toby_existing_pairs | awk -F: -v t="$TOBY_TAG" '$1 == t { print $2 }' | sort -u)
            TOBY_PROVIDER=$( { _toby_known_providers; echo "$_toby_existing_for_tag"; } | sort -u | while read -r _toby_p; do
                [[ -z "$_toby_p" ]] && continue
                if echo "$_toby_existing_for_tag" | grep -qxF "$_toby_p"; then
                    echo "${_toby_p} ***"
                else
                    echo "$_toby_p"
                fi
            done | _toby_pick "Provider for [$TOBY_TAG] (or type new, *** exists): ") \
                || { _toby_fail "no provider selected"; unset _toby_existing_for_tag _toby_p; return 1 2>/dev/null || exit 1; }
            TOBY_PROVIDER="${TOBY_PROVIDER% ***}"
            unset _toby_existing_for_tag _toby_p
        fi
        ;;
    *)
        echo "Usage: source toby.sh [-n] [<tag> <provider>]" >&2
        return 1 2>/dev/null || exit 1
        ;;
esac

[[ -z "$TOBY_TAG"      ]] && { _toby_fail "empty tag";      return 1 2>/dev/null || exit 1; }
[[ -z "$TOBY_PROVIDER" ]] && { _toby_fail "empty provider"; return 1 2>/dev/null || exit 1; }

TELL_ME_HOME="${TOBY_BASE_DIR}/ait-${TOBY_TAG}-${TOBY_PROVIDER}"
TELL_ME_GOBIN="$(go env GOPATH)/bin"

# ---------------------------------------------------------------------------
# Validate provider against assistant.yaml (existing ait-dir or base template)
# ---------------------------------------------------------------------------
_toby_validate_provider() {
    local provider="$1"
    local config_file="$TOBY_PROVIDERS_FILE"
    [[ -f "${TELL_ME_HOME}/configs/assistant.yaml" ]] \
        && config_file="${TELL_ME_HOME}/configs/assistant.yaml"

    if [[ "$(yq eval ".PROVIDERS[\"$provider\"]" "$config_file")" == "null" ]]; then
        echo "Error: provider '$provider' not in $config_file" >&2
        echo "Available: $(yq eval '.PROVIDERS | keys | join(", ")' "$config_file")" >&2
        return 1
    fi
}

if ! _toby_validate_provider "$TOBY_PROVIDER"; then
    return 1 2>/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Provision directory from ait-base if missing
# ---------------------------------------------------------------------------
if [[ ! -d "$TELL_ME_HOME" ]]; then
    if (( TOBY_CREATE == 0 )); then
        _toby_fail "directory $TELL_ME_HOME does not exist (use -n to create)"
        return 1 2>/dev/null || exit 1
    fi
    [[ -d "$TOBY_BASE_TEMPLATE" ]] \
        || { _toby_fail "template missing: $TOBY_BASE_TEMPLATE"; return 1 2>/dev/null || exit 1; }
    echo "Creating $TELL_ME_HOME from ait-base template..."
    if ! cp -r "$TOBY_BASE_TEMPLATE" "$TELL_ME_HOME"; then
        # Roll back any half-populated tree so the next invocation is clean.
        rm -rf -- "$TELL_ME_HOME"
        _toby_fail "failed to copy template into $TELL_ME_HOME"
        return 1 2>/dev/null || exit 1
    fi
fi

# ---------------------------------------------------------------------------
# .gitignore
# ---------------------------------------------------------------------------
_toby_ensure_gitignore() {
    local f="${TELL_ME_HOME}/.gitignore"
    touch "$f"
    local entry
    for entry in 'output/' 'secrets/'; do
        grep -qxF "$entry" "$f" || echo "$entry" >> "$f"
    done
}

# ---------------------------------------------------------------------------
# YAML edit helpers (in-place via temp file)
# ---------------------------------------------------------------------------
_toby_yq_inplace() {
    # $1 = expression, $2 = file
    local expr="$1" file="$2" tmp
    tmp=$(mktemp "${file}.XXXXXX") || return 1
    if yq eval "$expr" "$file" > "$tmp"; then
        mv "$tmp" "$file"
    else
        rm -f "$tmp"
        return 1
    fi
}

# Re-target API_KEY paths from the shared ait dir into TELL_ME_HOME and
# copy any source key files across.
_toby_update_api_key_paths() {
    local config_file="$1"
    local from="$2" to="$3"
    local provider api_key new_path dest_dir rel
    local updated=0 copied=0

    echo "Updating API_KEY paths in $(basename "$config_file")..."

    local -a providers
    mapfile -t providers < <(yq eval '.PROVIDERS | keys | .[]' "$config_file")

    for provider in "${providers[@]}"; do
        api_key=$(yq eval ".PROVIDERS[\"$provider\"].API_KEY" "$config_file")
        [[ "$api_key" == "null" || -z "$api_key" ]] && continue
        # Use literal-prefix checks (no glob/pattern expansion) for safety.
        [[ "${api_key:0:${#to}}"   == "$to"   ]] && continue   # already retargeted
        [[ "${api_key:0:${#from}}" == "$from" ]] || continue   # not under shared dir

        # Slice off the literal $from prefix and graft on $to.
        new_path="${to}${api_key:${#from}}"

        # For display: strip $TOBY_BASE_DIR/ literal prefix if present.
        if [[ "${new_path:0:${#TOBY_BASE_DIR}+1}" == "$TOBY_BASE_DIR/" ]]; then
            rel="${new_path:${#TOBY_BASE_DIR}+1}"
        else
            rel="$new_path"
        fi
        echo "  $provider: $(basename "$api_key") -> $rel"

        _toby_yq_inplace ".PROVIDERS[\"$provider\"].API_KEY = \"$new_path\"" "$config_file" \
            || { echo "    Warning: failed to rewrite YAML"; continue; }
        updated=$((updated + 1))

        dest_dir=$(dirname "$new_path")
        mkdir -p "$dest_dir"
        if [[ -f "$api_key" && ! -f "$new_path" ]]; then
            cp "$api_key" "$new_path"
            copied=$((copied + 1))
        elif [[ ! -f "$api_key" ]]; then
            echo "    Warning: source key file missing: $api_key"
        fi
    done

    if (( updated == 0 )); then
        echo "  No API_KEY paths needed updating"
    else
        echo "  Updated $updated path(s), copied $copied file(s)"
    fi
}

# Set MAX_HISTORY_TOKENS = MODELS[<provider's MODEL>].CONTEXT_WINDOW
_toby_update_max_history_tokens() {
    local config_file="$1" provider="$2"
    local model context_window

    model=$(yq eval ".PROVIDERS[\"$provider\"].MODEL" "$config_file")
    if [[ "$model" == "null" ]]; then
        echo "Warning: no MODEL for provider $provider; leaving MAX_HISTORY_TOKENS"
        return 0
    fi

    context_window=$(yq eval ".MODELS[\"$model\"].CONTEXT_WINDOW" "$config_file")
    if [[ "$context_window" == "null" ]]; then
        echo "Warning: no CONTEXT_WINDOW for model $model; leaving MAX_HISTORY_TOKENS"
        return 0
    fi
    if ! [[ "$context_window" =~ ^[0-9]+$ ]]; then
        echo "Warning: CONTEXT_WINDOW for $model is not a positive integer ('$context_window'); leaving MAX_HISTORY_TOKENS" >&2
        return 0
    fi

    _toby_yq_inplace ".MAX_HISTORY_TOKENS = $context_window" "$config_file" \
        || { echo "Error: failed to update MAX_HISTORY_TOKENS" >&2; return 1; }
    echo "  MAX_HISTORY_TOKENS = $context_window (model: $model)"
}

# Apply tag/provider edits to all role configs. Runs in a subshell so a
# failure cannot leave the user's CWD changed.
_toby_modify_configs() (
    local tag="$1" provider="$2"
    local config_dir="${TELL_ME_HOME}/configs"

    [[ -d "$config_dir" ]] || { echo "Error: missing $config_dir" >&2; return 1; }
    cd "$config_dir" || return 1

    [[ -f assistant.yaml ]] || { echo "Error: missing assistant.yaml" >&2; return 1; }

    if [[ "$(yq eval ".PROVIDERS[\"$provider\"]" assistant.yaml)" == "null" ]]; then
        echo "Error: provider '$provider' not in assistant.yaml" >&2
        return 1
    fi

    _toby_yq_inplace \
        ".MODE = \"assistant-$tag\" | .SELECTED_PROVIDER = \"$provider\"" \
        assistant.yaml \
        || { echo "Error: failed to update assistant.yaml" >&2; return 1; }

    _toby_update_api_key_paths assistant.yaml "$TOBY_SHARED_AIT_DIR" "$TELL_ME_HOME"
    _toby_update_max_history_tokens assistant.yaml "$provider" \
        || echo "Warning: MAX_HISTORY_TOKENS not updated" >&2

    # Each role config is regenerated from assistant.yaml with that role's
    # MODE and its original PERSON preserved.
    local file role original_person tmp
    for file in architect.yaml coder.yaml reviewer.yaml tester.yaml; do
        [[ -f "$file" ]] || { echo "Warning: $file missing, skipping" >&2; continue; }
        role="${file%.yaml}"
        original_person=$(yq eval '.PERSON' "$file")
        tmp=$(mktemp "${file}.XXXXXX") || return 1
        if yq eval ".MODE = \"${role}-${tag}\" | .PERSON = \"$original_person\"" \
                assistant.yaml > "$tmp"; then
            mv "$tmp" "$file"
        else
            rm -f "$tmp"
            echo "Error: failed to derive $file" >&2
            return 1
        fi
    done

    echo "Configuration files updated successfully"
)

# ---------------------------------------------------------------------------
# Environment setup: keys, aliases, completion, helper functions
# ---------------------------------------------------------------------------
_toby_setup_environment() {
    local secrets_dir="${TELL_ME_HOME}/secrets"
    local keys_file="${secrets_dir}/keys"

    _toby_ensure_gitignore

    if [[ ! -d "$secrets_dir" && -d "${TOBY_BASE_TEMPLATE}/secrets" ]]; then
        echo "Copying secrets directory from template..."
        cp -r "${TOBY_BASE_TEMPLATE}/secrets" "$TELL_ME_HOME/"
    fi

    if [[ -f "$keys_file" ]]; then
        echo "Sourcing $keys_file"
        # shellcheck disable=SC1090
        source "$keys_file"
    else
        echo "Warning: keys file not found at $keys_file"
    fi

    # Aliases (single-shell scope)
    alias c-install='(GOPROXY=direct go install github.com/gosharplite/tell-me-go/cmd/tell-me-go@latest)'
    # TOBY_DEV_GO_SRC: optional local checkout of tell-me-go for `c-install-dev`.
    local dev_src="${TOBY_DEV_GO_SRC:-${HOME}/tell-me-go/cmd/tell-me-go}"
    # shellcheck disable=SC2139  # we *want* expansion at definition time
    alias c-install-dev="go install '${dev_src}'"

    local bin="${TELL_ME_GOBIN}/tell-me-go"
    if [[ -x "$bin" ]]; then
        # shellcheck disable=SC1090
        eval "$("$bin" completion bash)"
    else
        echo "Warning: tell-me-go not found at $bin"
        echo "Run 'c-install' to install it."
    fi

    # Helper functions for each role config. Quoted to survive spaces.
    # Defined via eval so the path is baked in (cleaner backtraces) and
    # so we can attach completion uniformly.
    local role cfg
    local -A _role_map=(
        [c]=coder
        [t]=tester
        [r]=reviewer
        [a]=architect
        [b]=assistant
    )
    for role in "${!_role_map[@]}"; do
        cfg="${_role_map[$role]}.yaml"
        eval "${role}() { \"\$TELL_ME_GOBIN/tell-me-go\" -c \"\$TELL_ME_HOME/configs/${cfg}\" \"\$@\"; }"
        # complete may not exist if completion failed to load
        complete -F __start_tell-me-go "$role" 2>/dev/null || true
    done
}

# ---------------------------------------------------------------------------
# Drive the workflow
# ---------------------------------------------------------------------------
export TOBY_BASE_DIR TOBY_BASE_TEMPLATE TOBY_SHARED_AIT_DIR
export TOBY_TAG TOBY_PROVIDER TELL_ME_HOME TELL_ME_GOBIN

_toby_modify_configs "$TOBY_TAG" "$TOBY_PROVIDER" \
    || echo "Warning: configuration update reported errors" >&2

_toby_setup_environment

# ---------------------------------------------------------------------------
# Prompt: replace any leading "[...] " tag, then prepend the new one.
# Uses bash regex to be precise about a literal "[tag] " prefix and to
# avoid mangling \[ \] colour escapes elsewhere in PS1.
# ---------------------------------------------------------------------------
_toby_ps1_regex='^\[(\\\[\\e\[[0-9;]*m\\\]|[^]])*\]\ (.*)$'
if [[ "$PS1" =~ $_toby_ps1_regex ]]; then
    PS1="${BASH_REMATCH[2]}"
fi

# Colors for PS1 (must be wrapped in \[ \] to avoid prompt width issues)
_toby_red="\[\e[0;31m\]"
_toby_green="\[\e[0;32m\]"
_toby_reset="\[\e[0m\]"

PS1="[${_toby_red}${TOBY_TAG}${_toby_reset}|${_toby_green}${TOBY_PROVIDER}${_toby_reset}] $PS1"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
cat <<EOF
Prompt tag    : [$TOBY_TAG|$TOBY_PROVIDER]
Provider      : $TOBY_PROVIDER
TOBY_BASE_DIR : $TOBY_BASE_DIR
TELL_ME_HOME  : $TELL_ME_HOME
TELL_ME_GOBIN : $TELL_ME_GOBIN
EOF

# ---------------------------------------------------------------------------
# Cleanup internal helpers from the user's namespace.
# Keep _toby_setup_environment side-effects (aliases, functions, exports).
# Keep TOBY_BASE_DIR / TOBY_BASE_TEMPLATE / TOBY_SHARED_AIT_DIR exported so
# the user (and child processes) can inspect / reuse them.
# ---------------------------------------------------------------------------
unset -f _toby_fail _toby_realpath _toby_known_providers _toby_existing_pairs \
         _toby_pick _toby_validate_provider _toby_ensure_gitignore \
         _toby_yq_inplace _toby_update_api_key_paths \
         _toby_update_max_history_tokens _toby_modify_configs \
         _toby_setup_environment 2>/dev/null
unset TOBY_DEFAULT_BASE_DIR TOBY_PROVIDERS_FILE
unset TOBY_CREATE _toby_tag_prompt _toby_red _toby_green _toby_reset _toby_ps1_regex
