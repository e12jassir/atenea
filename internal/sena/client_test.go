package sena

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/session"
	"github.com/e12jassir/atenea/internal/vault"
)

func TestSenaLogin(t *testing.T) {
	v, err := vault.New()
	if err != nil {
		t.Skip("Vault no disponible:", err)
	}
	creds, err := v.GetCredentials(model.PlatformSena)
	if err != nil {
		t.Skip("Credenciales SENA no configuradas:", err)
	}

	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}

	err = client.Login(creds.DocType, creds.User, creds.Password)
	if err != nil {
		uRoot, _ := url.Parse("https://zajuna.sena.edu.co")
		uZajuna, _ := url.Parse("https://zajuna.sena.edu.co/zajuna/")
		fmt.Printf("Cookies en root (%s): %v\n", uRoot, client.jar.Cookies(uRoot))
		fmt.Printf("Cookies en /zajuna/ (%s): %v\n", uZajuna, client.jar.Cookies(uZajuna))
		t.Fatalf("Login falló: %v", err)
	}
	t.Log("Login exitoso!")
}

func TestExtractEvidenceCode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Evidencia: Informe de requerimientos GA1-220501092-AA1-EV01", "GA1-220501092-AA1-EV01"},
		{"GA2-220501093-AA2-EV02 Diagrama de clases", "GA2-220501093-AA2-EV02"},
		{"ga3-220501094-ev03 Taller practico", "GA3-220501094-EV03"},
		{"Evidencia: Foro social", ""},
		{"Informe de gestion general", ""},
	}

	for _, tc := range tests {
		got := extractEvidenceCode(tc.input)
		if got != tc.expected {
			t.Errorf("extractEvidenceCode(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestMatchEvidenceLinkNoGenericMatch(t *testing.T) {
	links := []linkInfo{
		{id: "1001", name: "Evidencia: Informe general de bienvenida"},
		{id: "1002", name: "Evidencia: Taller de arquitectura GA1-220501092-AA1-EV02"},
		{id: "1003", name: "Evidencia: Documento de especificación de requerimientos"},
	}

	// 1. Un título con código debe coincidir exactamente con el link que tiene el mismo código, NO con links[0]
	matched := matchEvidenceLink("Evidencia: Taller de arquitectura GA1-220501092-AA1-EV02", links)
	if matched != "1002" {
		t.Errorf("expected link 1002, got %q", matched)
	}

	// 2. Palabras genéricas como "Evidencia: Informe" NO deben emparejarse con links[0]
	matchedGeneric := matchEvidenceLink("Evidencia: Informe", links)
	if matchedGeneric != "" {
		t.Errorf("expected no match for generic words, got %q", matchedGeneric)
	}

	// 3. Título sin código con coincidencia normalizada estricta
	matchedNormalized := matchEvidenceLink("Evidencia: Documento de especificación de requerimientos", links)
	if matchedNormalized != "1003" {
		t.Errorf("expected link 1003 for normalized match, got %q", matchedNormalized)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGradeReportClassification(t *testing.T) {
	mockHTML := `
	<table>
		<tr>
			<td>Evidencia: GA1-220501092-AA1-EV01 Especificación de Requisitos</td>
			<td>A</td>
			<td>Excelente trabajo, cumple criterios.</td>
		</tr>
		<tr>
			<td>Evidencia: GA1-220501092-AA1-EV02 Modelo Entidad Relación</td>
			<td>D</td>
			<td>Faltan cardinalidades y claves foráneas. Reentregar.</td>
		</tr>
		<tr>
			<td>Evidencia: GA1-220501092-AA1-EV03 Taller de Normalización</td>
			<td>-</td>
			<td>&nbsp;</td>
		</tr>
	</table>`

	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}

	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(mockHTML)),
			Header:     make(http.Header),
		}, nil
	})

	rep, err := client.GetGradeReport()
	if err != nil {
		t.Fatalf("GetGradeReport failed: %v", err)
	}

	// Verificar separación A vs D
	if len(rep.Approved) != 1 {
		t.Fatalf("expected 1 approved evidence, got %d", len(rep.Approved))
	}
	if rep.Approved[0].Grade != "A" || rep.Approved[0].Status != "Aprobado (A)" {
		t.Errorf("unexpected approved evidence: %+v", rep.Approved[0])
	}

	if len(rep.Deficient) != 1 {
		t.Fatalf("expected 1 deficient evidence, got %d", len(rep.Deficient))
	}
	if rep.Deficient[0].Grade != "D" || rep.Deficient[0].Status != "Deficiente (D)" {
		t.Errorf("unexpected deficient evidence: %+v", rep.Deficient[0])
	}

	if len(rep.Pending) != 1 {
		t.Fatalf("expected 1 pending evidence, got %d", len(rep.Pending))
	}

	// GetApprovedGrades debe retornar SOLO notas A
	approved, err := client.GetApprovedGrades()
	if err != nil {
		t.Fatalf("GetApprovedGrades failed: %v", err)
	}
	if len(approved) != 1 || approved[0].Grade != "A" {
		t.Errorf("GetApprovedGrades should only contain grade A, got %+v", approved)
	}

	// GetDeficientGrades debe retornar la nota D como alerta prioritaria
	deficient, err := client.GetDeficientGrades()
	if err != nil {
		t.Fatalf("GetDeficientGrades failed: %v", err)
	}
	if len(deficient) != 1 || deficient[0].Grade != "D" {
		t.Errorf("GetDeficientGrades should contain grade D, got %+v", deficient)
	}

	// GetPendingEvidences NO debe ocultar las notas D: debe incluirlas como pendientes de acción
	pending, err := client.GetPendingEvidences()
	if err != nil {
		t.Fatalf("GetPendingEvidences failed: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("GetPendingEvidences should contain deficient + pending items, got %d", len(pending))
	}
	if pending[0].Grade != "D" {
		t.Errorf("first pending evidence should be the Deficient one (high priority), got %q", pending[0].Grade)
	}
}

