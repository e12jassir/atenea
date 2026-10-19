package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewWithDBPath_TableCreationAndPragmas(t *testing.T) {
	tempDir := t.TempDir()
	dbDir := filepath.Join(tempDir, "nested", "atenea")
	dbPath := filepath.Join(dbDir, "state.db")

	store, err := NewWithDBPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store with path %s: %v", dbPath, err)
	}
	defer store.Close()

	// Verify directory exists
	info, err := os.Stat(dbDir)
	if err != nil {
		t.Fatalf("database directory was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", dbDir)
	}

	// Verify PRAGMA foreign_keys is ON
	var foreignKeys int
	if err := store.db.QueryRow("PRAGMA foreign_keys;").Scan(&foreignKeys); err != nil {
		t.Fatalf("failed to query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("expected PRAGMA foreign_keys = 1, got %d", foreignKeys)
	}

	// Verify PRAGMA journal_mode is WAL (or memory/delete depending on OS filesystem support)
	var journalMode string
	if err := store.db.QueryRow("PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		t.Fatalf("failed to query journal_mode pragma: %v", err)
	}
	if journalMode != "wal" && journalMode != "memory" {
		t.Errorf("expected journal_mode wal, got %s", journalMode)
	}

	// Verify all required tables exist
	requiredTables := []string{"courses", "assignments", "announcements", "snapshots", "events"}
	for _, tableName := range requiredTables {
		var count int
		err := store.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?;", tableName).Scan(&count)
		if err != nil {
			t.Fatalf("failed to check table %s: %v", tableName, err)
		}
		if count != 1 {
			t.Errorf("expected table %q to exist, but count = %d", tableName, count)
		}
	}
}

