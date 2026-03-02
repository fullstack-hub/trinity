package session

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

// Message represents a single chat message in a session.
type Message struct {
	Role      string    `json:"role"`               // "user", "assistant", "system"
	Content   string    `json:"content"`
	Thinking  string    `json:"thinking,omitempty"`
	Model     string    `json:"model,omitempty"`
	Server    string    `json:"server,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Session represents a persistent conversation session.
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Workspace string    `json:"workspace"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Messages  []Message `json:"messages"`
}

// SessionInfo is a lightweight summary for listing.
type SessionInfo struct {
	ID           string
	Title        string
	Workspace    string
	MessageCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Store manages session persistence using JSON files.
type Store struct {
	dir string
}

// NewStore creates a session store at ~/.trinity/sessions/.
func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot find home directory: %w", err)
	}
	dir := filepath.Join(home, ".trinity", "sessions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create session directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

func generateID() string {
	b := make([]byte, 4) // 8 hex chars
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// Create creates a new session for the given workspace.
func (s *Store) Create(workspace string) *Session {
	now := time.Now()
	return &Session{
		ID:        generateID(),
		Workspace: workspace,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Save writes the session to disk atomically.
func (s *Store) Save(sess *Session) error {
	sess.UpdatedAt = time.Now()
	// Auto-generate title from first user message
	if sess.Title == "" {
		for _, m := range sess.Messages {
			if m.Role == "user" {
				title := m.Content
				runes := []rune(title)
				if len(runes) > 50 {
					title = string(runes[:50]) + "..."
				}
				sess.Title = title
				break
			}
		}
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(sess.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(sess.ID))
}

// Load reads a session from disk.
func (s *Store) Load(id string) (*Session, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, fmt.Errorf("session %s not found: %w", id, err)
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("corrupt session %s: %w", id, err)
	}
	return &sess, nil
}

// List returns session summaries, optionally filtered by workspace.
// Pass empty workspace to list all sessions.
func (s *Store) List(workspace string) ([]SessionInfo, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var infos []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var sess Session
		if json.Unmarshal(data, &sess) != nil {
			continue
		}
		if workspace != "" && sess.Workspace != workspace {
			continue
		}
		infos = append(infos, SessionInfo{
			ID:           sess.ID,
			Title:        sess.Title,
			Workspace:    sess.Workspace,
			MessageCount: len(sess.Messages),
			CreatedAt:    sess.CreatedAt,
			UpdatedAt:    sess.UpdatedAt,
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].UpdatedAt.After(infos[j].UpdatedAt)
	})
	return infos, nil
}

// Latest returns the most recently updated session for the given workspace.
func (s *Store) Latest(workspace string) (*Session, error) {
	infos, err := s.List(workspace)
	if err != nil {
		return nil, err
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("no sessions found")
	}
	return s.Load(infos[0].ID)
}

// Delete removes a session file.
func (s *Store) Delete(id string) error {
	return os.Remove(s.path(id))
}

// Rename changes a session's title.
func (s *Store) Rename(id, newTitle string) error {
	sess, err := s.Load(id)
	if err != nil {
		return err
	}
	sess.Title = newTitle
	return s.Save(sess)
}

// TimeAgo returns a human-readable relative time string.
func TimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
