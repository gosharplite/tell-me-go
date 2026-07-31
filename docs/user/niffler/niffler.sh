#!/usr/bin/env bash
# niffler.sh - Niffler environment manager for tell-me-go
#
# Niffler adds a GROUP layer above Flopsy's tag+provider model. Groups are
# self-contained templates under ait-base/ (actors, engineers). Each tag is
# provisioned from exactly one group.
#
# Usage:
#   source niffler.sh -n [-p] [-c] [<group> <tag> <provider>]   (provision)
#   source niffler.sh [<tag> <provider>]                         (switch inside current group)
#   source niffler.sh [<provider>]                               (switch provider, tag fixed)
#
#   -n  create/overwrite the tag workspace (only time <group> is used)
#   -p  use priority config (shared/priority Vertex headers); must be used with -n
#   -c  skip copying the docs folder; must be used with -n
#   --  end of options (subsequent args are positional)
#
# Directory structure:
#   ait-base/
#     <group>/                       ← self-contained template (actors, engineers)
#       configs/                     ← butler.yaml (template) + persona/role stubs
#       output/                      ← session data
#       secrets/                     ← API keys
#       docs/                        ← skills (engineers only, optional)
#   ait-<tag>/                       ← provisioned tag workspace
#
# Effects (in the *current* shell):
#   - Exports NIFFLER_BASE_DIR, NIFFLER_GROUP, NIFFLER_HOME, NIFFLER_TAG, NIFFLER_PROVIDER
#   - Copies ait-base/<group>/ → ait-<tag>/ if -n given
#   - Defines helper functions from configs/ (b for butler + one per persona/role)
#   - Prepends "[group/tag|provider]" to PS1

# ---------------------------------------------------------------------------
# Guard: must be sourced and only once
# ---------------------------------------------------------------------------
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    echo "Error: niffler.sh must be sourced, not executed." >&2
    echo "Run:   source ${0##*/}   (or: . ${0##*/})" >&2
    exit 1
fi

if [[ -n "${NIFFLER_SOURCED:-}" ]]; then
    if [[ -n "${NIFFLER_ORIGINAL_PS1:-}" ]]; then
        PS1="$NIFFLER_ORIGINAL_PS1"
    fi
    for _arg in "$@"; do
        if [[ "$_arg" == "-n" ]]; then
            echo "Error: -n not allowed when switching (tag is fixed)" >&2
            return 1 2>/dev/null || exit 1
        fi
    done
    NIFFLER_RESOURCING=1
    unset NIFFLER_SOURCED
    echo "Switching provider for [$NIFFLER_TAG]..." >&2
fi
export NIFFLER_SOURCED=1

if (( BASH_VERSINFO[0] < 4 )); then
    echo "Error: niffler.sh requires bash >= 4 (current: $BASH_VERSION)" >&2
    return 1 2>/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
NIFFLER_BASE_DIR="${NIFFLER_BASE_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
NIFFLER_TEMPLATE_DIR="${NIFFLER_BASE_DIR}/ait-base"

# ---------------------------------------------------------------------------
# Pre-flight: required tools
# ---------------------------------------------------------------------------
for _niffler_tool in yq go; do
    if ! command -v "$_niffler_tool" >/dev/null 2>&1; then
        echo "Error: required command not found: $_niffler_tool" >&2
        unset _niffler_tool
        return 1 2>/dev/null || exit 1
    fi
done
unset _niffler_tool

# ---------------------------------------------------------------------------
# Discovery helpers
# ---------------------------------------------------------------------------

# Groups are subdirectories of ait-base/ (actors, engineers).
_niffler_existing_groups() {
    local d
    for d in "$NIFFLER_TEMPLATE_DIR"/*/; do
        [[ -d "$d" ]] || continue
        local dirname="${d%/}"; dirname="${dirname##*/}"
        # Only list directories that contain configs/butler.yaml
        [[ -f "$d/configs/butler.yaml" ]] || continue
        printf '%s\n' "$dirname"
    done | sort -u
}