func TestHttpStatusChecking(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}

	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body:       io.NopCloser(bytes.NewBufferString("Forbidden")),
			Header:     make(http.Header),
		}, nil
	})

	_, err = client.GetGradeReport()
	if err == nil {
		t.Fatal("expected error on HTTP 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected error to mention HTTP status 403, got %v", err)
	}

	_, err = client.GetAnnouncements()
	if err == nil {
		t.Fatal("expected error on HTTP 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected error to mention HTTP status 403, got %v", err)
	}

	_, err = client.GetAnnouncementDetail("123")
	if err == nil {
		t.Fatal("expected error on HTTP 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected error to mention HTTP status 403, got %v", err)
	}

	_, err = client.AuditPendingEvidences()
	if err == nil {
		t.Fatal("expected error on HTTP 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected error to mention HTTP status 403, got %v", err)
	}
}

func TestRestoreSession_Success(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := session.NewFileStore(filepath.Join(tmpDir, "sessions.json"))
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}

	validCookies := []*http.Cookie{
		{Name: "MoodleSession", Value: "valid-moodle-session-token"},
		{Name: "PHPSESSID", Value: "valid-php-session-token"},
	}

	entry := &session.SessionEntry{
		Platform:  "sena",
		Cookies:   validCookies,
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}
	if err := store.Save(entry); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	restored, err := client.RestoreSession(store)
	if err != nil {
		t.Fatalf("RestoreSession returned unexpected error: %v", err)
	}
	if !restored {
		t.Fatal("expected RestoreSession to return true, got false")
	}

	if !client.hasMoodleCookie() {
		t.Fatal("expected client.hasMoodleCookie() to be true after session restore")
	}

	uRoot, _ := url.Parse("https://zajuna.sena.edu.co")
	uZajuna, _ := url.Parse("https://zajuna.sena.edu.co/zajuna/")

	rootCookies := client.jar.Cookies(uRoot)
	if len(rootCookies) < 2 {
		t.Errorf("expected at least 2 cookies in root, got %d", len(rootCookies))
	}

	zajunaCookies := client.jar.Cookies(uZajuna)
	if len(zajunaCookies) < 2 {
		t.Errorf("expected at least 2 cookies in /zajuna/, got %d", len(zajunaCookies))
	}
}

