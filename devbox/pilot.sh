#!/bin/bash
set -euo pipefail
export HOME=/workspace/home
export PATH=/opt/opencode/.opencode/bin:/usr/local/bin:/usr/bin:/bin
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0
mkdir -p "$HOME" /workspace/repo
cd /workspace/repo
git -c core.hooksPath=/dev/null init --quiet
git remote add origin https://github.com/ImDevinC/go-fifa.git

case "$PILOT_STEP" in
    implement)
        git fetch --quiet --depth=1 origin refs/heads/main
        base=$(git rev-parse FETCH_HEAD)
        git checkout --quiet --detach "$base"
        config="$HOME/.config/opencode"
        mkdir -p "$config"
        cp /opt/minions/pilot-opencode.json "$config/opencode.json"
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
        session=$(curl -fsS "${api_auth[@]}" --max-time 10 -H 'Content-Type: application/json' -d '{"agent":"pilot"}' "$url/session" | jq -er '.data.id')
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
        printf '%s\n' "$base"
        cat /workspace/candidate.diff
        ;;
    verify|publish)
        [[ "$PILOT_BASE" =~ ^[a-f0-9]{40}$ ]]
        git fetch --quiet --depth=1 origin "$PILOT_BASE"
        git read-tree "$PILOT_BASE"
        git apply --cached --whitespace=nowarn /task/patch.diff
        git diff --cached --quiet "$PILOT_BASE" -- .github .gitmodules go.mod go.sum
        # Permit ordinary source, docs, and fixtures only. Never publish links or gitlinks.
        while IFS= read -r -d '' path; do
            case "$path" in
                *.go|*.md|testdata/*.json) ;;
                *) exit 1 ;;
            esac
        done < <(git diff --cached --name-only -z --no-renames "$PILOT_BASE")
        tree=$(git write-tree)
        if git ls-tree -r "$tree" | grep -E '^(120000|160000) ' >/dev/null; then
            exit 1
        fi
        if [[ "$PILOT_STEP" = verify ]]; then
            git checkout-index -a
            # Do not trust output from the tested code as result metadata.
            go_output=$(mktemp)
            mkdir -p /workspace/go-tmp
            if timeout 180 env -i HOME="$HOME" PATH="$PATH" GOTMPDIR=/workspace/go-tmp CGO_ENABLED=0 GOPROXY=https://proxy.golang.org \
                GOSUMDB=sum.golang.org /opt/go-pilot/bin/go test -mod=readonly -buildvcs=false -v ./... >"$go_output" 2>&1; then
                git diff --quiet --no-ext-diff
                [[ "$(git write-tree)" = "$tree" ]]
                printf '%s' "$tree" > /dev/termination-log
            else
                exit 1
            fi
        else
            [[ "$tree" = "$PILOT_TREE" ]]
            [[ "$PILOT_ID" =~ ^[a-f0-9-]{36}$ ]]
            branch="minion/$PILOT_ID"
            export GIT_AUTHOR_NAME=Minions GIT_AUTHOR_EMAIL=minion@imdevinc.com
            export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
            export GIT_AUTHOR_DATE='2000-01-01T00:00:00Z' GIT_COMMITTER_DATE="$GIT_AUTHOR_DATE"
            head=$(git commit-tree "$tree" -p "$PILOT_BASE" -m "chore: apply minion change $PILOT_ID")
            remote=$(git ls-remote origin "refs/heads/$branch" | cut -f1)
            if [[ -z "$remote" ]]; then
                [[ "$(git ls-remote origin refs/heads/main | cut -f1)" = "$PILOT_BASE" ]]
                # GitHub accepts the push only if the run branch does not exist yet.
                git -c credential.helper='!f() { printf "username=x-access-token\npassword=%s\n" "$GH_TOKEN"; }; f' \
                    push --force-with-lease="refs/heads/$branch:" origin "$head:refs/heads/$branch"
            else
                [[ "$remote" = "$head" ]]
            fi
            printf '%s' "$head" > /dev/termination-log
        fi
        ;;
    *) exit 2 ;;
esac
