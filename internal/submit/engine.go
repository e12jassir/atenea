package submit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/e12jassir/atenea/internal/session"
)

// SubmitEngine define la interfaz estándar para el motor de envíos de evidencias.
type SubmitEngine interface {
	Submit(ctx context.Context, req SubmissionRequest) (*SubmissionReceipt, error)
}

// Config configura los parámetros del motor de envíos.
type Config struct {
	HTTPClient   *http.Client
	BaseURL      string            // URL base fija (prioritaria, ideal para pruebas unitarias)
	PlatformURLs map[string]string // Mapeo de plataforma a URL base
	ReceiptsDir  string            // Directorio de comprobantes (por defecto ~/.local/share/atenea/receipts)
	SessionStore session.SessionStore
}

// MoodleSubmitEngine orquesta la coreografía de 3 pasos de Moodle/Zajuna para entrega de tareas.
type MoodleSubmitEngine struct {
	client      *http.Client
	cfg         Config
	receiptsDir string
}

var _ SubmitEngine = (*MoodleSubmitEngine)(nil)

// DefaultReceiptsDir retorna la ruta por defecto del directorio de recibos: ~/.local/share/atenea/receipts
func DefaultReceiptsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("error obteniendo el directorio de usuario: %w", err)
	}
	return filepath.Join(home, ".local", "share", "atenea", "receipts"), nil
}

// NewEngine crea una nueva instancia de MoodleSubmitEngine con la configuración dada.
func NewEngine(cfg Config) (*MoodleSubmitEngine, error) {
	return NewMoodleSubmitEngine(cfg)
}

// NewMoodleSubmitEngine inicializa el cliente HTTP, jar de cookies y directorio de comprobantes.
func NewMoodleSubmitEngine(cfg Config) (*MoodleSubmitEngine, error) {
	client := cfg.HTTPClient
	if client == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, fmt.Errorf("error creando cookie jar: %w", err)
		}
		client = &http.Client{
			Jar:     jar,
			Timeout: 45 * time.Second,
		}
	} else if client.Jar == nil {
		// Asegurar que el cliente tenga cookiejar si fue inyectado sin uno
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, fmt.Errorf("error asignando cookie jar al cliente HTTP: %w", err)
		}
		client.Jar = jar
	}

	receiptsDir := cfg.ReceiptsDir
	if strings.TrimSpace(receiptsDir) == "" {
		defDir, err := DefaultReceiptsDir()
		if err != nil {
			return nil, err
		}
		receiptsDir = defDir
	}

	return &MoodleSubmitEngine{
		client:      client,
		cfg:         cfg,
		receiptsDir: receiptsDir,
	}, nil
}

// ReceiptsDir expone la ruta configurada para el almacenamiento de comprobantes.
func (e *MoodleSubmitEngine) ReceiptsDir() string {
	return e.receiptsDir
}

