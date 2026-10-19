package sima

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/e12jassir/atenea/internal/session"
)

// Helper to create a temporary SessionStore for testing
func newTestSessionStore(t *testing.T) session.SessionStore {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "sessions.json")
	store, err := session.NewFileStore(storePath)
	if err != nil {
		t.Fatalf("failed to create test session store: %v", err)
	}
	return store
}

func TestHttpStatusChecking(t *testing.T) {
	t.Run("Login GET non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("Service Unavailable"))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		err = client.Login("user", "pass")
		if err == nil {
			t.Fatal("expected error on HTTP 503, got nil")
		}
		if !strings.Contains(err.Error(), "503") {
			t.Errorf("expected error to contain status 503, got: %v", err)
		}
	})

	t.Run("Login POST non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && r.URL.Path == "/login/index.php" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<html><input name="logintoken" value="testtoken123"></html>`))
				return
			}
			if r.Method == "POST" && r.URL.Path == "/login/index.php" {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte("Bad Gateway"))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		err = client.Login("user", "pass")
		if err == nil {
			t.Fatal("expected error on HTTP 502, got nil")
		}
		if !strings.Contains(err.Error(), "502") {
			t.Errorf("expected error to contain status 502, got: %v", err)
		}
	})

	t.Run("GetUpcomingEvents non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Forbidden"))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		_, err = client.GetUpcomingEvents()
		if err == nil {
			t.Fatal("expected error on HTTP 403, got nil")
		}
		if !strings.Contains(err.Error(), "403") {
			t.Errorf("expected error to contain status 403, got: %v", err)
		}
	})

	t.Run("GetGradesOverview non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Server Error"))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		_, err = client.GetGradesOverview()
		if err == nil {
			t.Fatal("expected error on HTTP 500, got nil")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("expected error to contain status 500, got: %v", err)
		}
	})

	t.Run("GetCourseGradeBreakdown non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Not Found"))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		_, err = client.GetCourseGradeBreakdown("12345")
		if err == nil {
			t.Fatal("expected error on HTTP 404, got nil")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("expected error to contain status 404, got: %v", err)
		}
	})

	t.Run("CheckActivityStatus non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized"))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		status := client.CheckActivityStatus(ts.URL + "/mod/assign/view.php?id=10")
		if !strings.Contains(status, "401") {
			t.Errorf("expected status to mention HTTP 401, got: %q", status)
		}
	})

	t.Run("GetAnnouncementDetail non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Not Found"))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		_, err = client.GetAnnouncementDetail("999")
		if err == nil {
			t.Fatal("expected error on HTTP 404, got nil")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("expected error to contain status 404, got: %v", err)
		}
	})
}

