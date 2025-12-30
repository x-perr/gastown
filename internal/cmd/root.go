// Package cmd provides CLI commands for the gt tool.
package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/keepalive"
)

var rootCmd = &cobra.Command{
	Use:     "gt",
	Short:   "Gas Town - Multi-agent workspace manager",
	Version: Version,
	Long: `Gas Town (gt) manages multi-agent workspaces called rigs.

It coordinates agent spawning, work distribution, and communication
across distributed teams of AI agents working on shared codebases.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Signal agent activity by touching keepalive file
		// Build command path: gt status, gt mail send, etc.
		cmdPath := buildCommandPath(cmd)
		keepalive.TouchWithArgs(cmdPath, args)

		// Also signal town-level activity for daemon exponential backoff
		// This resets the backoff when any gt command runs
		keepalive.TouchTownActivity(cmdPath)
	},
}

// Execute runs the root command and returns an exit code.
// The caller (main) should call os.Exit with this code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		// Check for silent exit (scripting commands that signal status via exit code)
		if code, ok := IsSilentExit(err); ok {
			return code
		}
		// Other errors already printed by cobra
		return 1
	}
	return 0
}

// Command group IDs - used by subcommands to organize help output
const (
	GroupWork      = "work"
	GroupAgents    = "agents"
	GroupComm      = "comm"
	GroupServices  = "services"
	GroupWorkspace = "workspace"
	GroupConfig    = "config"
	GroupDiag      = "diag"
)

func init() {
	// Enable prefix matching for subcommands (e.g., "gt ref at" -> "gt refinery attach")
	cobra.EnablePrefixMatching = true

	// Define command groups (order determines help output order)
	rootCmd.AddGroup(
		&cobra.Group{ID: GroupWork, Title: "Work Management:"},
		&cobra.Group{ID: GroupAgents, Title: "Agent Management:"},
		&cobra.Group{ID: GroupComm, Title: "Communication:"},
		&cobra.Group{ID: GroupServices, Title: "Services:"},
		&cobra.Group{ID: GroupWorkspace, Title: "Workspace:"},
		&cobra.Group{ID: GroupConfig, Title: "Configuration:"},
		&cobra.Group{ID: GroupDiag, Title: "Diagnostics:"},
	)

	// Put help and completion in a sensible group
	rootCmd.SetHelpCommandGroupID(GroupDiag)
	rootCmd.SetCompletionCommandGroupID(GroupConfig)

	// Global flags can be added here
	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file")
}

// buildCommandPath walks the command hierarchy to build the full command path.
// For example: "gt mail send", "gt status", etc.
func buildCommandPath(cmd *cobra.Command) string {
	var parts []string
	for c := cmd; c != nil; c = c.Parent() {
		parts = append([]string{c.Name()}, parts...)
	}
	return strings.Join(parts, " ")
}
