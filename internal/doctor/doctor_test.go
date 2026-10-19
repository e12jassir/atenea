package doctor

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/session"
	"github.com/e12jassir/atenea/internal/vault"
)

func newMockServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/zajuna", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Zajuna OK"))
	})
	mux.HandleFunc("/sima", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/sima/landing", http.StatusSeeOther)
	})
	mux.HandleFunc("/sima/landing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("SIMA Landing OK"))
	})
	mux.HandleFunc("/ingles", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ingles/landing", http.StatusSeeOther)
	})
	mux.HandleFunc("/ingles/landing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Ingles Landing OK"))
	})
	mux.HandleFunc("/ajax", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":"Invalid json in request","errorcode":"codingerror"}`))
	})

	return httptest.NewServer(mux)
}

func TestRunDiagnostics_FullSuccessWithMocks(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	// Temp base dir for vault and cache
	tempDir := t.TempDir()

	// Configure Vault with all 3 platforms
	v, err := vault.NewWithDir(tempDir)
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}

	if err := v.SaveCredentials(model.Credentials{
		Platform: model.PlatformSena,
		DocType:  "CC",
		User:     "1043829100",
		Password: "sena_secret_password",
	}); err != nil {
		t.Fatalf("failed to save sena credentials: %v", err)
	}

	if err := v.SaveCredentials(model.Credentials{
		Platform: model.PlatformSima,
		User:     "1043829100",
		Password: "sima_secret_password",
	}); err != nil {
		t.Fatalf("failed to save sima credentials: %v", err)
	}

	if err := v.SaveCredentials(model.Credentials{
		Platform: model.PlatformIngles,
		User:     "1043829100",
		Password: "ingles_secret_password",
	}); err != nil {
		t.Fatalf("failed to save ingles credentials: %v", err)
	}

	// Configure Session Cache with active sessions
	cachePath := filepath.Join(tempDir, "sessions.json")
	store, err := session.NewFileStore(cachePath)
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}

	futureExp := time.Now().Add(24 * time.Hour)
	for _, p := range []string{"sena", "sima", "ingles"} {
		entry := &session.SessionEntry{
			Platform:  p,
			UserID:    "user_123",
			CreatedAt: time.Now(),
			ExpiresAt: futureExp,
			Cookies: []*http.Cookie{
				{Name: "session_" + p, Value: "secret_cookie"},
			},
		}
		if err := store.Save(entry); err != nil {
			t.Fatalf("failed to save session for %s: %v", p, err)
		}
	}

	cfg := DiagnosticConfig{
		BaseDir:    tempDir,
		CachePath:  cachePath,
		HTTPClient: server.Client(),
		Institutions: []InstitutionTarget{
			{Name: "SENA Zajuna", URL: server.URL + "/zajuna"},
			{Name: "UDC SIMA", URL: server.URL + "/sima"},
			{Name: "UDC Inglés", URL: server.URL + "/ingles"},
		},
		AjaxEndpoint: server.URL + "/ajax",
	}

	report, err := RunDiagnosticsWithConfig(cfg)
	if err != nil {
		t.Fatalf("RunDiagnosticsWithConfig failed: %v", err)
	}

	if report.OverallStatus != StatusOK {
		t.Errorf("expected overall status OK, got %s", report.OverallStatus)
	}

	// Check Vault
	if report.Vault.Status != StatusOK {
		t.Errorf("expected vault status OK, got %s", report.Vault.Status)
	}
	if !report.Vault.Initialized {
		t.Error("expected vault to be initialized")
	}
	for _, p := range []model.Platform{model.PlatformSena, model.PlatformSima, model.PlatformIngles} {
		pStat, ok := report.Vault.Platforms[p]
		if !ok || !pStat.Configured {
			t.Errorf("expected platform %s to be configured", p)
		}
		if pStat.Username == "" || strings.Contains(pStat.Username, "sena_secret_password") {
			t.Errorf("expected masked username for %s, got %s", p, pStat.Username)
		}
	}

	// Check Session
	if report.Session.Status != StatusOK {
		t.Errorf("expected session status OK, got %s", report.Session.Status)
	}
	if !report.Session.Exists {
		t.Error("expected session cache file to exist")
	}
	for _, p := range []string{"sena", "sima", "ingles"} {
		sStat, ok := report.Session.Platforms[p]
		if !ok || !sStat.Cached || !sStat.Active || sStat.Expired {
			t.Errorf("expected platform %s session to be active, got %+v", p, sStat)
		}
	}

	// Check Institutions
	if len(report.Institutions) != 3 {
		t.Fatalf("expected 3 institutions probed, got %d", len(report.Institutions))
	}
	for _, inst := range report.Institutions {
		if inst.Status != StatusOK {
			t.Errorf("expected institution %s status OK, got %s (HTTP %d, err: %s)",
				inst.Name, inst.Status, inst.StatusCode, inst.Error)
		}
	}

	// Check AJAX
	if report.Ajax.Status != StatusOK || !report.Ajax.Available {
		t.Errorf("expected AJAX status OK and available, got %+v", report.Ajax)
	}

	// Check PrintSummary
	var buf bytes.Buffer
	report.PrintSummary(&buf)
	summaryOutput := buf.String()

	if !strings.Contains(summaryOutput, "[OK]") {
		t.Error("expected summary to contain [OK]")
	}
	if strings.Contains(summaryOutput, "[FAIL]") {
		t.Errorf("summary should not contain [FAIL]:\n%s", summaryOutput)
	}
	if !strings.Contains(summaryOutput, "ATENEA DOCTOR") {
		t.Error("summary should contain title ATENEA DOCTOR")
	}
}