func TestLoginWorkflow(t *testing.T) {
	t.Run("Successful Login and Cookie / SessKey Capture", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && r.URL.Path == "/login/index.php" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<html><input name="logintoken" value="test-token-abc"></html>`))
				return
			}
			if r.Method == "POST" && r.URL.Path == "/login/index.php" {
				_ = r.ParseForm()
				if r.FormValue("username") == "esteban" && r.FormValue("password") == "secret123" {
					http.SetCookie(w, &http.Cookie{
						Name:  "MoodleSession",
						Value: "session-cookie-xyz",
						Path:  "/",
					})
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`<html><body>Logged in <a href="logout.php?sesskey=sk_alpha123">Logout</a></body></html>`))
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<html><div class="loginerrormessage">Datos erróneos. Por favor, inténtelo otra vez.</div></html>`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		store := newTestSessionStore(t)
		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		client.SetSessionStore(store, "sima")

		err = client.Login("esteban", "secret123")
		if err != nil {
			t.Fatalf("Login failed: %v", err)
		}

		if client.SessKey() != "sk_alpha123" {
			t.Errorf("expected sesskey 'sk_alpha123', got %q", client.SessKey())
		}

		// Verify that session was automatically saved in store
		entry, err := store.Load("sima")
		if err != nil {
			t.Fatalf("failed to load saved session from store: %v", err)
		}
		if entry.SessKey != "sk_alpha123" {
			t.Errorf("expected stored sesskey 'sk_alpha123', got %q", entry.SessKey)
		}
		ck := entry.GetCookie("MoodleSession")
		if ck == nil || ck.Value != "session-cookie-xyz" {
			t.Errorf("expected MoodleSession cookie 'session-cookie-xyz', got %+v", ck)
		}
	})

	t.Run("Invalid Credentials", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<html><input name="logintoken" value="test-token-abc"></html>`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><div class="loginerrormessage">Datos erróneos</div></html>`))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		err = client.Login("esteban", "wrongpass")
		if err == nil {
			t.Fatal("expected error on invalid credentials, got nil")
		}
		if !strings.Contains(err.Error(), "incorrectos") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestSessionRestoreAndSave(t *testing.T) {
	store := newTestSessionStore(t)

	client, err := NewClient("https://sima.unicartagena.edu.co")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Restore from empty store should return (false, nil)
	restored, err := client.RestoreSession(store, "sima")
	if err != nil {
		t.Fatalf("unexpected error on empty store: %v", err)
	}
	if restored {
		t.Fatal("expected restored=false for non-existent session")
	}

	// 2. Save active session to store
	expires := time.Now().Add(2 * time.Hour)
	entry := &session.SessionEntry{
		Platform: "sima",
		Cookies: []*http.Cookie{
			{Name: "MoodleSession", Value: "token-sima-456", Path: "/", Domain: "sima.unicartagena.edu.co"},
		},
		SessKey:   "sesskey-xyz-789",
		ExpiresAt: expires,
	}
	if err := store.Save(entry); err != nil {
		t.Fatalf("failed to save session to store: %v", err)
	}

	// 3. RestoreSession should now return (true, nil) and inject cookies
	client2, err := NewClient("https://sima.unicartagena.edu.co")
	if err != nil {
		t.Fatal(err)
	}
	restored, err = client2.RestoreSession(store, "sima")
	if err != nil {
		t.Fatalf("RestoreSession failed: %v", err)
	}
	if !restored {
		t.Fatal("expected restored=true for valid session")
	}
	if client2.SessKey() != "sesskey-xyz-789" {
		t.Errorf("expected restored sesskey 'sesskey-xyz-789', got %q", client2.SessKey())
	}
	if !client2.hasMoodleCookie() {
		t.Error("expected client to have MoodleSession cookie after restore")
	}

	// 4. Expired session should return (false, nil)
	expiredEntry := &session.SessionEntry{
		Platform: "sima",
		Cookies: []*http.Cookie{
			{Name: "MoodleSession", Value: "expired-token", Path: "/", Domain: "sima.unicartagena.edu.co"},
		},
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if err := store.Save(expiredEntry); err != nil {
		t.Fatalf("failed to save expired session: %v", err)
	}

	client3, err := NewClient("https://sima.unicartagena.edu.co")
	if err != nil {
		t.Fatal(err)
	}
	restored, err = client3.RestoreSession(store, "sima")
	if err != nil {
		t.Fatalf("unexpected error on expired session: %v", err)
	}
	if restored {
		t.Fatal("expected restored=false for expired session")
	}

	// 5. Nil store returns error
	_, err = client3.RestoreSession(nil, "sima")
	if err == nil {
		t.Fatal("expected error on nil store, got nil")
	}
}

func TestEnsureAuthenticated(t *testing.T) {
	t.Run("Reuses valid active cached session without re-login", func(t *testing.T) {
		loginCalled := false
		probeCalled := false

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/login/") {
				loginCalled = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<html><input name="logintoken" value="tok"></html>`))
				return
			}
			if r.URL.Path == "/my/" {
				probeCalled = true
				// Check that cookie was provided
				cookie, err := r.Cookie("MoodleSession")
				if err == nil && cookie.Value == "valid-live-cookie" {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`<html><body>Dashboard content - Bienvenido <a href="logout?sesskey=sk_probe">Salir</a></body></html>`))
					return
				}
				// If not authenticated, redirect to login
				http.Redirect(w, r, "/login/index.php", http.StatusFound)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		store := newTestSessionStore(t)
		// Seed store with valid session
		err := store.Save(&session.SessionEntry{
			Platform: "sima",
			Cookies: []*http.Cookie{
				{Name: "MoodleSession", Value: "valid-live-cookie", Path: "/"},
			},
			SessKey:   "sk_initial",
			ExpiresAt: time.Now().Add(2 * time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		// EnsureAuthenticated should probe /my/, see that session is alive, and NOT call login!
		err = client.EnsureAuthenticated(store, "sima", "user", "wrong_password_if_login_runs")
		if err != nil {
			t.Fatalf("EnsureAuthenticated failed: %v", err)
		}

		if !probeCalled {
			t.Error("expected probe endpoint to be called")
		}
		if loginCalled {
			t.Error("expected login NOT to be called when session is valid")
		}
	})

	t.Run("Re-authenticates when cached session was invalidated on server", func(t *testing.T) {
		loginPOSTCalled := false

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/my/" {
				// Server rejected old session, redirects to login
				http.Redirect(w, r, "/login/index.php", http.StatusFound)
				return
			}
			if r.Method == "GET" && r.URL.Path == "/login/index.php" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<html><input name="logintoken" value="tok123"></html>`))
				return
			}
			if r.Method == "POST" && r.URL.Path == "/login/index.php" {
				loginPOSTCalled = true
				http.SetCookie(w, &http.Cookie{
					Name:  "MoodleSession",
					Value: "newly-issued-cookie",
					Path:  "/",
				})
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<html><body>Login ok <span sesskey="sk_renewed"></span></body></html>`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		store := newTestSessionStore(t)
		// Stored session has expired/invalid token
		_ = store.Save(&session.SessionEntry{
			Platform: "sima",
			Cookies: []*http.Cookie{
				{Name: "MoodleSession", Value: "stale-invalid-cookie", Path: "/"},
			},
			ExpiresAt: time.Now().Add(1 * time.Hour),
		})

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		err = client.EnsureAuthenticated(store, "sima", "user", "pass")
		if err != nil {
			t.Fatalf("EnsureAuthenticated failed: %v", err)
		}

		if !loginPOSTCalled {
			t.Fatal("expected client to re-login via POST after probe failure")
		}

		// Verify store updated with new cookie
		entry, err := store.Load("sima")
		if err != nil {
			t.Fatalf("failed to load updated session: %v", err)
		}
		ck := entry.GetCookie("MoodleSession")
		if ck == nil || ck.Value != "newly-issued-cookie" {
			t.Errorf("expected updated cookie 'newly-issued-cookie', got %+v", ck)
		}
	})
}

func TestParsingEndpoints(t *testing.T) {
	t.Run("GetUpcomingEvents HTML parsing", func(t *testing.T) {
		var ts *httptest.Server
		ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/calendar/view.php" {
				mockHTML := fmt.Sprintf(`
				<div>
					<h3 class="name d-inline-block"><a href="%s/mod/assign/view.php?id=101">Entrega Taller 1</a></h3>
					<span data-timestamp="1760000000">15 de Octubre de 2026, 23:59</span>
					<div title="Curso" class="col-11"><a href="#">UDC - IS - Ingenieria de Software II</a></div>
				</div>
				<div>
					<h3 class="name d-inline-block"><a href="%s/mod/quiz/view.php?id=102">Cuestionario Unidad 1</a></h3>
					<span data-timestamp="1760086400">16 de Octubre de 2026, 20:00</span>
					<div title="Curso" class="col-11"><a href="#">UDC - BD - Bases de Datos</a></div>
				</div>
				`, ts.URL, ts.URL)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(mockHTML))
				return
			}
			if strings.Contains(r.URL.Path, "mod/assign") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<div>Enviado para calificar</div>`))
				return
			}
			if strings.Contains(r.URL.Path, "mod/quiz") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<div>Su calificación final en este cuestionario es 4.5/5.0 Finalizado</div>`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		events, err := client.GetUpcomingEvents()
		if err != nil {
			t.Fatalf("GetUpcomingEvents failed: %v", err)
		}

		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}

		if events[0].Title != "Entrega Taller 1" {
			t.Errorf("expected title 'Entrega Taller 1', got %q", events[0].Title)
		}
		if events[0].Course != "Ingenieria de Software II" {
			t.Errorf("expected course 'Ingenieria de Software II', got %q", events[0].Course)
		}
		if events[0].DueDate != "15 de Octubre de 2026, 23:59" {
			t.Errorf("expected due date '15 de Octubre de 2026, 23:59', got %q", events[0].DueDate)
		}
		if events[0].Timestamp != 1760000000 {
			t.Errorf("expected timestamp 1760000000, got %d", events[0].Timestamp)
		}
	})

	t.Run("GetGradesOverview HTML parsing", func(t *testing.T) {
		mockHTML := `
		<table>
			<tr>
				<td><a href="https://sima.unicartagena.edu.co/grade/report/user/index.php?id=501">UDC - SEM - Arquitectura de Software</a></td>
				<td>4.8</td>
			</tr>
			<tr>
				<td><a href="https://sima.unicartagena.edu.co/grade/report/user/index.php?id=502">UDC - SEM - Redes y Comunicaciones</a></td>
				<td></td>
			</tr>
		</table>
		`
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(mockHTML))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		grades, err := client.GetGradesOverview()
		if err != nil {
			t.Fatalf("GetGradesOverview failed: %v", err)
		}

		if len(grades) != 2 {
			t.Fatalf("expected 2 course grades, got %d", len(grades))
		}
		if grades[0].Course != "Arquitectura de Software" || grades[0].Grade != "4.8" || grades[0].ID != "501" {
			t.Errorf("unexpected grade[0]: %+v", grades[0])
		}
		if grades[1].Course != "Redes y Comunicaciones" || grades[1].Grade != "-" || grades[1].ID != "502" {
			t.Errorf("unexpected grade[1]: %+v", grades[1])
		}
	})

	t.Run("GetCourseGradeBreakdown HTML parsing", func(t *testing.T) {
		mockHTML := `
		<table>
			<tr>
				<td>Protocolo individual Unidad 1</td>
				<td>20 %</td>
				<td>4.5 Acciones</td>
				<td>0.0&ndash;5.0</td>
			</tr>
			<tr>
				<td>Evaluación parcial 1</td>
				<td>30 %</td>
				<td>4.0</td>
				<td>0.0&ndash;5.0</td>
			</tr>
		</table>
		`
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(mockHTML))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		items, err := client.GetCourseGradeBreakdown("501")
		if err != nil {
			t.Fatalf("GetCourseGradeBreakdown failed: %v", err)
		}

		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}
		if items[0].Category != "Protocolo" || items[0].Grade != "4.5" || items[0].Weight != "20 %" {
			t.Errorf("unexpected item[0]: %+v", items[0])
		}
		if items[1].Category != "Examen" || items[1].Grade != "4.0" || items[1].Weight != "30 %" {
			t.Errorf("unexpected item[1]: %+v", items[1])
		}
	})

	t.Run("GetAnnouncementDetail HTML parsing", func(t *testing.T) {
		mockHTML := `
		<div class="post-content-container">
			<style>.hidden { display: none; }</style>
			Estimados estudiantes,<br>
			Se ha publicado la guía de trabajo colaborativo.<br>
			Atentamente,<br>
			El Docente.
		</div>
		`
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(mockHTML))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		detail, err := client.GetAnnouncementDetail("888")
		if err != nil {
			t.Fatalf("GetAnnouncementDetail failed: %v", err)
		}

		if !strings.Contains(detail, "Estimados estudiantes") || !strings.Contains(detail, "guía de trabajo colaborativo") {
			t.Errorf("unexpected detail text: %s", detail)
		}
	})
}

func TestAdditionalEdgeCases(t *testing.T) {
	t.Run("GetAnnouncements non-200 on grades overview", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Error"))
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		_, err = client.GetAnnouncements()
		if err == nil {
			t.Fatal("expected error when GetGradesOverview returns 500, got nil")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("expected error to mention 500, got: %v", err)
		}
	})

	t.Run("SaveSession without cookies returns error", func(t *testing.T) {
		store := newTestSessionStore(t)
		client, err := NewClient("https://sima.unicartagena.edu.co")
		if err != nil {
			t.Fatal(err)
		}

		err = client.SaveSession(store, "sima")
		if err == nil {
			t.Fatal("expected error saving empty session, got nil")
		}
		if !strings.Contains(err.Error(), "no hay cookies") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("SaveSession with nil store returns error", func(t *testing.T) {
		client, err := NewClient("https://sima.unicartagena.edu.co")
		if err != nil {
			t.Fatal(err)
		}
		err = client.SaveSession(nil, "sima")
		if err == nil {
			t.Fatal("expected error on nil store, got nil")
		}
	})

	t.Run("EnsureAuthenticated fails on invalid credentials", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<html><input name="logintoken" value="tok"></html>`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><div class="loginerrormessage">Datos erróneos</div></html>`))
		}))
		defer ts.Close()

		store := newTestSessionStore(t)
		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		err = client.EnsureAuthenticated(store, "sima", "user", "badpass")
		if err == nil {
			t.Fatal("expected error on invalid credentials, got nil")
		}
		if !strings.Contains(err.Error(), "incorrectos") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("EnsureAuthenticated with nil store logs in successfully", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && r.URL.Path == "/login/index.php" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<html><input name="logintoken" value="tok"></html>`))
				return
			}
			if r.Method == "POST" && r.URL.Path == "/login/index.php" {
				http.SetCookie(w, &http.Cookie{
					Name:  "MoodleSession",
					Value: "valid-session",
					Path:  "/",
				})
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<html><body>Welcome</body></html>`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		client, err := NewClient(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		err = client.EnsureAuthenticated(nil, "sima", "user", "pass")
		if err != nil {
			t.Fatalf("EnsureAuthenticated failed with nil store: %v", err)
		}
		if !client.hasMoodleCookie() {
			t.Error("expected client to have MoodleSession cookie")
		}
	})
}
