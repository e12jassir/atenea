package sena

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/session"
)

const (
	BaseURL          = "https://zajuna.sena.edu.co"
	LoginPostURL     = BaseURL + "/controllers/login_user/lg.php"
	MoodleBase       = BaseURL + "/zajuna"
	AnnouncementsURL = MoodleBase + "/mod/forum/view.php?id=7955069"
	DefaultCourseID  = "125540"
)

type Client struct {
	http   *http.Client
	jar    *cookiejar.Jar
	course string
	store  session.SessionStore
}

func NewClient() (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: 20 * time.Second,
	}

	return &Client{
		http:   client,
		jar:    jar,
		course: DefaultCourseID,
	}, nil
}

func (c *Client) SetSessionStore(store session.SessionStore) {
	c.store = store
}

func (c *Client) SessionStore() session.SessionStore {
	return c.store
}

// HTTPClient expone el cliente HTTP interno configurado con el cookiejar de la sesión.
func (c *Client) HTTPClient() *http.Client {
	return c.http
}

func (c *Client) Login(docType, username, password string) error {
	// Paso 1: GET base para iniciar cookies
	req, err := http.NewRequest("GET", BaseURL, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req, BaseURL+"/")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("error al conectar con Zajuna: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error al conectar con Zajuna: HTTP %d", resp.StatusCode)
	}

	// Paso 2: Autenticación en SSO Gateway (lg.php)
	data := url.Values{}
	data.Set("typeDocument", docType)
	data.Set("document", username)
	data.Set("password", password)
	data.Set("form_login_user", "Iniciar sesión")

	postReq, err := http.NewRequest("POST", LoginPostURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	c.setHeaders(postReq, BaseURL+"/")
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	postResp, err := c.http.Do(postReq)
	if err != nil {
		return fmt.Errorf("error enviando credenciales a Zajuna: %w", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusOK {
		return fmt.Errorf("error enviando credenciales a Zajuna: HTTP %d", postResp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(postResp.Body)
	if err != nil {
		return fmt.Errorf("error leyendo respuesta de Zajuna: %w", err)
	}
	body := string(bodyBytes)

	if strings.Contains(postResp.Request.URL.RawQuery, "error=") {
		return errors.New("documento, tipo o contraseña incorrectos en Sofía Plus")
	}

	// Paso 3: Extraer campos del formulario auto-submit hacia Moodle
	inputRegex := regexp.MustCompile(`<input[^>]*name=['"]([^'"]+)['"][^>]*value=['"]([^'"]*)['"]`)
	matches := inputRegex.FindAllStringSubmatch(body, -1)
	formData := url.Values{}
	for _, m := range matches {
		if len(m) == 3 {
			formData.Set(m[1], m[2])
		}
	}

	if len(formData) == 0 && !strings.Contains(body, "loginForm") && !c.hasMoodleCookie() {
		return errors.New("no se pudo completar el enlace SSO hacia Moodle")
	}

	actionRegex := regexp.MustCompile(`id=['"]loginForm['"][^>]*action=['"]([^'"]+)['"]`)
	actionMatch := actionRegex.FindStringSubmatch(body)
	action := "/zajuna/login/index.php"
	if len(actionMatch) > 1 {
		action = actionMatch[1]
	}

	actionURL := action
	if !strings.HasPrefix(action, "http") {
		actionURL = BaseURL + action
	}

	moodleReq, err := http.NewRequest("POST", actionURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return err
	}
	c.setHeaders(moodleReq, BaseURL+"/")
	moodleReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	moodleResp, err := c.http.Do(moodleReq)
	if err != nil {
		return fmt.Errorf("error completando sesión en Moodle: %w", err)
	}
	defer moodleResp.Body.Close()

	if moodleResp.StatusCode != http.StatusOK {
		return fmt.Errorf("error completando sesión en Moodle: HTTP %d", moodleResp.StatusCode)
	}

	if !c.hasMoodleCookie() {
		return errors.New("la sesión Moodle no fue establecida")
	}

	if c.store != nil {
		cookies := c.getCookiesForSession()
		if err := c.store.Save(&session.SessionEntry{
			Platform:  "sena",
			Cookies:   cookies,
			ExpiresAt: time.Now().Add(2 * time.Hour),
		}); err != nil {
			return fmt.Errorf("error guardando sesión en store: %w", err)
		}
	}

	return nil
}

// RestoreSession loads a cached session for the "sena" platform from store.
// If valid and not expired, it injects the cookies into c.jar for
// https://zajuna.sena.edu.co and https://zajuna.sena.edu.co/zajuna/.
// Returns true, nil if the session was restored.
func (c *Client) RestoreSession(store session.SessionStore) (bool, error) {
	if store == nil {
		return false, nil
	}

	c.store = store

	entry, err := store.Load("sena")
	if err != nil {
		if errors.Is(err, session.ErrNotFound) || errors.Is(err, session.ErrExpired) {
			return false, nil
		}
		return false, err
	}

	if entry == nil || !entry.IsValid() || len(entry.Cookies) == 0 {
		return false, nil
	}

	var validCookies []*http.Cookie
	for _, ck := range entry.Cookies {
		if ck != nil {
			validCookies = append(validCookies, ck)
		}
	}
	if len(validCookies) == 0 {
		return false, nil
	}

	if c.jar == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return false, fmt.Errorf("error al inicializar cookiejar: %w", err)
		}
		c.jar = jar
		if c.http != nil {
			c.http.Jar = jar
		}
	}

	uRoot, err := url.Parse(BaseURL)
	if err != nil {
		return false, fmt.Errorf("URL base inválida: %w", err)
	}
	uZajuna, err := url.Parse(MoodleBase + "/")
	if err != nil {
		return false, fmt.Errorf("URL de Zajuna inválida: %w", err)
	}

	c.jar.SetCookies(uRoot, validCookies)
	c.jar.SetCookies(uZajuna, validCookies)

	return true, nil
}

// EnsureAuthenticated ensures the client has an active authenticated session.
// It first attempts to restore a cached session from store.
// If restored, it validates the session by performing a quick check to https://zajuna.sena.edu.co/zajuna/my/
// verifying it does not redirect to /login/.
// If not restored or session check fails, it calls Login and saves the new session to store.
func (c *Client) EnsureAuthenticated(store session.SessionStore, docType, user, pass string) error {
	c.store = store

	restored, err := c.RestoreSession(store)
	if err == nil && restored {
		if c.ValidateSession() {
			return nil
		}
	}

	if err := c.Login(docType, user, pass); err != nil {
		return err
	}

	if store != nil {
		cookies := c.getCookiesForSession()
		if err := store.Save(&session.SessionEntry{
			Platform:  "sena",
			Cookies:   cookies,
			ExpiresAt: time.Now().Add(2 * time.Hour),
		}); err != nil {
			return fmt.Errorf("error guardando sesión en store: %w", err)
		}
	}

	return nil
}

// ValidateSession performs a quick check to https://zajuna.sena.edu.co/zajuna/my/
// checking that it does NOT redirect to /login/.
// Returns true if the session is active and valid.
func (c *Client) ValidateSession() bool {
	if c.http == nil {
		return false
	}

	checkURL := MoodleBase + "/my/"
	req, err := http.NewRequest("GET", checkURL, nil)
	if err != nil {
		return false
	}
	c.setHeaders(req, BaseURL+"/")

	// Copy client to intercept redirects to /login/ without modifying global client
	checkClient := *c.http
	checkClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if strings.Contains(req.URL.Path, "/login/") || strings.Contains(req.URL.String(), "/login/") {
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}

	resp, err := checkClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// If redirect response with Location pointing to login
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		if strings.Contains(loc, "/login/") {
			return false
		}
	}

	// If final URL points to login
	if resp.Request != nil && resp.Request.URL != nil {
		if strings.Contains(resp.Request.URL.Path, "/login/") || strings.Contains(resp.Request.URL.String(), "/login/") {
			return false
		}
	}

	return resp.StatusCode == http.StatusOK
}

func (c *Client) getCookiesForSession() []*http.Cookie {
	if c.jar == nil {
		return nil
	}
	uRoot, _ := url.Parse(BaseURL)
	uZajuna, _ := url.Parse(MoodleBase + "/")
	uLogin, _ := url.Parse(LoginPostURL)

	var cookies []*http.Cookie
	seen := make(map[string]bool)

	for _, u := range []*url.URL{uZajuna, uRoot, uLogin} {
		if u == nil {
			continue
		}
		for _, ck := range c.jar.Cookies(u) {
			if ck == nil {
				continue
			}
			if !seen[ck.Name] {
				seen[ck.Name] = true
				ckCopy := *ck
				cookies = append(cookies, &ckCopy)
			}
		}
	}
	return cookies
}

func (c *Client) hasMoodleCookie() bool {
	u, _ := url.Parse(MoodleBase + "/")
	for _, cookie := range c.jar.Cookies(u) {
		if strings.HasPrefix(cookie.Name, "MoodleSession") {
			return true
		}
	}
	return false
}

func (c *Client) setHeaders(req *http.Request, referer string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.Header.Set("Origin", BaseURL)
}

type GradeReportResult struct {
	Approved  []model.Evidence
	Deficient []model.Evidence
	Graded    []model.Evidence
	Pending   []model.Evidence
}

// GetGradeReport consulta el libro de calificaciones y extrae aprobadas, no aprobadas y pendientes
func (c *Client) GetGradeReport() (*GradeReportResult, error) {
	reportURL := fmt.Sprintf("%s/grade/report/user/index.php?id=%s", MoodleBase, c.course)
	req, err := http.NewRequest("GET", reportURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, BaseURL+"/")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error consultando libro de calificaciones: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo libro de calificaciones: %w", err)
	}
	html := string(bodyBytes)

	trRegex := regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	tdRegex := regexp.MustCompile(`(?s)<t[dh][^>]*>(.*?)</t[dh]>`)
	tagRegex := regexp.MustCompile(`<[^>]+>`)
	spaceRegex := regexp.MustCompile(`\s+`)

	rows := trRegex.FindAllStringSubmatch(html, -1)
	res := &GradeReportResult{}

	for _, r := range rows {
		if len(r) < 2 {
			continue
		}
		cells := tdRegex.FindAllStringSubmatch(r[1], -1)
		var cleaned []string
		for _, cell := range cells {
			if len(cell) > 1 {
				t := tagRegex.ReplaceAllString(cell[1], "")
				t = spaceRegex.ReplaceAllString(t, " ")
				cleaned = append(cleaned, strings.TrimSpace(t))
			}
		}

		if len(cleaned) >= 2 {
			name := cleaned[0]
			grade := cleaned[1]
			feedback := ""
			if len(cleaned) > 2 {
				feedback = strings.TrimSpace(strings.ReplaceAll(cleaned[2], "&nbsp;", ""))
			}

			if strings.HasPrefix(name, "Evidencia") || strings.HasPrefix(name, "Foro") || strings.HasPrefix(name, "Taller") {
				cleanName := name
				if strings.HasPrefix(name, "Evidencia") && !strings.HasPrefix(name, "Evidencia: ") {
					cleanName = "Evidencia: " + strings.TrimSpace(strings.TrimPrefix(name, "Evidencia"))
				} else if strings.HasPrefix(name, "Foro") && !strings.HasPrefix(name, "Foro: ") {
					cleanName = "Foro: " + strings.TrimSpace(strings.TrimPrefix(name, "Foro"))
				} else if strings.HasPrefix(name, "Taller") && !strings.HasPrefix(name, "Taller: ") {
					cleanName = "Taller: " + strings.TrimSpace(strings.TrimPrefix(name, "Taller"))
				}

				if grade == "A" {
					ev := model.Evidence{
						Title:    cleanName,
						Course:   "ADSO Ficha 3545922",
						Status:   "Aprobado (A)",
						Grade:    "A",
						Feedback: feedback,
					}
					res.Approved = append(res.Approved, ev)
					res.Graded = append(res.Graded, ev)
				} else if grade == "D" {
					ev := model.Evidence{
						Title:    cleanName,
						Course:   "ADSO Ficha 3545922",
						Status:   "Deficiente (D)",
						Grade:    "D",
						Feedback: feedback,
					}
					res.Deficient = append(res.Deficient, ev)
					res.Graded = append(res.Graded, ev)
				} else {
					res.Pending = append(res.Pending, model.Evidence{
						Title:    cleanName,
						Course:   "ADSO Ficha 3545922",
						Status:   "Pendiente",
						Grade:    "-",
						Feedback: feedback,
					})
				}
			}
		}
	}

	return res, nil
}

