package watcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	TitleDeadline     = "Atenea: Plazo Próximo"
	TitleGradeUpdate  = "Atenea: Nueva Calificación"
	TitleAnnouncement = "Atenea: Nuevo Anuncio"

	DefaultPollInterval      = 5 * time.Minute
	DefaultDeadlineThreshold = 24 * time.Hour
)

// WatcherConfig define los parámetros operativos del daemon de monitoreo.
type WatcherConfig struct {
	PollInterval        time.Duration
	Platform            string
	NotifyDeadlines     bool
	NotifyGrades        bool
	NotifyAnnouncements bool

	// DeadlineThreshold define el umbral temporal para considerar una entrega como inminente.
	// Si es <= 0, se aplica el valor por defecto de 24 horas.
	DeadlineThreshold time.Duration

	// NotifyOnInitialPoll indica si deben emitirse notificaciones en el primer ciclo de escaneo.
	// Por defecto es false (crea la instantánea base de estado para comparar deltas posteriores).
	NotifyOnInitialPoll bool
}

// Daemon orquesta el monitoreo en segundo plano y el disparo de notificaciones reactivas.
type Daemon struct {
	cfg      WatcherConfig
	notifier Notifier
	fetcher  AcademicFetcher

	// Control de ciclo de vida
	mu      sync.Mutex
	running bool
	stopCh  chan struct{}

	// Rastreadores de estado para cálculo de deltas
	notifiedDeadlines map[string]bool
	knownGrades       map[string]string
	seenAnnouncements map[string]bool
	firstPollDone     bool

	// Inyección de reloj para pruebas deterministas
	nowFunc func() time.Time
}

// NewDaemon instancia un nuevo Daemon con la configuración, notificador y fetcher provistos.
func NewDaemon(cfg WatcherConfig, notifier Notifier, fetcher AcademicFetcher) *Daemon {
	if notifier == nil {
		notifier = NewLinuxNotifier()
	}
	if fetcher == nil {
		fetcher = &MockFetcher{}
	}

	return &Daemon{
		cfg:               cfg,
		notifier:          notifier,
		fetcher:           fetcher,
		notifiedDeadlines: make(map[string]bool),
		knownGrades:       make(map[string]string),
		seenAnnouncements: make(map[string]bool),
		nowFunc:           time.Now,
	}
}

// SetNowFunc permite sustituir la función que retorna el tiempo actual (ideal para pruebas).
func (d *Daemon) SetNowFunc(fn func() time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nowFunc = fn
}

func (d *Daemon) now() time.Time {
	if d.nowFunc != nil {
		return d.nowFunc()
	}
	return time.Now()
}

// IsRunning indica si el ciclo de escaneo del daemon está activo.
func (d *Daemon) IsRunning() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

// ResetState reinicia los mapas de estado y caché de eventos observados.
func (d *Daemon) ResetState() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.notifiedDeadlines = make(map[string]bool)
	d.knownGrades = make(map[string]string)
	d.seenAnnouncements = make(map[string]bool)
	d.firstPollDone = false
}

// Start arranca el ciclo periódico de monitoreo y reacciona a cancelaciones del contexto o llamadas a Stop().
func (d *Daemon) Start(ctx context.Context) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return errors.New("el daemon ya se encuentra en ejecución")
	}
	d.running = true
	d.stopCh = make(chan struct{})
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
	}()

	// Ejecución del escaneo inicial inmediato
	_ = d.Check(ctx)

	interval := d.cfg.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.stopCh:
			return nil
		case <-ticker.C:
			_ = d.Check(ctx)
		}
	}
}

// Stop detiene de forma limpia el daemon si se encuentra en ejecución.
func (d *Daemon) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return
	}

	select {
	case <-d.stopCh:
	default:
		close(d.stopCh)
	}
	d.running = false
}