// CalculateFileHashAndSize computa el hash criptográfico SHA-256 y tamaño en bytes del archivo local.
func CalculateFileHashAndSize(filePath string) (string, int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, fmt.Errorf("%w: %s", ErrFileNotFound, filePath)
		}
		return "", 0, err
	}
	if info.IsDir() {
		return "", 0, fmt.Errorf("la ruta especificada es un directorio: %s", filePath)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("error al abrir archivo: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", 0, fmt.Errorf("error calculando hash SHA-256: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), info.Size(), nil
}

// CalculateFileHashAndSize expone el cálculo de hash a nivel de instancia.
func (e *MoodleSubmitEngine) CalculateFileHashAndSize(filePath string) (string, int64, error) {
	return CalculateFileHashAndSize(filePath)
}

func (e *MoodleSubmitEngine) resolveBaseURL(platform string) (string, error) {
	if e.cfg.BaseURL != "" {
		return strings.TrimRight(e.cfg.BaseURL, "/"), nil
	}
	if e.cfg.PlatformURLs != nil {
		if u, ok := e.cfg.PlatformURLs[strings.ToLower(platform)]; ok && u != "" {
			return strings.TrimRight(u, "/"), nil
		}
	}
	switch strings.ToLower(platform) {
	case "sena":
		return "https://zajuna.sena.edu.co/zajuna", nil
	case "sima":
		return "https://sima.unicartagena.edu.co", nil
	case "ingles":
		return "https://ingles.unicartagena.edu.co", nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedPlatform, platform)
	}
}

// Submit ejecuta la coreografía de 3 pasos y validación para el envío formal de una evidencia.
func (e *MoodleSubmitEngine) Submit(ctx context.Context, req SubmissionRequest) (*SubmissionReceipt, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	baseURL, err := e.resolveBaseURL(req.Platform)
	if err != nil {
		return nil, err
	}

	// Restaurar sesión activa de la bóveda/store si está configurado
	if e.cfg.SessionStore != nil && req.Platform != "" {
		entry, err := e.cfg.SessionStore.Load(req.Platform)
		if err == nil && entry != nil && entry.IsValid() && len(entry.Cookies) > 0 {
			if u, err := url.Parse(baseURL); err == nil && e.client.Jar != nil {
				e.client.Jar.SetCookies(u, entry.Cookies)
			}
		}
	}

	// 0. Calcular SHA-256 y tamaño del archivo
	fileHash, fileSize, err := CalculateFileHashAndSize(req.FilePath)
	if err != nil {
		return nil, err
	}
	fileName := filepath.Base(req.FilePath)

	// =========================================================================
	// Fase 1: GET /mod/assign/view.php?id=<assign_id>&action=editsubmission
	// =========================================================================
	editURL := fmt.Sprintf("%s/mod/assign/view.php?id=%s&action=editsubmission", baseURL, url.QueryEscape(req.AssignmentID))
	getReq, err := http.NewRequestWithContext(ctx, "GET", editURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error construyendo petición Phase 1: %w", err)
	}
	e.setCommonHeaders(getReq, baseURL)

	getResp, err := e.client.Do(getReq)
	if err != nil {
		return nil, fmt.Errorf("error en petición Phase 1 hacia %s: %w", editURL, err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error al obtener formulario de entrega (HTTP %d)", getResp.StatusCode)
	}

	htmlBytes, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo respuesta HTML de Phase 1: %w", err)
	}
	html := string(htmlBytes)

	sessKey, err := extractSessKey(html)
	if err != nil {
		return nil, err
	}

	draftItemID, err := extractDraftItemID(html)
	if err != nil {
		return nil, err
	}

	repoID := extractRepoID(html)

	// =========================================================================
	// Fase 2: POST multipart/form-data a /repository/upload.php
	// =========================================================================
	uploadURL := fmt.Sprintf("%s/repository/upload.php", baseURL)
	bodyBuf := &bytes.Buffer{}
	writer := multipart.NewWriter(bodyBuf)

	_ = writer.WriteField("repo_id", repoID)
	_ = writer.WriteField("itemid", draftItemID)
	_ = writer.WriteField("sesskey", sessKey)
	_ = writer.WriteField("action", "upload")
	_ = writer.WriteField("title", fileName)
	_ = writer.WriteField("author", "Atenea")
	_ = writer.WriteField("license", "allrightsreserved")
	_ = writer.WriteField("savepath", "/")

	part, err := writer.CreateFormFile("repo_upload_file", fileName)
	if err != nil {
		return nil, fmt.Errorf("error creando parte multipart: %w", err)
	}

	fileContent, err := os.ReadFile(req.FilePath)
	if err != nil {
		return nil, fmt.Errorf("error leyendo archivo para subida: %w", err)
	}
	if _, err := part.Write(fileContent); err != nil {
		return nil, fmt.Errorf("error escribiendo archivo en stream multipart: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("error cerrando escritor multipart: %w", err)
	}

	uploadReq, err := http.NewRequestWithContext(ctx, "POST", uploadURL, bodyBuf)
	if err != nil {
		return nil, fmt.Errorf("error construyendo petición Phase 2: %w", err)
	}
	e.setCommonHeaders(uploadReq, editURL)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	uploadReq.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")

	uploadResp, err := e.client.Do(uploadReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUploadFailed, err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK && uploadResp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("%w: código de respuesta HTTP %d", ErrUploadFailed, uploadResp.StatusCode)
	}

	uploadRespBytes, _ := io.ReadAll(uploadResp.Body)
	if err := checkUploadError(uploadRespBytes); err != nil {
		return nil, err
	}

	// =========================================================================
	// Fase 3: POST a /mod/assign/view.php?id=<assign_id> con savechanges=1
	// =========================================================================
	saveURL := fmt.Sprintf("%s/mod/assign/view.php?id=%s", baseURL, url.QueryEscape(req.AssignmentID))
	formValues := url.Values{}
	formValues.Set("id", req.AssignmentID)
	formValues.Set("sesskey", sessKey)
	formValues.Set("action", "savesubmission")
	formValues.Set("savechanges", "1")
	formValues.Set("files_filemanager", draftItemID)
	if req.Comment != "" {
		formValues.Set("submission_comment[text]", req.Comment)
		formValues.Set("submission_comment[format]", "1")
	}

	saveReq, err := http.NewRequestWithContext(ctx, "POST", saveURL, strings.NewReader(formValues.Encode()))
	if err != nil {
		return nil, fmt.Errorf("error construyendo petición Phase 3: %w", err)
	}
	e.setCommonHeaders(saveReq, editURL)
	saveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var sawRedirect303 bool
	redirectClient := *e.client
	redirectClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && req.Response != nil {
			status := req.Response.StatusCode
			if status == http.StatusSeeOther || status == http.StatusFound {
				sawRedirect303 = true
			}
		}
		if len(via) >= 10 {
			return errors.New("demasiadas redirecciones")
		}
		return nil
	}

	saveResp, err := redirectClient.Do(saveReq)
	if err != nil {
		return nil, fmt.Errorf("error en petición Phase 3: %w", err)
	}
	defer saveResp.Body.Close()

	// =========================================================================
	// Fase 4: Verificación (Redirección 303 o texto confirmatorio en HTML)
	// =========================================================================
	saveBodyBytes, _ := io.ReadAll(saveResp.Body)
	saveBodyStr := string(saveBodyBytes)

	isConfirmed := sawRedirect303 ||
		saveResp.StatusCode == http.StatusSeeOther ||
		saveResp.StatusCode == http.StatusFound ||
		isConfirmationContent(saveBodyStr)

	if !isConfirmed {
		return nil, fmt.Errorf("%w: la respuesta del servidor no confirmó la entrega (HTTP %d)", ErrSubmissionFailed, saveResp.StatusCode)
	}

	// =========================================================================
	// Generar y persistir comprobante criptográfico (0600)
	// =========================================================================
	now := time.Now().UTC()
	receipt := &SubmissionReceipt{
		Platform:       req.Platform,
		AssignmentID:   req.AssignmentID,
		FileHashSHA256: fileHash,
		FileSize:       fileSize,
		FileName:       fileName,
		RemoteDraftID:  draftItemID,
		Timestamp:      now,
		Status:         "SUBMITTED",
	}

	receiptPath, err := e.saveReceipt(receipt)
	if err != nil {
		return nil, fmt.Errorf("error guardando comprobante: %w", err)
	}
	receipt.ReceiptPath = receiptPath

	return receipt, nil
}

// SubmitSimple es un wrapper conveniente de Submit sin requerir contexto explícito.
func (e *MoodleSubmitEngine) SubmitSimple(req SubmissionRequest) (*SubmissionReceipt, error) {
	return e.Submit(context.Background(), req)
}

// SaveReceipt expone la persistencia del comprobante local en modo 0600.
func (e *MoodleSubmitEngine) SaveReceipt(receipt *SubmissionReceipt) (string, error) {
	return e.saveReceipt(receipt)
}

func (e *MoodleSubmitEngine) saveReceipt(receipt *SubmissionReceipt) (string, error) {
	return SaveReceipt(e.receiptsDir, receipt)
}

// SaveReceipt persiste un SubmissionReceipt como JSON en el directorio dado con permisos 0600 y directorio 0700.
func SaveReceipt(dir string, receipt *SubmissionReceipt) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("directorio de recibos no especificado")
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("error creando directorio de recibos: %w", err)
	}
	_ = os.Chmod(dir, 0700)

	safePlatform := sanitizeComponent(receipt.Platform)
	safeAssignID := sanitizeComponent(receipt.AssignmentID)
	timestamp := receipt.Timestamp.Unix()

	baseName := fmt.Sprintf("%s_%s_%d.json", safePlatform, safeAssignID, timestamp)
	targetPath := filepath.Join(dir, baseName)

	if _, err := os.Stat(targetPath); err == nil {
		// Desambiguar si ya existe un archivo en el mismo segundo exacto
		baseName = fmt.Sprintf("%s_%s_%d_%d.json", safePlatform, safeAssignID, timestamp, receipt.Timestamp.Nanosecond())
		targetPath = filepath.Join(dir, baseName)
	}

	receipt.ReceiptPath = targetPath

	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error serializando comprobante JSON: %w", err)
	}

	// Escritura atómica vía archivo temporal
	tmpFile, err := os.CreateTemp(dir, ".receipt-*.tmp")
	if err != nil {
		return "", fmt.Errorf("error creando archivo temporal de comprobante: %w", err)
	}
	tmpPath := tmpFile.Name()

	if err := os.Chmod(tmpPath, 0600); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("error finalizando escritura de comprobante: %w", err)
	}

	_ = os.Chmod(targetPath, 0600)
	return targetPath, nil
}