func TestCoursesCRUD(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "courses_test.db")
	store, err := NewWithDBPath(dbPath)
	if err != nil {
		t.Fatalf("NewWithDBPath failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)

	courses := []CourseRecord{
		{ID: "course-sena-1", Platform: "sena", Code: "ADSO-3545922", Name: "Analisis y Desarrollo", UpdatedAt: now},
		{ID: "course-sima-1", Platform: "sima", Code: "UDC-62368", Name: "Ingenieria de Software", UpdatedAt: now},
	}

	if err := store.SaveCourses(courses); err != nil {
		t.Fatalf("SaveCourses failed: %v", err)
	}

	// Filter by SENA
	senaCourses, err := store.GetCourses("sena")
	if err != nil {
		t.Fatalf("GetCourses(sena) failed: %v", err)
	}
	if len(senaCourses) != 1 || senaCourses[0].ID != "course-sena-1" {
		t.Errorf("expected 1 sena course, got %+v", senaCourses)
	}

	// Filter by SIMA
	simaCourses, err := store.GetCourses("sima")
	if err != nil {
		t.Fatalf("GetCourses(sima) failed: %v", err)
	}
	if len(simaCourses) != 1 || simaCourses[0].ID != "course-sima-1" {
		t.Errorf("expected 1 sima course, got %+v", simaCourses)
	}

	// Query all
	allCourses, err := store.GetCourses("")
	if err != nil {
		t.Fatalf("GetCourses('') failed: %v", err)
	}
	if len(allCourses) != 2 {
		t.Errorf("expected 2 courses, got %d", len(allCourses))
	}

	// Test Upsert
	updatedCourse := []CourseRecord{
		{ID: "course-sena-1", Platform: "sena", Code: "ADSO-3545922-MOD", Name: "Analisis y Desarrollo de Software V2", UpdatedAt: now.Add(time.Hour)},
	}
	if err := store.SaveCourses(updatedCourse); err != nil {
		t.Fatalf("SaveCourses upsert failed: %v", err)
	}

	senaRefetched, err := store.GetCourses("sena")
	if err != nil {
		t.Fatalf("GetCourses(sena) failed: %v", err)
	}
	if len(senaRefetched) != 1 {
		t.Fatalf("expected 1 course after upsert, got %d", len(senaRefetched))
	}
	if senaRefetched[0].Code != "ADSO-3545922-MOD" || senaRefetched[0].Name != "Analisis y Desarrollo de Software V2" {
		t.Errorf("course was not properly updated on conflict: %+v", senaRefetched[0])
	}
}

func TestAssignmentsCRUD(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "assignments_test.db")
	store, err := NewWithDBPath(dbPath)
	if err != nil {
		t.Fatalf("NewWithDBPath failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)

	// Setup courses
	courses := []CourseRecord{
		{ID: "sena-c1", Platform: "sena", Code: "ADSO", Name: "ADSO Ficha", UpdatedAt: now},
		{ID: "sima-c1", Platform: "sima", Code: "SW", Name: "Ing Software", UpdatedAt: now},
	}
	if err := store.SaveCourses(courses); err != nil {
		t.Fatalf("SaveCourses failed: %v", err)
	}

	assignments := []AssignmentRecord{
		{ID: "ev-1", CourseID: "sena-c1", Code: "GA1-EV01", Title: "Mapa Conceptual", DueDate: "2026-10-25", Status: "Pendiente", Grade: "", UpdatedAt: now},
		{ID: "ev-2", CourseID: "sena-c1", Code: "GA1-EV02", Title: "Taller Arquitectura", DueDate: "2026-10-30", Status: "Aprobado (A)", Grade: "A", UpdatedAt: now},
		{ID: "ev-3", CourseID: "sima-c1", Code: "PARCIAL1", Title: "Parcial Unidad 1", DueDate: "2026-11-05", Status: "YA ENVIADO", Grade: "4.5", UpdatedAt: now},
	}

	if err := store.SaveAssignments(assignments); err != nil {
		t.Fatalf("SaveAssignments failed: %v", err)
	}

	// Query SENA
	senaAssigns, err := store.GetAssignments("sena")
	if err != nil {
		t.Fatalf("GetAssignments(sena) failed: %v", err)
	}
	if len(senaAssigns) != 2 {
		t.Fatalf("expected 2 sena assignments, got %d", len(senaAssigns))
	}
	if senaAssigns[0].ID != "ev-1" || senaAssigns[1].ID != "ev-2" {
		t.Errorf("unexpected sena assignments order or content: %+v", senaAssigns)
	}

	// Query SIMA
	simaAssigns, err := store.GetAssignments("sima")
	if err != nil {
		t.Fatalf("GetAssignments(sima) failed: %v", err)
	}
	if len(simaAssigns) != 1 || simaAssigns[0].ID != "ev-3" {
		t.Errorf("expected 1 sima assignment, got %+v", simaAssigns)
	}

	// Query all
	allAssigns, err := store.GetAssignments("")
	if err != nil {
		t.Fatalf("GetAssignments('') failed: %v", err)
	}
	if len(allAssigns) != 3 {
		t.Errorf("expected 3 total assignments, got %d", len(allAssigns))
	}

	// Upsert test: update ev-1 status and grade
	update := []AssignmentRecord{
		{ID: "ev-1", CourseID: "sena-c1", Code: "GA1-EV01", Title: "Mapa Conceptual", DueDate: "2026-10-25", Status: "Aprobado (A)", Grade: "A", UpdatedAt: now.Add(time.Hour)},
	}
	if err := store.SaveAssignments(update); err != nil {
		t.Fatalf("SaveAssignments upsert failed: %v", err)
	}

	refetched, err := store.GetAssignments("sena")
	if err != nil {
		t.Fatalf("GetAssignments(sena) failed: %v", err)
	}
	var foundEv1 *AssignmentRecord
	for _, a := range refetched {
		if a.ID == "ev-1" {
			foundEv1 = &a
			break
		}
	}
	if foundEv1 == nil || foundEv1.Status != "Aprobado (A)" || foundEv1.Grade != "A" {
		t.Errorf("assignment was not properly updated on conflict: %+v", foundEv1)
	}
}

func TestPendingAssignments(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pending_test.db")
	store, err := NewWithDBPath(dbPath)
	if err != nil {
		t.Fatalf("NewWithDBPath failed: %v", err)
	}
	defer store.Close()

	now := time.Now()

	courses := []CourseRecord{
		{ID: "c-sena", Platform: "sena", Code: "ADSO", Name: "ADSO", UpdatedAt: now},
	}
	if err := store.SaveCourses(courses); err != nil {
		t.Fatalf("SaveCourses failed: %v", err)
	}

	assignments := []AssignmentRecord{
		{ID: "p-1", CourseID: "c-sena", Title: "Tarea Pendiente", Status: "Pendiente", UpdatedAt: now},
		{ID: "p-2", CourseID: "c-sena", Title: "Tarea Deficiente", Status: "Deficiente (D)", Grade: "D", UpdatedAt: now},
		{ID: "p-3", CourseID: "c-sena", Title: "Tarea Sin Estado", Status: "", UpdatedAt: now},
		{ID: "p-4", CourseID: "c-sena", Title: "Tarea Pending", Status: "PENDING_SUBMISSION", UpdatedAt: now},
		{ID: "done-1", CourseID: "c-sena", Title: "Tarea Aprobada", Status: "Aprobado (A)", Grade: "A", UpdatedAt: now},
		{ID: "done-2", CourseID: "c-sena", Title: "Tarea Entregada", Status: "Entregado", UpdatedAt: now},
		{ID: "done-3", CourseID: "c-sena", Title: "Tarea Realizada", Status: "YA REALIZADO", UpdatedAt: now},
		{ID: "done-4", CourseID: "c-sena", Title: "Tarea Enviada", Status: "YA ENVIADO", UpdatedAt: now},
	}

	if err := store.SaveAssignments(assignments); err != nil {
		t.Fatalf("SaveAssignments failed: %v", err)
	}

	pending, err := store.GetPendingAssignments("sena")
	if err != nil {
		t.Fatalf("GetPendingAssignments failed: %v", err)
	}

	if len(pending) != 4 {
		t.Fatalf("expected exactly 4 pending assignments, got %d: %+v", len(pending), pending)
	}

	expectedPendingIDs := map[string]bool{
		"p-1": true,
		"p-2": true,
		"p-3": true,
		"p-4": true,
	}

	for _, a := range pending {
		if !expectedPendingIDs[a.ID] {
			t.Errorf("unexpected assignment in pending list: ID=%s Status=%s", a.ID, a.Status)
		}
	}
}

func TestAnnouncementsCRUD(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "announcements_test.db")
	store, err := NewWithDBPath(dbPath)
	if err != nil {
		t.Fatalf("NewWithDBPath failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)

	courses := []CourseRecord{
		{ID: "c-sena", Platform: "sena", Code: "ADSO", Name: "ADSO Ficha", UpdatedAt: now},
		{ID: "c-sima", Platform: "sima", Code: "UDC", Name: "UDC Software", UpdatedAt: now},
	}
	if err := store.SaveCourses(courses); err != nil {
		t.Fatalf("SaveCourses failed: %v", err)
	}

	announcements := []AnnouncementRecord{
		{ID: "ann-1", CourseID: "c-sena", Title: "Sesion sincronica manana", Author: "Haminton Paez", Date: "2026-10-18", Content: "Favor conectarse a las 7pm", UpdatedAt: now},
		{ID: "ann-2", CourseID: "c-sima", Title: "Cambio de aula parcial", Author: "Profesor UDC", Date: "2026-10-17", Content: "Aula 302", UpdatedAt: now},
	}

	if err := store.SaveAnnouncements(announcements); err != nil {
		t.Fatalf("SaveAnnouncements failed: %v", err)
	}

	// Filter SENA
	senaAnn, err := store.GetAnnouncements("sena")
	if err != nil {
		t.Fatalf("GetAnnouncements(sena) failed: %v", err)
	}
	if len(senaAnn) != 1 || senaAnn[0].ID != "ann-1" {
		t.Errorf("expected 1 sena announcement, got %+v", senaAnn)
	}

	// Filter SIMA
	simaAnn, err := store.GetAnnouncements("sima")
	if err != nil {
		t.Fatalf("GetAnnouncements(sima) failed: %v", err)
	}
	if len(simaAnn) != 1 || simaAnn[0].ID != "ann-2" {
		t.Errorf("expected 1 sima announcement, got %+v", simaAnn)
	}

	// Query All
	allAnn, err := store.GetAnnouncements("")
	if err != nil {
		t.Fatalf("GetAnnouncements('') failed: %v", err)
	}
	if len(allAnn) != 2 {
		t.Errorf("expected 2 announcements, got %d", len(allAnn))
	}

	// Upsert test
	updatedAnn := []AnnouncementRecord{
		{ID: "ann-1", CourseID: "c-sena", Title: "Sesion sincronica REPROGRAMADA", Author: "Haminton Paez", Date: "2026-10-19", Content: "Nueva fecha 8pm", UpdatedAt: now.Add(time.Hour)},
	}
	if err := store.SaveAnnouncements(updatedAnn); err != nil {
		t.Fatalf("SaveAnnouncements upsert failed: %v", err)
	}

	refetched, err := store.GetAnnouncements("sena")
	if err != nil {
		t.Fatalf("GetAnnouncements(sena) failed: %v", err)
	}
	if len(refetched) != 1 || refetched[0].Title != "Sesion sincronica REPROGRAMADA" {
		t.Errorf("announcement was not updated properly on conflict: %+v", refetched)
	}
}

func TestComputeDiff(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "diff_test.db")
	store, err := NewWithDBPath(dbPath)
	if err != nil {
		t.Fatalf("NewWithDBPath failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)

	courses := []CourseRecord{
		{ID: "c-sena", Platform: "sena", Code: "ADSO", Name: "ADSO", UpdatedAt: now},
	}
	if err := store.SaveCourses(courses); err != nil {
		t.Fatalf("SaveCourses failed: %v", err)
	}

	initialAssignments := []AssignmentRecord{
		{ID: "ev-01", CourseID: "c-sena", Code: "GA1-EV01", Title: "Diagrama de Clases", Status: "Pendiente", Grade: "", UpdatedAt: now},
		{ID: "ev-02", CourseID: "c-sena", Code: "GA1-EV02", Title: "Taller Arquitectura", Status: "Pendiente", Grade: "", UpdatedAt: now},
	}

	// Scenario 1: Initial diff against empty database detects all as NEW_ASSIGNMENT
	initialDiff, err := store.ComputeDiff("sena", initialAssignments)
	if err != nil {
		t.Fatalf("ComputeDiff failed: %v", err)
	}
	if len(initialDiff) != 2 {
		t.Fatalf("expected 2 new assignments in initial diff, got %d", len(initialDiff))
	}
	for _, e := range initialDiff {
		if e.Type != DeltaTypeNewAssignment {
			t.Errorf("expected type %s, got %s", DeltaTypeNewAssignment, e.Type)
		}
	}

	// Persist initial state
	if err := store.SaveAssignments(initialAssignments); err != nil {
		t.Fatalf("SaveAssignments failed: %v", err)
	}

	// Scenario 2: No changes between store and scan
	noDiff, err := store.ComputeDiff("sena", initialAssignments)
	if err != nil {
		t.Fatalf("ComputeDiff failed: %v", err)
	}
	if len(noDiff) != 0 {
		t.Errorf("expected 0 diff events for identical state, got %d", len(noDiff))
	}

	// Scenario 3: Mixed changes
	// - ev-01 gets a grade ("A") and status change ("Aprobado (A)")
	// - ev-02 gets a status change ("Entregado")
	// - ev-03 is a brand new assignment
	scannedBatch := []AssignmentRecord{
		{ID: "ev-01", CourseID: "c-sena", Code: "GA1-EV01", Title: "Diagrama de Clases", Status: "Aprobado (A)", Grade: "A", UpdatedAt: now.Add(time.Minute)},
		{ID: "ev-02", CourseID: "c-sena", Code: "GA1-EV02", Title: "Taller Arquitectura", Status: "Entregado", Grade: "", UpdatedAt: now.Add(time.Minute)},
		{ID: "ev-03", CourseID: "c-sena", Code: "GA1-EV03", Title: "Informe Hexagonal", Status: "Pendiente", Grade: "", UpdatedAt: now.Add(time.Minute)},
	}

	events, err := store.ComputeDiff("sena", scannedBatch)
	if err != nil {
		t.Fatalf("ComputeDiff failed: %v", err)
	}

	// Expected events:
	// 1. ev-01: StatusChanged
	// 2. ev-01: GradeUpdated
	// 3. ev-02: StatusChanged
	// 4. ev-03: NewAssignment
	if len(events) != 4 {
		t.Fatalf("expected 4 delta events, got %d: %+v", len(events), events)
	}

	typeCounts := make(map[string]int)
	for _, e := range events {
		typeCounts[e.Type]++
		if e.Platform != "sena" {
			t.Errorf("expected platform 'sena', got '%s'", e.Platform)
		}
	}

	if typeCounts[DeltaTypeNewAssignment] != 1 {
		t.Errorf("expected 1 NewAssignment event, got %d", typeCounts[DeltaTypeNewAssignment])
	}
	if typeCounts[DeltaTypeStatusChanged] != 2 {
		t.Errorf("expected 2 StatusChanged events, got %d", typeCounts[DeltaTypeStatusChanged])
	}
	if typeCounts[DeltaTypeGradeUpdated] != 1 {
		t.Errorf("expected 1 GradeUpdated event, got %d", typeCounts[DeltaTypeGradeUpdated])
	}

	// Verify events are persisted in the database events table
	persistedEvents, err := store.GetEvents("sena")
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}
	// Initial diff saved 2, this diff saved 4 -> total 6
	if len(persistedEvents) != 6 {
		t.Errorf("expected 6 total persisted events, got %d", len(persistedEvents))
	}

	// Query events for a different platform should return 0
	simaEvents, err := store.GetEvents("sima")
	if err != nil {
		t.Fatalf("GetEvents(sima) failed: %v", err)
	}
	if len(simaEvents) != 0 {
		t.Errorf("expected 0 sima events, got %d", len(simaEvents))
	}
}

func TestPersistenceAcrossRestarts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "restart_test.db")

	store1, err := NewWithDBPath(dbPath)
	if err != nil {
		t.Fatalf("NewWithDBPath failed: %v", err)
	}

	course := CourseRecord{ID: "c-persist", Platform: "sena", Code: "P1", Name: "Persistence 101", UpdatedAt: time.Now()}
	if err := store1.SaveCourses([]CourseRecord{course}); err != nil {
		t.Fatalf("SaveCourses failed: %v", err)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reopen store from disk
	store2, err := NewWithDBPath(dbPath)
	if err != nil {
		t.Fatalf("reopening store failed: %v", err)
	}
	defer store2.Close()

	courses, err := store2.GetCourses("sena")
	if err != nil {
		t.Fatalf("GetCourses on reopened store failed: %v", err)
	}
	if len(courses) != 1 || courses[0].ID != "c-persist" {
		t.Errorf("expected persisted course to survive reopen, got %+v", courses)
	}
}

func TestDefaultDBPath(t *testing.T) {
	path, err := DefaultDBPath()
	if err != nil {
		t.Fatalf("DefaultDBPath failed: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join(".local", "share", "atenea", "state.db")) {
		t.Errorf("unexpected default DB path: %s", path)
	}
}

func TestEmptyInputs(t *testing.T) {
	store, err := NewWithDBPath(filepath.Join(t.TempDir(), "empty_test.db"))
	if err != nil {
		t.Fatalf("NewWithDBPath failed: %v", err)
	}
	defer store.Close()

	if err := store.SaveCourses(nil); err != nil {
		t.Errorf("SaveCourses(nil) should not error: %v", err)
	}
	if err := store.SaveAssignments(nil); err != nil {
		t.Errorf("SaveAssignments(nil) should not error: %v", err)
	}
	if err := store.SaveAnnouncements(nil); err != nil {
		t.Errorf("SaveAnnouncements(nil) should not error: %v", err)
	}
	if err := store.SaveEvents(nil); err != nil {
		t.Errorf("SaveEvents(nil) should not error: %v", err)
	}
}

func TestCloseDoubleCall(t *testing.T) {
	store, err := NewWithDBPath(filepath.Join(t.TempDir(), "close_test.db"))
	if err != nil {
		t.Fatalf("NewWithDBPath failed: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Errorf("first Close() failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("second Close() should be safe no-op: %v", err)
	}
}

func TestInvalidPath(t *testing.T) {
	_, err := NewWithDBPath("")
	if err == nil {
		t.Error("expected error with empty path, got nil")
	}
}

var _ = sql.ErrNoRows
