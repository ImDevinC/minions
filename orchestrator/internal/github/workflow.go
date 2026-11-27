package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-github/v84/github"
)

// WorkflowRepository resolves the target branch using the App installation for this repository.
func (m *Manager) WorkflowRepository(ctx context.Context, repo string) (string, error) {
	owner, name, ok := parseRepo(repo)
	if !ok || strings.Contains(name, "/") {
		return "", fmt.Errorf("expected GitHub owner/repository")
	}
	token, err := m.WorkflowToken(ctx, repo, "read")
	if err != nil {
		return "", err
	}
	repository, _, err := github.NewClient(nil).WithAuthToken(token).Repositories.Get(ctx, owner, name)
	if err != nil {
		return "", err
	}
	if repository.GetDefaultBranch() == "" {
		return "", fmt.Errorf("repository has no default branch")
	}
	return repository.GetDefaultBranch(), nil
}

// WorkflowToken grants one stage access only to the target repository.
func (m *Manager) WorkflowToken(ctx context.Context, repo, access string) (string, error) {
	owner, name, ok := parseRepo(repo)
	if !ok || strings.Contains(name, "/") {
		return "", fmt.Errorf("expected GitHub owner/repository")
	}
	permissions := &github.InstallationPermissions{Contents: github.String("read")}
	switch access {
	case "read":
	case "push":
		permissions.Contents = github.String("write")
	case "draft":
		permissions.PullRequests = github.String("write")
	case "find-draft":
		permissions.PullRequests = github.String("read")
	case "checks":
		permissions.Checks = github.String("read")
	default:
		return "", fmt.Errorf("invalid workflow access")
	}
	app := github.NewClient(&http.Client{Transport: m.appTransport})
	installation, _, err := app.Apps.FindRepositoryInstallation(ctx, owner, name)
	if err != nil {
		return "", err
	}
	token, _, err := app.Apps.CreateInstallationToken(ctx, installation.GetID(), &github.InstallationTokenOptions{
		Repositories: []string{name}, Permissions: permissions,
	})
	if err != nil {
		return "", err
	}
	if token.GetToken() == "" {
		return "", fmt.Errorf("missing workflow installation token")
	}
	return token.GetToken(), nil
}

// WorkflowDraft returns a matching PR or creates it as a draft when permitted.
func (m *Manager) WorkflowDraft(ctx context.Context, repo, target, branch, base, head, evidence string, create bool) (string, error) {
	access := "find-draft"
	if create {
		access = "draft"
	}
	token, err := m.WorkflowToken(ctx, repo, access)
	if err != nil {
		return "", err
	}
	owner, name, _ := parseRepo(repo)
	client := github.NewClient(nil).WithAuthToken(token)
	prs, _, err := client.PullRequests.List(ctx, owner, name, &github.PullRequestListOptions{
		Head: owner + ":" + branch, Base: target, State: "all",
	})
	if err != nil {
		return "", err
	}
	if len(prs) != 0 {
		if len(prs) != 1 || prs[0].GetHead().GetSHA() != head {
			return "", fmt.Errorf("existing PR does not match verified head")
		}
		return prs[0].GetHTMLURL(), nil
	}
	if !create {
		return "", nil
	}
	for refName, expected := range map[string]string{"refs/heads/" + target: base, "refs/heads/" + branch: head} {
		ref, _, err := client.Git.GetRef(ctx, owner, name, refName)
		if err != nil {
			return "", err
		}
		if ref.GetObject().GetSHA() != expected {
			return "", fmt.Errorf("branch changed before draft creation")
		}
	}
	pr, _, err := client.PullRequests.Create(ctx, owner, name, &github.NewPullRequest{
		Title: github.String("chore: apply Minions change"),
		Head:  github.String(branch), Base: github.String(target), Draft: github.Bool(true),
		Body: github.String("Created by an AI coding agent.\n\n" + evidence + "\n\nReview the changes and GitHub checks before merging."),
	})
	if err != nil {
		return "", err
	}
	return pr.GetHTMLURL(), nil
}

// WorkflowCheck reports the GitHub checks observed on the published revision.
func (m *Manager) WorkflowCheck(ctx context.Context, repo, branch, head string) (string, error) {
	token, err := m.WorkflowToken(ctx, repo, "checks")
	if err != nil {
		return "", err
	}
	owner, name, _ := parseRepo(repo)
	client := github.NewClient(nil).WithAuthToken(token)
	ref, _, err := client.Git.GetRef(ctx, owner, name, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	if ref.GetObject().GetSHA() != head {
		return "head_changed", nil
	}
	options := &github.ListCheckRunsOptions{Filter: github.String("latest"), ListOptions: github.ListOptions{PerPage: 100}}
	var observed, waiting, passed bool
	for {
		checks, response, err := client.Checks.ListCheckRunsForRef(ctx, owner, name, head, options)
		if err != nil {
			return "", err
		}
		for _, check := range checks.CheckRuns {
			if check.GetHeadSHA() != head {
				continue
			}
			observed = true
			conclusion := check.GetConclusion()
			switch {
			case check.GetStatus() != "completed" || conclusion == "":
				waiting = true
			case conclusion == "success":
				passed = true
			case conclusion != "neutral" && conclusion != "skipped":
				return conclusion, nil
			}
		}
		if response.NextPage == 0 {
			break
		}
		options.Page = response.NextPage
	}
	switch {
	case !observed || waiting:
		return "", nil
	case passed:
		return "success", nil
	default:
		return "no_checks_passed", nil
	}
}
