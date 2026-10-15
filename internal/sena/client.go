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
	resp.Body.Close()

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

	bodyBytes, _ := io.ReadAll(postResp.Body)
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
	moodleResp.Body.Close()

	if !c.hasMoodleCookie() {
		return errors.New("la sesión Moodle no fue establecida")
	}

	return nil
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
	Graded  []model.Evidence
	Pending []model.Evidence
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

	bodyBytes, _ := io.ReadAll(resp.Body)
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

				if grade == "A" || grade == "D" {
					status := "Aprobado (A)"
					if grade == "D" {
						status = "Deficiente (D)"
					}
					res.Graded = append(res.Graded, model.Evidence{
						Title:    cleanName,
						Course:   "ADSO Ficha 3545922",
						Status:   status,
						Grade:    grade,
						Feedback: feedback,
					})
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

// GetPendingEvidences consulta únicamente las pendientes
func (c *Client) GetPendingEvidences() ([]model.Evidence, error) {
	rep, err := c.GetGradeReport()
	if err != nil {
		return nil, err
	}
	return rep.Pending, nil
}

// GetApprovedGrades consulta las calificaciones aprobadas
func (c *Client) GetApprovedGrades() ([]model.Evidence, error) {
	rep, err := c.GetGradeReport()
	if err != nil {
		return nil, err
	}
	return rep.Graded, nil
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

	bodyBytes, _ := io.ReadAll(resp.Body)
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
		return rep.Pending, nil
	}
	c.setHeaders(req, BaseURL+"/")
	resp, err := c.http.Do(req)
	if err != nil {
		return rep.Pending, nil
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	html := string(bodyBytes)

	reLink := regexp.MustCompile(`href="([^"]*assign/view\.php\?id=(\d+))"[^>]*>(.*?)</a>`)
	tagRegex := regexp.MustCompile(`<[^>]+>`)
	matches := reLink.FindAllStringSubmatch(html, -1)

	type linkInfo struct {
		id   string
		name string
	}
	var links []linkInfo
	for _, m := range matches {
		if len(m) >= 4 {
			cleanName := tagRegex.ReplaceAllString(m[3], "")
			links = append(links, linkInfo{id: m[2], name: strings.TrimSpace(cleanName)})
		}
	}

	// Consultar con worker pool concurrente en Go
	var wg sync.WaitGroup
	results := make([]model.Evidence, len(rep.Pending))
	copy(results, rep.Pending)

	sem := make(chan struct{}, 6) // máx 6 goroutines concurrentes

	for i := range results {
		ev := &results[i]
		var foundID string
		for _, l := range links {
			parts := strings.Split(ev.Title, " ")
			for _, p := range parts {
				if len(p) > 6 && strings.Contains(l.name, p) {
					foundID = l.id
					break
				}
			}
			if foundID != "" {
				break
			}
		}

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

	bodyBytes, _ := io.ReadAll(resp.Body)
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

	bodyBytes, _ := io.ReadAll(resp.Body)
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