_niffler_known_providers() {
    local butler_file="${NIFFLER_TEMPLATE_DIR}/${NIFFLER_GROUP:-actors}/configs/butler.yaml"
    yq eval '.PROVIDERS | keys | .[]' "$butler_file" 2>/dev/null
}

_niffler_existing_tags() {
    local dir
    for dir in "$NIFFLER_BASE_DIR"/ait-*; do
        [[ -d "$dir" ]] || continue
        local dirname="${dir##*/}"
        [[ "$dirname" == "ait-base" ]] && continue
        printf '%s\n' "${dirname#ait-}"
    done
}

# Discover persona configs from configs/ directory.
# Returns lines in the format:  <alias>|<mode>
# Skips butler.yaml and butler-priority.yaml (butler is handled specially).
_niffler_discover_personas() {
    local configs_dir="$1"
    local file alias mode

    [[ -d "$configs_dir" ]] || return 0

    for file in "$configs_dir"/*.yaml; do
        [[ -f "$file" ]] || continue
        local basename="${file##*/}"
        local name="${basename%.yaml}"

        [[ "$name" == "butler" || "$name" == "butler-priority" ]] && continue

        alias="${name:0:1}"
        mode=$(yq eval '.MODE' "$file" 2>/dev/null)
        [[ -n "$mode" && "$mode" != "null" ]] || continue
        printf '%s|%s\n' "$alias" "$mode"
    done
}

# ---------------------------------------------------------------------------
# Parse args / interactive selection
# ---------------------------------------------------------------------------
NIFFLER_CREATE=0
NIFFLER_PRIORITY=0
NIFFLER_CLEAN=0
while [[ "${1:-}" == -* ]]; do
    case "$1" in
        -n) NIFFLER_CREATE=1; shift ;;
        -p) NIFFLER_PRIORITY=1; shift ;;
        -c) NIFFLER_CLEAN=1; shift ;;
        -h|--help)
            echo "Usage:"
            echo "  source niffler.sh -n [-p] [-c] [<group> <tag> <provider>]"
            echo "  source niffler.sh [<tag> <provider>]"
            echo "  source niffler.sh [<provider>]"
            echo ""
            echo "  -n  create/overwrite tag workspace (only time <group> is accepted)"
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

if (( NIFFLER_PRIORITY == 1 && NIFFLER_CREATE == 0 )); then
    echo "Error: -p flag must be used with -n" >&2
    return 1 2>/dev/null || exit 1
fi

if (( NIFFLER_CLEAN == 1 && NIFFLER_CREATE == 0 )); then
    echo "Error: -c flag must be used with -n" >&2
    return 1 2>/dev/null || exit 1
fi

# ── Argument routing ──────────────────────────────────────────────────────
# With -n:  3 args = group tag provider
#           2 args = group tag (provider interactive)
#           1 arg  = error
#           0 args = interactive (group → tag → provider)
#
# Without -n (or re-sourcing):
#           3 args = error (group only with -n)
#           2 args = tag provider
#           1 arg  = provider only
#           0 args = interactive (skip group picker)
# ────────────────────────────────────────────────────────────────────────────

