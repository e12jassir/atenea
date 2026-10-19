package watcher

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/sena"
	"github.com/e12jassir/atenea/internal/sima"
)

// Deadline representa una entrega, actividad o parcial con fecha límite.
type Deadline struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Course     string    `json:"course"`
	DueDate    time.Time `json:"due_date"`
	DueDateRaw string    `json:"due_date_raw,omitempty"`
	Platform   string    `json:"platform"`
	Link       string    `json:"link,omitempty"`
}

// Grade representa la calificación asignada a una actividad o asignatura.
type Grade struct {
	ID       string `json:"id"`
	Course   string `json:"course"`
	Name     string `json:"name"`
	Grade    string `json:"grade"`
	Status   string `json:"status,omitempty"`
	Platform string `json:"platform"`
}

// Announcement representa un aviso o comunicado en el tablón oficial.
type Announcement struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Course   string `json:"course,omitempty"`
	Author   string `json:"author,omitempty"`
	Date     string `json:"date,omitempty"`
	Platform string `json:"platform"`
	Link     string `json:"link,omitempty"`
}

// AcademicFetcher define el contrato para consultar evidencias, calificaciones y anuncios académicos.
type AcademicFetcher interface {
	FetchDeadlines(ctx context.Context, platform string) ([]Deadline, error)
	FetchGrades(ctx context.Context, platform string) ([]Grade, error)
	FetchAnnouncements(ctx context.Context, platform string) ([]Announcement, error)
}

// MockFetcher es una implementación en memoria de AcademicFetcher para pruebas unitarias y entornos simulados.
type MockFetcher struct {
	mu            sync.Mutex
	Deadlines     []Deadline
	Grades        []Grade
	Announcements []Announcement
	Err           error
	FetchCount    int
}

// FetchDeadlines retorna las entregas configuradas en el mock, filtrando por plataforma si se especifica.
func (m *MockFetcher) FetchDeadlines(ctx context.Context, platform string) ([]Deadline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FetchCount++
	if m.Err != nil {
		return nil, m.Err
	}
	var res []Deadline
	for _, d := range m.Deadlines {
		if platform == "" || platform == "all" || strings.EqualFold(d.Platform, platform) {
			res = append(res, d)
		}
	}
	return res, nil
}

// FetchGrades retorna las calificaciones configuradas en el mock, filtrando por plataforma si se especifica.
func (m *MockFetcher) FetchGrades(ctx context.Context, platform string) ([]Grade, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	var res []Grade
	for _, g := range m.Grades {
		if platform == "" || platform == "all" || strings.EqualFold(g.Platform, platform) {
			res = append(res, g)
		}
	}
	return res, nil
}

// FetchAnnouncements retorna los anuncios configurados en el mock, filtrando por plataforma si se especifica.
func (m *MockFetcher) FetchAnnouncements(ctx context.Context, platform string) ([]Announcement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	var res []Announcement
	for _, a := range m.Announcements {
		if platform == "" || platform == "all" || strings.EqualFold(a.Platform, platform) {
			res = append(res, a)
		}
	}
	return res, nil
}

// SetDeadlines actualiza la lista de entregas en el mock.
func (m *MockFetcher) SetDeadlines(deadlines []Deadline) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Deadlines = deadlines
}

// SetGrades actualiza la lista de calificaciones en el mock.
func (m *MockFetcher) SetGrades(grades []Grade) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Grades = grades
}

// SetAnnouncements actualiza la lista de anuncios en el mock.
func (m *MockFetcher) SetAnnouncements(announcements []Announcement) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Announcements = announcements
}

// GetFetchCount retorna cuántas veces se ha invocado el fetcher.
func (m *MockFetcher) GetFetchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.FetchCount
}

// DefaultFetcher conecta los clientes de SENA Zajuna y UDC SIMA con el Daemon reactivo.
type DefaultFetcher struct {
	senaClient   *sena.Client
	simaClient   *sima.Client
	inglesClient *sima.Client
}

