package watcher

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

const (
	DefaultAppName = "Atenea"
	DefaultIcon    = "dialog-information"

	UrgencyLow      = "low"
	UrgencyNormal   = "normal"
	UrgencyCritical = "critical"
)

// Notifier es la interfaz para emitir notificaciones del sistema de escritorio.
type Notifier interface {
	Notify(title, message string, urgency string) error
}

// LinuxNotifier implementa Notifier utilizando el comando estándar notify-send de Linux.
// Incluye degradación elegante ante la ausencia o fallo de notify-send.
type LinuxNotifier struct {
	AppName        string
	Icon           string
	FallbackWriter io.Writer
	ExecCmd        func(name string, arg ...string) *exec.Cmd
	mu             sync.Mutex
}

// NewLinuxNotifier crea una instancia configurada de LinuxNotifier con valores predeterminados.
func NewLinuxNotifier() *LinuxNotifier {
	return &LinuxNotifier{
		AppName:        DefaultAppName,
		Icon:           DefaultIcon,
		FallbackWriter: os.Stderr,
		ExecCmd:        exec.Command,
	}
}

// Notify emite una notificación de escritorio con título, mensaje y nivel de urgencia.
// Si notify-send no está instalado en el sistema o su ejecución falla, realiza degradación elegante
// registrando el mensaje en FallbackWriter (por defecto os.Stderr) sin provocar fallos fatales.
func (n *LinuxNotifier) Notify(title, message string, urgency string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if urgency == "" {
		urgency = UrgencyNormal
	}

	appName := n.AppName
	if appName == "" {
		appName = DefaultAppName
	}

	icon := n.Icon
	if icon == "" {
		icon = DefaultIcon
	}

	writer := n.FallbackWriter
	if writer == nil {
		writer = os.Stderr
	}

	execCmd := n.ExecCmd
	if execCmd == nil {
		execCmd = exec.Command
	}

	cmd := execCmd("notify-send", "-a", appName, "-u", urgency, "-i", icon, title, message)
	if err := cmd.Run(); err != nil {
		// Degradación elegante: registrar mensaje en el fallback logger sin causar pánico
		fmt.Fprintf(writer, "[NOTIFICACIÓN FALLBACK] [%s] %s: %s (notify-send no disponible o falló: %v)\n",
			urgency, title, message, err)
		return nil
	}

	return nil
}

// MockNotifier es una implementación en memoria de Notifier diseñada para pruebas unitarias.
type MockNotifier struct {
	mu            sync.Mutex
	Notifications []NotificationRecord
	NotifyFunc    func(title, message, urgency string) error
}

// NotificationRecord almacena los datos de una notificación emitida hacia el mock.
type NotificationRecord struct {
	Title   string
	Message string
	Urgency string
}

// Notify registra la notificación en el historial interno del mock.
func (m *MockNotifier) Notify(title, message string, urgency string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Notifications = append(m.Notifications, NotificationRecord{
		Title:   title,
		Message: message,
		Urgency: urgency,
	})

	if m.NotifyFunc != nil {
		return m.NotifyFunc(title, message, urgency)
	}

	return nil
}

// Records retorna una copia de todas las notificaciones recibidas por el mock.
func (m *MockNotifier) Records() []NotificationRecord {
	m.mu.Lock()
	defer m.mu.Unlock()

	copied := make([]NotificationRecord, len(m.Notifications))
	copy(copied, m.Notifications)
	return copied
}

// Reset limpia el historial de notificaciones del mock.
func (m *MockNotifier) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Notifications = nil
}