if (( NIFFLER_CREATE == 1 )); then
    # ── Provisioning mode: group is required ───────────────────────────────
    case $# in
        3)
            NIFFLER_GROUP="$1"
            NIFFLER_TAG="$2"
            NIFFLER_PROVIDER="$3"
            ;;
        2)
            NIFFLER_GROUP="$1"
            NIFFLER_TAG="$2"
            NIFFLER_PROVIDER=$(_niffler_known_providers | sort -u | fzf --prompt="Provider for [$NIFFLER_GROUP/$NIFFLER_TAG]: " --height=40% --reverse)
            [[ -z "$NIFFLER_PROVIDER" ]] && { echo "Error: no provider selected" >&2; return 1 2>/dev/null || exit 1; }
            ;;
        1)
            echo "Error: with -n, provide at least <group> <tag> (got only group)" >&2
            echo "Usage: source niffler.sh -n <group> <tag> [<provider>]" >&2
            return 1 2>/dev/null || exit 1
            ;;
        0)
            if ! command -v fzf >/dev/null 2>&1; then
                echo "Error: fzf is required for interactive selection" >&2
                echo "Usage: source niffler.sh -n <group> <tag> [<provider>]" >&2
                return 1 2>/dev/null || exit 1
            fi

            # Step 1: pick group
            _groups=$(_niffler_existing_groups)
            if [[ -z "$_groups" ]]; then
                echo "Error: no groups found in ait-base/" >&2
                return 1 2>/dev/null || exit 1
            fi
            NIFFLER_GROUP=$(printf '%s\n' "$_groups" | fzf --prompt="Group: " --height=40% --reverse)
            [[ -z "$NIFFLER_GROUP" ]] && { echo "Error: no group selected" >&2; return 1 2>/dev/null || exit 1; }

            # Step 2: pick tag
            if _niffler_existing_tags | grep -q . 2>/dev/null; then
                _tag_out=$(_niffler_existing_tags | sort -u | fzf --prompt="Tag in [$NIFFLER_GROUP] (or type new): " --print-query --height=40% --reverse)
            else
                _tag_out=$(fzf --prompt="Tag (type name): " --print-query --height=40% --reverse < /dev/null)
            fi
            [[ -z "$_tag_out" ]] && { echo "Error: no tag selected" >&2; return 1 2>/dev/null || exit 1; }
            _tquery=$(printf '%s\n' "$_tag_out" | head -n1)
            _tselected=$(printf '%s\n' "$_tag_out" | tail -n +2 | head -n1)
            NIFFLER_TAG="${_tselected:-$_tquery}"

            # Step 3: pick provider
            NIFFLER_PROVIDER=$(_niffler_known_providers | sort -u | fzf --prompt="Provider for [$NIFFLER_GROUP/$NIFFLER_TAG]: " --height=40% --reverse)
            [[ -z "$NIFFLER_PROVIDER" ]] && { echo "Error: no provider selected" >&2; return 1 2>/dev/null || exit 1; }
            ;;
        *)
            echo "Usage: source niffler.sh -n <group> <tag> [<provider>]" >&2
            return 1 2>/dev/null || exit 1
            ;;
    esac
else
    # ── Non-provisioning mode: group is irrelevant ─────────────────────────
    if [[ -n "${NIFFLER_RESOURCING:-}" ]]; then
        case $# in
            3) echo "Error: too many arguments for provider switch" >&2; return 1 2>/dev/null || exit 1 ;;
            2) NIFFLER_TAG="$1"; NIFFLER_PROVIDER="$2" ;;
            1) NIFFLER_PROVIDER="$1" ;;
            0)
                NIFFLER_PROVIDER=$(_niffler_known_providers | sort -u | fzf --prompt="Provider for [$NIFFLER_GROUP/$NIFFLER_TAG]: " --height=40% --reverse)
                [[ -z "$NIFFLER_PROVIDER" ]] && { echo "Error: no provider selected" >&2; return 1 2>/dev/null || exit 1; }
                ;;
            *) echo "Usage: source niffler.sh [<tag> <provider>] or [<provider>]" >&2; return 1 2>/dev/null || exit 1 ;;
        esac
    else
        case $# in
            3) echo "Error: <group> is only used with -n. Try: source niffler.sh <tag> <provider>" >&2; return 1 2>/dev/null || exit 1 ;;
            2) NIFFLER_TAG="$1"; NIFFLER_PROVIDER="$2" ;;
            1) NIFFLER_PROVIDER="$1" ;;
            0)
                if ! command -v fzf >/dev/null 2>&1; then
                    echo "Error: fzf is required for interactive selection" >&2
                    echo "Usage: source niffler.sh <tag> <provider>" >&2
                    return 1 2>/dev/null || exit 1
                fi

                # Tags are unique — just pick tag, then provider. No group needed.
                _tag_out=$(_niffler_existing_tags | sort -u | fzf --prompt="Tag: " --height=40% --reverse)
                [[ -z "$_tag_out" ]] && { echo "Error: no tag selected" >&2; return 1 2>/dev/null || exit 1; }
                NIFFLER_TAG="$_tag_out"

                NIFFLER_PROVIDER=$(_niffler_known_providers | sort -u | fzf --prompt="Provider for [$NIFFLER_TAG]: " --height=40% --reverse)
                [[ -z "$NIFFLER_PROVIDER" ]] && { echo "Error: no provider selected" >&2; return 1 2>/dev/null || exit 1; }
                ;;
            *) echo "Usage: source niffler.sh [<tag> <provider>] or [<provider>]" >&2; return 1 2>/dev/null || exit 1 ;;
        esac
    fi
