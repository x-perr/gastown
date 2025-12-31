// Package mrqueue provides merge request queue storage.
// MRs are stored locally in .beads/mq/ and deleted after merge.
// This avoids sync overhead for transient MR state.
package mrqueue

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MR represents a merge request in the queue.
type MR struct {
	ID          string    `json:"id"`
	Branch      string    `json:"branch"`       // Source branch (e.g., "polecat/nux")
	Target      string    `json:"target"`       // Target branch (e.g., "main")
	SourceIssue string    `json:"source_issue"` // The work item being merged
	Worker      string    `json:"worker"`       // Who did the work
	Rig         string    `json:"rig"`          // Which rig
	Title       string    `json:"title"`        // MR title
	Priority    int       `json:"priority"`     // Priority (lower = higher priority)
	CreatedAt   time.Time `json:"created_at"`
	AgentBead   string    `json:"agent_bead,omitempty"` // Agent bead ID that created this MR (for traceability)
}

// Queue manages the MR storage.
type Queue struct {
	dir string // .beads/mq/ directory
}

// New creates a new MR queue for the given rig path.
func New(rigPath string) *Queue {
	return &Queue{
		dir: filepath.Join(rigPath, ".beads", "mq"),
	}
}

// NewFromWorkdir creates a queue by finding the rig root from a working directory.
func NewFromWorkdir(workdir string) (*Queue, error) {
	// Walk up to find .beads or rig root
	dir := workdir
	for {
		beadsDir := filepath.Join(dir, ".beads")
		if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
			return &Queue{dir: filepath.Join(beadsDir, "mq")}, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("could not find .beads directory from %s", workdir)
		}
		dir = parent
	}
}

// EnsureDir creates the MQ directory if it doesn't exist.
func (q *Queue) EnsureDir() error {
	return os.MkdirAll(q.dir, 0755)
}

// generateID creates a unique MR ID.
func generateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("mr-%d-%s", time.Now().Unix(), hex.EncodeToString(b))
}

// Submit adds a new MR to the queue.
func (q *Queue) Submit(mr *MR) error {
	if err := q.EnsureDir(); err != nil {
		return fmt.Errorf("creating mq directory: %w", err)
	}

	if mr.ID == "" {
		mr.ID = generateID()
	}
	if mr.CreatedAt.IsZero() {
		mr.CreatedAt = time.Now()
	}

	data, err := json.MarshalIndent(mr, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling MR: %w", err)
	}

	path := filepath.Join(q.dir, mr.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing MR file: %w", err)
	}

	return nil
}

// List returns all pending MRs, sorted by priority then creation time.
func (q *Queue) List() ([]*MR, error) {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Empty queue
		}
		return nil, fmt.Errorf("reading mq directory: %w", err)
	}

	var mrs []*MR
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		mr, err := q.load(filepath.Join(q.dir, entry.Name()))
		if err != nil {
			continue // Skip malformed files
		}
		mrs = append(mrs, mr)
	}

	// Sort by priority (lower first), then by creation time (older first)
	sort.Slice(mrs, func(i, j int) bool {
		if mrs[i].Priority != mrs[j].Priority {
			return mrs[i].Priority < mrs[j].Priority
		}
		return mrs[i].CreatedAt.Before(mrs[j].CreatedAt)
	})

	return mrs, nil
}

// Get retrieves a specific MR by ID.
func (q *Queue) Get(id string) (*MR, error) {
	path := filepath.Join(q.dir, id+".json")
	return q.load(path)
}

// load reads an MR from a file path.
func (q *Queue) load(path string) (*MR, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var mr MR
	if err := json.Unmarshal(data, &mr); err != nil {
		return nil, err
	}

	return &mr, nil
}

// Remove deletes an MR from the queue (after successful merge).
func (q *Queue) Remove(id string) error {
	path := filepath.Join(q.dir, id+".json")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // Already removed
	}
	return err
}

// Count returns the number of pending MRs.
func (q *Queue) Count() int {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return 0
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count
}

// Dir returns the queue directory path.
func (q *Queue) Dir() string {
	return q.dir
}