// NewDefaultFetcher inicializa un DefaultFetcher con los clientes correspondientes.
func NewDefaultFetcher(senaClient *sena.Client, simaClient *sima.Client, inglesClient *sima.Client) *DefaultFetcher {
	return &DefaultFetcher{
		senaClient:   senaClient,
		simaClient:   simaClient,
		inglesClient: inglesClient,
	}
}

// FetchDeadlines consulta las evidencias y actividades pendientes en SENA y SIMA.
func (df *DefaultFetcher) FetchDeadlines(ctx context.Context, platform string) ([]Deadline, error) {
	var deadlines []Deadline

	// 1. SENA Zajuna
	if df.senaClient != nil && (platform == "" || platform == "all" || strings.EqualFold(platform, string(model.PlatformSena))) {
		evs, err := df.senaClient.GetPendingEvidences()
		if err == nil {
			for _, ev := range evs {
				t := ev.Timestamp
				if t.IsZero() && ev.DueDate != "" {
					if pt, pErr := ParseDate(ev.DueDate); pErr == nil {
						t = pt
					}
				}
				deadlines = append(deadlines, Deadline{
					ID:         ev.ID,
					Title:      ev.Title,
					Course:     ev.Course,
					DueDate:    t,
					DueDateRaw: ev.DueDate,
					Platform:   string(model.PlatformSena),
					Link:       ev.Link,
				})
			}
		}
	}

	// 2. UDC SIMA
	if df.simaClient != nil && (platform == "" || platform == "all" || strings.EqualFold(platform, string(model.PlatformSima))) {
		events, err := df.simaClient.GetUpcomingEvents()
		if err == nil {
			for _, ev := range events {
				var t time.Time
				if ev.Timestamp > 0 {
					t = time.Unix(ev.Timestamp, 0)
				} else if ev.DueDate != "" {
					if pt, pErr := ParseDate(ev.DueDate); pErr == nil {
						t = pt
					}
				}
				deadlines = append(deadlines, Deadline{
					ID:         ev.ID,
					Title:      ev.Title,
					Course:     ev.Course,
					DueDate:    t,
					DueDateRaw: ev.DueDate,
					Platform:   string(model.PlatformSima),
					Link:       ev.Link,
				})
			}
		}
	}

	// 3. UDC Inglés
	if df.inglesClient != nil && (platform == "" || platform == "all" || strings.EqualFold(platform, string(model.PlatformIngles))) {
		events, err := df.inglesClient.GetUpcomingEvents()
		if err == nil {
			for _, ev := range events {
				var t time.Time
				if ev.Timestamp > 0 {
					t = time.Unix(ev.Timestamp, 0)
				} else if ev.DueDate != "" {
					if pt, pErr := ParseDate(ev.DueDate); pErr == nil {
						t = pt
					}
				}
				deadlines = append(deadlines, Deadline{
					ID:         ev.ID,
					Title:      ev.Title,
					Course:     ev.Course,
					DueDate:    t,
					DueDateRaw: ev.DueDate,
					Platform:   string(model.PlatformIngles),
					Link:       ev.Link,
				})
			}
		}
	}

	return deadlines, nil
}

// FetchGrades consulta el estado de calificaciones de las plataformas configuradas.
func (df *DefaultFetcher) FetchGrades(ctx context.Context, platform string) ([]Grade, error) {
	var grades []Grade

	// 1. SENA Zajuna
	if df.senaClient != nil && (platform == "" || platform == "all" || strings.EqualFold(platform, string(model.PlatformSena))) {
		rep, err := df.senaClient.GetGradeReport()
		if err == nil && rep != nil {
			for _, ev := range rep.Graded {
				grades = append(grades, Grade{
					ID:       ev.Title,
					Course:   ev.Course,
					Name:     ev.Title,
					Grade:    ev.Grade,
					Status:   ev.Status,
					Platform: string(model.PlatformSena),
				})
			}
		}
	}

	// 2. UDC SIMA
	if df.simaClient != nil && (platform == "" || platform == "all" || strings.EqualFold(platform, string(model.PlatformSima))) {
		cgList, err := df.simaClient.GetGradesOverview()
		if err == nil {
			for _, cg := range cgList {
				grades = append(grades, Grade{
					ID:       cg.ID,
					Course:   cg.Course,
					Name:     cg.Course,
					Grade:    cg.Grade,
					Status:   cg.Status,
					Platform: string(model.PlatformSima),
				})
			}
		}
	}

	return grades, nil
}

