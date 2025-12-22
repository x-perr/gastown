package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

// EphemeralExistsCheck verifies that .beads-ephemeral/ exists for each rig.
type EphemeralExistsCheck struct {
	FixableCheck
	missingRigs []string // Cached for fix
}

// NewEphemeralExistsCheck creates a new ephemeral exists check.
func NewEphemeralExistsCheck() *EphemeralExistsCheck {
	return &EphemeralExistsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "ephemeral-exists",
				CheckDescription: "Check if ephemeral beads directory exists for each rig",
			},
		},
	}
}

// Run checks if .beads-ephemeral/ exists for each rig.
func (c *EphemeralExistsCheck) Run(ctx *CheckContext) *CheckResult {
	c.missingRigs = nil // Reset cache

	// Find all rigs
	rigs, err := c.discoverRigs(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to discover rigs",
			Details: []string{err.Error()},
		}
	}

	if len(rigs) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No rigs configured",
		}
	}

	// Check each rig
	var missing []string
	for _, rigName := range rigs {
		ephemeralPath := filepath.Join(ctx.TownRoot, rigName, ".beads-ephemeral")
		if _, err := os.Stat(ephemeralPath); os.IsNotExist(err) {
			missing = append(missing, rigName)
		}
	}

	if len(missing) > 0 {
		c.missingRigs = missing
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d rig(s) missing ephemeral beads directory", len(missing)),
			Details: missing,
			FixHint: "Run 'gt doctor --fix' to create missing directories",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("All %d rig(s) have ephemeral beads directory", len(rigs)),
	}
}

// Fix creates missing .beads-ephemeral/ directories.
func (c *EphemeralExistsCheck) Fix(ctx *CheckContext) error {
	for _, rigName := range c.missingRigs {
		ephemeralPath := filepath.Join(ctx.TownRoot, rigName, ".beads-ephemeral")
		if err := os.MkdirAll(ephemeralPath, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", ephemeralPath, err)
		}
	}
	return nil
}

// discoverRigs finds all registered rigs.
func (c *EphemeralExistsCheck) discoverRigs(townRoot string) ([]string, error) {
	rigsPath := filepath.Join(townRoot, "mayor", "rigs.json")
	data, err := os.ReadFile(rigsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No rigs configured
		}
		return nil, err
	}

	var rigsConfig config.RigsConfig
	if err := json.Unmarshal(data, &rigsConfig); err != nil {
		return nil, err
	}

	var rigs []string
	for name := range rigsConfig.Rigs {
		rigs = append(rigs, name)
	}
	return rigs, nil
}

// EphemeralGitCheck verifies that .beads-ephemeral/ is a valid git repo.
type EphemeralGitCheck struct {
	FixableCheck
	invalidRigs []string // Cached for fix
}

// NewEphemeralGitCheck creates a new ephemeral git check.
func NewEphemeralGitCheck() *EphemeralGitCheck {
	return &EphemeralGitCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "ephemeral-git",
				CheckDescription: "Check if ephemeral beads directories are valid git repos",
			},
		},
	}
}

// Run checks if .beads-ephemeral/ directories are valid git repos.
func (c *EphemeralGitCheck) Run(ctx *CheckContext) *CheckResult {
	c.invalidRigs = nil // Reset cache

	// Find all rigs
	rigs, err := discoverRigs(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to discover rigs",
			Details: []string{err.Error()},
		}
	}

	if len(rigs) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No rigs configured",
		}
	}

	// Check each rig that has an ephemeral dir
	var invalid []string
	var checked int
	for _, rigName := range rigs {
		ephemeralPath := filepath.Join(ctx.TownRoot, rigName, ".beads-ephemeral")
		if _, err := os.Stat(ephemeralPath); os.IsNotExist(err) {
			continue // Skip if directory doesn't exist (handled by ephemeral-exists)
		}
		checked++

		// Check if it's a valid git repo
		gitDir := filepath.Join(ephemeralPath, ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			invalid = append(invalid, rigName)
		}
	}

	if checked == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No ephemeral beads directories to check",
		}
	}

	if len(invalid) > 0 {
		c.invalidRigs = invalid
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d ephemeral beads directory(ies) not initialized as git", len(invalid)),
			Details: invalid,
			FixHint: "Run 'gt doctor --fix' to initialize git repos",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("All %d ephemeral beads directories are valid git repos", checked),
	}
}

// Fix initializes git repos in ephemeral directories.
func (c *EphemeralGitCheck) Fix(ctx *CheckContext) error {
	for _, rigName := range c.invalidRigs {
		ephemeralPath := filepath.Join(ctx.TownRoot, rigName, ".beads-ephemeral")
		cmd := exec.Command("git", "init")
		cmd.Dir = ephemeralPath
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("initializing git in %s: %w", ephemeralPath, err)
		}

		// Create config.yaml for ephemeral beads
		configPath := filepath.Join(ephemeralPath, "config.yaml")
		configContent := "ephemeral: true\n# No sync-branch - ephemeral is local only\n"
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			return fmt.Errorf("creating config.yaml in %s: %w", ephemeralPath, err)
		}
	}
	return nil
}

// EphemeralOrphansCheck detects molecules started but never squashed (>24h old).
type EphemeralOrphansCheck struct {
	BaseCheck
}

// NewEphemeralOrphansCheck creates a new ephemeral orphans check.
func NewEphemeralOrphansCheck() *EphemeralOrphansCheck {
	return &EphemeralOrphansCheck{
		BaseCheck: BaseCheck{
			CheckName:        "ephemeral-orphans",
			CheckDescription: "Check for orphaned molecules (>24h old, never squashed)",
		},
	}
}

