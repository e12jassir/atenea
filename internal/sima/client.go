package sima

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/session"
)

type Client struct {
	http     *http.Client
	jar      *cookiejar.Jar
	baseURL  string
	sesskey  string
	store    session.SessionStore
	platform string
}

func NewClient(baseURL string) (*Client, error) {
	if baseURL == "" {
		baseURL = "https://sima.unicartagena.edu.co"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: 20 * time.Second,
	}

	return &Client{
		http:    client,
		jar:     jar,
		baseURL: baseURL,
	}, nil
}

// SetSessionStore configura el SessionStore y la plataforma por defecto para el cliente.
func (c *Client) SetSessionStore(store session.SessionStore, platform string) {
	c.store = store
	c.platform = platform
}

// SessKey retorna la clave de sesión Moodle actual si fue extraída.
func (c *Client) SessKey() string {
	return c.sesskey
}

// SetSessKey asigna manualmente la clave de sesión Moodle.
func (c *Client) SetSessKey(k string) {
	c.sesskey = k
}

// BaseURL retorna la URL base del cliente.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// HTTPClient expone el cliente HTTP interno.
func (c *Client) HTTPClient() *http.Client {
	return c.http
}

// CookieJar expone el cookie jar interno.
func (c *Client) CookieJar() *cookiejar.Jar {
	return c.jar
}

func (c *Client) detectPlatform() string {
	if strings.Contains(c.baseURL, "ingles") {
		return string(model.PlatformIngles)
	}
	return string(model.PlatformSima)
}

func (c *Client) extractSesskey(html string) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`["']?sesskey["']?\s*[:=]\s*["']?([a-zA-Z0-9_\-]+)["']?`),
		regexp.MustCompile(`[?&]sesskey=([a-zA-Z0-9_\-]+)`),
		regexp.MustCompile(`name=["']sesskey["']\s+value=["']([^"']+)["']`),
		regexp.MustCompile(`value=["']([^"']+)["']\s+name=["']sesskey["']`),
	}
	for _, p := range patterns {
		if m := p.FindStringSubmatch(html); len(m) > 1 {
			c.sesskey = m[1]
			return
		}
	}
}

// RestoreSession restaura una sesión activa desde SessionStore e inyecta las cookies en el Jar.
func (c *Client) RestoreSession(store session.SessionStore, platform string) (bool, error) {
	if store == nil {
		return false, errors.New("el almacén de sesiones no puede ser nil")
	}

	if platform == "" {
		platform = c.detectPlatform()
	}

	entry, err := store.Load(platform)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) || errors.Is(err, session.ErrExpired) {
			return false, nil
		}
		return false, fmt.Errorf("error al cargar sesión desde almacén: %w", err)
	}

	if entry == nil || !entry.IsValid() {
		return false, nil
	}

	u, err := url.Parse(c.baseURL + "/")
	if err != nil {
		return false, fmt.Errorf("error al analizar baseURL: %w", err)
	}

	if len(entry.Cookies) == 0 {
		return false, nil
	}

	cookiesToSet := make([]*http.Cookie, 0, len(entry.Cookies))
	for _, ck := range entry.Cookies {
		if ck == nil {
			continue
		}
		cCopy := *ck
		if cCopy.Domain != "" && cCopy.Domain != u.Hostname() && !strings.HasSuffix(u.Hostname(), "."+strings.TrimPrefix(cCopy.Domain, ".")) {
			cCopy.Domain = u.Hostname()
		}
		if cCopy.Path == "" {
			cCopy.Path = "/"
		}
		cookiesToSet = append(cookiesToSet, &cCopy)
	}

	c.jar.SetCookies(u, cookiesToSet)
	if entry.SessKey != "" {
		c.sesskey = entry.SessKey
	}
	c.store = store
	c.platform = platform

	return true, nil
}

// SaveSession extrae las cookies activas de c.baseURL y las persiste en SessionStore.
func (c *Client) SaveSession(store session.SessionStore, platform string) error {
	if store == nil {
		return errors.New("el almacén de sesiones no puede ser nil")
	}

	if platform == "" {
		platform = c.detectPlatform()
	}

	u, err := url.Parse(c.baseURL + "/")
	if err != nil {
		return fmt.Errorf("error al analizar baseURL: %w", err)
	}

	cookies := c.jar.Cookies(u)
	if len(cookies) == 0 {
		return errors.New("no hay cookies de sesión para guardar")
	}

	entry := &session.SessionEntry{
		Platform:  platform,
		Cookies:   cookies,
		SessKey:   c.sesskey,
		ExpiresAt: time.Now().Add(4 * time.Hour),
	}

	return store.Save(entry)
}