// FetchAnnouncements consulta los avisos recientes en SENA y SIMA.
func (df *DefaultFetcher) FetchAnnouncements(ctx context.Context, platform string) ([]Announcement, error) {
	var announcements []Announcement

	// 1. SENA Zajuna
	if df.senaClient != nil && (platform == "" || platform == "all" || strings.EqualFold(platform, string(model.PlatformSena))) {
		list, err := df.senaClient.GetAnnouncements()
		if err == nil {
			for _, a := range list {
				announcements = append(announcements, Announcement{
					ID:       a.ID,
					Title:    a.Title,
					Course:   a.Course,
					Author:   a.Author,
					Date:     a.Date,
					Platform: string(model.PlatformSena),
					Link:     a.Link,
				})
			}
		}
	}

	// 2. UDC SIMA
	if df.simaClient != nil && (platform == "" || platform == "all" || strings.EqualFold(platform, string(model.PlatformSima))) {
		list, err := df.simaClient.GetAnnouncements()
		if err == nil {
			for _, a := range list {
				announcements = append(announcements, Announcement{
					ID:       a.ID,
					Title:    a.Title,
					Course:   a.Course,
					Author:   a.Author,
					Date:     a.Date,
					Platform: string(model.PlatformSima),
					Link:     a.Link,
				})
			}
		}
	}

	return announcements, nil
}

var spanishMonths = map[string]time.Month{
	"enero":      time.January,
	"febrero":    time.February,
	"marzo":      time.March,
	"abril":      time.April,
	"mayo":       time.May,
	"junio":      time.June,
	"julio":      time.July,
	"agosto":     time.August,
	"septiembre": time.September,
	"setiembre":  time.September,
	"octubre":    time.October,
	"noviembre":  time.November,
	"diciembre":  time.December,
}

var spanishDateRegex = regexp.MustCompile(`(?i)(?:[a-záéíóú]+,\s*)?(\d{1,2})\s+de\s+([a-záéíóú]+)(?:\s+de\s+(\d{4}))?(?:,\s*(\d{1,2}):(\d{2}))?`)

// ParseDate interpreta fechas en formatos estándar de Moodle o ISO en español o neutro.
func ParseDate(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("fecha vacía")
	}

	// Formatos ISO estándar
	standardFormats := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"02/01/2006 15:04",
		"02/01/2006",
	}
	for _, f := range standardFormats {
		if t, err := time.Parse(f, trimmed); err == nil {
			return t, nil
		}
	}

	// Formato en español: "15 de Octubre de 2026, 23:59"
	match := spanishDateRegex.FindStringSubmatch(trimmed)
	if len(match) > 2 {
		day, err := strconv.Atoi(match[1])
		if err != nil {
			return time.Time{}, fmt.Errorf("día inválido: %w", err)
		}
		monthName := strings.ToLower(match[2])
		month, ok := spanishMonths[monthName]
		if !ok {
			return time.Time{}, fmt.Errorf("mes desconocido: %s", monthName)
		}

		year := time.Now().Year()
		if len(match) > 3 && match[3] != "" {
			if y, yErr := strconv.Atoi(match[3]); yErr == nil {
				year = y
			}
		}

		hour := 23
		min := 59
		if len(match) > 5 && match[4] != "" && match[5] != "" {
			if h, hErr := strconv.Atoi(match[4]); hErr == nil {
				hour = h
			}
			if m, mErr := strconv.Atoi(match[5]); mErr == nil {
				min = m
			}
		}

		loc := time.Local
		return time.Date(year, month, day, hour, min, 0, 0, loc), nil
	}

	return time.Time{}, fmt.Errorf("no se pudo interpretar la fecha: %s", raw)
}
