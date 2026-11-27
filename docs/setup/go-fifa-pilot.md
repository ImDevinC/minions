# go-fifa automatic draft PR pilot

The pilot is off by default. It accepts new maintenance requests from approved Discord users for `ImDevinC/go-fifa`. It does not accept Matrix or PR-feedback requests for this repo while enabled.

## Enable

1. Apply migrations `012_index_discord_deliveries.sql` and `013_pilot_workflow.sql`. Deploy the matching orchestrator, Discord bot, and control panel.
2. Build `devbox/` and set `PILOT_DEVBOX_IMAGE` on the orchestrator to its registry image digest (`…@sha256:…`). The pilot uses OpenCode 2.4.2 and Go 1.19.13. It does not upgrade either at startup.
3. Set `PILOT_DISCORD_USERS` to approved Discord user IDs, separated by commas. Keep the bot's guild and role restrictions in place.
4. Set `PILOT_DISCORD_TOKEN` on the orchestrator and bot to the same random secret of at least 32 characters. Do not reuse `INTERNAL_API_TOKEN`. Keep `DISCORD_BOT_WEBHOOK_URL` configured.
5. Set `PILOT_PROVIDER_SECRET` to a private Secret in the worker namespace. Set `PILOT_PROVIDER_KEY` to its provider API key name, such as `OPENROUTER_API_KEY`. Use a budget-limited key for `DEFAULT_MODEL`. Only the implementation pod receives this key.
6. Install a separate GitHub App on `go-fifa` only. Set `PILOT_GITHUB_APP_ID` and `PILOT_GITHUB_APP_PRIVATE_KEY` on the orchestrator. Grant Contents and Pull requests write permission, but no Workflows permission. Protect `main` with a ruleset that the app cannot bypass. Remove this repo from the legacy App installation.
7. Add `infra/pilot-network-policy.yaml` to the deployment. Verify that the cluster CNI enforces it, that its DNS selector matches the cluster, and that all local cluster and production ranges are blocked. This example permits public IPv4 HTTPS; it is not a domain allowlist. Use the cluster egress gateway for a stricter allowlist.
8. Confirm the orchestrator RBAC includes `pods/log: get`. Its existing pod and ConfigMap permissions are sufficient for the workflow.

## Run and verify

Submit a small, clear change through Discord. The bot may ask up to two rounds of questions before the run queues. Watch the control panel for implementation, local tests, draft PR creation, and GitHub Actions.

- The implementation pod has no GitHub or cluster credentials.
- The verification pod has no provider, GitHub, or cluster credentials. It applies the patch to a clean checkout of the saved base and runs Go tests. The patch can change Go source, Markdown, or JSON fixtures, not workflows or dependencies.
- The publisher reconstructs the tested Git tree without executing repository code. It uses a run-specific branch and opens one draft after local checks pass. It never merges or deploys. A failed or missing GitHub check leaves the draft open.
- A failed verification leaves the patch in PostgreSQL and stops publication. An empty patch creates no branch or PR.
- A run that failed during publication can still have a branch on GitHub. Check `minion/<run-id>` before retrying.

Before rollout, test a clear request, an ambiguous request, a failed local check, an orchestrator restart during a run, and cancellation. Use the control panel and the PR's Checks tab to compare local and GitHub results.

Do not disable the runner with active pilot runs unless you intend to pause them. Do not roll back to an orchestrator that predates pilot migrations while pilot runs are active.