func TestRunDiagnostics_VaultScenarios(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	t.Run("EmptyVault", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("HOME", tempDir)

		_, err := vault.NewWithDir(tempDir)
		if err != nil {
			t.Fatalf("failed to init vault: %v", err)
		}

		cfg := DiagnosticConfig{
			BaseDir:    tempDir,
			HTTPClient: server.Client(),
			Institutions: []InstitutionTarget{
				{Name: "SENA", URL: server.URL + "/zajuna"},
			},
			AjaxEndpoint: server.URL + "/ajax",
		}

		report, err := RunDiagnosticsWithConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if report.Vault.Status != StatusWarn {
			t.Errorf("expected vault status WARN for empty vault, got %s", report.Vault.Status)
		}
		if report.Vault.Platforms[model.PlatformSena].Configured {
			t.Error("expected sena to not be configured")
		}
	})

	t.Run("CorruptVaultFile", func(t *testing.T) {
		tempDir := t.TempDir()
		v, err := vault.NewWithDir(tempDir)
		if err != nil {
			t.Fatalf("failed to init vault: %v", err)
		}

		// Save a credential to generate key and file
		_ = v.SaveCredentials(model.Credentials{
			Platform: model.PlatformSena,
			User:     "test",
			Password: "pwd",
		})

		// Corrupt vault_go.enc
		encPath := filepath.Join(tempDir, "vault_go.enc")
		_ = os.WriteFile(encPath, []byte("totally corrupt garbage data"), 0600)

		cfg := DiagnosticConfig{
			BaseDir:    tempDir,
			HTTPClient: server.Client(),
			Institutions: []InstitutionTarget{
				{Name: "SENA", URL: server.URL + "/zajuna"},
			},
			AjaxEndpoint: server.URL + "/ajax",
		}

		report, err := RunDiagnosticsWithConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if report.Vault.Status != StatusFail {
			t.Errorf("expected vault status FAIL on corrupt vault file, got %s", report.Vault.Status)
		}
	})
}

