package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNotFound is returned when a requested session is not found in the store.
	ErrNotFound = errors.New("session not found")
	// ErrExpired is returned when a requested session has expired.
	ErrExpired = errors.New("session expired")
)

// SessionEntry represents an active HTTP session with cookies, sesskey, and metadata.
type SessionEntry struct {
	Platform  string         `json:"platform"`
	Cookies   []*http.Cookie `json:"cookies"`
	SessKey   string         `json:"sesskey,omitempty"`
	UserID    string         `json:"user_id,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	ExpiresAt time.Time      `json:"expires_at"`
}

// IsExpired returns true if the session has an expiration timestamp and it is in the past.
func (e *SessionEntry) IsExpired() bool {
	if e == nil {
		return true
	}
	if e.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(e.ExpiresAt)
}

// IsValid returns true if the session is non-nil and has not expired.
func (e *SessionEntry) IsValid() bool {
	return e != nil && !e.IsExpired()
}

// GetCookie returns the first cookie matching the given name, or nil if not found.
func (e *SessionEntry) GetCookie(name string) *http.Cookie {
	if e == nil {
		return nil
	}
	for _, c := range e.Cookies {
		if c != nil && c.Name == name {
			return c
		}
	}
	return nil
}

// SessionStore defines the interface for managing cached sessions across platforms.
type SessionStore interface {
	Load(platform string) (*SessionEntry, error)
	Save(entry *SessionEntry) error
	Clear(platform string) error
	ClearAll() error
}

// FileStore implements SessionStore backed by a local JSON file with atomic writes.
type FileStore struct {
	mu       sync.RWMutex
	filePath string
}

var _ SessionStore = (*FileStore)(nil)

// DefaultPath returns the default session cache path: ~/.cache/atenea/sessions.json
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "atenea", "sessions.json"), nil
}

// New creates a FileStore at the default path (~/.cache/atenea/sessions.json).
func New() (*FileStore, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return NewFileStore(path)
}

// NewFileStore creates a FileStore at a specific path, ensuring directory permissions 0700.
func NewFileStore(filePath string) (*FileStore, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, errors.New("file path cannot be empty")
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create session cache directory: %w", err)
	}
	_ = os.Chmod(dir, 0700)

	return &FileStore{
		filePath: filePath,
	}, nil
}

// FilePath returns the storage file path used by this store.
func (s *FileStore) FilePath() string {
	return s.filePath
}

// readSessionsLocked reads and deserializes the session map from disk. Must be called with lock held.
func (s *FileStore) readSessionsLocked() (map[string]*SessionEntry, error) {
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return make(map[string]*SessionEntry), nil
	}

	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read session cache file: %w", err)
	}

	if len(raw) == 0 {
		return make(map[string]*SessionEntry), nil
	}

	var data map[string]*SessionEntry
	if err := json.Unmarshal(raw, &data); err != nil {
		// Fallback check for wrapped format {"sessions": {...}}
		var wrapped struct {
			Sessions map[string]*SessionEntry `json:"sessions"`
		}
		if err2 := json.Unmarshal(raw, &wrapped); err2 == nil && wrapped.Sessions != nil {
			data = wrapped.Sessions
		} else {
			return nil, fmt.Errorf("failed to parse session cache file: %w", err)
		}
	}

	if data == nil {
		data = make(map[string]*SessionEntry)
	}

	return data, nil
}

// writeSessionsLocked writes the sessions map to disk atomically using a temp file and rename.
func (s *FileStore) writeSessionsLocked(sessions map[string]*SessionEntry) error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to ensure cache directory: %w", err)
	}
	_ = os.Chmod(dir, 0700)

	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}

	// Create temp file in same directory so os.Rename is an atomic operation within the same filesystem
	tmpFile, err := os.CreateTemp(dir, ".sessions-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary session file: %w", err)
	}
	tmpName := tmpFile.Name()

	var renamed bool
	defer func() {
		if !renamed {
			_ = os.Remove(tmpName)
		}
	}()

	// Set permissions 0600 on temporary file
	if err := tmpFile.Chmod(0600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to set permissions on temp session file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp session file: %w", err)
	}

	// Flush to disk
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp session file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp session file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpName, s.filePath); err != nil {
		return fmt.Errorf("failed to rename temp session file: %w", err)
	}
	renamed = true

	// Ensure destination file has 0600 permissions
	_ = os.Chmod(s.filePath, 0600)

	return nil
}

// Load retrieves the cached session for the specified platform if present and not expired.
func (s *FileStore) Load(platform string) (*SessionEntry, error) {
	platformKey := strings.ToLower(strings.TrimSpace(platform))
	if platformKey == "" {
		return nil, errors.New("platform cannot be empty")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions, err := s.readSessionsLocked()
	if err != nil {
		return nil, err
	}

	entry, ok := sessions[platformKey]
	if !ok || entry == nil {
		return nil, ErrNotFound
	}

	if entry.IsExpired() {
		return nil, ErrExpired
	}

	// Return deep copy of entry to prevent callers from mutating internal store state
	entryCopy := *entry
	if entry.Cookies != nil {
		entryCopy.Cookies = make([]*http.Cookie, len(entry.Cookies))
		for i, c := range entry.Cookies {
			if c != nil {
				cCopy := *c
				entryCopy.Cookies[i] = &cCopy
			}
		}
	}

	return &entryCopy, nil
}

// Save stores a session entry using an atomic write with 0600 permissions.
func (s *FileStore) Save(entry *SessionEntry) error {
	if entry == nil {
		return errors.New("cannot save nil session entry")
	}

	platformKey := strings.ToLower(strings.TrimSpace(entry.Platform))
	if platformKey == "" {
		return errors.New("platform cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.readSessionsLocked()
	if err != nil {
		sessions = make(map[string]*SessionEntry)
	}

	now := time.Now()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now

	// Clone entry for storing
	entryCopy := *entry
	entryCopy.Platform = platformKey
	if entry.Cookies != nil {
		entryCopy.Cookies = make([]*http.Cookie, len(entry.Cookies))
		for i, c := range entry.Cookies {
			if c != nil {
				cCopy := *c
				entryCopy.Cookies[i] = &cCopy
			}
		}
	}

	sessions[platformKey] = &entryCopy

	return s.writeSessionsLocked(sessions)
}

// Clear removes the cached session for the specified platform.
func (s *FileStore) Clear(platform string) error {
	platformKey := strings.ToLower(strings.TrimSpace(platform))
	if platformKey == "" {
		return errors.New("platform cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.readSessionsLocked()
	if err != nil {
		return err
	}

	if _, exists := sessions[platformKey]; !exists {
		return nil
	}

	delete(sessions, platformKey)
	return s.writeSessionsLocked(sessions)
}

// ClearAll removes the cache file entirely.
func (s *FileStore) ClearAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return nil
	}

	return os.Remove(s.filePath)
}