func (e *MoodleSubmitEngine) setCommonHeaders(req *http.Request, referer string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0 Atenea/2.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9,en;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
}

func sanitizeComponent(s string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", " ", "_", ":", "-", "..", "", "\"", "", "'", "")
	res := r.Replace(strings.TrimSpace(s))
	if res == "" {
		return "unknown"
	}
	return res
}

func extractSessKey(html string) (string, error) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`name=["']sesskey["']\s+value=["']([^"']+)["']`),
		regexp.MustCompile(`value=["']([^"']+)["']\s+name=["']sesskey["']`),
		regexp.MustCompile(`["']sesskey["']\s*[:=]\s*["']?([a-zA-Z0-9_\-]+)["']?`),
		regexp.MustCompile(`[?&]sesskey=([a-zA-Z0-9_\-]+)`),
		regexp.MustCompile(`M\.cfg\s*=\s*\{[^}]*["']sesskey["']\s*:\s*["']([^"']+)["']`),
	}
	for _, p := range patterns {
		if m := p.FindStringSubmatch(html); len(m) > 1 {
			return m[1], nil
		}
	}
	return "", ErrMissingSessKey
}

func extractDraftItemID(html string) (string, error) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`name=["']files_filemanager["']\s+value=["'](\d+)["']`),
		regexp.MustCompile(`value=["'](\d+)["']\s+name=["']files_filemanager["']`),
		regexp.MustCompile(`name=["'][^"']*draftitemid[^"']*["']\s+value=["'](\d+)["']`),
		regexp.MustCompile(`value=["'](\d+)["']\s+name=["'][^"']*draftitemid[^"']*["']`),
		regexp.MustCompile(`name=["'][^"']*filemanager[^"']*["']\s+value=["'](\d+)["']`),
		regexp.MustCompile(`value=["'](\d+)["']\s+name=["'][^"']*filemanager[^"']*["']`),
		regexp.MustCompile(`["']itemid["']\s*:\s*["']?(\d+)["']?`),
		regexp.MustCompile(`["']draftitemid["']\s*:\s*["']?(\d+)["']?`),
		regexp.MustCompile(`["']itemid["']\s*=\s*["']?(\d+)["']?`),
		regexp.MustCompile(`["']draftitemid["']\s*=\s*["']?(\d+)["']?`),
		regexp.MustCompile(`itemid=(\d+)`),
		regexp.MustCompile(`draftitemid=(\d+)`),
		regexp.MustCompile(`draftfile\.php/\d+/user/draft/(\d+)`),
	}
	for _, p := range patterns {
		if m := p.FindStringSubmatch(html); len(m) > 1 {
			return m[1], nil
		}
	}
	return "", ErrMissingDraftID
}