func (c *Client) probeSession() bool {
	if !c.hasMoodleCookie() {
		return false
	}

	endpoints := []string{"/my/", "/user/profile.php"}
	for _, ep := range endpoints {
		probeURL := c.baseURL + ep
		req, err := http.NewRequest("GET", probeURL, nil)
		if err != nil {
			continue
		}
		c.setHeaders(req)

		resp, err := c.http.Do(req)
		if err != nil {
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			continue
		}

		if resp.Request != nil && resp.Request.URL != nil {
			if strings.Contains(resp.Request.URL.Path, "/login/") {
				continue
			}
		}

		body := string(bodyBytes)
		if strings.Contains(body, `name="logintoken"`) || strings.Contains(body, `id="login"`) || strings.Contains(body, "loginerrormessage") {
			continue
		}

		if c.sesskey == "" {
			c.extractSesskey(body)
		}

		return true
	}

	return false
}

// EnsureAuthenticated intenta reutilizar la sesión en caché; si es inválida o expiró, autentica de nuevo y persiste la sesión.
func (c *Client) EnsureAuthenticated(store session.SessionStore, platform string, user, pass string) error {
	if platform == "" {
		platform = c.detectPlatform()
	}
	c.store = store
	c.platform = platform

	if store != nil {
		restored, err := c.RestoreSession(store, platform)
		if err == nil && restored {
			if c.probeSession() {
				return nil
			}
		}
	}

	// Reiniciar cookie jar para que cookies obsoletas no interfieran con un nuevo inicio de sesión
	if freshJar, err := cookiejar.New(nil); err == nil {
		c.jar = freshJar
		c.http.Jar = freshJar
	}

	if err := c.Login(user, pass); err != nil {
		return err
	}

	if store != nil {
		if err := c.SaveSession(store, platform); err != nil {
			return fmt.Errorf("error al guardar sesión en store: %w", err)
		}
	}

	return nil
}

