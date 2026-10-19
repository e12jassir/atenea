package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store defines the interface for persisting and querying cached academic state.
type Store interface {
	Close() error
	SaveCourses(courses []CourseRecord) error
	GetCourses(platform string) ([]CourseRecord, error)
	SaveAssignments(assignments []AssignmentRecord) error
	GetAssignments(platform string) ([]AssignmentRecord, error)
	GetPendingAssignments(platform string) ([]AssignmentRecord, error)
	SaveAnnouncements(announcements []AnnouncementRecord) error
	GetAnnouncements(platform string) ([]AnnouncementRecord, error)
	ComputeDiff(platform string, newAssignments []AssignmentRecord) ([]DeltaEvent, error)
	SaveEvents(events []DeltaEvent) error
	GetEvents(platform string) ([]DeltaEvent, error)
}

// SQLiteStore is the concrete SQLite implementation of the Store interface.
type SQLiteStore struct {
	mu     sync.RWMutex
	db     *sql.DB
	dbPath string
}

var _ Store = (*SQLiteStore)(nil)

// DefaultDBPath returns the standard state database location: ~/.local/share/atenea/state.db
func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "atenea", "state.db"), nil
}

// New creates and initializes a SQLiteStore using the default database path.
func New() (*SQLiteStore, error) {
	path, err := DefaultDBPath()
	if err != nil {
		return nil, err
	}
	return NewWithDBPath(path)
}

// NewWithDBPath creates and initializes a SQLiteStore at the specified path.
// It automatically ensures the parent directory exists with mode 0700.
func NewWithDBPath(dbPath string) (*SQLiteStore, error) {
	cleanPath := strings.TrimSpace(dbPath)
	if cleanPath == "" {
		return nil, errors.New("database path cannot be empty")
	}

	if cleanPath != ":memory:" && !strings.HasPrefix(cleanPath, "file::memory:") {
		dir := filepath.Dir(cleanPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
		_ = os.Chmod(dir, 0700)
	}

	db, err := sql.Open("sqlite", cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database at %s: %w", cleanPath, err)
	}

	// SQLite embedded databases should serialize connections in Go to prevent busy lockouts.
	db.SetMaxOpenConns(1)

	store := &SQLiteStore{
		db:     db,
		dbPath: cleanPath,
	}

	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if cleanPath != ":memory:" && !strings.HasPrefix(cleanPath, "file::memory:") {
		_ = os.Chmod(cleanPath, 0600)
	}

	return store, nil
}

// DBPath returns the database file path used by this store.
func (s *SQLiteStore) DBPath() string {
	return s.dbPath
}

// init configures required PRAGMAs and runs migration logic to create initial tables.
func (s *SQLiteStore) init() error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA busy_timeout = 5000;",
	}

	for _, p := range pragmas {
		if _, err := s.db.Exec(p); err != nil {
			return fmt.Errorf("failed to execute pragma %q: %w", p, err)
		}
	}

	migrations := `
	CREATE TABLE IF NOT EXISTS courses (
		id TEXT PRIMARY KEY,
		platform TEXT NOT NULL,
		code TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_courses_platform ON courses(platform);

	CREATE TABLE IF NOT EXISTS assignments (
		id TEXT PRIMARY KEY,
		course_id TEXT NOT NULL,
		platform TEXT NOT NULL DEFAULT '',
		code TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL,
		due_date TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT '',
		grade TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_assignments_course_id ON assignments(course_id);
	CREATE INDEX IF NOT EXISTS idx_assignments_platform ON assignments(platform);
	CREATE INDEX IF NOT EXISTS idx_assignments_status ON assignments(status);

	CREATE TABLE IF NOT EXISTS announcements (
		id TEXT PRIMARY KEY,
		course_id TEXT NOT NULL,
		platform TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL,
		author TEXT NOT NULL DEFAULT '',
		date TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_announcements_course_id ON announcements(course_id);
	CREATE INDEX IF NOT EXISTS idx_announcements_platform ON announcements(platform);

	CREATE TABLE IF NOT EXISTS snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		record_count INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_snapshots_platform_entity ON snapshots(platform, entity_type);

	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		platform TEXT NOT NULL,
		description TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_events_platform ON events(platform);
	CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);
	`

	if _, err := s.db.Exec(migrations); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// Close closes the underlying SQLite database connection.
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		return err
	}
	return nil
}