func TestRunDiagnostics_SessionScenarios(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	t.Run("NonExistentCacheFile", func(t *testing.T) {
		tempDir := t.TempDir()
		cachePath := filepath.Join(tempDir, "non_existent", "sessions.json")

		cfg := DiagnosticConfig{
			BaseDir:    tempDir,
			CachePath:  cachePath,
			HTTPClient: server.Client(),
			Institutions: []InstitutionTarget{
				{Name: "SENA", URL: server.URL + "/zajuna"},
			},
			AjaxEndpoint: server.URL + "/ajax",
		}

		report, err := RunDiagnosticsWithConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if report.Session.Status != StatusWarn {
			t.Errorf("expected session status WARN when file does not exist, got %s", report.Session.Status)
		}
		if report.Session.Exists {
			t.Error("expected Session.Exists to be false")
		}
	})

	t.Run("ExpiredSession", func(t *testing.T) {
		tempDir := t.TempDir()
		cachePath := filepath.Join(tempDir, "sessions.json")
		store, err := session.NewFileStore(cachePath)
		if err != nil {
			t.Fatalf("failed to create session store: %v", err)
		}

		// Save expired session
		pastTime := time.Now().Add(-2 * time.Hour)
		entry := &session.SessionEntry{
			Platform:  "sena",
			UserID:    "user_expired",
			CreatedAt: pastTime.Add(-1 * time.Hour),
			ExpiresAt: pastTime,
		}
		if err := store.Save(entry); err != nil {
			t.Fatalf("failed to save session: %v", err)
		}

		cfg := DiagnosticConfig{
			BaseDir:    tempDir,
			CachePath:  cachePath,
			HTTPClient: server.Client(),
			Institutions: []InstitutionTarget{
				{Name: "SENA", URL: server.URL + "/zajuna"},
			},
			AjaxEndpoint: server.URL + "/ajax",
		}

		report, err := RunDiagnosticsWithConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		senaStat := report.Session.Platforms["sena"]
		if !senaStat.Cached {
			t.Error("expected sena session to be cached")
		}
		if senaStat.Active {
			t.Error("expected sena session to NOT be active")
		}
		if !senaStat.Expired {
			t.Error("expected sena session to be expired")
		}
	})

	t.Run("CorruptCacheFile", func(t *testing.T) {
		tempDir := t.TempDir()
		cachePath := filepath.Join(tempDir, "sessions.json")
		if err := os.WriteFile(cachePath, []byte("{invalid-json-data"), 0600); err != nil {
			t.Fatalf("failed to write corrupt file: %v", err)
		}

		cfg := DiagnosticConfig{
			BaseDir:    tempDir,
			CachePath:  cachePath,
			HTTPClient: server.Client(),
			Institutions: []InstitutionTarget{
				{Name: "SENA", URL: server.URL + "/zajuna"},
			},
			AjaxEndpoint: server.URL + "/ajax",
		}

		report, err := RunDiagnosticsWithConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if report.Session.Status != StatusFail {
			t.Errorf("expected session status FAIL on corrupt JSON, got %s", report.Session.Status)
		}
	})
}

func TestRunDiagnostics_ConnectivityAndAjaxFailures(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fail500", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("500 Server Error"))
	})
	mux.HandleFunc("/warn404", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 Not Found"))
	})
	mux.HandleFunc("/ajax503", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("503 Service Unavailable"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	tempDir := t.TempDir()

	cfg := DiagnosticConfig{
		BaseDir:    tempDir,
		HTTPClient: server.Client(),
		Institutions: []InstitutionTarget{
			{Name: "Server 500", URL: server.URL + "/fail500"},
			{Name: "Server 404", URL: server.URL + "/warn404"},
			{Name: "Unreachable", URL: "http://127.0.0.1:1/does_not_exist"},
		},
		AjaxEndpoint: server.URL + "/ajax503",
	}

	report, err := RunDiagnosticsWithConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.OverallStatus != StatusFail {
		t.Errorf("expected overall status FAIL, got %s", report.OverallStatus)
	}

	// 500 check
	if report.Institutions[0].Status != StatusFail {
		t.Errorf("expected 500 server to have status FAIL, got %s", report.Institutions[0].Status)
	}

	// 404 check
	if report.Institutions[1].Status != StatusWarn {
		t.Errorf("expected 404 server to have status WARN, got %s", report.Institutions[1].Status)
	}

	// Unreachable check
	if report.Institutions[2].Status != StatusFail {
		t.Errorf("expected unreachable server to have status FAIL, got %s", report.Institutions[2].Status)
	}

	// AJAX 503 check
	if report.Ajax.Status != StatusFail || report.Ajax.Available {
		t.Errorf("expected AJAX 503 to be FAIL and unavailable, got %+v", report.Ajax)
	}

	// Verify recommendations include errors
	if len(report.Recommendations) == 0 {
		t.Error("expected recommendations to be populated on failure")
	}

	var buf bytes.Buffer
	report.PrintSummary(&buf)
	out := buf.String()
	if !strings.Contains(out, "[FAIL]") {
		t.Error("expected summary output to contain [FAIL]")
	}
	if !strings.Contains(out, "RECOMENDACIONES") {
		t.Error("expected summary output to contain RECOMENDACIONES")
	}
}

func TestMaskUsername(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"ab", "**"},
		{"abcd", "****"},
		{"12345", "1234*"},
		{"1043829100", "1043******"},
	}

	for _, tc := range tests {
		got := maskUsername(tc.input)
		if got != tc.expected {
			t.Errorf("maskUsername(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}
