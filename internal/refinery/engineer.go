// Package refinery provides the merge queue processing agent.
package refinery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mrqueue"
	"github.com/steveyegge/gastown/internal/rig"
)

// MergeQueueConfig holds configuration for the merge queue processor.
type MergeQueueConfig struct {
	// Enabled controls whether the merge queue is active.
	Enabled bool `json:"enabled"`

	// TargetBranch is the default branch to merge to (e.g., "main").
	TargetBranch string `json:"target_branch"`

	// IntegrationBranches enables per-epic integration branches.
	IntegrationBranches bool `json:"integration_branches"`

	// OnConflict is the strategy for handling conflicts: "assign_back" or "auto_rebase".
	OnConflict string `json:"on_conflict"`

	// RunTests controls whether to run tests before merging.
	RunTests bool `json:"run_tests"`

	// TestCommand is the command to run for testing.
	TestCommand string `json:"test_command"`

	// DeleteMergedBranches controls whether to delete branches after merge.
	DeleteMergedBranches bool `json:"delete_merged_branches"`

	// RetryFlakyTests is the number of times to retry flaky tests.
	RetryFlakyTests int `json:"retry_flaky_tests"`

	// PollInterval is how often to check for new MRs.
	PollInterval time.Duration `json:"poll_interval"`

	// MaxConcurrent is the maximum number of MRs to process concurrently.
	MaxConcurrent int `json:"max_concurrent"`
}

// DefaultMergeQueueConfig returns sensible defaults for merge queue configuration.
func DefaultMergeQueueConfig() *MergeQueueConfig {
	return &MergeQueueConfig{
		Enabled:              true,
		TargetBranch:         "main",
		IntegrationBranches:  true,
		OnConflict:           "assign_back",
		RunTests:             true,
		TestCommand:          "",
		DeleteMergedBranches: true,
		RetryFlakyTests:      1,
		PollInterval:         30 * time.Second,
		MaxConcurrent:        1,
	}
}

// Engineer is the merge queue processor that polls for ready merge-requests
// and processes them according to the merge queue design.
type Engineer struct {
	rig         *rig.Rig
	beads       *beads.Beads
	mrQueue     *mrqueue.Queue
	git         *git.Git
	config      *MergeQueueConfig
	workDir     string
	output      io.Writer // Output destination for user-facing messages
	eventLogger *mrqueue.EventLogger

	// stopCh is used for graceful shutdown
	stopCh chan struct{}
}

// NewEngineer creates a new Engineer for the given rig.
func NewEngineer(r *rig.Rig) *Engineer {
	return &Engineer{
		rig:         r,
		beads:       beads.New(r.Path),
		mrQueue:     mrqueue.New(r.Path),
		git:         git.NewGit(r.Path),
		config:      DefaultMergeQueueConfig(),
		workDir:     r.Path,
		output:      os.Stdout,
		eventLogger: mrqueue.NewEventLoggerFromRig(r.Path),
		stopCh:      make(chan struct{}),
	}
}

// SetOutput sets the output writer for user-facing messages.
// This is useful for testing or redirecting output.
func (e *Engineer) SetOutput(w io.Writer) {
	e.output = w
}

// LoadConfig loads merge queue configuration from the rig's config.json.
func (e *Engineer) LoadConfig() error {
	configPath := filepath.Join(e.rig.Path, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Use defaults if no config file
			return nil
		}
		return fmt.Errorf("reading config: %w", err)
	}

	// Parse config file to extract merge_queue section
	var rawConfig struct {
		MergeQueue json.RawMessage `json:"merge_queue"`
	}
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	if rawConfig.MergeQueue == nil {
		// No merge_queue section, use defaults
		return nil
	}

	// Parse merge_queue section into our config struct
	// We need special handling for poll_interval (string -> Duration)
	var mqRaw struct {
		Enabled              *bool   `json:"enabled"`
		TargetBranch         *string `json:"target_branch"`
		IntegrationBranches  *bool   `json:"integration_branches"`
		OnConflict           *string `json:"on_conflict"`
		RunTests             *bool   `json:"run_tests"`
		TestCommand          *string `json:"test_command"`
		DeleteMergedBranches *bool   `json:"delete_merged_branches"`
		RetryFlakyTests      *int    `json:"retry_flaky_tests"`
		PollInterval         *string `json:"poll_interval"`
		MaxConcurrent        *int    `json:"max_concurrent"`
	}

	if err := json.Unmarshal(rawConfig.MergeQueue, &mqRaw); err != nil {
		return fmt.Errorf("parsing merge_queue config: %w", err)
	}

	// Apply non-nil values to config (preserving defaults for missing fields)
	if mqRaw.Enabled != nil {
		e.config.Enabled = *mqRaw.Enabled
	}
	if mqRaw.TargetBranch != nil {
		e.config.TargetBranch = *mqRaw.TargetBranch
	}
	if mqRaw.IntegrationBranches != nil {
		e.config.IntegrationBranches = *mqRaw.IntegrationBranches
	}
	if mqRaw.OnConflict != nil {
		e.config.OnConflict = *mqRaw.OnConflict
	}
	if mqRaw.RunTests != nil {
		e.config.RunTests = *mqRaw.RunTests
	}
	if mqRaw.TestCommand != nil {
		e.config.TestCommand = *mqRaw.TestCommand
	}
	if mqRaw.DeleteMergedBranches != nil {
		e.config.DeleteMergedBranches = *mqRaw.DeleteMergedBranches
	}
	if mqRaw.RetryFlakyTests != nil {
		e.config.RetryFlakyTests = *mqRaw.RetryFlakyTests
	}
	if mqRaw.MaxConcurrent != nil {
		e.config.MaxConcurrent = *mqRaw.MaxConcurrent
	}
	if mqRaw.PollInterval != nil {
		dur, err := time.ParseDuration(*mqRaw.PollInterval)
		if err != nil {
			return fmt.Errorf("invalid poll_interval %q: %w", *mqRaw.PollInterval, err)
		}
		e.config.PollInterval = dur
	}

	return nil
}

