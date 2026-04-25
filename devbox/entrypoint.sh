#!/bin/bash
# Minion devbox entrypoint
# Clones repo, starts OpenCode serve, waits for readiness, creates session, sends task
# The agent autonomously completes the task including creating commits and PRs
#
# Required environment variables:
#   GITHUB_TOKEN            - GitHub token for cloning and API access
#   MINION_REPO             - Repository to clone (owner/repo format)
#   MINION_ID               - Unique minion identifier
#   ORCHESTRATOR_URL        - Callback URL for the orchestrator
#   INTERNAL_API_TOKEN      - Token for authenticating with orchestrator
#   OPENCODE_MODEL          - Model used by OpenCode config (from DEFAULT_MODEL)
#
# Required mounted files:
#   /task/task.txt          - Task description to send to OpenCode (via ConfigMap)
#
# Optional environment variables:
#   OPENCODE_PORT           - Port for OpenCode serve (default: 4096)
#   TASK_TIMEOUT            - Maximum task execution time in seconds (default: 1800 = 30 min)
#   MINION_BRANCH           - Target branch to clone (for PR feedback flow)
#
# Orchestrator receives callbacks at:
#   POST $ORCHESTRATOR_URL/api/minions/$MINION_ID/callback
#

set -euo pipefail

# Ensure PATH includes user bin directories (defensive for subshells)
export PATH="/home/minion/.local/bin:/opt/opencode/bin:${PATH}"

# Config
OPENCODE_PORT="${OPENCODE_PORT:-4096}"
OPENCODE_BASE="http://127.0.0.1:${OPENCODE_PORT}"
HEALTH_ENDPOINT="${OPENCODE_BASE}/global/health"
SESSION_ENDPOINT="${OPENCODE_BASE}/session"
HEALTH_TIMEOUT=60
HEALTH_INTERVAL=2
TASK_TIMEOUT="${TASK_TIMEOUT:-1800}"  # 30 minutes default
TASK_FILE="/task/task.txt"

# Logging helpers
log() {
    echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] $*" >&2
}

die() {
    log "FATAL: $*"
    exit 1
}

