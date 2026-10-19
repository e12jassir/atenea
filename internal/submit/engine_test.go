package submit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/e12jassir/atenea/internal/session"
)

// Helper para crear archivos temporales con contenido
func createTempTestFile(t *testing.T, dir, filename, content string) string {
	t.Helper()
	filePath := filepath.Join(dir, filename)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("error creando archivo de prueba: %v", err)
	}
	return filePath
}

func TestCalculateFileHashAndSize(t *testing.T) {
	tempDir := t.TempDir()
	content := "Atenea cryptographic submission payload\n"
	filePath := createTempTestFile(t, tempDir, "taller.pdf", content)

	expectedHashBytes := sha256.Sum256([]byte(content))
	expectedHash := hex.EncodeToString(expectedHashBytes[:])
	expectedSize := int64(len(content))

	// Standalone function
	hash, size, err := CalculateFileHashAndSize(filePath)
	if err != nil {
		t.Fatalf("CalculateFileHashAndSize falló: %v", err)
	}
	if hash != expectedHash {
		t.Errorf("hash esperado %s, obtenido %s", expectedHash, hash)
	}
	if size != expectedSize {
		t.Errorf("tamaño esperado %d, obtenido %d", expectedSize, size)
	}

	// Engine method
	engine, err := NewEngine(Config{ReceiptsDir: tempDir})
	if err != nil {
		t.Fatalf("NewEngine falló: %v", err)
	}
	engHash, engSize, err := engine.CalculateFileHashAndSize(filePath)
	if err != nil {
		t.Fatalf("engine.CalculateFileHashAndSize falló: %v", err)
	}
	if engHash != expectedHash || engSize != expectedSize {
		t.Errorf("engine method hash/size discrepancia: %s (%d)", engHash, engSize)
	}

	// Archivo inexistente
	_, _, err = CalculateFileHashAndSize(filepath.Join(tempDir, "inexistente.pdf"))
	if err == nil || !strings.Contains(err.Error(), ErrFileNotFound.Error()) {
		t.Errorf("se esperaba ErrFileNotFound, obtenido %v", err)
	}

	// Directorio como archivo
	_, _, err = CalculateFileHashAndSize(tempDir)
	if err == nil {
		t.Error("se esperaba error al pasar directorio como ruta de archivo")
	}
}

func TestSaveReceipt_PermissionsAndContent(t *testing.T) {
	tempDir := filepath.Join(t.TempDir(), "receipts_sub")
	now := time.Now().UTC()

	receipt := &SubmissionReceipt{
		Platform:       "sena",
		AssignmentID:   "GA2-220501093-AA3-EV01",
		FileHashSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		FileSize:       1024,
		FileName:       "evidencia.pdf",
		RemoteDraftID:  "8837419",
		Timestamp:      now,
		Status:         "SUBMITTED",
	}

	savedPath, err := SaveReceipt(tempDir, receipt)
	if err != nil {
		t.Fatalf("SaveReceipt falló: %v", err)
	}

	// Verificar que el path retornado coincide con el asignado
	if receipt.ReceiptPath != savedPath {
		t.Errorf("ReceiptPath esperado %s, obtenido %s", savedPath, receipt.ReceiptPath)
	}

	// Verificar permisos del directorio: 0700
	dirInfo, err := os.Stat(tempDir)
	if err != nil {
		t.Fatalf("error al leer dirInfo: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0700 {
		t.Errorf("permisos de directorio esperados 0700, obtenidos %04o", perm)
	}

	// Verificar permisos del archivo de comprobante: 0600
	fileInfo, err := os.Stat(savedPath)
	if err != nil {
		t.Fatalf("error al leer fileInfo: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0600 {
		t.Errorf("permisos de archivo esperados 0600, obtenidos %04o", perm)
	}

	// Verificar contenido JSON
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("error leyendo comprobante guardado: %v", err)
	}

	var parsed SubmissionReceipt
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("error parseando comprobante JSON: %v", err)
	}

	if parsed.Platform != receipt.Platform ||
		parsed.AssignmentID != receipt.AssignmentID ||
		parsed.FileHashSHA256 != receipt.FileHashSHA256 ||
		parsed.FileSize != receipt.FileSize ||
		parsed.FileName != receipt.FileName ||
		parsed.RemoteDraftID != receipt.RemoteDraftID ||
		parsed.Status != receipt.Status {
		t.Errorf("contenido de comprobante no coincide: %+v vs %+v", parsed, receipt)
	}

	// Probar desambiguación si se guarda otro recibo con misma fecha/id
	secondSavedPath, err := SaveReceipt(tempDir, receipt)
	if err != nil {
		t.Fatalf("segundo SaveReceipt falló: %v", err)
	}
	if secondSavedPath == savedPath {
		t.Errorf("se esperaba ruta desambiguada, pero ambas son %s", savedPath)
	}
}

