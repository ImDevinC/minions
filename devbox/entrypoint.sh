#!/bin/bash
# Minion devbox entrypoint
# Clones repo, starts OpenCode serve, waits for readiness, creates session, sends task
#
# Required environment variables:
#   GITHUB_TOKEN      - GitHub token for cloning and API access
#   MINION_REPO       - Repository to clone (owner/repo format)
#   MINION_TASK       - Task description to send to OpenCode
#   MINION_ID         - Unique minion identifier
#   ORCHESTRATOR_URL  - Callback URL for the orchestrator
#   MINION_MODEL      - LLM model to use (e.g., anthropic/claude-sonnet-4-5)
#   INTERNAL_API_TOKEN - Token for authenticating with orchestrator
#
# Optional environment variables:
#   OPENCODE_PORT   - Port for OpenCode serve (default: 4096)
#
# Orchestrator receives callbacks at:
#   POST $ORCHESTRATOR_URL/api/minions/$MINION_ID/callback

set -euo pipefail

# Config
OPENCODE_PORT="${OPENCODE_PORT:-4096}"
OPENCODE_BASE="http://127.0.0.1:${OPENCODE_PORT}"
HEALTH_ENDPOINT="${OPENCODE_BASE}/global/health"
SESSION_ENDPOINT="${OPENCODE_BASE}/session"
HEALTH_TIMEOUT=60
HEALTH_INTERVAL=2

# Logging helpers
log() {
    echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] $*" >&2
}

die() {
    log "FATAL: $*"
    exit 1
}