# Copy OpenCode config from image to writable home directory
# Required because /home/minion is mounted as emptyDir for writable home
setup_config() {
    log "Setting up OpenCode configuration"
    mkdir -p ~/.config/opencode
    # Copy opencode config files (skip containers directory)
    for item in /etc/opencode/*; do
        if [[ "$(basename "$item")" != "containers" ]]; then
            cp -r "$item" ~/.config/opencode/
        fi
    done

    # Symlink auth.json if mounted (for LLM provider authentication)
    # Uses symlink so token refreshes write back to the PVC
    if [[ -f /etc/opencode-share/auth.json ]]; then
        mkdir -p ~/.local/share/opencode
        ln -sf /etc/opencode-share/auth.json ~/.local/share/opencode/auth.json
        log "Linked auth.json from PVC to OpenCode data directory"
    fi

    # Set up containers configuration for Buildah/Skopeo (rootless container builds)
    # These tools need config files in ~/.config/containers/
    if [[ -d /etc/opencode/containers ]]; then
        log "Setting up containers configuration for Buildah/Skopeo"
        mkdir -p /etc/containers
        cp -r /etc/opencode/containers/* /etc/containers/
        
        # Create required directories for rootless container storage
        # - /run/containers for runtime state (mounted as emptyDir in pod spec)
        # - /var/lib/containers/storage for image/container storage
        mkdir -p /run/containers
        mkdir -p /var/lib/containers/storage
        log "Containers configuration ready for rootless builds"
    fi
}

# Validate required environment variables
validate_env() {
    local missing=()
    [[ -z "${GITHUB_TOKEN:-}" ]] && missing+=("GITHUB_TOKEN")
    [[ -z "${MINION_REPO:-}" ]] && missing+=("MINION_REPO")
    [[ -z "${MINION_ID:-}" ]] && missing+=("MINION_ID")
    [[ -z "${ORCHESTRATOR_URL:-}" ]] && missing+=("ORCHESTRATOR_URL")
    [[ -z "${INTERNAL_API_TOKEN:-}" ]] && missing+=("INTERNAL_API_TOKEN")
    [[ -z "${OPENCODE_MODEL:-}" ]] && missing+=("OPENCODE_MODEL")

    if [[ ${#missing[@]} -gt 0 ]]; then
        die "Missing required environment variables: ${missing[*]}"
    fi

    # Validate task file exists
    if [[ ! -f "$TASK_FILE" ]]; then
        die "Task file not found: ${TASK_FILE}"
    fi
}

# Clone repository using GITHUB_TOKEN
clone_repo() {
    log "Cloning repository: ${MINION_REPO}"

    # Configure git to use token for authentication
    # This approach is safer than embedding in URL (token not leaked in logs/errors)
    git config --global credential.helper store
    echo "https://x-access-token:${GITHUB_TOKEN}@github.com" > ~/.git-credentials
    chmod 600 ~/.git-credentials

    # Configure git user for commits (agent will use these)
    git config --global user.email "minion@imdevinc.com"
    git config --global user.name "Minion"

    # Clone with depth=1 for speed (full history not needed for most tasks)
    # If MINION_BRANCH is set, clone that specific branch (PR feedback flow)
    local clone_args=("--depth=1")
    if [[ -n "${MINION_BRANCH:-}" ]]; then
        log "Cloning specific branch for PR feedback flow: ${MINION_BRANCH}"
        clone_args+=("--branch" "${MINION_BRANCH}")
        # Export flag so agent knows this is a PR feedback scenario
        # Agent should push to existing branch, not create new PR
        export MINION_PR_FEEDBACK=1
    fi

    if ! git clone "${clone_args[@]}" "https://github.com/${MINION_REPO}.git" /workspace/repo 2>&1; then
        die "Failed to clone repository"
    fi

    log "Repository cloned successfully"
    cd /workspace/repo
}

# Upgrade OpenCode to latest version
upgrade_opencode() {
    log "Upgrading OpenCode to latest version"
    
    if opencode upgrade --method curl 2>&1 | tee /tmp/opencode-upgrade.log; then
        log "OpenCode upgrade completed successfully"
    else
        log "OpenCode upgrade failed, continuing with existing version"
        log "Upgrade logs:"
        cat /tmp/opencode-upgrade.log >&2 || true
    fi
}

# Start OpenCode serve in background
start_opencode() {
    log "Starting OpenCode serve on port ${OPENCODE_PORT}"

    # Run opencode serve in background, redirect output to log file
    opencode serve --port "${OPENCODE_PORT}" --hostname "0.0.0.0" > /tmp/opencode.log 2>&1 &
    OPENCODE_PID=$!

    log "OpenCode started with PID ${OPENCODE_PID}"
}

# Wait for OpenCode to be ready
wait_for_health() {
    log "Waiting for OpenCode health endpoint (timeout: ${HEALTH_TIMEOUT}s)"

    local elapsed=0
    while [[ $elapsed -lt $HEALTH_TIMEOUT ]]; do
        if curl -sf "${HEALTH_ENDPOINT}" > /dev/null 2>&1; then
            log "OpenCode is ready"
            return 0
        fi
        sleep "${HEALTH_INTERVAL}"
        elapsed=$((elapsed + HEALTH_INTERVAL))
    done

    # Dump logs for debugging
    log "OpenCode logs:"
    cat /tmp/opencode.log >&2 || true

    die "OpenCode health check timed out after ${HEALTH_TIMEOUT}s"
}

# Create a new session
create_session() {
    log "Creating new session"

    local response
    response=$(curl -sf -X POST "${SESSION_ENDPOINT}" \
        -H "Content-Type: application/json" \
        -d '{}')

    SESSION_ID=$(echo "$response" | jq -r '.id')

    if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
        log "Session creation response: $response"
        die "Failed to create session: no session ID returned"
    fi

    log "Session created: ${SESSION_ID}"
}

# Send task to OpenCode using prompt_async
# We use prompt_async because the task may take a long time to complete
# and we need to stream events (handled by orchestrator's SSE client)
send_task() {
    log "Sending task to session ${SESSION_ID}"

    # Read task content from mounted ConfigMap file
    local task_content
    task_content=$(cat "$TASK_FILE")

    # Build the message parts using jq for safe JSON construction
    # This prevents shell injection attacks from task content
    local request_body
    request_body=$(jq -n \
        --arg task "$task_content" \
        '{
            parts: [
                {
                    type: "text",
                    text: $task
                }
            ]
        }')
    log "Sending to ${SESSION_ENDPOINT}/${SESSION_ID}/prompt_async request body: $request_body"

    local http_code
    http_code=$(curl -sf -X POST "${SESSION_ENDPOINT}/${SESSION_ID}/prompt_async" \
        -H "Content-Type: application/json" \
        -d "$request_body" \
        -w "%{http_code}" \
        -o /dev/null)

    if [[ "$http_code" != "204" ]]; then
        die "Failed to send task: HTTP ${http_code}"
    fi

    log "Task sent successfully"
}

# Wait for session to complete by polling status
# Includes timeout and process monitoring for robustness
# Returns:
#   0 - Task completed successfully (session idle)
#   1 - Task failed (session error/failed status)
#   2 - Timeout exceeded
#   3 - OpenCode process died
wait_for_completion() {
    log "Waiting for task completion (timeout: ${TASK_TIMEOUT}s)"

    local elapsed=0
    local poll_interval=5

    # Give OpenCode time to start processing the async task before first poll
    sleep 2
    elapsed=2

    while [[ $elapsed -lt $TASK_TIMEOUT ]]; do
        # Check if OpenCode process is still running
        if ! kill -0 "$OPENCODE_PID" 2>/dev/null; then
            log "OpenCode process died (PID $OPENCODE_PID)"
            log "OpenCode logs:"
            cat /tmp/opencode.log >&2 || true
            return 3
        fi

        local status_response
        local session_status="unknown"

        # OpenCode returns {} when all sessions are idle.
        # Treat that as completion when the status endpoint call itself succeeds.
        if status_response=$(curl -sf "${SESSION_ENDPOINT}/status"); then
            log "Session status response: $status_response"
            session_status=$(echo "$status_response" | jq -r --arg id "$SESSION_ID" '
                if type == "object" and length == 0 then
                    "idle"
                else
                    .[$id] // "unknown"
                end
            ')
        fi

        case "$session_status" in
            "idle")
                log "Task completed (session idle)"
                log "OpenCode logs:"
                cat /tmp/opencode.log >&2 || true
                return 0
                ;;
            "busy"|"running"|"pending")
                # Still working, continue polling
                ;;
            "error"|"failed")
                log "Task failed: session status is ${session_status}"
                return 1
                ;;
            *)
                # Unknown status, keep polling (might be transient)
                ;;
        esac

        sleep "$poll_interval"
        elapsed=$((elapsed + poll_interval))
    done

    log "Task timed out after ${TASK_TIMEOUT}s"
    return 2
}

# Check if there are any uncommitted changes
has_uncommitted_changes() {
    # Check for staged, unstaged, or untracked files
    [[ -n "$(git status --porcelain)" ]]
}

# Check if we're on a non-default branch (agent created a branch)
is_on_feature_branch() {
    local current_branch
    current_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
    
    # Check if not on main, master, or detached HEAD
    [[ -n "$current_branch" && "$current_branch" != "main" && "$current_branch" != "master" && "$current_branch" != "HEAD" ]]
}

# Detect PR URL created by the agent
# Uses gh CLI to find PR for current branch
# Retries multiple times to handle GitHub API propagation delay
detect_pr_url() {
    log "Detecting PR URL for current branch"

    local current_branch
    current_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
    if [[ -z "$current_branch" || "$current_branch" == "HEAD" ]]; then
        log "Unable to detect current branch for PR lookup"
        return 1
    fi

    local pr_url=""
    local max_attempts=5
    local attempt=1
    local delay=2  # seconds between attempts

    while [[ $attempt -le $max_attempts ]]; do
        log "PR detection attempt ${attempt}/${max_attempts}"

        # Strategy 1: inspect PR associated with the checked-out branch.
        # `gh pr view HEAD` looks up a branch literally named "HEAD", which fails.
        pr_url=$(gh pr view --json url -q '.url' 2>/dev/null || echo "")
        if [[ -n "$pr_url" && "$pr_url" != "null" ]]; then
            log "Found PR via current branch context: ${pr_url}"
            echo "$pr_url"
            return 0
        fi

        # Strategy 2: inspect PR by explicit branch name.
        pr_url=$(gh pr view "$current_branch" --json url -q '.url' 2>/dev/null || echo "")
        if [[ -n "$pr_url" && "$pr_url" != "null" ]]; then
            log "Found PR via branch lookup (${current_branch}): ${pr_url}"
            echo "$pr_url"
            return 0
        fi

        # Strategy 3: fall back to listing PRs by head branch.
        pr_url=$(gh pr list --state all --head "$current_branch" --limit 1 --json url -q '.[0].url' 2>/dev/null || echo "")
        if [[ -n "$pr_url" && "$pr_url" != "null" ]]; then
            log "Found PR via head-branch search (${current_branch}): ${pr_url}"
            echo "$pr_url"
            return 0
        fi

        if [[ $attempt -lt $max_attempts ]]; then
            log "No PR found yet, retrying in ${delay}s..."
            sleep "$delay"
        fi

        attempt=$((attempt + 1))
    done

    log "No PR found for current branch after ${max_attempts} attempts"
    return 1
}

# Callback retry configuration
CALLBACK_MAX_RETRIES=5
CALLBACK_INITIAL_BACKOFF=1  # seconds
CALLBACK_MAX_BACKOFF=60     # seconds

# Send callback to orchestrator with exponential backoff retry
# Args: status, pr_url (optional), error_msg (optional), result_type (optional)
# Result types: "no_changes", "partial_work", "timeout", "process_error"
# On failure after all retries, returns non-zero
send_callback() {
    local status="$1"
    local pr_url="${2:-}"
    local error_msg="${3:-}"
    local result_type="${4:-}"

    local callback_url="${ORCHESTRATOR_URL}/api/minions/${MINION_ID}/callback"
    log "Sending callback to orchestrator: status=${status}${result_type:+ result_type=${result_type}}"

    local payload
    payload=$(jq -n \
        --arg status "$status" \
        --arg pr_url "$pr_url" \
        --arg error "$error_msg" \
        --arg session_id "${SESSION_ID:-}" \
        --arg result_type "$result_type" \
        '{
            status: $status,
            session_id: $session_id
        } + (if $pr_url != "" then {pr_url: $pr_url} else {} end)
          + (if $error != "" then {error: $error} else {} end)
          + (if $result_type != "" then {result_type: $result_type} else {} end)')

    local attempt=1
    local backoff=$CALLBACK_INITIAL_BACKOFF

    while [[ $attempt -le $CALLBACK_MAX_RETRIES ]]; do
        log "Callback attempt ${attempt}/${CALLBACK_MAX_RETRIES}"

        local http_code
        # Capture both curl exit code and HTTP status
        http_code=$(curl -s -X POST "${callback_url}" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${INTERNAL_API_TOKEN:-}" \
            -d "$payload" \
            -w "%{http_code}" \
            -o /dev/null 2>/dev/null) || http_code="000"

        if [[ "$http_code" -ge 200 && "$http_code" -lt 300 ]]; then
            log "Callback sent successfully"
            return 0
        fi

        log "Callback failed: HTTP ${http_code}"

        if [[ $attempt -lt $CALLBACK_MAX_RETRIES ]]; then
            log "Retrying in ${backoff}s..."
            sleep "$backoff"
            # Exponential backoff with cap
            backoff=$((backoff * 2))
            if [[ $backoff -gt $CALLBACK_MAX_BACKOFF ]]; then
                backoff=$CALLBACK_MAX_BACKOFF
            fi
        fi

        attempt=$((attempt + 1))
    done

    log "Callback failed after ${CALLBACK_MAX_RETRIES} attempts"
    return 1
}

# Handle task completion - detect what the agent did and report appropriately
handle_completion() {
    local completion_status=$1
    
    case $completion_status in
        0)
            # Task completed successfully - check what agent accomplished
            log "Task completed, checking results"
            
            local pr_url=""
            if pr_url=$(detect_pr_url); then
                # Agent created a PR - success!
                log "Agent created PR successfully"
                if ! send_callback "completed" "$pr_url"; then
                    log "FATAL: Failed to notify orchestrator after ${CALLBACK_MAX_RETRIES} attempts"
                    exit 1
                fi
            elif is_on_feature_branch; then
                # Agent created a branch but no PR - partial work
                log "Agent created branch but no PR found"
                if has_uncommitted_changes; then
                    if ! send_callback "failed" "" "Agent created branch with uncommitted changes but did not create PR" "partial_work"; then
                        log "FATAL: Failed to notify orchestrator after ${CALLBACK_MAX_RETRIES} attempts"
                        exit 1
                    fi
                else
                    if ! send_callback "failed" "" "Agent created branch and committed but did not create PR" "partial_work"; then
                        log "FATAL: Failed to notify orchestrator after ${CALLBACK_MAX_RETRIES} attempts"
                        exit 1
                    fi
                fi
                exit 1
            elif has_uncommitted_changes; then
                # Agent made changes but didn't commit or create PR
                log "Agent made changes but did not commit or create PR"
                if ! send_callback "failed" "" "Agent made file changes but did not commit or create PR" "partial_work"; then
                    log "FATAL: Failed to notify orchestrator after ${CALLBACK_MAX_RETRIES} attempts"
                    exit 1
                fi
                exit 1
            else
                # No changes at all - task completed with nothing to do
                log "No changes detected - task completed with no modifications needed"
                if ! send_callback "completed" "" "" "no_changes"; then
                    log "FATAL: Failed to notify orchestrator after ${CALLBACK_MAX_RETRIES} attempts"
                    exit 1
                fi
            fi
            ;;
        1)
            # Task failed (session error)
            log "Task execution failed"
            if ! send_callback "failed" "" "Task execution failed"; then
                log "FATAL: Failed to notify orchestrator after ${CALLBACK_MAX_RETRIES} attempts"
                exit 1
            fi
            exit 1
            ;;
        2)
            # Task timed out
            log "Task timed out after ${TASK_TIMEOUT}s"
            # Kill OpenCode process to stop any ongoing work
            # Best-effort: suppress errors (process may have already exited)
            kill "$OPENCODE_PID" 2>/dev/null || true
            # Check if there's partial work
            if pr_url=$(detect_pr_url 2>/dev/null); then
                # Agent managed to create PR before timeout - treat as success
                log "PR was created before timeout"
                if ! send_callback "completed" "$pr_url"; then
                    log "FATAL: Failed to notify orchestrator after ${CALLBACK_MAX_RETRIES} attempts"
                    exit 1
                fi
            else
                if ! send_callback "failed" "" "Task execution timed out after ${TASK_TIMEOUT}s" "timeout"; then
                    log "FATAL: Failed to notify orchestrator after ${CALLBACK_MAX_RETRIES} attempts"
                    exit 1
                fi
            fi
            # Timeout is a handled terminal condition for the pod lifecycle.
            # Exit 0 so Kubernetes marks the pod as Succeeded, then orchestrator
            # performs delayed cleanup of terminal pods.
            exit 0
            ;;
        3)
            # OpenCode process died
            log "OpenCode process died unexpectedly"
            if ! send_callback "failed" "" "OpenCode process died during task execution" "process_error"; then
                log "FATAL: Failed to notify orchestrator after ${CALLBACK_MAX_RETRIES} attempts"
                exit 1
            fi
            exit 1
            ;;
        *)
            # Unknown error
            log "Unknown completion status: ${completion_status}"
            if ! send_callback "failed" "" "Unknown error during task execution"; then
                log "FATAL: Failed to notify orchestrator after ${CALLBACK_MAX_RETRIES} attempts"
                exit 1
            fi
            exit 1
            ;;
    esac
}

# Main execution
main() {
    validate_env
    setup_config
    upgrade_opencode
    clone_repo
    start_opencode
    wait_for_health
    create_session
    send_task

    # Wait for task to complete and handle results
    local completion_status
    wait_for_completion && completion_status=0 || completion_status=$?
    
    handle_completion $completion_status
}

main "$@"