// GetPendingEvidences consulta las evidencias que requieren atención:
// primero las deficientes (D) que requieren reentrega urgente, seguidas de las pendientes por calificar.
func (c *Client) GetPendingEvidences() ([]model.Evidence, error) {
	rep, err := c.GetGradeReport()
	if err != nil {
		return nil, err
	}
	var allPending []model.Evidence
	allPending = append(allPending, rep.Deficient...)
	allPending = append(allPending, rep.Pending...)
	return allPending, nil
}

// GetApprovedGrades consulta únicamente las calificaciones aprobadas con juicio A
func (c *Client) GetApprovedGrades() ([]model.Evidence, error) {
	rep, err := c.GetGradeReport()
	if err != nil {
		return nil, err
	}
	return rep.Approved, nil
}

// GetDeficientGrades consulta las calificaciones con juicio Deficiente (D) que requieren reentrega urgente
func (c *Client) GetDeficientGrades() ([]model.Evidence, error) {
	rep, err := c.GetGradeReport()
	if err != nil {
		return nil, err
	}
	return rep.Deficient, nil
}

// GetFeedback consulta las evidencias que tienen retroalimentación del instructor
func (c *Client) GetFeedback() ([]model.Evidence, error) {
	rep, err := c.GetGradeReport()
	if err != nil {
		return nil, err
	}
	var withFb []model.Evidence
	for _, g := range rep.Graded {
		if g.Feedback != "" {
			withFb = append(withFb, g)
		}
	}
	return withFb, nil
}

