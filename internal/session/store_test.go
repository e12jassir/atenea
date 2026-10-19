package session

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "sessions.json")

	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("unexpected error creating store: %v", err)
	}

	expiresAt := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	entry := &SessionEntry{
		Platform: "sima",
		Cookies: []*http.Cookie{
			{
				Name:     "MoodleSession",
				Value:    "session_secret_12345",
				Path:     "/",
				Domain:   "sima.unicartagena.edu.co",
				Expires:  expiresAt,
				Secure:   true,
				HttpOnly: true,
			},
			{
				Name:  "MOODLEID1_",
				Value: "test_user_val",
				Path:  "/",
			},
		},
		SessKey:   "sesskey_abc987",
		UserID:    "user_456",
		ExpiresAt: expiresAt,
	}

	if err := store.Save(entry); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Verify file permissions (0600)
	fi, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("expected session file to exist: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %#o", fi.Mode().Perm())
	}

	// Verify directory permissions (0700)
	dirInfo, err := os.Stat(tempDir)
	if err != nil {
		t.Fatalf("expected dir to exist: %v", err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Errorf("expected dir mode 0700, got %#o", dirInfo.Mode().Perm())
	}

	// Load session (case-insensitive test)
	loaded, err := store.Load("SIMA")
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if loaded.Platform != "sima" {
		t.Errorf("expected platform 'sima', got '%s'", loaded.Platform)
	}
	if loaded.SessKey != "sesskey_abc987" {
		t.Errorf("expected sesskey 'sesskey_abc987', got '%s'", loaded.SessKey)
	}
	if loaded.UserID != "user_456" {
		t.Errorf("expected user_id 'user_456', got '%s'", loaded.UserID)
	}
	if len(loaded.Cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(loaded.Cookies))
	}
	if loaded.Cookies[0].Name != "MoodleSession" || loaded.Cookies[0].Value != "session_secret_12345" {
		t.Errorf("unexpected cookie 0: %+v", loaded.Cookies[0])
	}
	if loaded.CreatedAt.IsZero() {
		t.Errorf("expected CreatedAt to be populated")
	}
	if loaded.UpdatedAt.IsZero() {
		t.Errorf("expected UpdatedAt to be populated")
	}
	if !loaded.IsValid() {
		t.Errorf("expected session to be valid")
	}
	if loaded.IsExpired() {
		t.Errorf("expected session not to be expired")
	}

	// Test GetCookie helper
	cookie := loaded.GetCookie("MoodleSession")
	if cookie == nil || cookie.Value != "session_secret_12345" {
		t.Errorf("expected MoodleSession cookie to be found, got %+v", cookie)
	}
	missingCookie := loaded.GetCookie("NonExistent")
	if missingCookie != nil {
		t.Errorf("expected nil for non-existent cookie, got %+v", missingCookie)
	}
}

func TestExpiration(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "sessions.json")
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1. Expired session
	expiredEntry := &SessionEntry{
		Platform:  "sena",
		SessKey:   "expired_key",
		ExpiresAt: time.Now().Add(-2 * time.Hour),
		Cookies: []*http.Cookie{
			{Name: "ZajunaSession", Value: "expired"},
		},
	}

	if !expiredEntry.IsExpired() {
		t.Errorf("expected entry to be expired")
	}
	if expiredEntry.IsValid() {
		t.Errorf("expected entry to not be valid")
	}

	if err := store.Save(expiredEntry); err != nil {
		t.Fatalf("failed to save expired entry: %v", err)
	}

	loaded, err := store.Load("sena")
	if !errors.Is(err, ErrExpired) {
		t.Errorf("expected ErrExpired, got err=%v, loaded=%+v", err, loaded)
	}

	// 2. Non-expired session
	validEntry := &SessionEntry{
		Platform:  "sena",
		SessKey:   "fresh_key",
		ExpiresAt: time.Now().Add(4 * time.Hour),
		Cookies: []*http.Cookie{
			{Name: "ZajunaSession", Value: "fresh"},
		},
	}

	if validEntry.IsExpired() {
		t.Errorf("expected fresh entry to not be expired")
	}
	if !validEntry.IsValid() {
		t.Errorf("expected fresh entry to be valid")
	}

	if err := store.Save(validEntry); err != nil {
		t.Fatalf("failed to update entry: %v", err)
	}

	loadedValid, err := store.Load("sena")
	if err != nil {
		t.Fatalf("unexpected error loading fresh session: %v", err)
	}
	if loadedValid.SessKey != "fresh_key" {
		t.Errorf("expected sesskey 'fresh_key', got '%s'", loadedValid.SessKey)
	}

	// 3. Zero expiration time (infinite / persistent session)
	zeroExpEntry := &SessionEntry{
		Platform: "persistent",
		SessKey:  "infinite_key",
	}
	if zeroExpEntry.IsExpired() {
		t.Errorf("expected zero expiration entry to not be expired")
	}
	if !zeroExpEntry.IsValid() {
		t.Errorf("expected zero expiration entry to be valid")
	}
}