// Config returns the current merge queue configuration.
func (e *Engineer) Config() *MergeQueueConfig {
	return e.config
}

// ProcessResult contains the result of processing a merge request.
type ProcessResult struct {
	Success     bool
	MergeCommit string
	Error       string
	Conflict    bool
	TestsFailed bool
}

// ProcessMR processes a single merge request from a beads issue.
// Note: The Refinery agent primarily processes merges via git commands
// in the role prompt, not via this Go code path.
func (e *Engineer) ProcessMR(ctx context.Context, mr *beads.Issue) ProcessResult {
	// Parse MR fields from description
	mrFields := beads.ParseMRFields(mr)
	if mrFields == nil {
		return ProcessResult{
			Success: false,
			Error:   "no MR fields found in description",
		}
	}

	// Log what we would process
	fmt.Fprintln(e.output, "[Engineer] Would process:")
	fmt.Fprintf(e.output, "  Branch: %s\n", mrFields.Branch)
	fmt.Fprintf(e.output, "  Target: %s\n", mrFields.Target)
	fmt.Fprintf(e.output, "  Worker: %s\n", mrFields.Worker)

	// This code path is not used in production - Refinery agent uses git commands
	return ProcessResult{
		Success: false,
		Error:   "ProcessMR: use Refinery agent's git-based merge flow instead",
	}
}

// handleSuccess handles a successful merge completion.
// Steps:
// 1. Update MR with merge_commit SHA
// 2. Close MR with reason 'merged'
// 3. Close source issue with reference to MR
// 4. Delete source branch if configured
// 5. Log success
func (e *Engineer) handleSuccess(mr *beads.Issue, result ProcessResult) {
	// Parse MR fields from description
	mrFields := beads.ParseMRFields(mr)
	if mrFields == nil {
		mrFields = &beads.MRFields{}
	}

	// 1. Update MR with merge_commit SHA
	mrFields.MergeCommit = result.MergeCommit
	mrFields.CloseReason = "merged"
	newDesc := beads.SetMRFields(mr, mrFields)
	if err := e.beads.Update(mr.ID, beads.UpdateOptions{Description: &newDesc}); err != nil {
		fmt.Fprintf(e.output, "[Engineer] Warning: failed to update MR %s with merge commit: %v\n", mr.ID, err)
	}

	// 2. Close MR with reason 'merged'
	if err := e.beads.CloseWithReason("merged", mr.ID); err != nil {
		fmt.Fprintf(e.output, "[Engineer] Warning: failed to close MR %s: %v\n", mr.ID, err)
	}

	// 3. Close source issue with reference to MR
	if mrFields.SourceIssue != "" {
		closeReason := fmt.Sprintf("Merged in %s", mr.ID)
		if err := e.beads.CloseWithReason(closeReason, mrFields.SourceIssue); err != nil {
			fmt.Fprintf(e.output, "[Engineer] Warning: failed to close source issue %s: %v\n", mrFields.SourceIssue, err)
		} else {
			fmt.Fprintf(e.output, "[Engineer] Closed source issue: %s\n", mrFields.SourceIssue)
		}
	}

	// 3.5. Clear agent bead's active_mr reference (traceability cleanup)
	if mrFields.AgentBead != "" {
		if err := e.beads.UpdateAgentActiveMR(mrFields.AgentBead, ""); err != nil {
			fmt.Fprintf(e.output, "[Engineer] Warning: failed to clear agent bead %s active_mr: %v\n", mrFields.AgentBead, err)
		}
	}

	// 4. Delete source branch if configured (local only - branches never go to origin)
	if e.config.DeleteMergedBranches && mrFields.Branch != "" {
		if err := e.git.DeleteBranch(mrFields.Branch, true); err != nil {
			fmt.Fprintf(e.output, "[Engineer] Warning: failed to delete branch %s: %v\n", mrFields.Branch, err)
		} else {
			fmt.Fprintf(e.output, "[Engineer] Deleted local branch: %s\n", mrFields.Branch)
		}
	}

	// 5. Log success
	fmt.Fprintf(e.output, "[Engineer] ✓ Merged: %s (commit: %s)\n", mr.ID, result.MergeCommit)
}