func TestRestoreSession_Expired(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := session.NewFileStore(filepath.Join(tmpDir, "sessions.json"))
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}

	entry := &session.SessionEntry{
		Platform: "sena",
		Cookies: []*http.Cookie{
			{Name: "MoodleSession", Value: "expired-token"},
		},
		ExpiresAt: time.Now().Add(-10 * time.Minute),
	}
	if err := store.Save(entry); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	restored, err := client.RestoreSession(store)
	if err != nil {
		t.Fatalf("RestoreSession returned unexpected error: %v", err)
	}
	if restored {
		t.Fatal("expected RestoreSession to return false for expired session, got true")
	}
	if client.hasMoodleCookie() {
		t.Fatal("expected client.hasMoodleCookie() to be false")
	}
}

func TestRestoreSession_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := session.NewFileStore(filepath.Join(tmpDir, "sessions.json"))
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	restored, err := client.RestoreSession(store)
	if err != nil {
		t.Fatalf("RestoreSession returned unexpected error: %v", err)
	}
	if restored {
		t.Fatal("expected RestoreSession to return false when no session found, got true")
	}
}

func TestRestoreSession_NilStore(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	restored, err := client.RestoreSession(nil)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if restored {
		t.Fatal("expected false for nil store")
	}
}

func TestRestoreSession_EmptyCookies(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := session.NewFileStore(filepath.Join(tmpDir, "sessions.json"))
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}

	entry := &session.SessionEntry{
		Platform:  "sena",
		Cookies:   []*http.Cookie{},
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}
	if err := store.Save(entry); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	restored, err := client.RestoreSession(store)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if restored {
		t.Fatal("expected false for empty cookies")
	}
}