fi

[[ -z "$NIFFLER_TAG"      ]] && { echo "Error: empty tag" >&2;      return 1 2>/dev/null || exit 1; }
[[ "$NIFFLER_TAG" == "base" ]] && { echo "Error: 'base' is reserved (ait-base is the template)" >&2; return 1 2>/dev/null || exit 1; }
[[ -z "$NIFFLER_PROVIDER" ]] && { echo "Error: empty provider" >&2; return 1 2>/dev/null || exit 1; }

NIFFLER_HOME="${NIFFLER_BASE_DIR}/ait-${NIFFLER_TAG}"

# Validate provider against workspace butler (non-provisioning) or template butler (provisioning)
if (( NIFFLER_CREATE == 1 )); then
    [[ -z "$NIFFLER_GROUP" ]] && { echo "Error: empty group" >&2; return 1 2>/dev/null || exit 1; }
    NIFFLER_GROUP_TEMPLATE="${NIFFLER_TEMPLATE_DIR}/${NIFFLER_GROUP}"
    if [[ ! -d "$NIFFLER_GROUP_TEMPLATE" ]]; then
        echo "Error: group template not found: $NIFFLER_GROUP_TEMPLATE" >&2
        return 1 2>/dev/null || exit 1
    fi
    _niffler_butler="${NIFFLER_GROUP_TEMPLATE}/configs/butler.yaml"
else
    _niffler_butler="${NIFFLER_HOME}/configs/butler.yaml"
fi