# Validate required environment variables
validate_env() {
    local missing=()
    [[ -z "${GITHUB_TOKEN:-}" ]] && missing+=("GITHUB_TOKEN")
    [[ -z "${MINION_REPO:-}" ]] && missing+=("MINION_REPO")
    [[ -z "${MINION_TASK:-}" ]] && missing+=("MINION_TASK")
    [[ -z "${MINION_ID:-}" ]] && missing+=("MINION_ID")
    [[ -z "${ORCHESTRATOR_URL:-}" ]] && missing+=("ORCHESTRATOR_URL")
    [[ -z "${MINION_MODEL:-}" ]] && missing+=("MINION_MODEL")
    [[ -z "${INTERNAL_API_TOKEN:-}" ]] && missing+=("INTERNAL_API_TOKEN")

    if [[ ${#missing[@]} -gt 0 ]]; then
        die "Missing required environment variables: ${missing[*]}"
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

    # Clone with depth=1 for speed (full history not needed for most tasks)
    if ! git clone --depth=1 "https://github.com/${MINION_REPO}.git" /workspace/repo 2>&1; then
        die "Failed to clone repository"
    fi

    log "Repository cloned successfully"
    cd /workspace/repo
}

# Start OpenCode serve in background
start_opencode() {
    log "Starting OpenCode serve on port ${OPENCODE_PORT}"

    # Run opencode serve in background, redirect output to log file
    opencode serve --port "${OPENCODE_PORT}" --hostname "127.0.0.1" > /tmp/opencode.log 2>&1 &
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

    # Build the message parts using jq for safe JSON construction
    # This prevents shell injection attacks from task content
    local request_body
    request_body=$(jq -n \
        --arg model "$MINION_MODEL" \
        --arg task "$MINION_TASK" \
        '{
            model: $model,
            parts: [
                {
                    type: "text",
                    text: $task
                }
            ]
        }')

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
# The orchestrator's SSE client handles event streaming; we just need
# to know when to proceed to PR creation
wait_for_completion() {
    log "Waiting for task completion"

    while true; do
        local status_response
        status_response=$(curl -sf "${SESSION_ENDPOINT}/status" || echo "{}")

        local session_status
        session_status=$(echo "$status_response" | jq -r --arg id "$SESSION_ID" '.[$id] // "unknown"')

        case "$session_status" in
            "idle")
                log "Task completed (session idle)"
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

        sleep 5
    done
}

# Check if there are any changes to commit
has_changes() {
    # Check for staged, unstaged, or untracked files
    [[ -n "$(git status --porcelain)" ]]
}

# Create branch, commit, push, and create PR
create_pr() {
    log "Checking for changes"

    if ! has_changes; then
        log "No changes detected"
        return 1  # Signal "no changes" (not an error, handled separately)
    fi

    local branch_name="minion/${MINION_ID}"
    log "Creating branch: ${branch_name}"

    # Create and checkout the new branch
    git checkout -b "${branch_name}"

    # Stage all changes
    git add -A

    # Create commit with descriptive message
    # Use the first line of the task as commit subject, truncated if needed
    local commit_subject
    commit_subject=$(echo "$MINION_TASK" | head -c 72 | tr '\n' ' ')
    
    # Configure git user for commit
    git config user.email "minion@anomaly.co"
    git config user.name "Minion"

    git commit -m "feat: ${commit_subject}

Automated change by Minion ${MINION_ID}

Task:
${MINION_TASK}"

    log "Committed changes"

    # Push to origin
    log "Pushing branch to origin"
    git push -u origin "${branch_name}"

    log "Creating PR via gh CLI"

    # Create PR - gh uses GITHUB_TOKEN automatically
    local pr_url
    local pr_body
    pr_body=$(jq -n \
        --arg task "$MINION_TASK" \
        --arg minion_id "$MINION_ID" \
        '
## Minion Task

\($task)

---
_Automated PR by Minion `\($minion_id)`_
')

    # gh pr create returns the PR URL on success
    pr_url=$(gh pr create \
        --title "feat: ${commit_subject}" \
        --body "$pr_body" \
        --head "${branch_name}" \
        2>&1) || {
        local exit_code=$?
        log "gh pr create stderr: ${pr_url}"
        return $exit_code
    }

    log "PR created: ${pr_url}"
    echo "${pr_url}"
}

# Send callback to orchestrator
send_callback() {
    local status="$1"
    local pr_url="${2:-}"
    local error_msg="${3:-}"

    local callback_url="${ORCHESTRATOR_URL}/api/minions/${MINION_ID}/callback"
    log "Sending callback to orchestrator: status=${status}"

    local payload
    payload=$(jq -n \
        --arg status "$status" \
        --arg pr_url "$pr_url" \
        --arg error "$error_msg" \
        --arg session_id "${SESSION_ID:-}" \
        '{
            status: $status,
            session_id: $session_id
        } + (if $pr_url != "" then {pr_url: $pr_url} else {} end)
          + (if $error != "" then {error: $error} else {} end)')

    local http_code
    http_code=$(curl -sf -X POST "${callback_url}" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer ${INTERNAL_API_TOKEN:-}" \
        -d "$payload" \
        -w "%{http_code}" \
        -o /dev/null) || {
        log "Callback failed: HTTP ${http_code}"
        return 1
    }

    if [[ "$http_code" -ge 200 && "$http_code" -lt 300 ]]; then
        log "Callback sent successfully"
        return 0
    else
        log "Callback failed: HTTP ${http_code}"
        return 1
    fi
}

# Main execution
main() {
    validate_env
    clone_repo
    start_opencode
    wait_for_health
    create_session
    send_task

    # Wait for task to complete
    if wait_for_completion; then
        log "Task completed successfully"
        
        # Attempt to create PR
        local pr_url
        if pr_url=$(create_pr); then
            log "PR created successfully"
            send_callback "completed" "$pr_url"
        else
            # No changes or PR creation failed
            # create_pr returns 1 for "no changes", other codes for errors
            if ! has_changes; then
                log "No changes to commit - task completed with no modifications"
                # Note: devbox-5 will handle this case with a proper "no changes" status
                send_callback "completed" "" "No changes"
            else
                log "PR creation failed"
                send_callback "failed" "" "PR creation failed"
                exit 1
            fi
        fi
    else
        log "Task failed"
        send_callback "failed" "" "Task execution failed"
        exit 1
    fi
}

main "$@"
