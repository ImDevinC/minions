---
name: git-workflow
description: Git commit and PR workflow for this repository. Use this skill when making commits or submitting changes. Enforces conventional commits, branch-based development, and PR submission.
---

# Git Workflow Skill

This skill defines how commits and pull requests should be made in this repository.

## Core Rules

1. **Never push directly to main** - All changes must go through a pull request
2. **Use conventional commits** - All commits must follow the conventional commits specification
3. **Branch-based development** - Create a feature/fix branch for all changes

## Workflow

### 1. Create a Branch

Always create a new branch from an up-to-date main:

```bash
git checkout main
git pull origin main
git checkout -b <type>/<description>
```

Branch naming convention:
- `feat/<description>` - New features
- `fix/<description>` - Bug fixes
- `chore/<description>` - Maintenance tasks
- `docs/<description>` - Documentation changes
- `refactor/<description>` - Code refactoring

### 2. Make Commits

All commits must follow the conventional commits format:

```
<type>(<scope>): <subject>

<body>

<footer>
```

#### Types

| Type | Description | Version Bump |
|------|-------------|--------------|
| `feat` | New feature | Minor (0.X.0) |
| `fix` | Bug fix | Patch (0.0.X) |
| `perf` | Performance improvement | Patch |
| `refactor` | Code refactoring | Patch |
| `docs` | Documentation only | None |
| `style` | Code style (formatting) | None |
| `test` | Adding/updating tests | None |
| `chore` | Maintenance tasks | None |
| `ci` | CI/CD changes | None |
| `build` | Build system changes | None |
| `revert` | Reverting commits | Depends |

#### Breaking Changes

For breaking changes, add `!` after the type or include `BREAKING CHANGE:` in the footer:

```
feat!: remove deprecated API endpoint

BREAKING CHANGE: The /v1/users endpoint has been removed. Use /v2/users instead.
```

Breaking changes trigger a major version bump (X.0.0).

#### Scope (Optional)

Scope indicates which service/component is affected:
- `orchestrator`
- `discord-bot`
- `control-panel`
- `devbox`
- `infra`

Example: `feat(orchestrator): add health check endpoint`

#### Subject Rules

- Use imperative mood ("add" not "added" or "adds")
- No period at the end
- Max 72 characters
- Lowercase type, any case for subject

#### Body (Optional)

- Explain what and why, not how
- Wrap at 100 characters per line
- Separate from subject with blank line

### 3. Push and Create PR

```bash
git push -u origin <branch-name>
gh pr create --title "<conventional commit title>" --body "<description>"
```

#### PR Requirements

- Title should match conventional commit format
- Body should include:
  - Summary of changes (bullet points)
  - Any relevant context
  - Disclosure statement (required)

#### PR Body Template

```markdown
## Summary

- Change 1
- Change 2
- Change 3
```

### 4. After PR Merge

The CI/CD pipeline will automatically:
1. Run commitlint to validate commit messages
2. Run semantic-release to create version tags (if applicable)
3. Build and push Docker images (for version tags)

## Examples

### Good Commits

```
feat(orchestrator): add Kubernetes pod cleanup on task completion

Implement automatic cleanup of completed minion pods to prevent
resource exhaustion. Pods are removed 5 minutes after task completion.
```

```
fix: correct image path in deployment manifest

The deployment was pointing to the wrong container registry.
```

```
chore: update dependencies to latest versions
```

### Bad Commits

```
# Missing type
updated the code

# Past tense
fixed the bug

# Too vague
feat: changes

# Direct push to main (process violation)
git push origin main  # NEVER DO THIS
```

## Enforcement

- **Commitlint**: Validates commit messages on PR (`.github/workflows/commitlint.yaml`)
- **Branch protection**: Main branch should have protection rules enabled
- **Semantic-release**: Parses commits to determine version bumps

## Quick Reference

```bash
# Start new work
git checkout main && git pull origin main
git checkout -b feat/my-feature

# Commit changes
git add -A
git commit -m "feat(scope): add new capability"

# Push and create PR
git push -u origin feat/my-feature
gh pr create --title "feat(scope): add new capability" --body "## Summary
- Added X
- Updated Y
```