// CheckAssignSubmission consulta el estado real de un envío en Moodle
func (c *Client) CheckAssignSubmission(assignID string) string {
	url := fmt.Sprintf("%s/mod/assign/view.php?id=%s", MoodleBase, assignID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "DESCONOCIDO"
	}
	c.setHeaders(req, BaseURL+"/")

	resp, err := c.http.Do(req)
	if err != nil {
		return "DESCONOCIDO"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "DESCONOCIDO"
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "DESCONOCIDO"
	}
	html := string(bodyBytes)

	if strings.Contains(html, "Enviado para calificar") {
		return "ENVIADO"
	} else if strings.Contains(html, "Todavía no se han realizado envíos") || strings.Contains(html, "No se han realizado envíos") {
		return "NO ENVIADO"
	} else if strings.Contains(html, "Borrador") {
		return "BORRADOR"
	}
	return "PENDIENTE"
}

var evidenceCodeRegex = regexp.MustCompile(`(?i)\b(GA\d+-[A-Za-z0-9-]+-EV\d+)\b`)

type linkInfo struct {
	id   string
	name string
}

func extractEvidenceCode(s string) string {
	m := evidenceCodeRegex.FindStringSubmatch(s)
	if len(m) > 1 {
		return strings.ToUpper(m[1])
	}
	return ""
}