func TestMoodleSubmissionDance_FullSuccess(t *testing.T) {
	tempDir := t.TempDir()
	content := "Solución a taller de algoritmia - Atenea\n"
	filePath := createTempTestFile(t, tempDir, "algoritmos.py", content)

	expectedHashBytes := sha256.Sum256([]byte(content))
	expectedHash := hex.EncodeToString(expectedHashBytes[:])

	const (
		testAssignID = "54321"
		testSessKey  = "sesskey_mock_abcdef123"
		testDraftID  = "77112233"
	)

	phase1Called := false
	phase2Called := false
	phase3Called := false
	phase4RedirectCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/mod/assign/view.php":
			action := r.URL.Query().Get("action")
			id := r.URL.Query().Get("id")

			if action == "editsubmission" {
				// Fase 1: Formulario con sesskey y draftitemid
				phase1Called = true
				if id != testAssignID {
					http.Error(w, "invalid assign id", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Editar entrega</title></head>
<body>
<form id="mform1" method="post" action="/mod/assign/view.php">
	<input type="hidden" name="sesskey" value="%s">
	<input type="hidden" name="files_filemanager" value="%s">
	<input type="hidden" name="repo_id" value="4">
</form>
</body>
</html>`, testSessKey, testDraftID)
				return
			}

			// Fase 4: Página resultante tras la redirección
			phase4RedirectCalled = true
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `<html><body>
<div class="submissionstatussubmitted">Enviado para calificar</div>
</body></html>`)

		case r.Method == "POST" && r.URL.Path == "/repository/upload.php":
			// Fase 2: Subida de archivo multipart
			phase2Called = true
			err := r.ParseMultipartForm(10 << 20)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if r.FormValue("sesskey") != testSessKey {
				http.Error(w, "invalid sesskey in upload", http.StatusForbidden)
				return
			}
			if r.FormValue("itemid") != testDraftID {
				http.Error(w, "invalid draft item id", http.StatusBadRequest)
				return
			}
			if r.FormValue("repo_id") != "4" {
				http.Error(w, "invalid repo_id", http.StatusBadRequest)
				return
			}

			file, header, err := r.FormFile("repo_upload_file")
			if err != nil {
				http.Error(w, "missing file", http.StatusBadRequest)
				return
			}
			defer file.Close()

			if header.Filename != "algoritmos.py" {
				http.Error(w, "wrong filename", http.StatusBadRequest)
				return
			}

			uploadedBytes, _ := io.ReadAll(file)
			if string(uploadedBytes) != content {
				http.Error(w, "file content mismatch", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"event":"fileattached","file":"algoritmos.py","id":99}`))

		case r.Method == "POST" && r.URL.Path == "/mod/assign/view.php":
			// Fase 3: Guardar cambios y confirmar
			phase3Called = true
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if r.FormValue("id") != testAssignID {
				http.Error(w, "wrong id", http.StatusBadRequest)
				return
			}
			if r.FormValue("sesskey") != testSessKey {
				http.Error(w, "wrong sesskey", http.StatusForbidden)
				return
			}
			if r.FormValue("savechanges") != "1" {
				http.Error(w, "missing savechanges", http.StatusBadRequest)
				return
			}
			if r.FormValue("files_filemanager") != testDraftID {
				http.Error(w, "wrong draft item id", http.StatusBadRequest)
				return
			}
			if r.FormValue("submission_comment[text]") != "Entrega final para revisión" {
				http.Error(w, "wrong comment", http.StatusBadRequest)
				return
			}

			// Retornar 303 See Other
			http.Redirect(w, r, "/mod/assign/view.php?id="+testAssignID, http.StatusSeeOther)

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	engine, err := NewEngine(Config{
		HTTPClient:  ts.Client(),
		BaseURL:     ts.URL,
		ReceiptsDir: filepath.Join(tempDir, "receipts"),
	})
	if err != nil {
		t.Fatalf("NewEngine falló: %v", err)
	}

	req := SubmissionRequest{
		Platform:     "sima",
		AssignmentID: testAssignID,
		FilePath:     filePath,
		Comment:      "Entrega final para revisión",
	}

	receipt, err := engine.Submit(context.Background(), req)
	if err != nil {
		t.Fatalf("Submit falló: %v", err)
	}

	if !phase1Called || !phase2Called || !phase3Called || !phase4RedirectCalled {
		t.Fatalf("no se ejecutaron todas las fases: P1=%v, P2=%v, P3=%v, P4=%v",
			phase1Called, phase2Called, phase3Called, phase4RedirectCalled)
	}

	if receipt.Platform != "sima" {
		t.Errorf("plataforma esperada 'sima', obtenida '%s'", receipt.Platform)
	}
	if receipt.AssignmentID != testAssignID {
		t.Errorf("assignmentID esperado '%s', obtenido '%s'", testAssignID, receipt.AssignmentID)
	}
	if receipt.FileHashSHA256 != expectedHash {
		t.Errorf("hash esperado '%s', obtenido '%s'", expectedHash, receipt.FileHashSHA256)
	}
	if receipt.RemoteDraftID != testDraftID {
		t.Errorf("remoteDraftID esperado '%s', obtenido '%s'", testDraftID, receipt.RemoteDraftID)
	}
	if receipt.Status != "SUBMITTED" {
		t.Errorf("status esperado 'SUBMITTED', obtenido '%s'", receipt.Status)
	}

	// Verificar existencia física del recibo
	receiptData, err := os.ReadFile(receipt.ReceiptPath)
	if err != nil {
		t.Fatalf("error leyendo archivo de recibo físico: %v", err)
	}
	var storedReceipt SubmissionReceipt
	if err := json.Unmarshal(receiptData, &storedReceipt); err != nil {
		t.Fatalf("error deserializando recibo: %v", err)
	}
	if storedReceipt.FileHashSHA256 != expectedHash {
		t.Errorf("hash en disco %s != %s", storedReceipt.FileHashSHA256, expectedHash)
	}
}

func TestMoodleSubmissionDance_EnglishConfirmation(t *testing.T) {
	tempDir := t.TempDir()
	filePath := createTempTestFile(t, tempDir, "essay.docx", "English Essay\n")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Query().Get("action") == "editsubmission":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `{"sesskey":"key_en_999","draftitemid":"554433"}`)
		case r.Method == "POST" && r.URL.Path == "/repository/upload.php":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"success"}`))
		case r.Method == "POST" && r.URL.Path == "/mod/assign/view.php":
			// Confirmación directa sin 303 (status 200 con texto en inglés)
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<div>Submission status: Submitted for grading</div>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	engine, err := NewEngine(Config{
		HTTPClient:  ts.Client(),
		BaseURL:     ts.URL,
		ReceiptsDir: tempDir,
	})
	if err != nil {
		t.Fatalf("NewEngine falló: %v", err)
	}

	receipt, err := engine.Submit(nil, SubmissionRequest{
		Platform:     "ingles",
		AssignmentID: "8899",
		FilePath:     filePath,
	})
	if err != nil {
		t.Fatalf("Submit con texto en inglés falló: %v", err)
	}

	if receipt.RemoteDraftID != "554433" {
		t.Errorf("remoteDraftID esperado '554433', obtenido '%s'", receipt.RemoteDraftID)
	}
}

func TestMoodleSubmissionDance_Failures(t *testing.T) {
	tempDir := t.TempDir()
	filePath := createTempTestFile(t, tempDir, "tarea.pdf", "Contenido\n")

	t.Run("Phase 1: Formulario retorna 404", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer ts.Close()

		engine, _ := NewEngine(Config{HTTPClient: ts.Client(), BaseURL: ts.URL, ReceiptsDir: tempDir})
		_, err := engine.Submit(context.Background(), SubmissionRequest{
			Platform:     "sima",
			AssignmentID: "111",
			FilePath:     filePath,
		})
		if err == nil {
			t.Fatal("se esperaba error por HTTP 404 en Phase 1")
		}
	})

	t.Run("Phase 1: Falta sesskey en HTML", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`<html><body><input name="files_filemanager" value="12345"></body></html>`))
		}))
		defer ts.Close()

		engine, _ := NewEngine(Config{HTTPClient: ts.Client(), BaseURL: ts.URL, ReceiptsDir: tempDir})
		_, err := engine.Submit(context.Background(), SubmissionRequest{
			Platform:     "sima",
			AssignmentID: "111",
			FilePath:     filePath,
		})
		if !errors.Is(err, ErrMissingSessKey) {
			t.Fatalf("se esperaba ErrMissingSessKey, obtenido: %v", err)
		}
	})

	t.Run("Phase 1: Falta draftitemid en HTML", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`<html><body><input name="sesskey" value="sess123"></body></html>`))
		}))
		defer ts.Close()

		engine, _ := NewEngine(Config{HTTPClient: ts.Client(), BaseURL: ts.URL, ReceiptsDir: tempDir})
		_, err := engine.Submit(context.Background(), SubmissionRequest{
			Platform:     "sima",
			AssignmentID: "111",
			FilePath:     filePath,
		})
		if !errors.Is(err, ErrMissingDraftID) {
			t.Fatalf("se esperaba ErrMissingDraftID, obtenido: %v", err)
		}
	})

	t.Run("Phase 2: Error 500 en upload", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/repository/upload.php" {
				http.Error(w, "internal upload error", http.StatusInternalServerError)
				return
			}
			w.Write([]byte(`<html><input name="sesskey" value="k1"><input name="files_filemanager" value="99"></html>`))
		}))
		defer ts.Close()

		engine, _ := NewEngine(Config{HTTPClient: ts.Client(), BaseURL: ts.URL, ReceiptsDir: tempDir})
		_, err := engine.Submit(context.Background(), SubmissionRequest{
			Platform:     "sima",
			AssignmentID: "111",
			FilePath:     filePath,
		})
		if !errors.Is(err, ErrUploadFailed) {
			t.Fatalf("se esperaba ErrUploadFailed, obtenido: %v", err)
		}
	})

	t.Run("Phase 2: JSON retorna error de Moodle", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/repository/upload.php" {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"error":"El archivo excede el tamaño máximo permitido por el curso"}`))
				return
			}
			w.Write([]byte(`<html><input name="sesskey" value="k1"><input name="files_filemanager" value="99"></html>`))
		}))
		defer ts.Close()

		engine, _ := NewEngine(Config{HTTPClient: ts.Client(), BaseURL: ts.URL, ReceiptsDir: tempDir})
		_, err := engine.Submit(context.Background(), SubmissionRequest{
			Platform:     "sima",
			AssignmentID: "111",
			FilePath:     filePath,
		})
		if !errors.Is(err, ErrUploadFailed) {
			t.Fatalf("se esperaba ErrUploadFailed, obtenido: %v", err)
		}
	})

	t.Run("Phase 3 & 4: Confirmación rechazada / sin confirmación", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && r.URL.Query().Get("action") == "editsubmission":
				w.Write([]byte(`<html><input name="sesskey" value="k1"><input name="files_filemanager" value="99"></html>`))
			case r.Method == "POST" && r.URL.Path == "/repository/upload.php":
				w.Write([]byte(`{"status":"ok"}`))
			case r.Method == "POST" && r.URL.Path == "/mod/assign/view.php":
				// Retorna 200 pero sin ningún texto confirmatorio
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`<html><body>Ha ocurrido un error inesperado al procesar el formulario</body></html>`))
			}
		}))
		defer ts.Close()

		engine, _ := NewEngine(Config{HTTPClient: ts.Client(), BaseURL: ts.URL, ReceiptsDir: tempDir})
		_, err := engine.Submit(context.Background(), SubmissionRequest{
			Platform:     "sima",
			AssignmentID: "111",
			FilePath:     filePath,
		})
		if !errors.Is(err, ErrSubmissionFailed) {
			t.Fatalf("se esperaba ErrSubmissionFailed, obtenido: %v", err)
		}
	})
}