func (c *Client) Login(username, password string) error {
	loginURL := c.baseURL + "/login/index.php"

	req, err := http.NewRequest("GET", loginURL, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("error al conectar con SIMA: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error al conectar con SIMA: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error al leer página de inicio de sesión: %w", err)
	}
	html := string(bodyBytes)

	tokenRegex := regexp.MustCompile(`name=["']logintoken["']\s+value=["']([^"']+)["']`)
	tokenMatch := tokenRegex.FindStringSubmatch(html)
	if len(tokenMatch) < 2 {
		return errors.New("no se pudo encontrar el token de inicio de sesión")
	}
	token := tokenMatch[1]

	data := url.Values{}
	data.Set("username", username)
	data.Set("password", password)
	data.Set("logintoken", token)
	data.Set("anchor", "")

	postReq, err := http.NewRequest("POST", loginURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	c.setHeaders(postReq)
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("Referer", loginURL)

	postResp, err := c.http.Do(postReq)
	if err != nil {
		return fmt.Errorf("error al enviar credenciales: %w", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusOK {
		return fmt.Errorf("error al autenticar en SIMA: HTTP %d", postResp.StatusCode)
	}

	resBytes, err := io.ReadAll(postResp.Body)
	if err != nil {
		return fmt.Errorf("error al leer respuesta de autenticación: %w", err)
	}
	resHTML := string(resBytes)

	if strings.Contains(resHTML, "loginerrormessage") || strings.Contains(resHTML, "Datos erróneos") {
		return errors.New("usuario o contraseña incorrectos en SIMA")
	}

	if !c.hasMoodleCookie() {
		return errors.New("no se obtuvo cookie de sesión activa")
	}

	c.extractSesskey(resHTML)

	if c.store != nil {
		platform := c.platform
		if platform == "" {
			platform = c.detectPlatform()
		}
		_ = c.SaveSession(c.store, platform)
	}

	return nil
}

func (c *Client) hasMoodleCookie() bool {
	u, err := url.Parse(c.baseURL + "/")
	if err != nil {
		return false
	}
	for _, cookie := range c.jar.Cookies(u) {
		if strings.HasPrefix(cookie.Name, "MoodleSession") {
			return true
		}
	}
	return false
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
}

// CheckActivityStatus consulta el estado real en vivo de una tarea o cuestionario
func (c *Client) CheckActivityStatus(activityURL string) string {
	if activityURL == "" {
		return "PENDIENTE"
	}
	req, err := http.NewRequest("GET", activityURL, nil)
	if err != nil {
		return "ERROR (URL inválida)"
	}
	c.setHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return "ERROR (Conexión fallida)"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("ERROR (HTTP %d)", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "ERROR (Lectura fallida)"
	}
	html := string(bodyBytes)

	if strings.Contains(activityURL, "mod/quiz") {
		if strings.Contains(html, "Finalizado") || strings.Contains(html, "Su calificación final") {
			re := regexp.MustCompile(`Su calificación final en este cuestionario es\s*([0-9.,]+/[0-9.,]+)`)
			m := re.FindStringSubmatch(html)
			if len(m) > 1 {
				return fmt.Sprintf("REALIZADO (Nota: %s)", m[1])
			}
			return "REALIZADO"
		}
		return "PENDIENTE POR PRESENTAR"
	} else if strings.Contains(activityURL, "mod/assign") {
		if strings.Contains(html, "Enviado para calificar") {
			return "ENVIADO (Esperando nota)"
		} else if strings.Contains(html, "Todavía no se han realizado envíos") || strings.Contains(html, "No se han realizado envíos") {
			return "PENDIENTE POR ENVIAR"
		} else if strings.Contains(html, "Borrador") {
			return "BORRADOR"
		}
	}
	return "PENDIENTE"
}

// GetUpcomingEvents consulta las actividades próximas en el calendario con estado en vivo
func (c *Client) GetUpcomingEvents() ([]model.SimaEvent, error) {
	calendarURL := c.baseURL + "/calendar/view.php?view=upcoming"
	req, err := http.NewRequest("GET", calendarURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error al conectar con calendario de SIMA: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error al consultar calendario de SIMA: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error al leer calendario de SIMA: %w", err)
	}
	html := string(bodyBytes)

	chunks := strings.Split(html, `<h3 class="name d-inline-block">`)
	if len(chunks) <= 1 {
		chunks = strings.Split(html, `<h3 class="name`)
	}

	tagRegex := regexp.MustCompile(`<[^>]+>`)
	spaceRegex := regexp.MustCompile(`\s+`)
	tsRegex := regexp.MustCompile(`data-timestamp=["'](\d+)["'][^>]*>\s*([^<]+)\s*</span>`)
	linkRegex := regexp.MustCompile(`<a href=["']([^"']*mod/[^"']*)["']`)
	courseRegex := regexp.MustCompile(`(?s)title="Curso".*?class="col-11">\s*<a[^>]*>([^<]+)</a>`)

	var events []model.SimaEvent

	for _, cChunk := range chunks[1:] {
		title := "Actividad"
		parts := strings.Split(cChunk, "</h3>")
		if len(parts) > 1 {
			raw := tagRegex.ReplaceAllString(parts[0], "")
			raw = strings.TrimSpace(spaceRegex.ReplaceAllString(raw, " "))
			if raw != "" {
				title = raw
			}
		}

		tsMatch := tsRegex.FindStringSubmatch(cChunk)
		linkMatch := linkRegex.FindStringSubmatch(cChunk)
		courseMatch := courseRegex.FindStringSubmatch(cChunk)

		dueDate := "Sin fecha límite"
		var ts int64
		if len(tsMatch) > 2 {
			dueDate = strings.TrimSpace(tsMatch[2])
			ts, _ = strconv.ParseInt(tsMatch[1], 10, 64)
		}

		link := ""
		if len(linkMatch) > 1 {
			link = linkMatch[1]
		}

		course := "General"
		if len(courseMatch) > 1 {
			raw := strings.TrimSpace(courseMatch[1])
			parts := strings.Split(raw, " - ")
			if len(parts) >= 3 {
				course = strings.TrimSpace(parts[2])
			} else {
				course = raw
			}
		}

		events = append(events, model.SimaEvent{
			Title:     title,
			Course:    course,
			DueDate:   dueDate,
			Timestamp: ts,
			Link:      link,
			Status:    "CONSULTANDO...",
		})
	}

	// Consultar estado en vivo de las actividades concurrentemente
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	for i := range events {
		ev := &events[i]
		if ev.Link != "" {
			wg.Add(1)
			go func(idx int, targetURL string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				events[idx].Status = c.CheckActivityStatus(targetURL)
			}(i, ev.Link)
		}
	}
	wg.Wait()

	return events, nil
}

// GetGradesOverview obtiene el reporte de notas acumuladas por asignatura
func (c *Client) GetGradesOverview() ([]model.CourseGrade, error) {
	url := c.baseURL + "/grade/report/overview/index.php"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error al conectar con reporte de notas en SIMA: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error al consultar reporte de notas en SIMA: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error al leer reporte de notas en SIMA: %w", err)
	}
	html := string(bodyBytes)

	re := regexp.MustCompile(`(?s)<tr[^>]*>\s*<td[^>]*><a[^>]+href="[^"]*id=(\d+)[^"]*"[^>]*>([^<]+)</a></td>\s*<td[^>]*>([^<]*)</td>`)
	matches := re.FindAllStringSubmatch(html, -1)
	var grades []model.CourseGrade
	for _, m := range matches {
		if len(m) >= 4 {
			rawName := strings.TrimSpace(m[2])
			parts := strings.Split(rawName, " - ")
			cname := rawName
			if len(parts) >= 3 {
				cname = strings.TrimSpace(parts[2])
			}
			val := strings.TrimSpace(m[3])
			if val == "" {
				val = "-"
			}
			grades = append(grades, model.CourseGrade{
				ID:     m[1],
				Course: cname,
				Grade:  val,
				Status: "Semestre Actual",
			})
		}
	}
	return grades, nil
}

// GetCourseGradeBreakdown obtiene el desglose de notas por unidad, PCC y exámenes de una materia
func (c *Client) GetCourseGradeBreakdown(courseID string) ([]model.GradeItem, error) {
	url := fmt.Sprintf("%s/grade/report/user/index.php?id=%s", c.baseURL, courseID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error al conectar con desglose de notas en SIMA: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error al consultar desglose de notas en SIMA: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error al leer desglose de notas en SIMA: %w", err)
	}
	html := string(bodyBytes)

	trRegex := regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	tdRegex := regexp.MustCompile(`(?s)<t[dh][^>]*>(.*?)</t[dh]>`)
	tagRegex := regexp.MustCompile(`<[^>]+>`)
	spaceRegex := regexp.MustCompile(`\s+`)
	cleanPrefix := regexp.MustCompile(`^(Tarea|Cuestionario|Foro|Contenido interactivo|Cálculo total)`)

	rows := trRegex.FindAllStringSubmatch(html, -1)
	var items []model.GradeItem

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

		if len(cleaned) >= 3 {
			name := cleaned[0]
			weight := cleaned[1]
			grade := cleaned[2]
			gradeRange := ""
			if len(cleaned) > 3 {
				gradeRange = strings.ReplaceAll(cleaned[3], "&ndash;", "-")
			}

			// Filtrar filas de encabezados o de calendario colateral
			if name == "Ítem de calificación" || strings.Contains(name, "Sin eventos") || strings.Contains(name, "Lunes") ||
				strings.Contains(name, "evento") || strings.Contains(name, "&nbsp;") || strings.Contains(weight, "evento") ||
				strings.Contains(grade, "evento") || name == "&nbsp;" {
				continue
			}

			cleanName := cleanPrefix.ReplaceAllString(name, "")
			cleanName = strings.ReplaceAll(cleanName, "&amp;", "&")
			cleanName = strings.ReplaceAll(cleanName, "&#039;", "'")
			cleanName = strings.ReplaceAll(cleanName, "&quot;", "\"")
			cleanName = strings.TrimSpace(cleanName)
			if cleanName == "" || cleanName == "&nbsp;" {
				continue
			}

			if idx := strings.Index(grade, " Acciones"); idx != -1 {
				grade = strings.TrimSpace(grade[:idx])
			}
			if grade == "" {
				grade = "-"
			}

			category := "Evidencia"
			if strings.Contains(cleanName, "Total") {
				category = "Acumulado"
			} else if strings.Contains(cleanName, "Evaluación") || strings.Contains(cleanName, "Cuestionario") {
				category = "Examen"
			} else if strings.Contains(cleanName, "Trabajo colaborativo") || strings.Contains(cleanName, "Protocolo colaborativo") {
				category = "PCC"
			} else if strings.Contains(cleanName, "Protocolo individual") {
				category = "Protocolo"
			}

			items = append(items, model.GradeItem{
				Name:     cleanName,
				Weight:   weight,
				Grade:    grade,
				Range:    gradeRange,
				Category: category,
			})
		}
	}

	return items, nil
}

// GetAnnouncements consulta los anuncios y foros de las asignaturas
func (c *Client) GetAnnouncements() ([]model.Announcement, error) {
	courses, err := c.GetGradesOverview()
	if err != nil {
		return nil, err
	}

	var allAnnouncements []model.Announcement
	discRegex := regexp.MustCompile(`(?s)href=['"](https?://[^'"]*mod/forum/discuss\.php\?d=(\d+))['"][^>]*>(.*?)</a>`)
	authorRegex := regexp.MustCompile(`(?s)class=['"]author[^'"]*['"][^>]*>(.*?)</td>`)
	tagRegex := regexp.MustCompile(`<[^>]+>`)
	spaceRegex := regexp.MustCompile(`\s+`)

	for _, cr := range courses {
		courseURL := fmt.Sprintf("%s/course/view.php?id=%s", c.baseURL, cr.ID)
		req, err := http.NewRequest("GET", courseURL, nil)
		if err != nil {
			continue
		}
		c.setHeaders(req)
		resp, err := c.http.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		reForum := regexp.MustCompile(`href=['"]([^'"]*mod/forum/view\.php\?id=\d+)['"]`)
		forumMatches := reForum.FindAllStringSubmatch(string(bodyBytes), -1)

		for _, fm := range forumMatches {
			if len(fm) < 2 {
				continue
			}
			fReq, err := http.NewRequest("GET", fm[1], nil)
			if err != nil {
				continue
			}
			c.setHeaders(fReq)
			fResp, err := c.http.Do(fReq)
			if err != nil {
				continue
			}
			if fResp.StatusCode != http.StatusOK {
				fResp.Body.Close()
				continue
			}
			fBytes, err := io.ReadAll(fResp.Body)
			fResp.Body.Close()
			if err != nil {
				continue
			}

			fHTML := string(fBytes)
			discMatches := discRegex.FindAllStringSubmatch(fHTML, -1)
			authorMatches := authorRegex.FindAllStringSubmatch(fHTML, -1)

			for i, dm := range discMatches {
				if len(dm) >= 4 {
					title := tagRegex.ReplaceAllString(dm[3], "")
					title = strings.TrimSpace(spaceRegex.ReplaceAllString(title, " "))
					link := dm[1]
					discID := dm[2]

					authorDate := "Docente"
					if i < len(authorMatches) && len(authorMatches[i]) >= 2 {
						authorDate = tagRegex.ReplaceAllString(authorMatches[i][1], "")
						authorDate = strings.TrimSpace(spaceRegex.ReplaceAllString(authorDate, " "))
					}

					allAnnouncements = append(allAnnouncements, model.Announcement{
						ID:     discID,
						Course: cr.Course,
						Title:  title,
						Author: authorDate,
						Link:   link,
					})
				}
			}
		}
	}

	return allAnnouncements, nil
}

// GetAnnouncementDetail obtiene el texto completo de un comunicado en SIMA
func (c *Client) GetAnnouncementDetail(discussID string) (string, error) {
	url := fmt.Sprintf("%s/mod/forum/discuss.php?d=%s", c.baseURL, discussID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("error al conectar con detalle de comunicado: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("error al consultar detalle de comunicado en SIMA: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error al leer detalle de comunicado: %w", err)
	}
	html := string(bodyBytes)

	rePost := regexp.MustCompile(`(?s)class=['"]post-content-container[^'"]*['"][^>]*>(.*?)</div>\s*</div>\s*</div>`)
	m := rePost.FindStringSubmatch(html)
	if len(m) < 2 {
		reAlt := regexp.MustCompile(`(?s)class=['"]forumpost[^'"]*['"][^>]*>(.*?)</div>\s*</div>`)
		m = reAlt.FindStringSubmatch(html)
	}
	if len(m) < 2 {
		reSingle := regexp.MustCompile(`(?s)class=['"](?:post-content-container|forumpost)[^'"]*['"][^>]*>(.*?)</div>`)
		m = reSingle.FindStringSubmatch(html)
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
