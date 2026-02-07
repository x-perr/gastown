package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpawnedPolecatInfoAgentID(t *testing.T) {
	tests := []struct {
		name        string
		rigName     string
		polecatName string
		want        string
	}{
		{
			name:        "standard names",
			rigName:     "gastown",
			polecatName: "Toast",
			want:        "gastown/polecats/Toast",
		},
		{
			name:        "different rig",
			rigName:     "beads",
			polecatName: "Worker",
			want:        "beads/polecats/Worker",
		},
		{
			name:        "hyphenated polecat name",
			rigName:     "greenplace",
			polecatName: "Max-01",
			want:        "greenplace/polecats/Max-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &SpawnedPolecatInfo{
				RigName:     tt.rigName,
				PolecatName: tt.polecatName,
			}
			got := info.AgentID()
			if got != tt.want {
				t.Errorf("AgentID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSpawnedPolecatInfoSessionStarted(t *testing.T) {
	tests := []struct {
		name string
		pane string
		want bool
	}{
		{
			name: "no pane - not started",
			pane: "",
			want: false,
		},
		{
			name: "has pane - started",
			pane: "%42",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &SpawnedPolecatInfo{Pane: tt.pane}
			got := info.SessionStarted()
			if got != tt.want {
				t.Errorf("SessionStarted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVerifyWorktreeExists(t *testing.T) {
	t.Run("valid worktree with .git file", func(t *testing.T) {
		dir := t.TempDir()
		// Create a .git file (worktrees have a .git file pointing to the main repo)
		gitFile := filepath.Join(dir, ".git")
		if err := os.WriteFile(gitFile, []byte("gitdir: /some/path/.git/worktrees/foo\n"), 0644); err != nil {
			t.Fatalf("write .git file: %v", err)
		}

		if err := verifyWorktreeExists(dir); err != nil {
			t.Errorf("verifyWorktreeExists() returned error for valid worktree: %v", err)
		}
	})

	t.Run("valid clone with .git directory", func(t *testing.T) {
		dir := t.TempDir()
		// Create a .git directory (regular clones have a .git directory)
		gitDir := filepath.Join(dir, ".git")
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}

		if err := verifyWorktreeExists(dir); err != nil {
			t.Errorf("verifyWorktreeExists() returned error for valid clone: %v", err)
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		err := verifyWorktreeExists("/tmp/nonexistent-worktree-path-xyz")
		if err == nil {
			t.Error("verifyWorktreeExists() should return error for nonexistent directory")
		}
	})

	t.Run("path is a file not directory", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "notadir")
		if err := os.WriteFile(filePath, []byte("not a directory"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := verifyWorktreeExists(filePath)
		if err == nil {
			t.Error("verifyWorktreeExists() should return error when path is a file")
		}
	})

	t.Run("directory without .git", func(t *testing.T) {
		dir := t.TempDir()
		// Empty directory - no .git file or directory

		err := verifyWorktreeExists(dir)
		if err == nil {
			t.Error("verifyWorktreeExists() should return error when .git is missing")
		}
	})
}

func TestSlingSpawnOptionsDefaults(t *testing.T) {
	// Verify zero-value SlingSpawnOptions represents no overrides
	opts := SlingSpawnOptions{}
	if opts.Force {
		t.Error("Force should default to false")
	}
	if opts.Account != "" {
		t.Error("Account should default to empty")
	}
	if opts.Create {
		t.Error("Create should default to false")
	}
	if opts.HookBead != "" {
		t.Error("HookBead should default to empty")
	}
	if opts.Agent != "" {
		t.Error("Agent should default to empty")
	}
}

func TestSpawnedPolecatInfoFields(t *testing.T) {
	info := &SpawnedPolecatInfo{
		RigName:     "gastown",
		PolecatName: "Furiosa",
		ClonePath:   "/home/user/ai/gastown/polecats/Furiosa/gastown",
		SessionName: "gt-gastown-Furiosa",
		Pane:        "",
		account:     "work",
		agent:       "claude",
	}

	// Verify all fields are accessible and correct
	if info.RigName != "gastown" {
		t.Errorf("RigName = %q, want gastown", info.RigName)
	}
	if info.PolecatName != "Furiosa" {
		t.Errorf("PolecatName = %q, want Furiosa", info.PolecatName)
	}
	if info.ClonePath != "/home/user/ai/gastown/polecats/Furiosa/gastown" {
		t.Errorf("ClonePath = %q, unexpected", info.ClonePath)
	}
	if info.SessionName != "gt-gastown-Furiosa" {
		t.Errorf("SessionName = %q, want gt-gastown-Furiosa", info.SessionName)
	}
	if info.SessionStarted() {
		t.Error("SessionStarted() should be false before StartSession")
	}
	if info.AgentID() != "gastown/polecats/Furiosa" {
		t.Errorf("AgentID() = %q, want gastown/polecats/Furiosa", info.AgentID())
	}

	// Simulate session start
	info.Pane = "%5"
	if !info.SessionStarted() {
		t.Error("SessionStarted() should be true after setting Pane")
	}
}

func TestVerifyWorktreeExistsEdgeCases(t *testing.T) {
	t.Run("symlinked .git file", func(t *testing.T) {
		dir := t.TempDir()

		// Create a real .git file and symlink to it
		realGit := filepath.Join(dir, ".git-real")
		if err := os.WriteFile(realGit, []byte("gitdir: /path/to/repo\n"), 0644); err != nil {
			t.Fatalf("write .git-real: %v", err)
		}

		gitLink := filepath.Join(dir, ".git")
		if err := os.Symlink(realGit, gitLink); err != nil {
			t.Fatalf("symlink .git: %v", err)
		}

		// Symlinked .git should be accepted
		if err := verifyWorktreeExists(dir); err != nil {
			t.Errorf("verifyWorktreeExists() should accept symlinked .git: %v", err)
		}
	})

	t.Run("empty .git file", func(t *testing.T) {
		dir := t.TempDir()

		// Empty .git file - technically invalid but verifyWorktreeExists only
		// checks existence, not content
		gitFile := filepath.Join(dir, ".git")
		if err := os.WriteFile(gitFile, []byte(""), 0644); err != nil {
			t.Fatalf("write .git: %v", err)
		}

		if err := verifyWorktreeExists(dir); err != nil {
			t.Errorf("verifyWorktreeExists() returned error for empty .git file: %v", err)
		}
	})
}
