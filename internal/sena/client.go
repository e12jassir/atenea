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

	// Verificar si hay error en la query del URL final
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
	u, _ := url.Parse(BaseURL)
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

// GetPendingEvidences consulta el libro de calificaciones y extrae evidencias pendientes
func (c *Client) GetPendingEvidences() ([]model.Evidence, error) {
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
	var pending []model.Evidence

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

			if strings.HasPrefix(name, "Evidencia") || strings.HasPrefix(name, "Foro") || strings.HasPrefix(name, "Taller") {
				if grade != "A" && grade != "D" {
					pending = append(pending, model.Evidence{
						Title:  name,
						Course: "ADSO Ficha 3545922",
						Status: "Pendiente por Calificar",
						Grade:  grade,
					})
				}
			}
		}
	}

	return pending, nil
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
				ID:      discID,
				Title:   title,
				Link:    link,
				Author:  authorDate,
			})
		}
	}

	return announcements, nil
}