func TestClear(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "sessions.json")
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Save multiple platforms
	platforms := []string{"sena", "sima", "ingles"}
	for _, p := range platforms {
		err := store.Save(&SessionEntry{
			Platform:  p,
			SessKey:   "key_" + p,
			ExpiresAt: time.Now().Add(1 * time.Hour),
		})
		if err != nil {
			t.Fatalf("failed to save %s: %v", p, err)
		}
	}

	// Verify all exist
	for _, p := range platforms {
		if _, err := store.Load(p); err != nil {
			t.Fatalf("expected %s to be present: %v", p, err)
		}
	}

	// Clear "sena"
	if err := store.Clear("sena"); err != nil {
		t.Fatalf("failed to clear sena: %v", err)
	}

	// "sena" should now be ErrNotFound
	_, err = store.Load("sena")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for cleared platform, got %v", err)
	}

	// "sima" and "ingles" should still be intact
	if _, err := store.Load("sima"); err != nil {
		t.Errorf("expected sima to remain present: %v", err)
	}
	if _, err := store.Load("ingles"); err != nil {
		t.Errorf("expected ingles to remain present: %v", err)
	}

	// Clearing non-existent platform is a no-op and returns nil
	if err := store.Clear("unknown_platform"); err != nil {
		t.Errorf("expected nil when clearing unknown platform, got %v", err)
	}

	// ClearAll removes the file
	if err := store.ClearAll(); err != nil {
		t.Fatalf("failed ClearAll: %v", err)
	}

	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Errorf("expected session file to be removed after ClearAll")
	}
}

func TestAtomicFileCreation(t *testing.T) {
	tempDir := t.TempDir()
	// Deep nested directory to test recursive directory creation with mode 0700
	nestedDir := filepath.Join(tempDir, "deep", "nested", "cache")
	storePath := filepath.Join(nestedDir, "sessions.json")

	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store with nested dir: %v", err)
	}

	entry := &SessionEntry{
		Platform:  "sena",
		SessKey:   "atomic_sesskey",
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}

	if err := store.Save(entry); err != nil {
		t.Fatalf("failed to save entry: %v", err)
	}

	// Check directory permissions
	dirFi, err := os.Stat(nestedDir)
	if err != nil {
		t.Fatalf("expected nested dir to exist: %v", err)
	}
	if dirFi.Mode().Perm() != 0700 {
		t.Errorf("expected nested dir mode 0700, got %#o", dirFi.Mode().Perm())
	}

	// Ensure no temporary files (.sessions-*.tmp) remain
	files, err := os.ReadDir(nestedDir)
	if err != nil {
		t.Fatalf("failed to read nested dir: %v", err)
	}

	for _, f := range files {
		if filepath.Ext(f.Name()) == ".tmp" {
			t.Errorf("found dangling temp file: %s", f.Name())
		}
	}

	// Verify file mode
	fileFi, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("failed to stat session file: %v", err)
	}
	if fileFi.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %#o", fileFi.Mode().Perm())
	}
}

func TestConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "sessions.json")
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const goroutines = 20
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Writer goroutines
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			platform := "platform_a"
			if id%2 == 0 {
				platform = "platform_b"
			}
			for j := 0; j < iterations; j++ {
				_ = store.Save(&SessionEntry{
					Platform:  platform,
					SessKey:   "key",
					ExpiresAt: time.Now().Add(time.Hour),
				})
			}
		}(i)
	}

	// Reader goroutines
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			platform := "platform_a"
			if id%2 == 0 {
				platform = "platform_b"
			}
			for j := 0; j < iterations; j++ {
				_, _ = store.Load(platform)
			}
		}(i)
	}

	// Clearer goroutines
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			platform := "platform_c"
			for j := 0; j < iterations; j++ {
				_ = store.Clear(platform)
			}
		}(i)
	}

	wg.Wait()
}

func TestNilAndEmptyInputs(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "sessions.json")
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Save nil
	if err := store.Save(nil); err == nil {
		t.Errorf("expected error saving nil entry")
	}

	// Save empty platform
	if err := store.Save(&SessionEntry{Platform: ""}); err == nil {
		t.Errorf("expected error saving empty platform")
	}

	// Load empty platform
	if _, err := store.Load(""); err == nil {
		t.Errorf("expected error loading empty platform")
	}

	// Clear empty platform
	if err := store.Clear(""); err == nil {
		t.Errorf("expected error clearing empty platform")
	}

	// Load non-existent
	if _, err := store.Load("non_existent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for non_existent, got %v", err)
	}

	// Nil receiver helper methods
	var nilEntry *SessionEntry
	if !nilEntry.IsExpired() {
		t.Errorf("expected nil entry to be expired")
	}
	if nilEntry.IsValid() {
		t.Errorf("expected nil entry to not be valid")
	}
	if nilEntry.GetCookie("test") != nil {
		t.Errorf("expected nil cookie for nil entry")
	}

	// Empty path for NewFileStore
	if _, err := NewFileStore(""); err == nil {
		t.Errorf("expected error for empty file path in NewFileStore")
	}
}
