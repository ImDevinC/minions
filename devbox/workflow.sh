#!/bin/bash
set -euo pipefail
export HOME=/workspace/home
export PATH=/opt/opencode/.opencode/bin:/usr/local/bin:/usr/bin:/bin
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0
[[ "$WORKFLOW_REPO" =~ ^[a-zA-Z0-9-]+/[a-zA-Z0-9_.-]+$ ]]
[[ "${WORKFLOW_REPO#*/}" != "." && "${WORKFLOW_REPO#*/}" != ".." ]]
git check-ref-format "refs/heads/$WORKFLOW_TARGET"
mkdir -p "$HOME" /workspace/repo
cd /workspace/repo

github_git() {
    git -c credential.helper= \
        -c credential.helper='!f() { if [ "$1" = get ]; then printf "username=x-access-token\npassword=%s\n" "$GH_TOKEN"; fi; }; f' "$@"
}

fetch_base() {
    git init --quiet
    git config core.hooksPath /dev/null
    git remote add origin "https://github.com/$WORKFLOW_REPO.git"
    github_git fetch --quiet --depth=1 origin "$1"
}

base_ref() {
    if [[ -z "$WORKFLOW_BASE" ]]; then
        printf 'refs/heads/%s\n' "$WORKFLOW_TARGET"
    else
        [[ "$WORKFLOW_BASE" =~ ^[a-f0-9]{40}$ ]]
        printf '%s\n' "$WORKFLOW_BASE"
    fi
}

unsupported() {
    printf unsupported > /dev/termination-log
    exit 1
}

reject_links() {
    git ls-tree -r "$1" > /workspace/tree
    if grep -Eq '^(120000|160000) ' /workspace/tree; then exit 1; fi
}