func TestMoodleSubmissionDance_ValidationErrors(t *testing.T) {
	tempDir := t.TempDir()
	filePath := createTempTestFile(t, tempDir, "dummy.txt", "payload")

	engine, _ := NewEngine(Config{ReceiptsDir: tempDir})

	// ID vacío
	_, err := engine.Submit(context.Background(), SubmissionRequest{
		Platform: "sima",
		FilePath: filePath,
	})
	if !errors.Is(err, ErrEmptyAssignmentID) {
		t.Errorf("se esperaba ErrEmptyAssignmentID, obtenido: %v", err)
	}

	// FilePath vacío
	_, err = engine.Submit(context.Background(), SubmissionRequest{
		Platform:     "sima",
		AssignmentID: "123",
	})
	if !errors.Is(err, ErrEmptyFilePath) {
		t.Errorf("se esperaba ErrEmptyFilePath, obtenido: %v", err)
	}

	// Archivo no existe
	_, err = engine.Submit(context.Background(), SubmissionRequest{
		Platform:     "sima",
		AssignmentID: "123",
		FilePath:     filepath.Join(tempDir, "no_existe.pdf"),
	})
	if !errors.Is(err, ErrFileNotFound) {
		t.Errorf("se esperaba ErrFileNotFound, obtenido: %v", err)
	}

	// Plataforma no soportada sin BaseURL
	_, err = engine.Submit(context.Background(), SubmissionRequest{
		Platform:     "canvas_lms",
		AssignmentID: "123",
		FilePath:     filePath,
	})
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("se esperaba ErrUnsupportedPlatform, obtenido: %v", err)
	}
}

