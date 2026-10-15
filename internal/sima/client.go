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
	"time"

	"github.com/e12jassir/atenea/internal/model"
)

type Client struct {
	http    *http.Client
	jar     *cookiejar.Jar
	baseURL string
}

func NewClient(baseURL string) (*Client, error) {
	if baseURL == "" {
		baseURL = "https://sima.unicartagena.edu.co"
	}

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

func (c *Client) Login(username, password string) error {
	loginURL := c.baseURL + "/login/index.php"

	// Paso 1: Obtener token de login
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

	bodyBytes, _ := io.ReadAll(resp.Body)
	html := string(bodyBytes)

	tokenRegex := regexp.MustCompile(`name=["']logintoken["']\s+value=["']([^"']+)["']`)
	tokenMatch := tokenRegex.FindStringSubmatch(html)
	if len(tokenMatch) < 2 {
		return errors.New("no se pudo encontrar el token de inicio de sesión")
	}
	token := tokenMatch[1]

	// Paso 2: Enviar credenciales
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

	resBytes, _ := io.ReadAll(postResp.Body)
	resHTML := string(resBytes)

	if strings.Contains(resHTML, "loginerrormessage") || strings.Contains(resHTML, "Datos erróneos") {
		return errors.New("usuario o contraseña incorrectos en SIMA")
	}

	if !c.hasMoodleCookie() {
		return errors.New("no se obtuvo cookie de sesión activa")
	}

	return nil
}

func (c *Client) hasMoodleCookie() bool {
	u, _ := url.Parse(c.baseURL)
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

// GetUpcomingEvents consulta las actividades próximas en el calendario
func (c *Client) GetUpcomingEvents() ([]model.SimaEvent, error) {
	calendarURL := c.baseURL + "/calendar/view.php?view=upcoming"
	req, err := http.NewRequest("GET", calendarURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	html := string(bodyBytes)

	chunks := strings.Split(html, `<h3 class="name d-inline-block">`)
	if len(chunks) <= 1 {
		chunks = strings.Split(html, `<h3 class="name`)
	}

	titleRegex := regexp.MustCompile(`>([^<]+)</h3>`)
	tsRegex := regexp.MustCompile(`data-timestamp=["'](\d+)["'][^>]*>\s*([^<]+)\s*</span>`)
	linkRegex := regexp.MustCompile(`<a href=["']([^"']*mod/[^"']*)["']`)
	courseRegex := regexp.MustCompile(`calendar/view\.php\?view=day[^>]*>([^<]+)</a>`)

	var events []model.SimaEvent

	for _, c := range chunks[1:] {
		titleMatch := titleRegex.FindStringSubmatch(c)
		tsMatch := tsRegex.FindStringSubmatch(c)
		linkMatch := linkRegex.FindStringSubmatch(c)
		courseMatch := courseRegex.FindStringSubmatch(c)

		title := "Actividad"
		if len(titleMatch) > 1 {
			title = strings.TrimSpace(titleMatch[1])
		}

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

		course := "Asignatura UDC"
		if len(courseMatch) > 1 {
			course = strings.TrimSpace(courseMatch[1])
		}

		status := "PENDIENTE"
		if strings.Contains(c, "realizado") || strings.Contains(c, "enviado") {
			status = "YA ENVIADO"
		}

		events = append(events, model.SimaEvent{
			Title:     title,
			Course:    course,
			DueDate:   dueDate,
			Timestamp: ts,
			Link:      link,
			Status:    status,
		})
	}

	return events, nil
}
