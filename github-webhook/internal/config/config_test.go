package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadApprovedRepos(t *testing.T) {
	content := `# Comment line (ignored)

imdevinc/minions
imdevinc
MyOrg/*
ImDevinC/Other-Repo
`
	path := filepath.Join(t.TempDir(), "approved-repos.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	repos, err := loadApprovedRepos(path)
	if err != nil {
		t.Fatalf("loadApprovedRepos() error = %v", err)
	}

	want := map[string]bool{
		"imdevinc/minions":    true, // exact repo
		"imdevinc":            true, // owner/org entry
		"myorg/*":             true, // wildcard entry
		"imdevinc/other-repo": true, // exact repo, lowercased
	}

	if len(repos) != len(want) {
		t.Fatalf("loadApprovedRepos() loaded %d entries, want %d: %v", len(repos), len(want), repos)
	}

	for entry, present := range want {
		if !repos[entry] {
			t.Errorf("loadApprovedRepos() missing entry %q in %v", entry, repos)
		}
		if !present {
			t.Errorf("test bug: want map entry %q should be true", entry)
		}
	}
}

func TestLoadApprovedRepos_MissingFile(t *testing.T) {
	if _, err := loadApprovedRepos(filepath.Join(t.TempDir(), "does-not-exist.txt")); err == nil {
		t.Fatal("loadApprovedRepos() expected error for missing file, got nil")
	}
}