func normalizeForMatching(s string) string {
	s = strings.ToLower(s)
	genericWords := []string{
		"evidencia:", "evidencia",
		"informe:", "informe",
		"taller:", "taller",
		"proyecto:", "proyecto",
		"foro:", "foro",
		"cuestionario:", "cuestionario",
		"actividad:", "actividad",
	}
	for _, w := range genericWords {
		s = strings.ReplaceAll(s, w, " ")
	}

	var runes []rune
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			runes = append(runes, r)
		} else if len(runes) > 0 && runes[len(runes)-1] != ' ' {
			runes = append(runes, ' ')
		}
	}
	return strings.TrimSpace(string(runes))
}

func matchEvidenceLink(title string, links []linkInfo) string {
	evCode := extractEvidenceCode(title)
	if evCode != "" {
		for _, l := range links {
			linkCode := extractEvidenceCode(l.name)
			if linkCode != "" && linkCode == evCode {
				return l.id
			}
			if strings.Contains(strings.ToUpper(l.name), evCode) {
				return l.id
			}
		}
	}

	normEv := normalizeForMatching(title)
	if len(normEv) >= 6 {
		for _, l := range links {
			normLink := normalizeForMatching(l.name)
			if len(normLink) >= 6 {
				if normEv == normLink {
					return l.id
				}
				if len(normEv) >= 12 && strings.Contains(normLink, normEv) {
					return l.id
				}
				if len(normLink) >= 12 && strings.Contains(normEv, normLink) {
					return l.id
				}
			}
		}
	}

	return ""
}

// AuditPendingEvidences audita concurrentemente con goroutines el estado de entrega en vivo
func (c *Client) AuditPendingEvidences() ([]model.Evidence, error) {
	rep, err := c.GetGradeReport()
	if err != nil {
		return nil, err
	}

	// Obtener enlaces de assign/view.php del libro de notas
	reportURL := fmt.Sprintf("%s/grade/report/user/index.php?id=%s", MoodleBase, c.course)
	req, err := http.NewRequest("GET", reportURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, BaseURL+"/")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error obteniendo libro de calificaciones para auditoria: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo libro de calificaciones para auditoria: %w", err)
	}
	html := string(bodyBytes)

	reLink := regexp.MustCompile(`href="([^"]*assign/view\.php\?id=(\d+))"[^>]*>(.*?)</a>`)
	tagRegex := regexp.MustCompile(`<[^>]+>`)
	matches := reLink.FindAllStringSubmatch(html, -1)

	var links []linkInfo
	for _, m := range matches {
		if len(m) >= 4 {
			cleanName := tagRegex.ReplaceAllString(m[3], "")
			links = append(links, linkInfo{id: m[2], name: strings.TrimSpace(cleanName)})
		}
	}

	// Consultar con worker pool concurrente en Go
	var pendingList []model.Evidence
	pendingList = append(pendingList, rep.Deficient...)
	pendingList = append(pendingList, rep.Pending...)

	var wg sync.WaitGroup
	results := make([]model.Evidence, len(pendingList))
	copy(results, pendingList)

	sem := make(chan struct{}, 6) // máx 6 goroutines concurrentes

	for i := range results {
		ev := &results[i]
		foundID := matchEvidenceLink(ev.Title, links)

		if foundID != "" {
			wg.Add(1)
			go func(idx int, aid string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				status := c.CheckAssignSubmission(aid)
				results[idx].Status = status
			}(i, foundID)
		}
	}

	wg.Wait()
	return results, nil
}