reject_nested_project() {
    local path dir
    while IFS= read -r -d '' path; do
        dir=$path
        while [[ "$dir" = */* ]]; do
            dir=${dir%/*}
            if [[ -f "$dir/$1" ]]; then unsupported; fi
        done
    done < /workspace/paths
}

case "$WORKFLOW_STEP" in
    fetch)
        ref=$(base_ref)
        fetch_base "$ref"
        ;;
    implement)
        base=$(git rev-parse FETCH_HEAD)
        reject_links "$base"
        git checkout --quiet --detach "$base"
        config="$HOME/.config/opencode"
        mkdir -p "$config"
        cp /opt/minions/workflow-opencode.json "$config/opencode.json"
        export OPENCODE_PASSWORD
        OPENCODE_PASSWORD=$(cat /proc/sys/kernel/random/uuid)
        api_auth=(--user "opencode:$OPENCODE_PASSWORD")
        opencode serve --hostname 127.0.0.1 --port 4096 >/tmp/opencode.log 2>&1 &
        server=$!
        trap 'kill "$server" 2>/dev/null || true' EXIT
        url=http://127.0.0.1:4096/api
        for i in {1..30}; do
            if curl -fsS "${api_auth[@]}" --max-time 2 "$url/info" >/dev/null 2>&1; then break; fi
            sleep 1
        done
        session=$(curl -fsS "${api_auth[@]}" --max-time 10 -H 'Content-Type: application/json' -d '{"agent":"maintenance"}' "$url/session" | jq -er '.data.id')
        jq -n --rawfile text /task/task.txt '{text:$text}' |
            curl -fsS "${api_auth[@]}" --max-time 15 -H 'Content-Type: application/json' -d @- "$url/session/$session/prompt" >/dev/null
        curl -fsS "${api_auth[@]}" --max-time 1500 -X POST "$url/experimental/session/$session/wait" >/dev/null
        curl -fsS "${api_auth[@]}" --max-time 10 "$url/session/$session" | jq -e '.data.outcome == "succeeded"' >/dev/null
        kill "$server"
        wait "$server" || true
        trap - EXIT
        git add -A
        git diff --cached --binary --no-ext-diff --no-textconv "$base" > /workspace/candidate.diff
        test "$(wc -c < /workspace/candidate.diff)" -le 240000
        digest=$(sha256sum /workspace/candidate.diff)
        printf '%s:%s\n' "$base" "${digest%% *}"
        cat /workspace/candidate.diff
        ;;
    verify|publish)
        [[ "$WORKFLOW_BASE" =~ ^[a-f0-9]{40}$ ]]
        if [[ "$WORKFLOW_STEP" = publish ]]; then
            fetch_base "$WORKFLOW_BASE"
        fi
        [[ "$(git rev-parse FETCH_HEAD)" = "$WORKFLOW_BASE" ]]
        git read-tree "$WORKFLOW_BASE"
        git apply --cached --whitespace=nowarn /task/patch.diff
        git diff --cached --quiet "$WORKFLOW_BASE" -- .github .gitmodules .gitattributes .opencode \
            ':(glob)**/go.mod' ':(glob)**/go.sum' ':(glob)**/go.work*' \
            ':(glob)**/package.json' ':(glob)**/package-lock.json' ':(glob)**/npm-shrinkwrap.json' \
            ':(glob)**/.npmrc' ':(glob)**/.gitattributes' ':(glob)**/opencode.json*'
        tree=$(git write-tree)
        reject_links "$tree"
        if [[ "$WORKFLOW_STEP" = verify ]]; then
            git diff --cached --check "$WORKFLOW_BASE"
            git diff --cached --name-only --no-renames -z "$WORKFLOW_BASE" > /workspace/paths
            docs_only=true
            go_change=false
            npm_change=false
            while IFS= read -r -d '' path; do
                case "$path" in
                    *.md|*.rst) ;;
                    *.go|*/testdata/*.json|testdata/*.json) docs_only=false; go_change=true ;;
                    *.js|*.jsx|*.ts|*.tsx|*.mjs|*.cjs|*.mts|*.cts|*.css|*.html|*.json)
                        docs_only=false; npm_change=true ;;
                    *) unsupported ;;
                esac
            done < /workspace/paths
            git checkout-index -a
            if "$docs_only"; then
                profile=docs
            elif "$go_change" && ! "$npm_change" && [[ -f go.mod ]]; then
                reject_nested_project go.mod
                profile=go
                mkdir -p /workspace/go-tmp
                timeout 220 env -i HOME="$HOME" PATH="$PATH" GOTMPDIR=/workspace/go-tmp \
                    CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off go test -mod=readonly -buildvcs=false ./... > /tmp/verify.log 2>&1
            elif "$npm_change" && ! "$go_change" && [[ -f package-lock.json && ! -f npm-shrinkwrap.json ]] &&
                    jq -e '(.scripts.test | type == "string" and length > 0) and ((.workspaces // []) | length == 0)' package.json >/dev/null; then
                reject_nested_project package.json
                profile=npm
                timeout 100 env -i HOME="$HOME" PATH="$PATH" CI=true npm ci --ignore-scripts --no-audit --no-fund > /tmp/verify.log 2>&1
                timeout 120 env -i HOME="$HOME" PATH="$PATH" CI=true npm --ignore-scripts test >> /tmp/verify.log 2>&1
            else
                unsupported
            fi
            git diff --quiet --no-ext-diff
            [[ "$(git write-tree)" = "$tree" ]]
            printf '%s:%s' "$profile" "$tree" > /dev/termination-log
        else
            [[ "$tree" = "$WORKFLOW_TREE" ]]
            [[ "$WORKFLOW_ID" =~ ^[a-f0-9-]{36}$ ]]
            branch="minion/$WORKFLOW_ID"
            export GIT_AUTHOR_NAME=Minions GIT_AUTHOR_EMAIL=minion@imdevinc.com
            export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
            export GIT_AUTHOR_DATE='2000-01-01T00:00:00Z' GIT_COMMITTER_DATE="$GIT_AUTHOR_DATE"
            head=$(git commit-tree "$tree" -p "$WORKFLOW_BASE" -m "chore: apply minion change $WORKFLOW_ID")
            remote=$(github_git ls-remote origin "refs/heads/$branch" | cut -f1)
            if [[ -z "$remote" ]]; then
                [[ "$(github_git ls-remote origin "refs/heads/$WORKFLOW_TARGET" | cut -f1)" = "$WORKFLOW_BASE" ]]
                # GitHub accepts the push only if the run branch does not exist yet.
                github_git push --force-with-lease="refs/heads/$branch:" origin "$head:refs/heads/$branch"
            else
                [[ "$remote" = "$head" ]]
            fi
            printf '%s' "$head" > /dev/termination-log
        fi
        ;;
    *) exit 2 ;;
esac