// SaveCourses upserts a slice of CourseRecords in a single transaction.
func (s *SQLiteStore) SaveCourses(courses []CourseRecord) error {
	if len(courses) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO courses (id, platform, code, name, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			platform = excluded.platform,
			code = excluded.code,
			name = excluded.name,
			updated_at = excluded.updated_at;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare courses statement: %w", err)
	}
	defer stmt.Close()

	platformCounts := make(map[string]int)

	for _, c := range courses {
		now := c.UpdatedAt
		if now.IsZero() {
			now = time.Now()
		}
		if _, err := stmt.Exec(c.ID, c.Platform, c.Code, c.Name, now); err != nil {
			return fmt.Errorf("failed to upsert course %s: %w", c.ID, err)
		}
		platformCounts[c.Platform]++

		// Backfill platform into existing assignments and announcements referencing this course
		if c.Platform != "" {
			_, _ = tx.Exec(`UPDATE assignments SET platform = ? WHERE course_id = ? AND (platform = '' OR platform IS NULL)`, c.Platform, c.ID)
			_, _ = tx.Exec(`UPDATE announcements SET platform = ? WHERE course_id = ? AND (platform = '' OR platform IS NULL)`, c.Platform, c.ID)
		}
	}

	now := time.Now()
	for p, count := range platformCounts {
		_, _ = tx.Exec(`INSERT INTO snapshots (platform, entity_type, record_count, created_at) VALUES (?, 'courses', ?, ?)`, p, count, now)
	}

	return tx.Commit()
}