// GetAnnouncements consulta el tablón de anuncios de Zajuna
func (c *Client) GetAnnouncements() ([]model.Announcement, error) {
	req, err := http.NewRequest("GET", AnnouncementsURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, BaseURL+"/")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error consultando anuncios de Zajuna: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo anuncios de Zajuna: %w", err)
	}
	html := string(bodyBytes)

	trRegex := regexp.MustCompile(`(?s)<tr[^>]*class=['"]discussion[^'"]*['"][^>]*>(.*?)</tr>`)
	linkRegex := regexp.MustCompile(`(?s)href=['"](https://zajuna\.sena\.edu\.co/zajuna/mod/forum/discuss\.php\?d=(\d+))['"][^>]*>(.*?)</a>`)
	authorRegex := regexp.MustCompile(`(?s)class=['"]author[^'"]*['"][^>]*>(.*?)</td>`)
	tagRegex := regexp.MustCompile(`<[^>]+>`)
	spaceRegex := regexp.MustCompile(`\s+`)

	rows := trRegex.FindAllStringSubmatch(html, -1)
	var announcements []model.Announcement

	for _, r := range rows {
		if len(r) < 2 {
			continue
		}
		linkMatch := linkRegex.FindStringSubmatch(r[1])
		authorMatch := authorRegex.FindStringSubmatch(r[1])

		if len(linkMatch) >= 4 {
			title := tagRegex.ReplaceAllString(linkMatch[3], "")
			title = strings.TrimSpace(spaceRegex.ReplaceAllString(title, " "))
			link := linkMatch[1]
			discID := linkMatch[2]

			authorDate := ""
			if len(authorMatch) >= 2 {
				authorDate = tagRegex.ReplaceAllString(authorMatch[1], "")
				authorDate = strings.TrimSpace(spaceRegex.ReplaceAllString(authorDate, " "))
			}

			announcements = append(announcements, model.Announcement{
				ID:     discID,
				Title:  title,
				Link:   link,
				Author: authorDate,
			})
		}
	}

	return announcements, nil
}

// GetAnnouncementDetail obtiene el texto completo de un comunicado
func (c *Client) GetAnnouncementDetail(discussID string) (string, error) {
	url := fmt.Sprintf("%s/mod/forum/discuss.php?d=%s", MoodleBase, discussID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	c.setHeaders(req, BaseURL+"/")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("error consultando detalle de anuncio: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error leyendo detalle de anuncio: %w", err)
	}
	html := string(bodyBytes)

	rePost := regexp.MustCompile(`(?s)class=['"]post-content-container[^'"]*['"][^>]*>(.*?)</div>\s*</div>\s*</div>`)
	m := rePost.FindStringSubmatch(html)
	if len(m) < 2 {
		reAlt := regexp.MustCompile(`(?s)class=['"]forumpost[^'"]*['"][^>]*>(.*?)</div>\s*</div>`)
		m = reAlt.FindStringSubmatch(html)
	}

	if len(m) >= 2 {
		raw := m[1]
		reStyle := regexp.MustCompile(`(?s)<style[^>]*>.*?</style>`)
		clean := reStyle.ReplaceAllString(raw, "")
		clean = regexp.MustCompile(`<br\s*/?>`).ReplaceAllString(clean, "\n")
		clean = regexp.MustCompile(`</(p|div|tr|h\d|li)>`).ReplaceAllString(clean, "\n")
		clean = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(clean, " ")
		lines := strings.Split(clean, "\n")
		var trimmed []string
		for _, l := range lines {
			t := strings.TrimSpace(l)
			if t != "" {
				trimmed = append(trimmed, t)
			}
		}
		return strings.Join(trimmed, "\n"), nil
	}

	return "No se pudo extraer el cuerpo del anuncio.", nil
}
