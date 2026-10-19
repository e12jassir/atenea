package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/session"
	"github.com/e12jassir/atenea/internal/vault"
)

// Status representa el estado de un componente diagnosticado
type Status string

const (
	StatusOK   Status = "OK"
	StatusWarn Status = "WARN"
	StatusFail Status = "FAIL"
)

// PlatformVaultStatus resume el estado de credenciales de una plataforma
type PlatformVaultStatus struct {
	Platform   model.Platform `json:"platform"`
	Configured bool           `json:"configured"`
	Username   string         `json:"username,omitempty"`
	DocType    string         `json:"doc_type,omitempty"`
}

// VaultDiagnostic contiene el resultado de diagnóstico de la bóveda
type VaultDiagnostic struct {
	Status          Status                                `json:"status"`
	Path            string                                `json:"path"`
	Initialized     bool                                  `json:"initialized"`
	Platforms       map[model.Platform]PlatformVaultStatus `json:"platforms"`
	Error           string                                `json:"error,omitempty"`
	Recommendations []string                              `json:"recommendations,omitempty"`
}

// PlatformSessionStatus resume el estado de sesión en caché de una plataforma
type PlatformSessionStatus struct {
	Platform     string    `json:"platform"`
	Cached       bool      `json:"cached"`
	Active       bool      `json:"active"`
	Expired      bool      `json:"expired"`
	UserID       string    `json:"user_id,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	CookiesCount int       `json:"cookies_count,omitempty"`
}

// SessionDiagnostic contiene el resultado de diagnóstico del caché de sesiones
type SessionDiagnostic struct {
	Status          Status                           `json:"status"`
	Path            string                           `json:"path"`
	Exists          bool                             `json:"exists"`
	Platforms       map[string]PlatformSessionStatus `json:"platforms"`
	Error           string                           `json:"error,omitempty"`
	Recommendations []string                         `json:"recommendations,omitempty"`
}

// ConnectivityDiagnostic contiene el resultado de conectividad con una institución
type ConnectivityDiagnostic struct {
	Status          Status        `json:"status"`
	Name            string        `json:"name"`
	URL             string        `json:"url"`
	StatusCode      int           `json:"status_code"`
	Latency         time.Duration `json:"latency"`
	Error           string        `json:"error,omitempty"`
	Recommendations []string      `json:"recommendations,omitempty"`
}

// AjaxDiagnostic contiene el resultado de verificación del endpoint AJAX de Moodle
type AjaxDiagnostic struct {
	Status          Status        `json:"status"`
	URL             string        `json:"url"`
	StatusCode      int           `json:"status_code"`
	Latency         time.Duration `json:"latency"`
	Available       bool          `json:"available"`
	Error           string        `json:"error,omitempty"`
	Recommendations []string      `json:"recommendations,omitempty"`
}

// DiagnosticReport consolida el reporte completo de diagnóstico del sistema
type DiagnosticReport struct {
	Timestamp       time.Time                `json:"timestamp"`
	OverallStatus   Status                   `json:"overall_status"`
	Vault           VaultDiagnostic          `json:"vault"`
	Session         SessionDiagnostic        `json:"session"`
	Institutions    []ConnectivityDiagnostic `json:"institutions"`
	Ajax            AjaxDiagnostic           `json:"ajax"`
	Recommendations []string                 `json:"recommendations"`
}

// InstitutionTarget define un objetivo institucional para prueba de conectividad
type InstitutionTarget struct {
	Name string
	URL  string
}

// DiagnosticConfig permite configurar endpoints y dependencias para pruebas y ejecución
type DiagnosticConfig struct {
	BaseDir      string
	CachePath    string
	HTTPClient   *http.Client
	Institutions []InstitutionTarget
	AjaxEndpoint string
}

// DefaultInstitutions retorna las instituciones soportadas por defecto
func DefaultInstitutions() []InstitutionTarget {
	return []InstitutionTarget{
		{Name: "SENA Zajuna", URL: "https://zajuna.sena.edu.co"},
		{Name: "UDC SIMA", URL: "https://sima.unicartagena.edu.co/login/index.php"},
		{Name: "UDC Inglés", URL: "https://ingles.unicartagena.edu.co/login/index.php"},
	}
}

// DefaultAjaxEndpoint retorna la URL estándar del endpoint AJAX de Moodle
func DefaultAjaxEndpoint() string {
	return "https://sima.unicartagena.edu.co/lib/ajax/service.php"
}

// RunDiagnostics ejecuta el conjunto de diagnósticos usando la configuración por defecto
// o la ruta base provista (si baseDir != "").
func RunDiagnostics(baseDir string) (*DiagnosticReport, error) {
	return RunDiagnosticsWithConfig(DiagnosticConfig{
		BaseDir: baseDir,
	})
}

// RunDiagnosticsWithConfig ejecuta el conjunto de diagnósticos con una configuración explícita
func RunDiagnosticsWithConfig(cfg DiagnosticConfig) (*DiagnosticReport, error) {
	report := &DiagnosticReport{
		Timestamp: time.Now(),
	}

	// 1. Vault Status
	checkVault(cfg, report)

	// 2. Session Cache
	checkSession(cfg, report)

	// Setup HTTP Client
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 5 * time.Second,
		}
	}

	institutions := cfg.Institutions
	if len(institutions) == 0 {
		institutions = DefaultInstitutions()
	}

	ajaxEndpoint := cfg.AjaxEndpoint
	if ajaxEndpoint == "" {
		ajaxEndpoint = DefaultAjaxEndpoint()
	}

	// 3 & 4. Probing Institutions and AJAX concurrently
	var wg sync.WaitGroup
	institutionResults := make([]ConnectivityDiagnostic, len(institutions))

	for i, target := range institutions {
		wg.Add(1)
		go func(idx int, tgt InstitutionTarget) {
			defer wg.Done()
			institutionResults[idx] = probeInstitution(client, tgt)
		}(i, target)
	}

	var ajaxResult AjaxDiagnostic
	wg.Add(1)
	go func() {
		defer wg.Done()
		ajaxResult = probeAjax(client, ajaxEndpoint)
	}()

	wg.Wait()

	report.Institutions = institutionResults
	report.Ajax = ajaxResult

	// Consolidar estado general y recomendaciones
	computeOverallStatus(report)

	return report, nil
}

func checkVault(cfg DiagnosticConfig, report *DiagnosticReport) {
	diag := VaultDiagnostic{
		Status:    StatusOK,
		Platforms: make(map[model.Platform]PlatformVaultStatus),
	}

	var v *vault.Vault
	var err error

	if cfg.BaseDir != "" {
		diag.Path = cfg.BaseDir
		v, err = vault.NewWithDir(cfg.BaseDir)
	} else {
		home, hErr := os.UserHomeDir()
		if hErr == nil {
			diag.Path = filepath.Join(home, ".config", "atenea")
		}
		v, err = vault.New()
	}

	if err != nil {
		diag.Status = StatusFail
		diag.Initialized = false
		diag.Error = err.Error()
		diag.Recommendations = append(diag.Recommendations,
			fmt.Sprintf("Verificar permisos y accesibilidad del directorio de configuración: %s", diag.Path))
		report.Vault = diag
		return
	}

	diag.Initialized = true
	platforms := []model.Platform{
		model.PlatformSena,
		model.PlatformSima,
		model.PlatformIngles,
	}

	configuredCount := 0
	hasCorruptError := false

	for _, p := range platforms {
		creds, err := v.GetCredentials(p)
		if err == nil && creds.User != "" && creds.Password != "" {
			configuredCount++
			diag.Platforms[p] = PlatformVaultStatus{
				Platform:   p,
				Configured: true,
				Username:   maskUsername(creds.User),
				DocType:    creds.DocType,
			}
		} else {
			diag.Platforms[p] = PlatformVaultStatus{
				Platform:   p,
				Configured: false,
			}
			if err != nil && !errors.Is(err, vault.ErrNotFound) && !errors.Is(err, os.ErrNotExist) {
				hasCorruptError = true
				diag.Error = err.Error()
			}
			diag.Recommendations = append(diag.Recommendations,
				fmt.Sprintf("[%s] Credenciales no configuradas. Ejecuta 'atenea auth %s' para configurarlas.", p, p))
		}
	}

	if hasCorruptError {
		diag.Status = StatusFail
		diag.Recommendations = append(diag.Recommendations,
			"El archivo de bóveda parece estar corrupto o la clave no coincide. Reconfigura credenciales con 'atenea auth <modulo>'.")
	} else if configuredCount == len(platforms) {
		diag.Status = StatusOK
	} else if configuredCount > 0 {
		diag.Status = StatusWarn
	} else {
		diag.Status = StatusWarn
		diag.Recommendations = append(diag.Recommendations,
			"No hay ninguna credencial configurada en la bóveda. Ejecuta 'atenea auth <sena|sima|ingles>'.")
	}

	report.Vault = diag
}

func checkSession(cfg DiagnosticConfig, report *DiagnosticReport) {
	cachePath := cfg.CachePath
	if cachePath == "" {
		if cfg.BaseDir != "" {
			// Check standard locations inside baseDir
			candidate1 := filepath.Join(cfg.BaseDir, "sessions.json")
			candidate2 := filepath.Join(cfg.BaseDir, "cache", "sessions.json")
			if _, err := os.Stat(candidate1); err == nil {
				cachePath = candidate1
			} else if _, err := os.Stat(candidate2); err == nil {
				cachePath = candidate2
			} else {
				cachePath = candidate1
			}
		} else {
			defPath, err := session.DefaultPath()
			if err == nil {
				cachePath = defPath
			} else {
				cachePath = "~/.cache/atenea/sessions.json"
			}
		}
	}

	diag := SessionDiagnostic{
		Status:    StatusOK,
		Path:      cachePath,
		Platforms: make(map[string]PlatformSessionStatus),
	}

	// 1. Check session.NewFileStore() initialization
	store, err := session.NewFileStore(cachePath)
	if err != nil {
		diag.Status = StatusFail
		diag.Error = fmt.Sprintf("Error al inicializar almacén de sesiones: %v", err)
		diag.Recommendations = append(diag.Recommendations,
			fmt.Sprintf("Verificar permisos del directorio de caché: %s", filepath.Dir(cachePath)))
		report.Session = diag
		return
	}
	_ = store

	// 2. Check if cache file exists
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		diag.Exists = false
		diag.Status = StatusWarn
		platforms := []string{"sena", "sima", "ingles"}
		for _, p := range platforms {
			diag.Platforms[p] = PlatformSessionStatus{
				Platform: p,
				Cached:   false,
			}
		}
		diag.Recommendations = append(diag.Recommendations,
			"No se encontró archivo de caché de sesiones. Se generará automáticamente al autenticar o consultar evidencias.")
		report.Session = diag
		return
	} else if err != nil {
		diag.Status = StatusFail
		diag.Error = fmt.Sprintf("Error al verificar archivo de caché: %v", err)
		report.Session = diag
		return
	}

	diag.Exists = true

	// Read and parse sessions
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		diag.Status = StatusFail
		diag.Error = fmt.Sprintf("Error al leer archivo de caché: %v", err)
		report.Session = diag
		return
	}

	if len(raw) == 0 {
		diag.Status = StatusWarn
		platforms := []string{"sena", "sima", "ingles"}
		for _, p := range platforms {
			diag.Platforms[p] = PlatformSessionStatus{
				Platform: p,
				Cached:   false,
			}
		}
		diag.Recommendations = append(diag.Recommendations,
			"El archivo de caché de sesiones está vacío. Se poblará al iniciar sesión.")
		report.Session = diag
		return
	}

	var sessionMap map[string]*session.SessionEntry
	if err := json.Unmarshal(raw, &sessionMap); err != nil {
		var wrapped struct {
			Sessions map[string]*session.SessionEntry `json:"sessions"`
		}
		if err2 := json.Unmarshal(raw, &wrapped); err2 == nil && wrapped.Sessions != nil {
			sessionMap = wrapped.Sessions
		} else {
			diag.Status = StatusFail
			diag.Error = fmt.Sprintf("Archivo de caché corrupto: %v", err)
			diag.Recommendations = append(diag.Recommendations,
				fmt.Sprintf("El archivo de caché está dañado. Elimínalo con: rm %s", cachePath))
			report.Session = diag
			return
		}
	}

	platforms := []string{"sena", "sima", "ingles"}
	activeCount := 0
	expiredCount := 0

	for _, p := range platforms {
		entry, ok := sessionMap[p]
		if !ok || entry == nil {
			diag.Platforms[p] = PlatformSessionStatus{
				Platform: p,
				Cached:   false,
			}
			diag.Recommendations = append(diag.Recommendations,
				fmt.Sprintf("[%s] No hay sesión en caché. Se autenticará en la próxima consulta.", p))
		} else {
			isExpired := entry.IsExpired()
			status := PlatformSessionStatus{
				Platform:     p,
				Cached:       true,
				Active:       !isExpired,
				Expired:      isExpired,
				UserID:       entry.UserID,
				CreatedAt:    entry.CreatedAt,
				ExpiresAt:    entry.ExpiresAt,
				CookiesCount: len(entry.Cookies),
			}
			diag.Platforms[p] = status

			if isExpired {
				expiredCount++
				diag.Recommendations = append(diag.Recommendations,
					fmt.Sprintf("[%s] Sesión en caché expirada. Se renovará automáticamente en la próxima consulta.", p))
			} else {
				activeCount++
			}
		}
	}

	if activeCount == len(platforms) {
		diag.Status = StatusOK
	} else if activeCount > 0 {
		diag.Status = StatusOK
	} else if expiredCount > 0 {
		diag.Status = StatusWarn
	} else {
		diag.Status = StatusWarn
	}

	report.Session = diag
}

func probeInstitution(client *http.Client, target InstitutionTarget) ConnectivityDiagnostic {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	diag := ConnectivityDiagnostic{
		Name: target.Name,
		URL:  target.URL,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err != nil {
		diag.Status = StatusFail
		diag.Error = err.Error()
		diag.Recommendations = append(diag.Recommendations,
			fmt.Sprintf("Error creando solicitud HTTP para %s: %v", target.Name, err))
		return diag
	}

	// Browser headers to avoid institutional WAF / nginx blocks
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9,en;q=0.8")
	req.Header.Set("Sec-Ch-Ua", "\"Chromium\";v=\"130\", \"Google Chrome\";v=\"130\"")
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", "\"Linux\"")

	start := time.Now()
	resp, err := client.Do(req)
	diag.Latency = time.Since(start)

	if err != nil {
		diag.Status = StatusFail
		diag.Error = err.Error()
		diag.Recommendations = append(diag.Recommendations,
			fmt.Sprintf("Fallo de conexión con %s (%s). Verifica tu red o DNS.", target.Name, target.URL))
		return diag
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	diag.StatusCode = resp.StatusCode

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		diag.Status = StatusOK
	} else if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		diag.Status = StatusWarn
		diag.Recommendations = append(diag.Recommendations,
			fmt.Sprintf("%s respondió con código HTTP %d.", target.Name, resp.StatusCode))
	} else {
		diag.Status = StatusFail
		diag.Recommendations = append(diag.Recommendations,
			fmt.Sprintf("%s respondió con error de servidor HTTP %d. La plataforma podría estar en mantenimiento.", target.Name, resp.StatusCode))
	}

	return diag
}

func probeAjax(client *http.Client, ajaxURL string) AjaxDiagnostic {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	diag := AjaxDiagnostic{
		URL: ajaxURL,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ajaxURL, nil)
	if err != nil {
		diag.Status = StatusFail
		diag.Available = false
		diag.Error = err.Error()
		diag.Recommendations = append(diag.Recommendations,
			fmt.Sprintf("Error creando solicitud HTTP para endpoint AJAX: %v", err))
		return diag
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json,text/javascript,*/*;q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	start := time.Now()
	resp, err := client.Do(req)
	diag.Latency = time.Since(start)

	if err != nil {
		diag.Status = StatusFail
		diag.Available = false
		diag.Error = err.Error()
		diag.Recommendations = append(diag.Recommendations,
			fmt.Sprintf("Endpoint AJAX inaccesible (%s): %v", ajaxURL, err))
		return diag
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	diag.StatusCode = resp.StatusCode

	// Moodle service.php without arguments returns HTTP 200 with JSON error message ("codingerror"),
	// or HTTP 400 with JSON, or 303.
	isJSON := strings.Contains(resp.Header.Get("Content-Type"), "json") ||
		strings.HasPrefix(strings.TrimSpace(string(bodyBytes)), "{")

	if resp.StatusCode == http.StatusOK || (resp.StatusCode >= 200 && resp.StatusCode < 400) || (resp.StatusCode == http.StatusBadRequest && isJSON) {
		diag.Available = true
		diag.Status = StatusOK
	} else if resp.StatusCode >= 500 {
		diag.Available = false
		diag.Status = StatusFail
		diag.Recommendations = append(diag.Recommendations,
			fmt.Sprintf("Endpoint AJAX respondió con error de servidor HTTP %d.", resp.StatusCode))
	} else {
		diag.Available = false
		diag.Status = StatusWarn
		diag.Recommendations = append(diag.Recommendations,
			fmt.Sprintf("Endpoint AJAX respondió con código inesperado HTTP %d.", resp.StatusCode))
	}

	return diag
}

func computeOverallStatus(report *DiagnosticReport) {
	hasFail := false
	hasWarn := false

	if report.Vault.Status == StatusFail {
		hasFail = true
	} else if report.Vault.Status == StatusWarn {
		hasWarn = true
	}

	if report.Session.Status == StatusFail {
		hasFail = true
	} else if report.Session.Status == StatusWarn {
		hasWarn = true
	}

	for _, inst := range report.Institutions {
		if inst.Status == StatusFail {
			hasFail = true
		} else if inst.Status == StatusWarn {
			hasWarn = true
		}
	}

	if report.Ajax.Status == StatusFail {
		hasFail = true
	} else if report.Ajax.Status == StatusWarn {
		hasWarn = true
	}

	if hasFail {
		report.OverallStatus = StatusFail
	} else if hasWarn {
		report.OverallStatus = StatusWarn
	} else {
		report.OverallStatus = StatusOK
	}

	// Consolidar recomendaciones deduplicadas
	seen := make(map[string]bool)
	var recs []string

	addRecs := func(list []string) {
		for _, r := range list {
			r = strings.TrimSpace(r)
			if r != "" && !seen[r] {
				seen[r] = true
				recs = append(recs, r)
			}
		}
	}

	addRecs(report.Vault.Recommendations)
	addRecs(report.Session.Recommendations)
	for _, inst := range report.Institutions {
		addRecs(inst.Recommendations)
	}
	addRecs(report.Ajax.Recommendations)

	report.Recommendations = recs
}

// PrintSummary imprime un resumen formateado y limpio en la terminal
func (r *DiagnosticReport) PrintSummary(w io.Writer) {
	fmt.Fprintln(w, strings.Repeat("=", 80))
	fmt.Fprintln(w, "ATENEA DOCTOR - Reporte de Diagnóstico del Sistema")
	fmt.Fprintln(w, strings.Repeat("=", 80))
	fmt.Fprintf(w, "Fecha y hora:   %s\n", r.Timestamp.Format("2006-01-02 15:04:05 -0700"))
	fmt.Fprintf(w, "Estado General: [%s]\n", r.OverallStatus)
	fmt.Fprintln(w, strings.Repeat("=", 80))

	// [1] Bóveda
	fmt.Fprintln(w, "\n[1] ESTADO DE LA BÓVEDA SEGURA (AES-256-GCM)")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	fmt.Fprintf(w, "    Ruta:          %s\n", r.Vault.Path)
	initText := "Sí"
	if !r.Vault.Initialized {
		initText = "No"
	}
	fmt.Fprintf(w, "    Inicializada:  %s\n", initText)
	if r.Vault.Error != "" {
		fmt.Fprintf(w, "    Error:         %s\n", r.Vault.Error)
	}
	fmt.Fprintln(w, "    Credenciales por plataforma:")
	for _, p := range []model.Platform{model.PlatformSena, model.PlatformSima, model.PlatformIngles} {
		pStat, ok := r.Vault.Platforms[p]
		label := platformLabel(string(p))
		if ok && pStat.Configured {
			docInfo := ""
			if pStat.DocType != "" {
				docInfo = fmt.Sprintf("[%s] ", pStat.DocType)
			}
			fmt.Fprintf(w, "      [OK]   %-26s Configurado (%s%s)\n", label+":", docInfo, pStat.Username)
		} else {
			fmt.Fprintf(w, "      [WARN] %-26s No configurado\n", label+":")
		}
	}

	// [2] Caché de Sesiones
	fmt.Fprintln(w, "\n[2] CACHÉ DE SESIONES")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	fmt.Fprintf(w, "    Ruta:          %s\n", r.Session.Path)
	existsText := "Sí"
	if !r.Session.Exists {
		existsText = "No"
	}
	fmt.Fprintf(w, "    Presente:      %s\n", existsText)
	if r.Session.Error != "" {
		fmt.Fprintf(w, "    Error:         %s\n", r.Session.Error)
	}
	fmt.Fprintln(w, "    Sesiones por plataforma:")
	for _, p := range []string{"sena", "sima", "ingles"} {
		sStat, ok := r.Session.Platforms[p]
		label := platformLabel(p)
		if ok && sStat.Cached {
			if sStat.Active {
				expiryInfo := ""
				if !sStat.ExpiresAt.IsZero() {
					remaining := time.Until(sStat.ExpiresAt).Round(time.Minute)
					expiryInfo = fmt.Sprintf(" (expira en %v, %d cookies)", remaining, sStat.CookiesCount)
				}
				fmt.Fprintf(w, "      [OK]   %-26s Sesión activa%s\n", label+":", expiryInfo)
			} else {
				expiredAgo := time.Since(sStat.ExpiresAt).Round(time.Minute)
				fmt.Fprintf(w, "      [WARN] %-26s Sesión expirada (hace %v)\n", label+":", expiredAgo)
			}
		} else {
			fmt.Fprintf(w, "      [WARN] %-26s No hay sesión en caché\n", label+":")
		}
	}

	// [3] Conectividad Institucional
	fmt.Fprintln(w, "\n[3] CONECTIVIDAD INSTITUCIONAL (Timeout: 5s)")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	for _, inst := range r.Institutions {
		latencyStr := formatLatency(inst.Latency)
		if inst.Status == StatusOK {
			fmt.Fprintf(w, "    [OK]   %-24s HTTP %-3d (%s) - %s\n",
				inst.Name+":", inst.StatusCode, latencyStr, inst.URL)
		} else if inst.Status == StatusWarn {
			fmt.Fprintf(w, "    [WARN] %-24s HTTP %-3d (%s) - %s\n",
				inst.Name+":", inst.StatusCode, latencyStr, inst.URL)
		} else {
			errDesc := inst.Error
			if errDesc == "" {
				errDesc = fmt.Sprintf("HTTP %d", inst.StatusCode)
			}
			fmt.Fprintf(w, "    [FAIL] %-24s Error (%s) [%s] - %s\n",
				inst.Name+":", latencyStr, errDesc, inst.URL)
		}
	}

	// [4] Moodle AJAX Endpoint
	fmt.Fprintln(w, "\n[4] ENDPOINT AJAX DE MOODLE")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	ajaxLatency := formatLatency(r.Ajax.Latency)
	if r.Ajax.Status == StatusOK {
		fmt.Fprintf(w, "    [OK]   SIMA AJAX Service:        HTTP %-3d (%s) [Disponible]\n",
			r.Ajax.StatusCode, ajaxLatency)
	} else if r.Ajax.Status == StatusWarn {
		fmt.Fprintf(w, "    [WARN] SIMA AJAX Service:        HTTP %-3d (%s) [Respuesta Inesperada]\n",
			r.Ajax.StatusCode, ajaxLatency)
	} else {
		errDesc := r.Ajax.Error
		if errDesc == "" {
			errDesc = fmt.Sprintf("HTTP %d", r.Ajax.StatusCode)
		}
		fmt.Fprintf(w, "    [FAIL] SIMA AJAX Service:        Error (%s) [%s]\n",
			ajaxLatency, errDesc)
	}
	fmt.Fprintf(w, "           URL: %s\n", r.Ajax.URL)

	// Recomendaciones
	fmt.Fprintln(w, "\n"+strings.Repeat("=", 80))
	fmt.Fprintln(w, "RECOMENDACIONES DE ACCIÓN:")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	if len(r.Recommendations) == 0 {
		fmt.Fprintln(w, "  ✓ Todos los subsistemas se encuentran en óptimo estado operativo.")
	} else {
		for _, rec := range r.Recommendations {
			fmt.Fprintf(w, "  • %s\n", rec)
		}
	}
	fmt.Fprintln(w, strings.Repeat("=", 80))
}

func maskUsername(u string) string {
	u = strings.TrimSpace(u)
	if len(u) <= 4 {
		return strings.Repeat("*", len(u))
	}
	return u[:4] + strings.Repeat("*", len(u)-4)
}

func platformLabel(p string) string {
	switch strings.ToLower(p) {
	case "sena":
		return "SENA Zajuna ADSO"
	case "sima":
		return "UDC SIMA"
	case "ingles":
		return "UDC Inglés"
	default:
		return p
	}
}

func formatLatency(d time.Duration) string {
	ms := d.Milliseconds()
	if ms == 0 && d > 0 {
		return "<1ms"
	}
	return fmt.Sprintf("%dms", ms)
}