func TestEnsureAuthenticated_RestoredValidSession(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := session.NewFileStore(filepath.Join(tmpDir, "sessions.json"))
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}

	entry := &session.SessionEntry{
		Platform: "sena",
		Cookies: []*http.Cookie{
			{Name: "MoodleSession", Value: "existing-valid-moodle-session"},
		},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := store.Save(entry); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	validationCalled := false
	loginCalled := false

	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/zajuna/my/") {
			validationCalled = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("Dashboard OK")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		if strings.Contains(req.URL.Path, "lg.php") || strings.Contains(req.URL.Path, "login/index.php") {
			loginCalled = true
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("OK")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	if err := client.EnsureAuthenticated(store, "CC", "123456", "secret"); err != nil {
		t.Fatalf("EnsureAuthenticated failed: %v", err)
	}

	if !validationCalled {
		t.Fatal("expected session validation check to be called")
	}
	if loginCalled {
		t.Fatal("Login should not have been called when session was successfully restored and validated")
	}
}

func TestEnsureAuthenticated_RestoredSession_RedirectsToLogin_ReLogins(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := session.NewFileStore(filepath.Join(tmpDir, "sessions.json"))
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}

	entry := &session.SessionEntry{
		Platform: "sena",
		Cookies: []*http.Cookie{
			{Name: "MoodleSession", Value: "stale-moodle-session"},
		},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := store.Save(entry); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	loginDanceCompleted := false

	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// 1. Session check to /zajuna/my/ redirects to login
		if strings.Contains(req.URL.Path, "/zajuna/my/") {
			hdr := make(http.Header)
			hdr.Set("Location", "https://zajuna.sena.edu.co/zajuna/login/index.php")
			return &http.Response{
				StatusCode: http.StatusFound,
				Body:       io.NopCloser(bytes.NewBufferString("Redirecting to login")),
				Header:     hdr,
				Request:    req,
			}, nil
		}

		// 2. Login step 1: GET BaseURL
		if req.Method == "GET" && (req.URL.Path == "" || req.URL.Path == "/") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("Welcome")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}

		// 3. Login step 2: POST lg.php
		if req.Method == "POST" && strings.Contains(req.URL.Path, "lg.php") {
			body := `<form id="loginForm" action="/zajuna/login/index.php"><input name="sesskey" value="xyz" /></form>`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}

		// 4. Login step 3: POST /zajuna/login/index.php
		if req.Method == "POST" && strings.Contains(req.URL.Path, "login/index.php") {
			loginDanceCompleted = true
			hdr := make(http.Header)
			hdr.Add("Set-Cookie", "MoodleSession=brand-new-session-key; Path=/zajuna/")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("Logged In")),
				Header:     hdr,
				Request:    req,
			}, nil
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	if err := client.EnsureAuthenticated(store, "CC", "123456", "secret"); err != nil {
		t.Fatalf("EnsureAuthenticated failed: %v", err)
	}

	if !loginDanceCompleted {
		t.Fatal("expected login dance to be completed when session was invalid")
	}

	// Verify store was updated with new session
	updated, err := store.Load("sena")
	if err != nil {
		t.Fatalf("failed to load updated session: %v", err)
	}
	cookie := updated.GetCookie("MoodleSession")
	if cookie == nil || cookie.Value != "brand-new-session-key" {
		t.Fatalf("expected updated MoodleSession cookie 'brand-new-session-key', got %+v", cookie)
	}
}

func TestEnsureAuthenticated_NoCachedSession(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := session.NewFileStore(filepath.Join(tmpDir, "sessions.json"))
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && (req.URL.Path == "" || req.URL.Path == "/") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("Welcome")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		if req.Method == "POST" && strings.Contains(req.URL.Path, "lg.php") {
			body := `<form id="loginForm" action="/zajuna/login/index.php"><input name="sesskey" value="xyz" /></form>`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		if req.Method == "POST" && strings.Contains(req.URL.Path, "login/index.php") {
			hdr := make(http.Header)
			hdr.Add("Set-Cookie", "MoodleSession=initial-login-cookie; Path=/zajuna/")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("Logged In")),
				Header:     hdr,
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	if err := client.EnsureAuthenticated(store, "CC", "123456", "secret"); err != nil {
		t.Fatalf("EnsureAuthenticated failed: %v", err)
	}

	saved, err := store.Load("sena")
	if err != nil {
		t.Fatalf("failed to load saved session: %v", err)
	}
	if saved.GetCookie("MoodleSession") == nil || saved.GetCookie("MoodleSession").Value != "initial-login-cookie" {
		t.Fatalf("saved session missing expected MoodleSession cookie: %+v", saved)
	}
}

func TestValidateSession(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}

	// 1. Success case: returns 200 OK
	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("Dashboard")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	if !client.ValidateSession() {
		t.Error("expected ValidateSession to return true for 200 OK")
	}

	// 2. Redirect to login: returns 302 with Location pointing to login
	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hdr := make(http.Header)
		hdr.Set("Location", "https://zajuna.sena.edu.co/zajuna/login/index.php")
		return &http.Response{
			StatusCode: http.StatusFound,
			Body:       io.NopCloser(bytes.NewBufferString("Redirect")),
			Header:     hdr,
			Request:    req,
		}, nil
	})
	if client.ValidateSession() {
		t.Error("expected ValidateSession to return false when redirected to login")
	}

	// 3. Error case: returns 401
	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(bytes.NewBufferString("Unauthorized")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	if client.ValidateSession() {
		t.Error("expected ValidateSession to return false on HTTP 401")
	}
}
