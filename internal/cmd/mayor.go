package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

// MayorSessionName is the tmux session name for the Mayor.
const MayorSessionName = "gt-mayor"

var mayorCmd = &cobra.Command{
	Use:   "mayor",
	Short: "Manage the Mayor session",
	Long: `Manage the Mayor tmux session.

The Mayor is the global coordinator for Gas Town, running as a persistent
tmux session. Use the subcommands to start, stop, attach, and check status.`,
}

var mayorStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Mayor session",
	Long: `Start the Mayor tmux session.

Creates a new detached tmux session for the Mayor and launches Claude.
The session runs in the workspace root directory.`,
	RunE: runMayorStart,
}

var mayorStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Mayor session",
	Long: `Stop the Mayor tmux session.

Attempts graceful shutdown first (Ctrl-C), then kills the tmux session.`,
	RunE: runMayorStop,
}

var mayorAttachCmd = &cobra.Command{
	Use:     "attach",
	Aliases: []string{"at"},
	Short:   "Attach to the Mayor session",
	Long: `Attach to the running Mayor tmux session.

Attaches the current terminal to the Mayor's tmux session.
Detach with Ctrl-B D.`,
	RunE: runMayorAttach,
}

var mayorStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check Mayor session status",
	Long:  `Check if the Mayor tmux session is currently running.`,
	RunE:  runMayorStatus,
}

var mayorRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Mayor session",
	Long: `Restart the Mayor tmux session.

Stops the current session (if running) and starts a fresh one.`,
	RunE: runMayorRestart,
}

func init() {
	mayorCmd.AddCommand(mayorStartCmd)
	mayorCmd.AddCommand(mayorStopCmd)
	mayorCmd.AddCommand(mayorAttachCmd)
	mayorCmd.AddCommand(mayorStatusCmd)
	mayorCmd.AddCommand(mayorRestartCmd)

	rootCmd.AddCommand(mayorCmd)
}

func runMayorStart(cmd *cobra.Command, args []string) error {
	// Find workspace root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	t := tmux.NewTmux()

	// Check if session already exists
	running, err := t.HasSession(MayorSessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if running {
		return fmt.Errorf("Mayor session already running. Attach with: gt mayor attach")
	}

	// Create session in workspace root
	fmt.Println("Starting Mayor session...")
	if err := t.NewSession(MayorSessionName, townRoot); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	// Set environment
	t.SetEnvironment(MayorSessionName, "GT_ROLE", "mayor")

	// Launch Claude with full permissions (Mayor is trusted)
	command := "claude --dangerously-skip-permissions"
	if err := t.SendKeys(MayorSessionName, command); err != nil {
		return fmt.Errorf("sending command: %w", err)
	}

	fmt.Printf("%s Mayor session started. Attach with: %s\n",
		style.Bold.Render("✓"),
		style.Dim.Render("gt mayor attach"))

	return nil
}

func runMayorStop(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	// Check if session exists
	running, err := t.HasSession(MayorSessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return errors.New("Mayor session is not running")
	}

	fmt.Println("Stopping Mayor session...")

	// Try graceful shutdown first
	t.SendKeysRaw(MayorSessionName, "C-c")
	time.Sleep(100 * time.Millisecond)

	// Kill the session
	if err := t.KillSession(MayorSessionName); err != nil {
		return fmt.Errorf("killing session: %w", err)
	}

	fmt.Printf("%s Mayor session stopped.\n", style.Bold.Render("✓"))
	return nil
}

func runMayorAttach(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	// Check if session exists
	running, err := t.HasSession(MayorSessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return errors.New("Mayor session is not running. Start with: gt mayor start")
	}

	// Use exec to replace current process with tmux attach
	// This is the standard pattern for attaching to tmux sessions
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found: %w", err)
	}

	return execCommand(tmuxPath, "attach-session", "-t", MayorSessionName)
}

// execCommand replaces the current process with the given command.
// This is used for attaching to tmux sessions.
func execCommand(name string, args ...string) error {
	// On Unix, we would use syscall.Exec to replace the process
	// For portability, we use exec.Command and wait
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runMayorStatus(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	running, err := t.HasSession(MayorSessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}

	if running {
		// Get session info for more details
		info, err := t.GetSessionInfo(MayorSessionName)
		if err == nil {
			status := "detached"
			if info.Attached {
				status = "attached"
			}
			fmt.Printf("%s Mayor session is %s\n",
				style.Bold.Render("●"),
				style.Bold.Render("running"))
			fmt.Printf("  Status: %s\n", status)
			fmt.Printf("  Created: %s\n", info.Created)
			fmt.Printf("\nAttach with: %s\n", style.Dim.Render("gt mayor attach"))
		} else {
			fmt.Printf("%s Mayor session is %s\n",
				style.Bold.Render("●"),
				style.Bold.Render("running"))
		}
	} else {
		fmt.Printf("%s Mayor session is %s\n",
			style.Dim.Render("○"),
			"not running")
		fmt.Printf("\nStart with: %s\n", style.Dim.Render("gt mayor start"))
	}

	return nil
}

func runMayorRestart(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	// Stop if running
	running, err := t.HasSession(MayorSessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if running {
		fmt.Println("Stopping Mayor session...")
		t.SendKeysRaw(MayorSessionName, "C-c")
		time.Sleep(100 * time.Millisecond)
		if err := t.KillSession(MayorSessionName); err != nil {
			return fmt.Errorf("killing session: %w", err)
		}
		fmt.Printf("%s Mayor session stopped.\n", style.Bold.Render("✓"))
	}

	// Start fresh
	return runMayorStart(cmd, args)
}