// GetCourses returns courses filtered by platform. If platform is empty, returns all courses.
func (s *SQLiteStore) GetCourses(platform string) ([]CourseRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, platform, code, name, updated_at
		FROM courses
		WHERE (? = '' OR platform = ?)
		ORDER BY name ASC
	`
	rows, err := s.db.Query(query, platform, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to query courses: %w", err)
	}
	defer rows.Close()

	result := make([]CourseRecord, 0)
	for rows.Next() {
		var c CourseRecord
		var updatedAt time.Time
		if err := rows.Scan(&c.ID, &c.Platform, &c.Code, &c.Name, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan course: %w", err)
		}
		c.UpdatedAt = updatedAt
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return result, nil
}

// SaveAssignments upserts a slice of AssignmentRecords in a single transaction.
func (s *SQLiteStore) SaveAssignments(assignments []AssignmentRecord) error {
	if len(assignments) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO assignments (id, course_id, platform, code, title, due_date, status, grade, updated_at)
		VALUES (
			?,
			?,
			COALESCE(
				NULLIF((SELECT platform FROM courses WHERE id = ?), ''),
				CASE
					WHEN LOWER(?) LIKE '%sena%' THEN 'sena'
					WHEN LOWER(?) LIKE '%sima%' THEN 'sima'
					WHEN LOWER(?) LIKE '%ingles%' THEN 'ingles'
					ELSE ''
				END
			),
			?,
			?,
			?,
			?,
			?,
			?
		)
		ON CONFLICT(id) DO UPDATE SET
			course_id = excluded.course_id,
			platform = CASE
				WHEN excluded.platform != '' THEN excluded.platform
				WHEN assignments.platform != '' THEN assignments.platform
				ELSE COALESCE((SELECT platform FROM courses WHERE id = excluded.course_id), '')
			END,
			code = excluded.code,
			title = excluded.title,
			due_date = excluded.due_date,
			status = excluded.status,
			grade = excluded.grade,
			updated_at = excluded.updated_at;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare assignments statement: %w", err)
	}
	defer stmt.Close()

	for _, a := range assignments {
		now := a.UpdatedAt
		if now.IsZero() {
			now = time.Now()
		}
		if _, err := stmt.Exec(a.ID, a.CourseID, a.CourseID, a.CourseID, a.CourseID, a.CourseID, a.Code, a.Title, a.DueDate, a.Status, a.Grade, now); err != nil {
			return fmt.Errorf("failed to upsert assignment %s: %w", a.ID, err)
		}
	}

	// Record snapshot entry for assignments
	_, _ = tx.Exec(`
		INSERT INTO snapshots (platform, entity_type, record_count, created_at)
		SELECT COALESCE(NULLIF(platform, ''), 'all'), 'assignments', COUNT(*), ?
		FROM assignments
		GROUP BY platform
	`, time.Now())

	return tx.Commit()
}

// GetAssignments returns assignments filtered by platform. If platform is empty, returns all assignments.
func (s *SQLiteStore) GetAssignments(platform string) ([]AssignmentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT a.id, a.course_id, a.code, a.title, a.due_date, a.status, a.grade, a.updated_at
		FROM assignments a
		LEFT JOIN courses c ON a.course_id = c.id
		WHERE (? = '' OR a.platform = ? OR c.platform = ?)
		ORDER BY a.due_date ASC, a.title ASC
	`
	rows, err := s.db.Query(query, platform, platform, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to query assignments: %w", err)
	}
	defer rows.Close()

	result := make([]AssignmentRecord, 0)
	for rows.Next() {
		var a AssignmentRecord
		var updatedAt time.Time
		if err := rows.Scan(&a.ID, &a.CourseID, &a.Code, &a.Title, &a.DueDate, &a.Status, &a.Grade, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan assignment: %w", err)
		}
		a.UpdatedAt = updatedAt
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return result, nil
}

// GetPendingAssignments returns assignments that require learner action (pending or deficient grades).
func (s *SQLiteStore) GetPendingAssignments(platform string) ([]AssignmentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT a.id, a.course_id, a.code, a.title, a.due_date, a.status, a.grade, a.updated_at
		FROM assignments a
		LEFT JOIN courses c ON a.course_id = c.id
		WHERE (? = '' OR a.platform = ? OR c.platform = ?)
		  AND (
			LOWER(a.status) LIKE '%pendiente%'
			OR LOWER(a.status) LIKE '%deficiente%'
			OR LOWER(a.status) LIKE '%pending%'
			OR a.status = ''
			OR (
				LOWER(a.status) NOT LIKE '%aprobado%'
				AND LOWER(a.status) NOT LIKE '%entregado%'
				AND LOWER(a.status) NOT LIKE '%enviado%'
				AND LOWER(a.status) NOT LIKE '%realizado%'
				AND LOWER(a.status) NOT LIKE '%approved%'
				AND LOWER(a.status) NOT LIKE '%submitted%'
			)
		  )
		ORDER BY a.due_date ASC, a.title ASC
	`
	rows, err := s.db.Query(query, platform, platform, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending assignments: %w", err)
	}
	defer rows.Close()

	result := make([]AssignmentRecord, 0)
	for rows.Next() {
		var a AssignmentRecord
		var updatedAt time.Time
		if err := rows.Scan(&a.ID, &a.CourseID, &a.Code, &a.Title, &a.DueDate, &a.Status, &a.Grade, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan pending assignment: %w", err)
		}
		a.UpdatedAt = updatedAt
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return result, nil
}

// SaveAnnouncements upserts a slice of AnnouncementRecords in a single transaction.
func (s *SQLiteStore) SaveAnnouncements(announcements []AnnouncementRecord) error {
	if len(announcements) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO announcements (id, course_id, platform, title, author, date, content, updated_at)
		VALUES (
			?,
			?,
			COALESCE(
				NULLIF((SELECT platform FROM courses WHERE id = ?), ''),
				CASE
					WHEN LOWER(?) LIKE '%sena%' THEN 'sena'
					WHEN LOWER(?) LIKE '%sima%' THEN 'sima'
					WHEN LOWER(?) LIKE '%ingles%' THEN 'ingles'
					ELSE ''
				END
			),
			?,
			?,
			?,
			?,
			?
		)
		ON CONFLICT(id) DO UPDATE SET
			course_id = excluded.course_id,
			platform = CASE
				WHEN excluded.platform != '' THEN excluded.platform
				WHEN announcements.platform != '' THEN announcements.platform
				ELSE COALESCE((SELECT platform FROM courses WHERE id = excluded.course_id), '')
			END,
			title = excluded.title,
			author = excluded.author,
			date = excluded.date,
			content = excluded.content,
			updated_at = excluded.updated_at;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare announcements statement: %w", err)
	}
	defer stmt.Close()

	for _, a := range announcements {
		now := a.UpdatedAt
		if now.IsZero() {
			now = time.Now()
		}
		if _, err := stmt.Exec(a.ID, a.CourseID, a.CourseID, a.CourseID, a.CourseID, a.CourseID, a.Title, a.Author, a.Date, a.Content, now); err != nil {
			return fmt.Errorf("failed to upsert announcement %s: %w", a.ID, err)
		}
	}

	// Record snapshot metadata
	_, _ = tx.Exec(`
		INSERT INTO snapshots (platform, entity_type, record_count, created_at)
		SELECT COALESCE(NULLIF(platform, ''), 'all'), 'announcements', COUNT(*), ?
		FROM announcements
		GROUP BY platform
	`, time.Now())

	return tx.Commit()
}

// GetAnnouncements returns announcements filtered by platform. If platform is empty, returns all announcements.
func (s *SQLiteStore) GetAnnouncements(platform string) ([]AnnouncementRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT a.id, a.course_id, a.title, a.author, a.date, a.content, a.updated_at
		FROM announcements a
		LEFT JOIN courses c ON a.course_id = c.id
		WHERE (? = '' OR a.platform = ? OR c.platform = ?)
		ORDER BY a.updated_at DESC, a.date DESC
	`
	rows, err := s.db.Query(query, platform, platform, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to query announcements: %w", err)
	}
	defer rows.Close()

	result := make([]AnnouncementRecord, 0)
	for rows.Next() {
		var a AnnouncementRecord
		var updatedAt time.Time
		if err := rows.Scan(&a.ID, &a.CourseID, &a.Title, &a.Author, &a.Date, &a.Content, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan announcement: %w", err)
		}
		a.UpdatedAt = updatedAt
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return result, nil
}

// ComputeDiff compares incoming assignments against the current snapshot and returns DeltaEvents.
// It detects new assignments, grade updates/publications, and status changes.
// Any detected delta events are automatically recorded in the events table.
func (s *SQLiteStore) ComputeDiff(platform string, newAssignments []AssignmentRecord) ([]DeltaEvent, error) {
	existing, err := s.GetAssignments(platform)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing assignments for diff: %w", err)
	}

	oldMap := make(map[string]AssignmentRecord, len(existing))
	for _, a := range existing {
		oldMap[a.ID] = a
	}

	var events []DeltaEvent
	now := time.Now()

	for _, newA := range newAssignments {
		oldA, exists := oldMap[newA.ID]
		if !exists {
			desc := fmt.Sprintf("New assignment detected: %s", newA.Title)
			if newA.Code != "" {
				desc = fmt.Sprintf("New assignment detected: %s (%s)", newA.Title, newA.Code)
			}
			events = append(events, DeltaEvent{
				Type:        DeltaTypeNewAssignment,
				EntityID:    newA.ID,
				Platform:    platform,
				Description: desc,
				CreatedAt:   now,
			})
			continue
		}

		// Status change detected
		if newA.Status != "" && newA.Status != oldA.Status {
			desc := fmt.Sprintf("Status changed for %s: %s -> %s", newA.Title, oldA.Status, newA.Status)
			if oldA.Status == "" {
				desc = fmt.Sprintf("Status set for %s: %s", newA.Title, newA.Status)
			}
			events = append(events, DeltaEvent{
				Type:        DeltaTypeStatusChanged,
				EntityID:    newA.ID,
				Platform:    platform,
				Description: desc,
				CreatedAt:   now,
			})
		}

		// Grade change / new grade detected
		if newA.Grade != "" && newA.Grade != oldA.Grade {
			desc := fmt.Sprintf("Grade published for %s: %s", newA.Title, newA.Grade)
			if oldA.Grade != "" {
				desc = fmt.Sprintf("Grade updated for %s: %s (previously: %s)", newA.Title, newA.Grade, oldA.Grade)
			}
			events = append(events, DeltaEvent{
				Type:        DeltaTypeGradeUpdated,
				EntityID:    newA.ID,
				Platform:    platform,
				Description: desc,
				CreatedAt:   now,
			})
		}
	}

	if len(events) > 0 {
		if err := s.SaveEvents(events); err != nil {
			return nil, fmt.Errorf("failed to persist delta events: %w", err)
		}
	}

	return events, nil
}

// SaveEvents persists a batch of DeltaEvents into the events table.
func (s *SQLiteStore) SaveEvents(events []DeltaEvent) error {
	if len(events) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin events transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO events (type, entity_id, platform, description, created_at)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare events statement: %w", err)
	}
	defer stmt.Close()

	for _, e := range events {
		now := e.CreatedAt
		if now.IsZero() {
			now = time.Now()
		}
		if _, err := stmt.Exec(e.Type, e.EntityID, e.Platform, e.Description, now); err != nil {
			return fmt.Errorf("failed to insert event: %w", err)
		}
	}

	return tx.Commit()
}

// GetEvents retrieves delta events filtered by platform. If platform is empty, returns all events.
func (s *SQLiteStore) GetEvents(platform string) ([]DeltaEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT type, entity_id, platform, description, created_at
		FROM events
		WHERE (? = '' OR platform = ?)
		ORDER BY created_at DESC, id DESC
	`
	rows, err := s.db.Query(query, platform, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	result := make([]DeltaEvent, 0)
	for rows.Next() {
		var e DeltaEvent
		var createdAt time.Time
		if err := rows.Scan(&e.Type, &e.EntityID, &e.Platform, &e.Description, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		e.CreatedAt = createdAt
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return result, nil
}