if [[ "$(yq eval ".PROVIDERS[\"$NIFFLER_PROVIDER\"]" "$_niffler_butler")" == "null" ]]; then
    echo "Error: provider '$NIFFLER_PROVIDER' not found" >&2
    echo "Available: $(yq eval '.PROVIDERS | keys | join(", ")' "$_niffler_butler")" >&2
    return 1 2>/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Provision tag workspace from group template
# ---------------------------------------------------------------------------
if (( NIFFLER_CREATE == 1 )); then
    if [[ -d "$NIFFLER_HOME" ]]; then
        echo "Overwriting $NIFFLER_HOME from $NIFFLER_GROUP template..."
        rm -rf -- "$NIFFLER_HOME"
    else
        echo "Creating $NIFFLER_HOME from $NIFFLER_GROUP template..."
    fi

    if ! cp -r "$NIFFLER_GROUP_TEMPLATE" "$NIFFLER_HOME"; then
        rm -rf -- "$NIFFLER_HOME"
        echo "Error: failed to copy template into $NIFFLER_HOME" >&2
        return 1 2>/dev/null || exit 1
    fi

    # ── Validate personas (non-butler configs) ─────────────────────────────
    _niffler_configs_dir="${NIFFLER_HOME}/configs"
    if [[ -d "$_niffler_configs_dir" ]]; then
        _niffler_seen_aliases=("b")  # 'b' is reserved for butler
        for _var_file in "$_niffler_configs_dir"/*.yaml; do
            [[ -f "$_var_file" ]] || continue
            _var_basename="${_var_file##*/}"
            _var_name="${_var_basename%.yaml}"

            [[ "$_var_name" == "butler" || "$_var_name" == "butler-priority" ]] && continue

            _var_alias="${_var_name:0:1}"

            if [[ "$_var_alias" == "b" ]]; then
                echo "Error: '$_var_name' has first letter 'b', which is reserved for butler" >&2
                rm -rf -- "$NIFFLER_HOME"
                return 1 2>/dev/null || exit 1
            fi

            for _seen in "${_niffler_seen_aliases[@]}"; do
                if [[ "$_seen" == "$_var_alias" ]]; then
                    echo "Error: duplicate first letter '$_var_alias' — names must have unique first letters" >&2
                    rm -rf -- "$NIFFLER_HOME"
                    return 1 2>/dev/null || exit 1
                fi
            done
            _niffler_seen_aliases+=("$_var_alias")
        done
    fi

    # ── Merge persona stubs into full configs (using butler as template) ───
    _niffler_butler_source="${NIFFLER_HOME}/configs/butler.yaml"
    if (( NIFFLER_PRIORITY == 1 )) && [[ -f "${NIFFLER_HOME}/configs/butler-priority.yaml" ]]; then
        _niffler_butler_source="${NIFFLER_HOME}/configs/butler-priority.yaml"
    fi

    if [[ -d "$_niffler_configs_dir" ]]; then
        echo "Generating configs..."
        for _var_file in "$_niffler_configs_dir"/*.yaml; do
            [[ -f "$_var_file" ]] || continue
            _var_basename="${_var_file##*/}"
            _var_name="${_var_basename%.yaml}"

            [[ "$_var_name" == "butler" || "$_var_name" == "butler-priority" ]] && continue

            _var_mode=$(yq eval '.MODE' "$_var_file")
            _var_person=$(yq eval '.PERSON' "$_var_file")

            _var_out="${NIFFLER_HOME}/configs/${_var_name}.yaml"
            yq eval ".MODE = \"${_var_mode}\" | .PERSON = \"${_var_person}\"" \
                "$_niffler_butler_source" > "$_var_out" \
                || echo "Warning: failed to generate ${_var_name}.yaml" >&2
        done
    fi

    # If non-priority, remove unused priority config
    if (( NIFFLER_PRIORITY == 0 )); then
        rm -f "${NIFFLER_HOME}/configs/butler-priority.yaml"
    fi

    # .gitignore
    cat > "${NIFFLER_HOME}/.gitignore" <<'GITIGNORE'
output/
secrets/
GITIGNORE

    if (( NIFFLER_CLEAN == 1 )); then
        rm -rf -- "${NIFFLER_HOME}/docs"
    fi
elif [[ ! -d "$NIFFLER_HOME" ]]; then
    echo "Error: $NIFFLER_HOME does not exist (use -n to create)" >&2
    return 1 2>/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Source secrets
# ---------------------------------------------------------------------------
_niffler_keys_file="${NIFFLER_HOME}/secrets/keys"
if [[ -f "$_niffler_keys_file" ]]; then
    echo "Sourcing $_niffler_keys_file"
    source "$_niffler_keys_file"
else
    echo "Warning: keys file not found at $_niffler_keys_file" >&2
fi

# ---------------------------------------------------------------------------
# Exports
# ---------------------------------------------------------------------------
export NIFFLER_BASE_DIR NIFFLER_GROUP NIFFLER_HOME NIFFLER_TAG NIFFLER_PROVIDER

# ---------------------------------------------------------------------------
# Bash completion
# ---------------------------------------------------------------------------
NIFFLER_GOBIN="$(go env GOPATH)/bin"
export NIFFLER_GOBIN

if [[ -x "$NIFFLER_GOBIN/tell-me-go" ]]; then
    eval "$("$NIFFLER_GOBIN/tell-me-go" completion bash)"
fi

# ---------------------------------------------------------------------------
# Install alias
# ---------------------------------------------------------------------------
alias c-install='(GOPROXY=direct go install github.com/gosharplite/tell-me-go/cmd/tell-me-go@latest)'

# ---------------------------------------------------------------------------
# Helper functions
# ---------------------------------------------------------------------------

_niffler_run() {
    local role="$1" config="$2" provider="$3"
    shift 3

    TELL_ME_MODE="${role}" \
    TELL_ME_SELECTED_PROVIDER="$provider" \
    TELL_ME_HOME="$NIFFLER_HOME" \
    "$NIFFLER_GOBIN/tell-me-go" -c "$config" "$@"
}