// Run checks for orphaned molecules.
func (c *EphemeralOrphansCheck) Run(ctx *CheckContext) *CheckResult {
	rigs, err := discoverRigs(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to discover rigs",
			Details: []string{err.Error()},
		}
	}

	if len(rigs) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No rigs configured",
		}
	}

	var orphans []string
	cutoff := time.Now().Add(-24 * time.Hour)

	for _, rigName := range rigs {
		ephemeralPath := filepath.Join(ctx.TownRoot, rigName, ".beads-ephemeral")
		if _, err := os.Stat(ephemeralPath); os.IsNotExist(err) {
			continue
		}

		// Look for molecule directories or issue files older than 24h
		issuesPath := filepath.Join(ephemeralPath, "issues.jsonl")
		info, err := os.Stat(issuesPath)
		if err != nil {
			continue // No issues file
		}

		// Check if the issues file is old and non-empty
		if info.ModTime().Before(cutoff) && info.Size() > 0 {
			orphans = append(orphans, fmt.Sprintf("%s: issues.jsonl last modified %s",
				rigName, info.ModTime().Format("2006-01-02 15:04")))
		}
	}

	if len(orphans) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d rig(s) have stale ephemeral data (>24h old)", len(orphans)),
			Details: orphans,
			FixHint: "Manual review required - these may contain unsquashed work",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "No orphaned molecules found",
	}
}

// EphemeralSizeCheck warns if ephemeral repo is too large (>100MB).
type EphemeralSizeCheck struct {
	BaseCheck
}

// NewEphemeralSizeCheck creates a new ephemeral size check.
func NewEphemeralSizeCheck() *EphemeralSizeCheck {
	return &EphemeralSizeCheck{
		BaseCheck: BaseCheck{
			CheckName:        "ephemeral-size",
			CheckDescription: "Check if ephemeral beads directories are too large (>100MB)",
		},
	}
}

// Run checks the size of ephemeral beads directories.
func (c *EphemeralSizeCheck) Run(ctx *CheckContext) *CheckResult {
	rigs, err := discoverRigs(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to discover rigs",
			Details: []string{err.Error()},
		}
	}

	if len(rigs) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No rigs configured",
		}
	}

	const maxSize = 100 * 1024 * 1024 // 100MB
	var oversized []string

	for _, rigName := range rigs {
		ephemeralPath := filepath.Join(ctx.TownRoot, rigName, ".beads-ephemeral")
		if _, err := os.Stat(ephemeralPath); os.IsNotExist(err) {
			continue
		}

		size, err := dirSize(ephemeralPath)
		if err != nil {
			continue
		}

		if size > maxSize {
			oversized = append(oversized, fmt.Sprintf("%s: %s",
				rigName, formatSize(size)))
		}
	}

	if len(oversized) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d rig(s) have oversized ephemeral directories", len(oversized)),
			Details: oversized,
			FixHint: "Consider cleaning up old completed molecules",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "All ephemeral directories within size limits",
	}
}

// EphemeralStaleCheck detects molecules with no activity in the last hour.
type EphemeralStaleCheck struct {
	BaseCheck
}

// NewEphemeralStaleCheck creates a new ephemeral stale check.
func NewEphemeralStaleCheck() *EphemeralStaleCheck {
	return &EphemeralStaleCheck{
		BaseCheck: BaseCheck{
			CheckName:        "ephemeral-stale",
			CheckDescription: "Check for stale molecules (no activity in last hour)",
		},
	}
}

// Run checks for stale molecules.
func (c *EphemeralStaleCheck) Run(ctx *CheckContext) *CheckResult {
	rigs, err := discoverRigs(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to discover rigs",
			Details: []string{err.Error()},
		}
	}

	if len(rigs) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No rigs configured",
		}
	}

	var stale []string
	cutoff := time.Now().Add(-1 * time.Hour)

	for _, rigName := range rigs {
		ephemeralPath := filepath.Join(ctx.TownRoot, rigName, ".beads-ephemeral")
		if _, err := os.Stat(ephemeralPath); os.IsNotExist(err) {
			continue
		}

		// Check for any recent activity in the ephemeral directory
		// We look at the most recent modification time of any file
		var mostRecent time.Time
		_ = filepath.Walk(ephemeralPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && info.ModTime().After(mostRecent) {
				mostRecent = info.ModTime()
			}
			return nil
		})

		// If there are files and the most recent is older than 1 hour
		if !mostRecent.IsZero() && mostRecent.Before(cutoff) {
			stale = append(stale, fmt.Sprintf("%s: last activity %s ago",
				rigName, formatDuration(time.Since(mostRecent))))
		}
	}

	if len(stale) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d rig(s) have stale ephemeral activity", len(stale)),
			Details: stale,
			FixHint: "Check if polecats are stuck or crashed",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "No stale ephemeral activity detected",
	}
}

// Helper functions

// discoverRigs finds all registered rigs (shared helper).
func discoverRigs(townRoot string) ([]string, error) {
	rigsPath := filepath.Join(townRoot, "mayor", "rigs.json")
	data, err := os.ReadFile(rigsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var rigsConfig config.RigsConfig
	if err := json.Unmarshal(data, &rigsConfig); err != nil {
		return nil, err
	}

	var rigs []string
	for name := range rigsConfig.Rigs {
		rigs = append(rigs, name)
	}
	return rigs, nil
}

// dirSize calculates the total size of a directory.
func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// formatSize formats bytes as human-readable size.
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

// formatDuration formats a duration as human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0f seconds", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0f minutes", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
	return fmt.Sprintf("%.1f days", d.Hours()/24)
}