// Check ejecuta un ciclo de inspección puntual evaluando entregas inminentes, notas actualizadas y anuncios nuevos.
func (d *Daemon) Check(ctx context.Context) error {
	var pendingNotifications []NotificationRecord

	// 1. Verificación de Entregas Inminentes (< 24h)
	if d.cfg.NotifyDeadlines && d.fetcher != nil {
		deadlines, err := d.fetcher.FetchDeadlines(ctx, d.cfg.Platform)
		if err == nil {
			d.mu.Lock()
			now := d.now()
			threshold := d.cfg.DeadlineThreshold
			if threshold <= 0 {
				threshold = DefaultDeadlineThreshold
			}

			for _, dl := range deadlines {
				if dl.DueDate.IsZero() {
					continue
				}
				key := deadlineKey(dl)
				timeUntil := dl.DueDate.Sub(now)

				// Plazo inminente: vence dentro del umbral (< 24h) y aún no ha vencido
				if timeUntil > 0 && timeUntil <= threshold {
					if !d.notifiedDeadlines[key] {
						d.notifiedDeadlines[key] = true
						pendingNotifications = append(pendingNotifications, NotificationRecord{
							Title:   TitleDeadline,
							Message: formatDeadlineMessage(dl, timeUntil),
							Urgency: UrgencyCritical,
						})
					}
				}
			}
			d.mu.Unlock()
		}
	}

	// 2. Verificación de Calificaciones (Delta Event)
	if d.cfg.NotifyGrades && d.fetcher != nil {
		grades, err := d.fetcher.FetchGrades(ctx, d.cfg.Platform)
		if err == nil {
			d.mu.Lock()
			if !d.firstPollDone && !d.cfg.NotifyOnInitialPoll {
				// Instantánea base inicial: guardar notas actuales sin disparar notificaciones masivas
				for _, g := range grades {
					d.knownGrades[gradeKey(g)] = g.Grade
				}
			} else {
				for _, g := range grades {
					key := gradeKey(g)
					prevGrade, exists := d.knownGrades[key]
					if !exists {
						d.knownGrades[key] = g.Grade
						if isValidGrade(g.Grade) {
							pendingNotifications = append(pendingNotifications, NotificationRecord{
								Title:   TitleGradeUpdate,
								Message: formatGradeMessage(g, ""),
								Urgency: UrgencyNormal,
							})
						}
					} else if prevGrade != g.Grade && isValidGrade(g.Grade) {
						// Cambio detectado en la calificación (evento delta)
						d.knownGrades[key] = g.Grade
						pendingNotifications = append(pendingNotifications, NotificationRecord{
							Title:   TitleGradeUpdate,
							Message: formatGradeMessage(g, prevGrade),
							Urgency: UrgencyNormal,
						})
					}
				}
			}
			d.mu.Unlock()
		}
	}

	// 3. Verificación de Nuevos Anuncios
	if d.cfg.NotifyAnnouncements && d.fetcher != nil {
		announcements, err := d.fetcher.FetchAnnouncements(ctx, d.cfg.Platform)
		if err == nil {
			d.mu.Lock()
			if !d.firstPollDone && !d.cfg.NotifyOnInitialPoll {
				// Instantánea base inicial: marcar anuncios existentes como conocidos
				for _, a := range announcements {
					d.seenAnnouncements[announcementKey(a)] = true
				}
			} else {
				for _, a := range announcements {
					key := announcementKey(a)
					if !d.seenAnnouncements[key] {
						d.seenAnnouncements[key] = true
						pendingNotifications = append(pendingNotifications, NotificationRecord{
							Title:   TitleAnnouncement,
							Message: formatAnnouncementMessage(a),
							Urgency: UrgencyNormal,
						})
					}
				}
			}
			d.mu.Unlock()
		}
	}

	d.mu.Lock()
	d.firstPollDone = true
	d.mu.Unlock()

	// Despacho de notificaciones acumuladas
	if d.notifier != nil {
		for _, item := range pendingNotifications {
			_ = d.notifier.Notify(item.Title, item.Message, item.Urgency)
		}
	}

	return nil
}

func deadlineKey(dl Deadline) string {
	if dl.ID != "" {
		return fmt.Sprintf("%s:%s", dl.Platform, dl.ID)
	}
	return fmt.Sprintf("%s:%s:%s", dl.Platform, dl.Course, dl.Title)
}

func gradeKey(g Grade) string {
	if g.ID != "" {
		return fmt.Sprintf("%s:%s", g.Platform, g.ID)
	}
	return fmt.Sprintf("%s:%s:%s", g.Platform, g.Course, g.Name)
}

func announcementKey(a Announcement) string {
	if a.ID != "" {
		return fmt.Sprintf("%s:%s", a.Platform, a.ID)
	}
	return fmt.Sprintf("%s:%s:%s", a.Platform, a.Course, a.Title)
}

func isValidGrade(g string) bool {
	g = fmt.Sprint(g)
	return g != "" && g != "-" && g != "Pendiente" && g != "CONSULTANDO..."
}

func formatDeadlineMessage(dl Deadline, remaining time.Duration) string {
	durStr := formatRemainingDuration(remaining)
	if dl.Course != "" {
		return fmt.Sprintf("%s: '%s' vence en %s", dl.Course, dl.Title, durStr)
	}
	return fmt.Sprintf("'%s' vence en %s", dl.Title, durStr)
}

func formatGradeMessage(g Grade, prev string) string {
	course := g.Course
	if course == "" {
		course = "General"
	}
	if prev != "" && isValidGrade(prev) {
		return fmt.Sprintf("%s: %s actualizada a %s (anterior: %s)", course, g.Name, g.Grade, prev)
	}
	return fmt.Sprintf("%s: %s calificada con %s", course, g.Name, g.Grade)
}

func formatAnnouncementMessage(a Announcement) string {
	course := a.Course
	if course == "" {
		course = "General"
	}
	if a.Author != "" {
		return fmt.Sprintf("[%s] %s (por %s)", course, a.Title, a.Author)
	}
	return fmt.Sprintf("[%s] %s", course, a.Title)
}

func formatRemainingDuration(d time.Duration) string {
	if d <= 0 {
		return "menos de 1 minuto"
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if hours > 0 {
		if mins > 0 {
			return fmt.Sprintf("%dh %dm", hours, mins)
		}
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", mins)
}
