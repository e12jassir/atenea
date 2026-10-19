package store

import "time"

// DeltaEvent types representing changes detected between snapshots.
const (
	DeltaTypeNewAssignment = "NEW_ASSIGNMENT"
	DeltaTypeGradeUpdated  = "GRADE_UPDATED"
	DeltaTypeStatusChanged = "STATUS_CHANGED"
)

// CourseRecord represents an academic course/subject from a supported LMS platform.
type CourseRecord struct {
	ID        string    `json:"id"`
	Platform  string    `json:"platform"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AssignmentRecord represents a homework, evidence, forum task, or exam associated with a course.
type AssignmentRecord struct {
	ID        string    `json:"id"`
	CourseID  string    `json:"course_id"`
	Code      string    `json:"code"`
	Title     string    `json:"title"`
	DueDate   string    `json:"due_date"`
	Status    string    `json:"status"`
	Grade     string    `json:"grade"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AnnouncementRecord represents an official board notice or instructor communication.
type AnnouncementRecord struct {
	ID        string    `json:"id"`
	CourseID  string    `json:"course_id"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	Date      string    `json:"date"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DeltaEvent represents a state transition or update discovered during sync diffing.
type DeltaEvent struct {
	Type        string    `json:"type"`
	EntityID    string    `json:"entity_id"`
	Platform    string    `json:"platform"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// SnapshotRecord represents historical synchronization metadata.
type SnapshotRecord struct {
	ID          int64     `json:"id"`
	Platform    string    `json:"platform"`
	EntityType  string    `json:"entity_type"`
	RecordCount int       `json:"record_count"`
	CreatedAt   time.Time `json:"created_at"`
}