func extractRepoID(html string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`name=["']repo_id["']\s+value=["'](\d+)["']`),
		regexp.MustCompile(`["']repo_id["']\s*:\s*["']?(\d+)["']?`),
	}
	for _, p := range patterns {
		if m := p.FindStringSubmatch(html); len(m) > 1 {
			return m[1]
		}
	}
	return "4"
}

func checkUploadError(respBytes []byte) error {
	var jsonResp struct {
		Error     string `json:"error"`
		ErrorCode string `json:"errcode"`
	}
	if err := json.Unmarshal(respBytes, &jsonResp); err == nil {
		if jsonResp.Error != "" {
			return fmt.Errorf("%w: %s", ErrUploadFailed, jsonResp.Error)
		}
		if jsonResp.ErrorCode != "" {
			return fmt.Errorf("%w: código de error %s", ErrUploadFailed, jsonResp.ErrorCode)
		}
	}
	return nil
}

func isConfirmationContent(body string) bool {
	lower := strings.ToLower(body)
	indicators := []string{
		"enviado para calificar",
		"submitted for grading",
		"submissionstatussubmitted",
		"status-submitted",
		"entrega enviada",
		"cambios guardados",
		"borrador (no enviado)",
		"early",
	}
	for _, ind := range indicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}
	return false
}
