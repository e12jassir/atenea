package watcher

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLinuxNotifier_MockExec(t *testing.T) {
	notifier := NewLinuxNotifier()

	var capturedName string
	var capturedArgs []string

	notifier.ExecCmd = func(name string, arg ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = arg
		// Simula comando exitoso ejecutando "true"
		return exec.Command("true")
	}

	title := "Atenea: Plazo Próximo"
	msg := "Evidencia GA1-220501092-AA1 vence en 2 horas"
	urgency := UrgencyCritical

	err := notifier.Notify(title, msg, urgency)
	if err != nil {
		t.Fatalf("se esperaba nil en Notify(), se obtuvo error: %v", err)
	}

	if capturedName != "notify-send" {
		t.Errorf("nombre de comando esperado 'notify-send', obtenido '%s'", capturedName)
	}

	expectedArgs := []string{"-a", "Atenea", "-u", "critical", "-i", "dialog-information", title, msg}
	if len(capturedArgs) != len(expectedArgs) {
		t.Fatalf("longitud de argumentos esperada %d, obtenida %d (%v)", len(expectedArgs), len(capturedArgs), capturedArgs)
	}

	for i, arg := range expectedArgs {
		if capturedArgs[i] != arg {
			t.Errorf("argumento en índice %d esperado '%s', obtenido '%s'", i, arg, capturedArgs[i])
		}
	}
}

func TestLinuxNotifier_GracefulFallback(t *testing.T) {
	var fallbackBuf bytes.Buffer

	notifier := NewLinuxNotifier()
	notifier.FallbackWriter = &fallbackBuf
	notifier.ExecCmd = func(name string, arg ...string) *exec.Cmd {
		// Simula que notify-send no existe o falla en ejecución
		return exec.Command("comando-que-no-existe-en-el-sistema-12345")
	}

	err := notifier.Notify("Atenea: Test Fallback", "Mensaje de degradación", UrgencyNormal)
	if err != nil {
		t.Fatalf("la degradación elegante no debe retornar error fatal, se obtuvo: %v", err)
	}

	out := fallbackBuf.String()
	if !strings.Contains(out, "[NOTIFICACIÓN FALLBACK]") {
		t.Errorf("se esperaba encabezado de fallback en el log, obtenido: %s", out)
	}
	if !strings.Contains(out, "Atenea: Test Fallback") {
		t.Errorf("se esperaba el título en el log de fallback, obtenido: %s", out)
	}
	if !strings.Contains(out, "Mensaje de degradación") {
		t.Errorf("se esperaba el mensaje en el log de fallback, obtenido: %s", out)
	}
}

func TestLinuxNotifier_DefaultValues(t *testing.T) {
	var capturedArgs []string

	notifier := &LinuxNotifier{
		ExecCmd: func(name string, arg ...string) *exec.Cmd {
			capturedArgs = arg
			return exec.Command("true")
		},
	}

	// Notificar con urgencia vacía para validar fallback a "normal" y nombres por defecto
	err := notifier.Notify("Título", "Mensaje", "")
	if err != nil {
		t.Fatalf("Notify() falló: %v", err)
	}

	// Validar que -u vaya seguido de "normal"
	urgencyIdx := -1
	for i, arg := range capturedArgs {
		if arg == "-u" && i+1 < len(capturedArgs) {
			urgencyIdx = i + 1
			break
		}
	}

	if urgencyIdx == -1 || capturedArgs[urgencyIdx] != UrgencyNormal {
		t.Errorf("se esperaba urgencia por defecto '%s', se obtuvo '%v'", UrgencyNormal, capturedArgs)
	}
}