func TestMoodleSubmissionDance_PlatformURLs(t *testing.T) {
	engine, err := NewEngine(Config{ReceiptsDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine falló: %v", err)
	}

	tests := []struct {
		platform string
		expected string
	}{
		{"sena", "https://zajuna.sena.edu.co/zajuna"},
		{"SENA", "https://zajuna.sena.edu.co/zajuna"},
		{"sima", "https://sima.unicartagena.edu.co"},
		{"SIMA", "https://sima.unicartagena.edu.co"},
		{"ingles", "https://ingles.unicartagena.edu.co"},
		{"INGLES", "https://ingles.unicartagena.edu.co"},
	}

	for _, tt := range tests {
		u, err := engine.resolveBaseURL(tt.platform)
		if err != nil {
			t.Errorf("resolveBaseURL(%s) error inesperado: %v", tt.platform, err)
		}
		if u != tt.expected {
			t.Errorf("resolveBaseURL(%s) = %s, esperado %s", tt.platform, u, tt.expected)
		}
	}
}

func TestMoodleSubmissionDance_SessionStoreIntegration(t *testing.T) {
	tempDir := t.TempDir()
	storeFile := filepath.Join(tempDir, "sessions.json")
	store, err := session.NewFileStore(storeFile)
	if err != nil {
		t.Fatalf("NewFileStore falló: %v", err)
	}

	// Guardar sesión para "sima"
	uTest, _ := url.Parse("http://127.0.0.1:9099")
	err = store.Save(&session.SessionEntry{
		Platform: "sima",
		Cookies: []*http.Cookie{
			{Name: "MoodleSession", Value: "session_val_12345", Domain: "127.0.0.1", Path: "/"},
		},
		SessKey:   "stored_sesskey",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})
	if err != nil {
		t.Fatalf("store.Save falló: %v", err)
	}

	engine, err := NewEngine(Config{
		BaseURL:      uTest.String(),
		SessionStore: store,
		ReceiptsDir:  tempDir,
	})
	if err != nil {
		t.Fatalf("NewEngine falló: %v", err)
	}

	// Simular carga de sesión al enviar
	req := SubmissionRequest{
		Platform:     "sima",
		AssignmentID: "123",
		FilePath:     createTempTestFile(t, tempDir, "doc.pdf", "data"),
	}
	// El resolve y la inyección se ejecutan en Submit. Comprobamos que el cookie jar se actualiza.
	_, _ = engine.Submit(context.Background(), req)

	cookies := engine.client.Jar.Cookies(uTest)
	found := false
	for _, ck := range cookies {
		if ck.Name == "MoodleSession" && ck.Value == "session_val_12345" {
			found = true
			break
		}
	}
	if !found {
		t.Error("la cookie MoodleSession no fue restaurada en el jar del cliente HTTP")
	}
}
