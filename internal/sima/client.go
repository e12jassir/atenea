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
	u, _ := url.Parse(c.baseURL + "/")
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
		return "PENDIENTE"
	}
	c.setHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return "PENDIENTE"
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
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
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	html := string(bodyBytes)

	chunks := strings.Split(html, `<h3 class="name d-inline-block">`)
	if len(chunks) <= 1 {
		chunks = strings.Split(html, `<h3 class="name`)
	}

	tsRegex := regexp.MustCompile(`data-timestamp=["'](\d+)["'][^>]*>\s*([^<]+)\s*</span>`)
	linkRegex := regexp.MustCompile(`<a href=["']([^"']*mod/[^"']*)["']`)
	courseRegex := regexp.MustCompile(`(?s)title="Curso".*?class="col-11">\s*<a[^>]*>([^<]+)</a>`)

	var events []model.SimaEvent

	for _, cChunk := range chunks[1:] {
		title := "Actividad"
		parts := strings.Split(cChunk, "</h3>")
		if len(parts) > 1 {
			raw := parts[0]
			if idx := strings.LastIndex(raw, ">"); idx != -1 {
				raw = raw[idx+1:]
			}
			raw = strings.TrimSpace(raw)
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
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
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
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
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
	discRegex := regexp.MustCompile(`(?s)href=['"](https://[^'"]*mod/forum/discuss\.php\?d=(\d+))['"][^>]*>(.*?)</a>`)
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
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

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
			fBytes, _ := io.ReadAll(fResp.Body)
			fResp.Body.Close()

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