func TestDaemon_Lifecycle_StartAndStop(t *testing.T) {
	cfg := WatcherConfig{
		PollInterval: 10 * time.Millisecond,
	}
	mockNotifier := &MockNotifier{}
	mockFetcher := &MockFetcher{}

	daemon := NewDaemon(cfg, mockNotifier, mockFetcher)

	ctx := context.Background()
	errCh := make(chan error, 1)

	go func() {
		errCh <- daemon.Start(ctx)
	}()

	// Esperar a que el daemon esté corriendo
	deadline := time.Now().Add(500 * time.Millisecond)
	for !daemon.IsRunning() {
		if time.Now().After(deadline) {
			t.Fatal("el daemon no inició dentro del tiempo límite esperado")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Detener daemon
	daemon.Stop()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() debía retornar nil al llamar Stop(), se obtuvo: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("daemon.Stop() no detuvo la ejecución de Start()")
	}

	if daemon.IsRunning() {
		t.Errorf("el daemon no debería reportarse como en ejecución tras Stop()")
	}
}

func TestDaemon_Lifecycle_CancelContext(t *testing.T) {
	cfg := WatcherConfig{
		PollInterval: 10 * time.Millisecond,
	}
	mockNotifier := &MockNotifier{}
	mockFetcher := &MockFetcher{}

	daemon := NewDaemon(cfg, mockNotifier, mockFetcher)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- daemon.Start(ctx)
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for !daemon.IsRunning() {
		if time.Now().After(deadline) {
			t.Fatal("el daemon no inició dentro del tiempo límite esperado")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Cancelar contexto
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start() debía retornar context.Canceled, se obtuvo: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("la cancelación del contexto no detuvo la ejecución de Start()")
	}

	if daemon.IsRunning() {
		t.Errorf("el daemon no debería reportarse como en ejecución tras cancelación de contexto")
	}
}

func TestDaemon_Lifecycle_Ticker(t *testing.T) {
	cfg := WatcherConfig{
		PollInterval:    15 * time.Millisecond,
		NotifyDeadlines: true,
	}
	mockNotifier := &MockNotifier{}
	mockFetcher := &MockFetcher{}

	daemon := NewDaemon(cfg, mockNotifier, mockFetcher)

	ctx := context.Background()
	go func() {
		_ = daemon.Start(ctx)
	}()

	// Dejar correr suficiente para múltiples ticks (>= 3 ticks)
	time.Sleep(70 * time.Millisecond)
	daemon.Stop()

	count := mockFetcher.GetFetchCount()
	if count < 2 {
		t.Errorf("se esperaban al menos 2 llamadas periódicas por ticker, se obtuvieron: %d", count)
	}
}

func TestDaemon_Trigger_ApproachingDeadline(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	cfg := WatcherConfig{
		NotifyDeadlines:   true,
		DeadlineThreshold: 24 * time.Hour,
	}
	mockNotifier := &MockNotifier{}
	mockFetcher := &MockFetcher{
		Deadlines: []Deadline{
			{
				ID:       "dl-1",
				Title:    "Taller de Algoritmos",
				Course:   "ADSO 3545922",
				DueDate:  now.Add(3 * time.Hour), // Plazo inminente (< 24h)
				Platform: "sena",
			},
			{
				ID:       "dl-2",
				Title:    "Parcial Final",
				Course:   "Ingeniería de Software",
				DueDate:  now.Add(48 * time.Hour), // Lejana (> 24h)
				Platform: "sima",
			},
			{
				ID:       "dl-3",
				Title:    "Actividad Vencida",
				Course:   "Inglés Institucional",
				DueDate:  now.Add(-2 * time.Hour), // Vencida
				Platform: "ingles",
			},
		},
	}

	daemon := NewDaemon(cfg, mockNotifier, mockFetcher)
	daemon.SetNowFunc(func() time.Time { return now })

	err := daemon.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() retornó error inesperado: %v", err)
	}

	records := mockNotifier.Records()
	if len(records) != 1 {
		t.Fatalf("se esperaba exactamente 1 notificación de entrega inminente, se obtuvieron: %d (%+v)", len(records), records)
	}

	rec := records[0]
	if rec.Title != TitleDeadline {
		t.Errorf("título esperado '%s', obtenido '%s'", TitleDeadline, rec.Title)
	}
	if rec.Urgency != UrgencyCritical {
		t.Errorf("urgencia esperada '%s', obtenida '%s'", UrgencyCritical, rec.Urgency)
	}
	if !strings.Contains(rec.Message, "Taller de Algoritmos") {
		t.Errorf("el mensaje debía mencionar la actividad inminente, obtenido: %s", rec.Message)
	}
}

func TestDaemon_Trigger_DeadlineDeduplication(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	cfg := WatcherConfig{
		NotifyDeadlines: true,
	}
	mockNotifier := &MockNotifier{}
	mockFetcher := &MockFetcher{
		Deadlines: []Deadline{
			{
				ID:       "dl-1",
				Title:    "Evidencia Crítica",
				Course:   "ADSO",
				DueDate:  now.Add(2 * time.Hour),
				Platform: "sena",
			},
		},
	}

	daemon := NewDaemon(cfg, mockNotifier, mockFetcher)
	daemon.SetNowFunc(func() time.Time { return now })

	// Primera comprobación -> debe notificar
	_ = daemon.Check(context.Background())
	if len(mockNotifier.Records()) != 1 {
		t.Fatalf("primera comprobación debía notificar, recibidos: %d", len(mockNotifier.Records()))
	}

	// Segunda comprobación con la misma entrega -> no debe duplicar
	_ = daemon.Check(context.Background())
	if len(mockNotifier.Records()) != 1 {
		t.Errorf("no debía emitir notificaciones duplicadas para la misma entrega, total recibidas: %d", len(mockNotifier.Records()))
	}
}

func TestDaemon_Trigger_GradeUpdate_DeltaEvent(t *testing.T) {
	cfg := WatcherConfig{
		NotifyGrades: true,
	}
	mockNotifier := &MockNotifier{}
	mockFetcher := &MockFetcher{
		Grades: []Grade{
			{
				ID:       "g-1",
				Course:   "ADSO",
				Name:     "Evidencia 1",
				Grade:    "Pendiente",
				Platform: "sena",
			},
		},
	}

	daemon := NewDaemon(cfg, mockNotifier, mockFetcher)

	// Ciclo 1: Instantánea base inicial (no genera spam de notas preexistentes)
	_ = daemon.Check(context.Background())
	if len(mockNotifier.Records()) != 0 {
		t.Fatalf("el escaneo inicial no debía emitir notificaciones de notas preexistentes, recibidas: %d", len(mockNotifier.Records()))
	}

	// Ciclo 2: Se actualiza la calificación (evento delta)
	mockFetcher.SetGrades([]Grade{
		{
			ID:       "g-1",
			Course:   "ADSO",
			Name:     "Evidencia 1",
			Grade:    "A",
			Platform: "sena",
		},
	})

	_ = daemon.Check(context.Background())
	records := mockNotifier.Records()
	if len(records) != 1 {
		t.Fatalf("se esperaba 1 notificación de actualización de calificación, obtenidas: %d", len(records))
	}

	rec := records[0]
	if rec.Title != TitleGradeUpdate {
		t.Errorf("título esperado '%s', obtenido '%s'", TitleGradeUpdate, rec.Title)
	}
	if rec.Urgency != UrgencyNormal {
		t.Errorf("urgencia esperada '%s', obtenida '%s'", UrgencyNormal, rec.Urgency)
	}
	if !strings.Contains(rec.Message, "Evidencia 1") || !strings.Contains(rec.Message, "A") {
		t.Errorf("mensaje de nota actualizado incompleto: %s", rec.Message)
	}
}

func TestDaemon_Trigger_NewAnnouncement(t *testing.T) {
	cfg := WatcherConfig{
		NotifyAnnouncements: true,
	}
	mockNotifier := &MockNotifier{}
	mockFetcher := &MockFetcher{
		Announcements: []Announcement{
			{
				ID:       "ann-1",
				Title:    "Bienvenida al trimestre",
				Course:   "ADSO",
				Author:   "Haminton Páez",
				Platform: "sena",
			},
		},
	}

	daemon := NewDaemon(cfg, mockNotifier, mockFetcher)

	// Ciclo 1: Registro de anuncios preexistentes
	_ = daemon.Check(context.Background())
	if len(mockNotifier.Records()) != 0 {
		t.Fatalf("el escaneo inicial no debía notificar anuncios preexistentes, recibidos: %d", len(mockNotifier.Records()))
	}

	// Ciclo 2: Publicación de un nuevo comunicado oficial
	mockFetcher.SetAnnouncements([]Announcement{
		{
			ID:       "ann-1",
			Title:    "Bienvenida al trimestre",
			Course:   "ADSO",
			Author:   "Haminton Páez",
			Platform: "sena",
		},
		{
			ID:       "ann-2",
			Title:    "Sesión sincrónica extraordinaria",
			Course:   "ADSO",
			Author:   "Joana Zuluaga",
			Platform: "sena",
		},
	})

	_ = daemon.Check(context.Background())
	records := mockNotifier.Records()
	if len(records) != 1 {
		t.Fatalf("se esperaba 1 notificación de nuevo anuncio, obtenidas: %d", len(records))
	}

	rec := records[0]
	if rec.Title != TitleAnnouncement {
		t.Errorf("título esperado '%s', obtenido '%s'", TitleAnnouncement, rec.Title)
	}
	if rec.Urgency != UrgencyNormal {
		t.Errorf("urgencia esperada '%s', obtenida '%s'", UrgencyNormal, rec.Urgency)
	}
	if !strings.Contains(rec.Message, "Sesión sincrónica extraordinaria") {
		t.Errorf("mensaje no contiene el título del nuevo comunicado: %s", rec.Message)
	}
}

func TestDaemon_DisabledNotificationFlags(t *testing.T) {
	now := time.Now()

	cfg := WatcherConfig{
		NotifyDeadlines:     false,
		NotifyGrades:        false,
		NotifyAnnouncements: false,
		NotifyOnInitialPoll: true,
	}
	mockNotifier := &MockNotifier{}
	mockFetcher := &MockFetcher{
		Deadlines: []Deadline{
			{ID: "d1", DueDate: now.Add(1 * time.Hour), Platform: "sena"},
		},
		Grades: []Grade{
			{ID: "g1", Grade: "A", Platform: "sena"},
		},
		Announcements: []Announcement{
			{ID: "a1", Title: "Aviso", Platform: "sena"},
		},
	}

	daemon := NewDaemon(cfg, mockNotifier, mockFetcher)
	_ = daemon.Check(context.Background())

	if len(mockNotifier.Records()) != 0 {
		t.Errorf("con todas las opciones desactivadas no debía emitirse ninguna notificación, obtenidas: %d", len(mockNotifier.Records()))
	}
}

func TestDateParser(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantYear  int
		wantMonth time.Month
		wantDay   int
		wantHour  int
		wantMin   int
		wantErr   bool
	}{
		{
			name:      "Moodle con día de la semana y hora",
			raw:       "jueves, 15 de octubre de 2026, 23:59",
			wantYear:  2026,
			wantMonth: time.October,
			wantDay:   15,
			wantHour:  23,
			wantMin:   59,
			wantErr:   false,
		},
		{
			name:      "Moodle simple con mayúscula",
			raw:       "20 de Mayo de 2026, 14:30",
			wantYear:  2026,
			wantMonth: time.May,
			wantDay:   20,
			wantHour:  14,
			wantMin:   30,
			wantErr:   false,
		},
		{
			name:      "Formato ISO con tiempo",
			raw:       "2026-10-19 18:00:00",
			wantYear:  2026,
			wantMonth: time.October,
			wantDay:   19,
			wantHour:  18,
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:    "Cadena vacía debe fallar",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "Texto inválido",
			raw:     "fecha totalmente desconocida",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDate(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDate(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if !tt.wantErr {
				if got.Year() != tt.wantYear || got.Month() != tt.wantMonth || got.Day() != tt.wantDay {
					t.Errorf("fecha esperada %04d-%02d-%02d, obtenida %04d-%02d-%02d",
						tt.wantYear, tt.wantMonth, tt.wantDay, got.Year(), got.Month(), got.Day())
				}
				if got.Hour() != tt.wantHour || got.Minute() != tt.wantMin {
					t.Errorf("hora esperada %02d:%02d, obtenida %02d:%02d",
						tt.wantHour, tt.wantMin, got.Hour(), got.Minute())
				}
			}
		})
	}
}
