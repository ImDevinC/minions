#!/bin/bash
# Verify a fixture from a non-main branch without credentials or network access.
set -euo pipefail
image=${1:?usage: bash devbox/workflow-smoke.sh IMAGE}
scratch=$(mktemp -d "${TMPDIR:-/tmp}/minions-workflow.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir "$scratch/source"
cd "$scratch/source"
git init -qb release/default
printf '# Sample\n' > README.md
printf 'module example.org/example\n\ngo 1.24\n' > go.mod
printf 'package example\n\nfunc value() int { return 1 }\n' > example.go
printf 'package example\nimport "testing"\nfunc TestValue(t *testing.T) { if value() < 1 { t.Fatal("value less than 1") } }\n' > example_test.go
printf '{"name":"sample","version":"1.0.0","scripts":{"test":"node --test"}}\n' > package.json
printf '{"name":"sample","version":"1.0.0","lockfileVersion":3,"requires":true,"packages":{"":{"name":"sample","version":"1.0.0"}}}\n' > package-lock.json
printf 'require("node:assert").equal(1, 1)\n' > example.test.js
printf '[sample]\n' > sample.toml
git add .
git -c user.name=Test -c user.email=test@example.org commit -qm initial
base=$(git rev-parse HEAD)

run_case() {
    local file=$1 change=$2 expected=$3 expected_exit=${4:-0} actual=0
    printf '%s\n' "$change" >> "$file"
    git diff --binary > "$scratch/patch.diff"
    git show "HEAD:$file" > "$file"
    : > "$scratch/result"
    chmod 666 "$scratch/result"
    docker run --rm --cap-drop ALL --security-opt no-new-privileges --read-only --network none \
        --tmpfs /workspace:exec,uid=1000,gid=1000 --tmpfs /tmp:mode=1777 \
        --mount "type=bind,src=$scratch/source,dst=/fixture,readonly" \
        --mount "type=bind,src=$scratch/patch.diff,dst=/task/patch.diff,readonly" \
        --mount "type=bind,src=$scratch/result,dst=/dev/termination-log" \
        -e WORKFLOW_BASE="$base" -e WORKFLOW_REPO=example/project \
        -e WORKFLOW_TARGET=release/default -e WORKFLOW_STEP=verify \
        --entrypoint /bin/bash "$image" -ec '
            mkdir /workspace/repo
            cd /workspace/repo
            git init -q
            git -c safe.directory=/fixture fetch -q /fixture refs/heads/release/default
            /opt/minions/workflow.sh
        ' || actual=$?
    [[ "$actual" == "$expected_exit" ]]
    if [[ -n "$expected" ]]; then
        grep -Ex "$expected" "$scratch/result"
    else
        test ! -s "$scratch/result"
    fi
}

run_case README.md 'A docs change.' 'docs:[a-f0-9]{40}'
run_case example.go 'func extra() int { return 2 }' 'go:[a-f0-9]{40}'
run_case example.test.js 'require("node:assert").equal(2, 2)' 'npm:[a-f0-9]{40}'
run_case sample.toml 'value = 1' unsupported 1
run_case example.test.js 'require("node:assert").equal(1, 2)' '' 1
run_case package.json '{"protected":true}' '' 1
