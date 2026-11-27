package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/go-github/v84/github"
)

const PilotOwner = "ImDevinC"
const PilotName = "go-fifa"
const PilotRepo = PilotOwner + "/" + PilotName

// PilotToken grants publication access only to go-fifa, without workflow write access.
func (m *Manager) PilotToken(ctx context.Context) (string, error) {
	app := github.NewClient(&http.Client{Transport: m.appTransport})
	installation, _, err := app.Apps.FindRepositoryInstallation(ctx, PilotOwner, PilotName)
	if err != nil {
		return "", err
	}
	token, _, err := app.Apps.CreateInstallationToken(ctx, installation.GetID(), &github.InstallationTokenOptions{
		Repositories: []string{PilotName},
		Permissions:  &github.InstallationPermissions{Contents: github.String("write"), PullRequests: github.String("write")},
	})
	if err != nil {
		return "", err
	}
	if token.GetToken() == "" {
		return "", fmt.Errorf("missing pilot installation token")
	}
	return token.GetToken(), nil
}

// PilotDraft finds the existing PR or opens a draft for the exact verified head.
func (m *Manager) PilotDraft(ctx context.Context, branch, base, head string) (string, error) {
	return m.pilotDraft(ctx, branch, base, head, true)
}

// FindPilotDraft recovers an accepted PR without performing any GitHub write.
func (m *Manager) FindPilotDraft(ctx context.Context, branch, head string) (string, error) {
	return m.pilotDraft(ctx, branch, "", head, false)
}

// pilotDraft reconciles the PR before allowing creation.
func (m *Manager) pilotDraft(ctx context.Context, branch, base, head string, create bool) (string, error) {
	token, err := m.PilotToken(ctx)
	if err != nil {
		return "", err
	}
	client := github.NewClient(nil).WithAuthToken(token)
	prs, _, err := client.PullRequests.List(ctx, PilotOwner, PilotName, &github.PullRequestListOptions{
		Head: PilotOwner + ":" + branch, Base: "main", State: "all",
	})
	if err != nil {
		return "", err
	}
	if len(prs) > 0 {
		if len(prs) != 1 || prs[0].GetHead().GetSHA() != head {
			return "", fmt.Errorf("existing PR does not match verified head")
		}
		return prs[0].GetHTMLURL(), nil
	}
	if !create {
		return "", nil
	}
	for refName, expected := range map[string]string{"refs/heads/main": base, "refs/heads/" + branch: head} {
		ref, _, err := client.Git.GetRef(ctx, PilotOwner, PilotName, refName)
		if err != nil {
			return "", err
		}
		if ref.GetObject().GetSHA() != expected {
			return "", fmt.Errorf("branch changed before draft creation")
		}
	}
	pr, _, err := client.PullRequests.Create(ctx, PilotOwner, PilotName, &github.NewPullRequest{
		Title: github.String("chore: apply Minions change"),
		Head:  github.String(branch), Base: github.String("main"), Draft: github.Bool(true),
		Body: github.String("Created by an AI coding agent.\n\nLocal validation passed: `go test -mod=readonly -v ./...` (Go 1.19.13). Review the changes and GitHub Actions results before merging."),
	})
	if err != nil {
		return "", err
	}
	return pr.GetHTMLURL(), nil
}

// PilotCheck returns the conclusion of the expected GitHub Actions check on the verified revision.
func (m *Manager) PilotCheck(ctx context.Context, branch, head string) (string, error) {
	client := github.NewClient(nil)
	ref, _, err := client.Git.GetRef(ctx, PilotOwner, PilotName, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	if ref.GetObject().GetSHA() != head {
		return "head_changed", nil
	}
	checks, _, err := client.Checks.ListCheckRunsForRef(ctx, PilotOwner, PilotName, head, &github.ListCheckRunsOptions{
		CheckName: github.String("test"), Filter: github.String("latest"),
	})
	if err != nil {
		return "", err
	}
	for _, check := range checks.CheckRuns {
		if check.GetHeadSHA() == head && check.GetApp().GetSlug() == "github-actions" && check.GetName() == "test" {
			if check.GetStatus() != "completed" {
				return "", nil
			}
			return check.GetConclusion(), nil
		}
	}
	return "", nil
}