// handleFailure handles a failed merge request.
// Reopens the MR for rework and logs the failure.
func (e *Engineer) handleFailure(mr *beads.Issue, result ProcessResult) {
	// Reopen the MR (back to open status for rework)
	open := "open"
	if err := e.beads.Update(mr.ID, beads.UpdateOptions{Status: &open}); err != nil {
		fmt.Fprintf(e.output, "[Engineer] Warning: failed to reopen MR %s: %v\n", mr.ID, err)
	}

	// Log the failure
	fmt.Fprintf(e.output, "[Engineer] ✗ Failed: %s - %s\n", mr.ID, result.Error)
}

// ProcessMRFromQueue processes a merge request from wisp queue.
// Note: The Refinery agent primarily processes merges via git commands
// in the role prompt, not via this Go code path.
func (e *Engineer) ProcessMRFromQueue(ctx context.Context, mr *mrqueue.MR) ProcessResult {
	// MR fields are directly on the struct (no parsing needed)
	fmt.Fprintln(e.output, "[Engineer] Processing MR from queue:")
	fmt.Fprintf(e.output, "  Branch: %s\n", mr.Branch)
	fmt.Fprintf(e.output, "  Target: %s\n", mr.Target)
	fmt.Fprintf(e.output, "  Worker: %s\n", mr.Worker)
	fmt.Fprintf(e.output, "  Source: %s\n", mr.SourceIssue)

	// Emit merge_started event
	if err := e.eventLogger.LogMergeStarted(mr); err != nil {
		fmt.Fprintf(e.output, "[Engineer] Warning: failed to log merge_started event: %v\n", err)
	}

	// This code path is not used in production - Refinery agent uses git commands
	return ProcessResult{
		Success: false,
		Error:   "ProcessMRFromQueue: use Refinery agent's git-based merge flow instead",
	}
}

// handleSuccessFromQueue handles a successful merge from wisp queue.
func (e *Engineer) handleSuccessFromQueue(mr *mrqueue.MR, result ProcessResult) {
	// Emit merged event
	if err := e.eventLogger.LogMerged(mr, result.MergeCommit); err != nil {
		fmt.Fprintf(e.output, "[Engineer] Warning: failed to log merged event: %v\n", err)
	}

	// 1. Close source issue with reference to MR
	if mr.SourceIssue != "" {
		closeReason := fmt.Sprintf("Merged in %s", mr.ID)
		if err := e.beads.CloseWithReason(closeReason, mr.SourceIssue); err != nil {
			fmt.Fprintf(e.output, "[Engineer] Warning: failed to close source issue %s: %v\n", mr.SourceIssue, err)
		} else {
			fmt.Fprintf(e.output, "[Engineer] Closed source issue: %s\n", mr.SourceIssue)
		}
	}

	// 1.5. Clear agent bead's active_mr reference (traceability cleanup)
	if mr.AgentBead != "" {
		if err := e.beads.UpdateAgentActiveMR(mr.AgentBead, ""); err != nil {
			fmt.Fprintf(e.output, "[Engineer] Warning: failed to clear agent bead %s active_mr: %v\n", mr.AgentBead, err)
		}
	}

	// 2. Delete source branch if configured (local only)
	if e.config.DeleteMergedBranches && mr.Branch != "" {
		if err := e.git.DeleteBranch(mr.Branch, true); err != nil {
			fmt.Fprintf(e.output, "[Engineer] Warning: failed to delete branch %s: %v\n", mr.Branch, err)
		} else {
			fmt.Fprintf(e.output, "[Engineer] Deleted local branch: %s\n", mr.Branch)
		}
	}

	// 3. Remove MR from queue (ephemeral - just delete the file)
	if err := e.mrQueue.Remove(mr.ID); err != nil {
		fmt.Fprintf(e.output, "[Engineer] Warning: failed to remove MR from queue: %v\n", err)
	}

	// 4. Log success
	fmt.Fprintf(e.output, "[Engineer] ✓ Merged: %s (commit: %s)\n", mr.ID, result.MergeCommit)
}

// handleFailureFromQueue handles a failed merge from wisp queue.
func (e *Engineer) handleFailureFromQueue(mr *mrqueue.MR, result ProcessResult) {
	// Emit merge_failed event
	if err := e.eventLogger.LogMergeFailed(mr, result.Error); err != nil {
		fmt.Fprintf(e.output, "[Engineer] Warning: failed to log merge_failed event: %v\n", err)
	}

	// MR stays in queue for retry - no action needed on the file
	// Log the failure
	fmt.Fprintf(e.output, "[Engineer] ✗ Failed: %s - %s\n", mr.ID, result.Error)
	fmt.Fprintln(e.output, "[Engineer] MR remains in queue for retry")
}
