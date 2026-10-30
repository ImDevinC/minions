# Stripe Minions gap analysis

## Summary

Minions has the task execution platform. The largest gap is an enforced development workflow.
The current system asks the agent to test, push, and create a pull request.
Stripe controls those steps through code and limits CI execution to two rounds.

Prioritize workflow control, local validation, CI repair, and task credential separation.
Then add richer request context and shared tools. Use measured startup delays to prioritize warm environments.

## Review scope and sources

- Review date: October 30, 2026.
- Repository baseline: [`a4ea0a23a79ed5645375b9a7277aa729bbc2048a`][baseline].
- Primary sources: [Stripe Part 1][stripe-1] and [Stripe Part 2][stripe-2], published in February 2026.
- Secondary source: [Port's Stripe overview][port], updated September 10, 2026.

Findings describe the tracked implementation and manifests, not a verified running deployment.
Code links use the reviewed commit so their evidence remains stable.
Implementation proposals and estimates below are recommendations, not claims about Stripe's unpublished internals.

## Existing capabilities

| Capability | Repository evidence | Assessment |
| --- | --- | --- |
| Unattended execution | [Devbox entrypoint][entrypoint] and [agent prompt][prompt] | Present, with an execution timeout and completion callbacks. |
| Separate task environments | [Pod creation][pods] | Each task receives a pod and fresh workspace. Credential isolation needs further work. |
| Concurrent independent tasks | [Spawner][spawner] | Running tasks can overlap. Pod startup is processed serially. |
| Chat submission | [Discord handler][discord] and [Matrix handler][matrix] | Present, including clarification before execution. |
| PR follow-up | [GitHub webhook handler][github-webhook] | Comments and reviews with a bot mention start a fresh run on the existing PR branch. |
| Live and stored activity | [Event streaming][streaming] and [control panel][detail] | Present, including tool activity and subtasks. |
| Token and cost tracking | [Task storage][minion-store] and [control panel][detail] | Present. |
| Restart recovery | [Reconciler][reconciler] and [orchestrator startup][orchestrator-main] | Task reconciliation and event reconnection exist. This is not persisted workflow-step recovery. |

PR follow-up and run visibility already cover important parts of Stripe's documented user experience.
The choice of Discord, Matrix, Kubernetes, or OpenCode is not itself a capability gap.

## Priority and effort

Estimates assume one engineer, existing infrastructure, and a small initial repository set.
They include focused verification. They exclude building Stripe-scale infrastructure or custom integrations for every external system.
Ranges overlap where work shares a foundation; do not sum them as a committed delivery schedule.

| Priority | Gap | Initial effort |
| --- | --- | --- |
| First | Code-controlled workflows, or blueprints | 2–3 engineer-weeks |
| First | Enforced local checks and bounded CI repair | 2–3 additional engineer-weeks |
| First | Task credential separation and network policy corrections | 1–2 engineer-weeks |
| Next | Conversation and linked-content collection | 4–7 engineer-days |
| Next | Shared MCP tools and selected tool groups | 1–2 engineer-weeks with existing servers |
| Verify first | Shared, scoped coding rules | 2–5 engineer-days for initial verification and limited integration |
| Later | Startup improvements | 3–5 engineer-days |
| Later | Reliable warm environment pool | 2–4 engineer-weeks |
| Later | Web and CLI submission | 3–7 engineer-days |
| Later | Dashboard follow-up | 2–4 engineer-days |
| With publication step | Repository PR template support | 1–2 engineer-days |

## 1. Code-controlled workflows

**Stripe:** Blueprints combine deterministic code with agent steps. Each agent step can use different tools, prompts, and context.
Teams can define specialized workflows, including migrations. See [Part 2, Blueprints][stripe-2].

**Current implementation:** [The entrypoint][entrypoint-main] sends one task and waits for its outcome.
[The prompt][prompt-new-task] delegates tests, Git operations, and PR creation to the agent.
There is no platform workflow engine enforcing those development steps.

**Implementation work:**

1. Add a small Go workflow engine within the existing orchestrator.
2. Store workflow version, current step, attempts, inputs, outputs, and errors in PostgreSQL.
3. Support deterministic steps and agent steps with selected tools and scoped context.
4. Move branch creation, validation, pushing, and PR publication into deterministic steps.
5. Remove conflicting publication instructions from implementation-agent prompts.
6. Make step transitions durable and repeated external operations safe after restarts.
7. Start with implementation and PR-feedback workflows. Add specialized migration workflows later.

The controller should own publication credentials and validate step outputs.
Prompt changes alone cannot guarantee that an agent with unrestricted shell credentials follows the workflow.

**Completion criteria:** Required steps cannot be skipped. Restart recovery cannot create duplicate PRs or repeat completed publication operations.

## 2. Enforced local checks and bounded CI repair

**Stripe:** Selected local checks run automatically before pushing. Available automatic fixes run before agent repair.
At most two CI rounds occur, then the branch returns to a human. See [Part 1][stripe-1] and [Part 2][stripe-2].

**Current implementation:** The prompt says to test if possible.
The [GitHub event switch][github-events] handles comments and reviews, not CI completion.
[Completion handling][completion] can report success because a PR exists, including after a timeout.
Consequently, `completed` does not establish that CI passed.

**Implementation work:**

1. Define repository configuration for setup, formatting, linting, tests, and required CI checks.
2. Run configured local checks deterministically. Record output and bound local repair attempts.
3. Push changes and record the exact commit SHA and CI round.
4. Receive CI events or poll results for that SHA. Collect failure logs and supported automatic fixes.
5. Invoke one agent repair step when unresolved failures remain.
6. Run local checks, push again, and collect the second CI result.
7. Return `ready_for_review` or `needs_human`, with the branch and remaining failures.

Handle duplicate events, stale commit results, cancellations, missing checks, and CI timeouts explicitly.
An absent or skipped required check must not silently count as a pass.
Some repositories start CI only after PR creation. Support an early draft PR, then update its final status and summary.
Long CI waits also require a defined workspace retention or recreation policy and renewable task credentials.

**Completion criteria:** Results match the submitted SHA. A run cannot exceed two CI rounds.
Preserve partial work and failure details when human action is needed.

## 3. Isolation between tasks and control services

**Stripe:** Devboxes run in QA without production access, real user data, or arbitrary network egress.
A separate tool-control framework prevents destructive tool actions. See [Part 2][stripe-2].

**Current implementation:** Separate pods and network policies provide a foundation, but several boundaries remain broader:

- [Devbox egress][egress] permits arbitrary public HTTPS destinations.
- [Each devbox receives `INTERNAL_API_TOKEN`][pod-env].
- [The API router][api-router] accepts that shared token for all protected endpoints, including task creation and deletion.
- [Pods run as UID 0 with additional Buildah capabilities][pod-security].
- [GitHub token transports][github-tokens] are cached per installation without an explicit per-repository restriction.

A task therefore holds credentials that reach beyond its own run.
The tracked [devbox ingress policy][devbox-ingress] also denies all ingress, conflicting with orchestrator SSE connections.

**Implementation work:**

1. Issue short-lived task credentials restricted to the run and permitted operations.
2. Keep control-service credentials outside agent environments. Use distinct credentials for worker API access.
3. Restrict GitHub credentials to the target repository and required operations.
4. Use a proxy or domain-aware network policy for approved external destinations.
5. Permit orchestrator-to-devbox event traffic explicitly.
6. Separate container-building privileges from ordinary coding tasks.
7. Enforce external operation permissions at tool services.

**Completion criteria:** A task cannot read, create, terminate, or report completion for another task.
Verify network policies in the target cluster, including allowed event traffic and blocked destinations.
The initial estimate covers credential separation and policy corrections. Stronger execution isolation depends on the cluster.

## 4. Automatic request-context collection

**Stripe:** A run can read its initiating Slack thread and links.
Relevant MCP calls retrieve linked context before coding starts. See [Part 1][stripe-1].

**Current implementation:** [Discord][discord-task] and [Matrix][matrix-task] submit parsed task text.
The PR-feedback prompt asks the agent to retrieve PR context itself.
There is no shared deterministic conversation and link collection stage.

**Implementation work:**

1. Collect the relevant conversation thread and reply chain.
2. Extract supported links, initially GitHub issues, PRs, and the main documentation system.
3. Retrieve content through deterministic adapters with the requester's applicable access limits.
4. Store a bounded context package with source URLs and retrieval errors.
5. Pass the package to clarification and implementation.
6. Keep retrieved content distinct from user instructions.

**Completion criteria:** A task can use thread context and supported links without asking the coding agent to discover them.
Retrieval failures remain visible, and context size has a defined limit.

## 5. Shared MCP tools and selected tool groups

**Stripe:** Toolshed provides shared tools for documentation, tickets, build status, code search, and other systems.
Minions receive a small default subset. Users can select additional tool groups. See [Part 2][stripe-2].

**Current implementation:** [The devbox configuration][runtime-config] does not configure a shared MCP service or selectable tool groups.
Target repositories may provide their own runtime configuration. That does not establish centrally managed tool access.

**Implementation work:**

1. Connect existing MCP servers for documentation, tickets, code search, and CI.
2. Define tool groups selected by repository, workflow, or user.
3. Expose only the required tools to each agent step.
4. Centralize service authentication and operation permissions.
5. Reuse tools for deterministic context collection.

**Completion criteria:** Tool access is explicit, reproducible, and enforced outside the model.
The useful capability is shared, curated access, not Stripe's tool count.
Custom connectors require separate estimates.

## 6. Prepared environments and fast allocation

**Stripe:** Warm devboxes contain repositories, caches, and running development services before allocation.
Stripe targets approximately ten-second availability. See [Part 2, Devboxes][stripe-2].

**Current implementation:** Tasks use fresh workspaces and a general-purpose [devbox image][devbox-image].
[Startup][entrypoint-main] upgrades the runtime and clones the repository during each run.
[The spawner][serial-spawn] waits for each pod's readiness before processing the next task.

**Implementation work:**

1. Measure allocation, image pull, setup, and time-to-first-agent-action separately.
2. Define repository-specific environment and setup profiles.
3. Pin tested runtime versions in images.
4. Prepare dependency and build caches without sharing task mutations or credentials.
5. Add bounded concurrent allocation with database-backed task claims.
6. Add a warm pool when startup measurements justify its ongoing cost.
7. Implement allocation leases, expiry, replenishment, and clean workspace creation.

**Completion criteria:** Startup targets are measured, and allocated environments remain clean and isolated between tasks.
Image pre-pulling alone does not provide prepared repositories, dependencies, or services.

## Smaller or partially covered gaps

| Capability | Current assessment | Implementation work |
| --- | --- | --- |
| Shared, scoped rules | Exact directory/glob compatibility is unverified. Runtime rules may already cover part of this. | Verify rule loading on the pinned runtime. Add shared distribution or translation only where needed. |
| Web and CLI submission | The [dashboard][dashboard] monitors runs. No dedicated submission CLI exists in the reviewed tree. | Extend task identity handling and add authenticated submission clients. |
| Dashboard follow-up | GitHub follow-up exists, but the dashboard cannot submit further instructions. | Create a linked run against the same branch from a follow-up action. |
| Repository PR templates | The [global prompt][prompt-new-task] uses a fixed summary format. | Load the repository template during deterministic publication and include validation results. |

Stripe documents these user-entry and context capabilities in [Part 1][stripe-1] and [Part 2][stripe-2].
Preserve shared rule files used by human-operated agents rather than maintaining duplicate instructions.
Verify runtime behavior before treating rule support as absent or building a compatibility layer.

## Recommended delivery sequence

### First release: enforced execution and useful human handoff

Build the workflow foundation, credential separation, local checks, and CI loop together.
Budget approximately **5–8 engineer-weeks** for a tested first release on a small repository set.

```text
Collect available context -> Prepare workspace -> Agent implements
  -> Run local checks -> Push -> CI round 1
  -> If needed: automatic fixes + agent repair -> Run local checks
  -> Push -> CI round 2 -> Human review
```

Successful CI round 1 goes directly to human review. The second round occurs only when repair is needed.
The first release can use current task text and PR metadata as its context input.

Verify these behaviors with focused integration and end-to-end tests:

- Required checks cannot be silently skipped.
- CI results belong to the submitted commit.
- A run cannot exceed two CI rounds.
- Duplicate events and process restarts do not duplicate publication.
- Failed validation produces a branch and clear human handoff.
- A task cannot control another task.

### Next: context quality and tool access

Add conversation history, linked-content collection, shared tools, and verified scoped rules.
Measure first-round CI pass rate, human follow-up frequency, and cost per useful result.

### Later: allocation speed and additional entry points

Use startup measurements to prioritize caches and warm pools.
Add web submission, CLI submission, and dashboard follow-up through the same task API.

## Source qualifications

- "One-shot" does not mean one model call or guaranteed success. Stripe supports internal repair and later human follow-up.
- Two CI rounds is a maximum, not a requirement to run CI twice after success.
- Stripe describes automatically created flaky-test tickets with a button to start a Minion.
  Fully automatic flaky-test repair is not established by the two primary posts.
- Port mentions browser verification as a possible future direction based on a secondary conference summary.
  Treat it as a possible extension, not an established parity requirement.
- The sources establish concurrent independent tasks, not a required coordinated multi-agent architecture.
- Port's model-router tag does not establish a specific routing implementation in Stripe's two posts.
- Durable step recovery is our implementation recommendation. Stripe does not disclose its workflow persistence design.
- PR volume and tool counts describe Stripe's scale. They are not implementation acceptance criteria for this project.

## Open decisions

1. Which repositories and CI systems should the first workflow support?
2. Which documentation and ticket sources matter most?
3. Is the deployment for personal use or multiple mutually untrusted users?
4. Which checks define readiness, and how should missing checks or infrastructure failures affect the result?
5. What startup target and idle infrastructure cost would justify a warm pool?

[baseline]: https://github.com/ImDevinC/minions/tree/a4ea0a23a79ed5645375b9a7277aa729bbc2048a
[stripe-1]: https://stripe.dev/blog/minions-stripes-one-shot-end-to-end-coding-agents
[stripe-2]: https://stripe.dev/blog/minions-stripes-one-shot-end-to-end-coding-agents-part-2
[port]: https://software-factories.port.io/companies/stripe
[entrypoint]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/devbox/entrypoint.sh
[entrypoint-main]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/devbox/entrypoint.sh#L619-L638
[prompt]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/devbox/config/MINION_PROMPT.md
[prompt-new-task]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/devbox/config/MINION_PROMPT.md#L83-L127
[pods]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/internal/k8s/pods.go
[spawner]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/internal/spawner/spawner.go
[serial-spawn]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/internal/spawner/spawner.go#L136-L153
[discord]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/discord-bot/internal/handler/message.go
[matrix]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/matrix-bot/internal/handler/message.go
[github-webhook]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/github-webhook/internal/handler/webhook.go
[streaming]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/internal/streaming/sse.go
[detail]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/control-panel/src/components/minion-detail-client.tsx
[minion-store]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/internal/db/minions.go
[reconciler]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/internal/reconciler/reconciler.go
[orchestrator-main]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/cmd/orchestrator/main.go
[github-events]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/github-webhook/internal/handler/webhook.go#L75-L86
[completion]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/devbox/entrypoint.sh#L483-L565
[egress]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/infra/network-policies.yaml#L37-L51
[pod-env]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/internal/k8s/pods.go#L580-L587
[api-router]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/internal/api/router.go#L68-L104
[pod-security]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/internal/k8s/pods.go#L352-L403
[github-tokens]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/orchestrator/internal/github/tokens.go#L163-L197
[devbox-ingress]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/infra/network-policies.yaml#L64-L77
[discord-task]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/discord-bot/internal/handler/message.go#L130-L180
[matrix-task]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/matrix-bot/internal/handler/message.go#L165-L210
[runtime-config]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/devbox/config/opencode.json
[devbox-image]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/devbox/Dockerfile
[dashboard]: https://github.com/ImDevinC/minions/blob/a4ea0a23a79ed5645375b9a7277aa729bbc2048a/control-panel/src/app/page.tsx#L73-L100