# b (butler) — always available
b() { _niffler_run butler "${NIFFLER_HOME}/configs/butler.yaml" "$NIFFLER_PROVIDER" "$@"; }
complete -F __start_tell-me-go b 2>/dev/null || true

# ── Define shell functions for all personas/roles ──────────────────────────
_niffler_configs_dir="${NIFFLER_HOME}/configs"
_niffler_commands="b:butler"

if [[ -d "$_niffler_configs_dir" ]]; then
    _niffler_seen_aliases=("b")
    _niffler_persona_entries=()

    while IFS='|' read -r _valias _vmode; do
        [[ -z "$_valias" ]] && continue

        if [[ "$_valias" == "b" ]]; then
            echo "Warning: alias 'b' conflicts with butler, skipping" >&2
            continue
        fi

        for _seen in "${_niffler_seen_aliases[@]}"; do
            if [[ "$_seen" == "$_valias" ]]; then
                echo "Warning: duplicate alias '$_valias', only the first will be used" >&2
                continue 2
            fi
        done

        _niffler_seen_aliases+=("$_valias")
        _niffler_persona_entries+=("${_valias}|${_vmode}")
    done < <(_niffler_discover_personas "$_niffler_configs_dir")

    for _entry in "${_niffler_persona_entries[@]}"; do
        _valias="${_entry%%|*}"
        _vmode="${_entry##*|}"

        eval "${_valias}() { _niffler_run ${_vmode} \"${NIFFLER_HOME}/configs/${_vmode}.yaml\" \"\$NIFFLER_PROVIDER\" \"\$@\"; }"
        complete -F __start_tell-me-go "$_valias" 2>/dev/null || true

        _niffler_commands+=", ${_valias}:${_vmode}"
    done
fi

# ---------------------------------------------------------------------------
# Prompt
# ---------------------------------------------------------------------------
_niffler_green="\[\e[0;32m\]"
_niffler_cyan="\[\e[0;36m\]"
_niffler_yellow="\[\e[0;33m\]"
_niffler_reset="\[\e[0m\]"

_niffler_stripped_ps1="${PS1#\[*|*\] }"
_niffler_stripped_ps1="${_niffler_stripped_ps1#\[*/*|*\] }"
if [[ -z "${NIFFLER_ORIGINAL_PS1:-}" ]]; then
    NIFFLER_ORIGINAL_PS1="$_niffler_stripped_ps1"
fi

PS1="[${_niffler_green}${NIFFLER_TAG}${_niffler_reset}|${_niffler_cyan}${NIFFLER_PROVIDER}${_niffler_reset}] $NIFFLER_ORIGINAL_PS1"
unset _niffler_stripped_ps1

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
cat <<EOF
Group        : $NIFFLER_GROUP
Tag          : $NIFFLER_TAG
Provider     : $NIFFLER_PROVIDER
NIFFLER_HOME : $NIFFLER_HOME

Commands:
EOF

printf '  %s "prompt"        %s\n' "b" "butler (assistant)"

if [[ -d "$_niffler_configs_dir" ]]; then
    while IFS='|' read -r _valias _vmode; do
        [[ -z "$_valias" ]] && continue
        printf '  %s "prompt"        %s\n' "$_valias" "$_vmode"
    done < <(_niffler_discover_personas "$_niffler_configs_dir")
fi

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------
unset -f _niffler_known_providers _niffler_existing_groups _niffler_existing_tags _niffler_discover_personas 2>/dev/null
unset _niffler_green _niffler_cyan _niffler_reset _niffler_tool _niffler_keys_file NIFFLER_RESOURCING
unset _niffler_configs_dir _niffler_butler_source _niffler_butler
unset _niffler_persona_entries _niffler_seen_aliases _niffler_commands
unset _valias _vmode _var_file _var_basename _var_name _var_mode _var_person _var_out _entry _seen _arg
unset _groups _tag_out _tquery _tselected
unset NIFFLER_CREATE NIFFLER_PRIORITY NIFFLER_CLEAN
