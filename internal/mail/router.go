package mail

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/steveyegge/gastown/internal/tmux"
)

// Router handles message delivery via beads.
type Router struct {
	workDir string // directory to run bd commands in
	tmux    *tmux.Tmux
}

// NewRouter creates a new mail router.
// workDir should be a directory containing a .beads database.
func NewRouter(workDir string) *Router {
	return &Router{
		workDir: workDir,
		tmux:    tmux.NewTmux(),
	}
}

// Send delivers a message via beads mail.
func (r *Router) Send(msg *Message) error {
	// Convert addresses to beads identities
	toIdentity := addressToIdentity(msg.To)
	fromIdentity := addressToIdentity(msg.From)

	// Build command: bd mail send <recipient> -s <subject> -m <body> --identity <sender>
	args := []string{"mail", "send", toIdentity,
		"-s", msg.Subject,
		"-m", msg.Body,
		"--identity", fromIdentity,
	}

	// Add --urgent flag for high priority
	if msg.Priority == PriorityHigh {
		args = append(args, "--urgent")
	}

	cmd := exec.Command("bd", args...)
	cmd.Dir = r.workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return errors.New(errMsg)
		}
		return fmt.Errorf("sending message: %w", err)
	}

	// Optionally notify if recipient is a polecat with active session
	if isPolecat(msg.To) && msg.Priority == PriorityHigh {
		r.notifyPolecat(msg)
	}

	return nil
}

// GetMailbox returns a Mailbox for the given address.
func (r *Router) GetMailbox(address string) (*Mailbox, error) {
	return NewMailboxFromAddress(address, r.workDir), nil
}

// notifyPolecat sends a notification to a polecat's tmux session.
func (r *Router) notifyPolecat(msg *Message) error {
	// Parse rig/polecat from address
	parts := strings.SplitN(msg.To, "/", 2)
	if len(parts) != 2 {
		return nil
	}

	rig := parts[0]
	polecat := parts[1]

	// Generate session name (matches session.Manager)
	sessionID := fmt.Sprintf("gt-%s-%s", rig, polecat)

	// Check if session exists
	hasSession, err := r.tmux.HasSession(sessionID)
	if err != nil || !hasSession {
		return nil // No active session, skip notification
	}

	// Inject notification
	notification := fmt.Sprintf("[MAIL] %s", msg.Subject)
	return r.tmux.SendKeys(sessionID, notification)
}

// isPolecat checks if an address points to a polecat.
func isPolecat(address string) bool {
	// Not mayor, not refinery, has rig/name format
	if strings.HasPrefix(address, "mayor") {
		return false
	}

	parts := strings.SplitN(address, "/", 2)
	if len(parts) != 2 {
		return false
	}

	target := parts[1]
	return target != "" && target != "refinery"
}
