# Default task workflow

New tasks use the staged workflow on GitHub repositories accessible to the Minions GitHub App. No repository-specific opt-in is required. Feedback on an existing GitHub PR branch keeps its current path.

## Setup

- Apply migrations through `013_staged_workflow.sql` before starting the new orchestrator. Use matching orchestrator, bot, control panel and devbox releases. Set `DEVBOX_IMAGE` to the matching image, preferably by digest.
- Reuse the existing [GitHub App](github-app.md). Install it only on the repositories Minions should access. Grant Contents and Pull requests read/write and Checks read. Accept updated permissions on the installation. Protect the default branch with a ruleset the App cannot bypass.
- Set `DEFAULT_MODEL` to an OpenCode `provider/model` identifier. Give the orchestrator the provider key as `DEVBOX_<PROVIDER>_API_KEY`, for example `DEVBOX_OPENROUTER_API_KEY`. Only the implementation container receives the selected provider key.
- Keep the Discord guild and role restrictions in place. If you set `WORKFLOW_DISCORD_TOKEN`, use the same separate secret on the orchestrator and Discord bot. The bot sends it as `X-Minions-Workflow-Token` in addition to the internal API token.
- Include [`infra/workflow-network-policy.yaml`](../../infra/workflow-network-policy.yaml). Check that your CNI enforces it and that the DNS selector and blocked address ranges match your cluster. The example permits public IPv4 HTTPS for GitHub, providers and dependencies. It is not a domain allowlist. The orchestrator also needs permission to read pod logs.

## What happens after submission

1. The Discord bot saves the first question before posting it. It accepts answers from the requester on the active question in the original channel. It can ask one follow-up. Matrix keeps its existing question and reply flow.
2. The runner saves the repository's default branch. An init container fetches the source with a repository-scoped, read-only token. OpenCode edits files without receiving a GitHub token, and the runner saves the patch in PostgreSQL.
3. A separate pod applies the patch to the saved base. It receives no provider key, and only its fetch init container receives a read token.
4. After verification, a publisher reconstructs the same Git tree without running repository code. It pushes to `minion/<run-id>` and the orchestrator opens a draft PR. The workflow does not merge or deploy.
5. The control panel shows the stage, local verification result, and draft link. Discord and Matrix report publication and the final result. The runner reports GitHub check runs observed on the verified head. It does not check merge eligibility or legacy commit statuses. Failed or missing checks leave the draft open.

## Supported verification

| Profile | Runs on | Local check |
| --- | --- | --- |
| Documentation | Markdown and reStructuredText only | `git diff --check`; no application tests |
| Go | Go source and JSON fixtures in a single root module | `go test -mod=readonly -buildvcs=false ./...`, with CGO and workspaces disabled |
| npm | JavaScript, TypeScript, CSS, HTML and JSON in a root project with a lockfile and test script | `npm ci --ignore-scripts` and `npm --ignore-scripts test` |

The image supplies the Go and Node toolchains. Unsupported file types, projects and mixed Go/npm changes stop before publication. Dependency manifests, GitHub workflows, and Git and OpenCode configuration are protected. The workflow rejects trees with symlinks or submodules.

If the default branch advances before publication, the run stops rather than rebasing an unverified tree. A failure after a push can leave the run branch on GitHub. Check it before retrying.
