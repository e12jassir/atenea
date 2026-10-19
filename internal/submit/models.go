package submit

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

var (
	// ErrEmptyAssignmentID se retorna cuando el ID de la tarea/asignación está vacío.
	ErrEmptyAssignmentID = errors.New("el ID de asignación no puede estar vacío")
	// ErrEmptyFilePath se retorna cuando la ruta del archivo está vacía.
	ErrEmptyFilePath = errors.New("la ruta del archivo no puede estar vacía")
	// ErrFileNotFound se retorna cuando el archivo a enviar no existe en el sistema de archivos.
	ErrFileNotFound = errors.New("el archivo para la entrega no fue encontrado")
	// ErrMissingSessKey se retorna cuando no se puede extraer la clave de sesión Moodle del formulario.
	ErrMissingSessKey = errors.New("no se pudo extraer la sesskey de la página de entrega")
	// ErrMissingDraftID se retorna cuando no se puede extraer el draftitemid del formulario de entrega.
	ErrMissingDraftID = errors.New("no se pudo extraer el draftitemid de la página de entrega")
	// ErrUploadFailed se retorna cuando falla la subida multipart del archivo al repositorio Moodle.
	ErrUploadFailed = errors.New("falló la subida del archivo al repositorio Moodle")
	// ErrSubmissionFailed se retorna cuando Moodle rechaza o no confirma la entrega final.
	ErrSubmissionFailed = errors.New("la confirmación final de la entrega falló en Moodle")
	// ErrUnsupportedPlatform se retorna cuando la plataforma indicada no está soportada.
	ErrUnsupportedPlatform = errors.New("plataforma no soportada para entrega")
)

// SubmissionRequest contiene los parámetros de entrada para enviar una evidencia a Moodle/Zajuna.
type SubmissionRequest struct {
	Platform     string `json:"platform"`
	CourseID     string `json:"course_id,omitempty"`
	AssignmentID string `json:"assignment_id"`
	FilePath     string `json:"file_path"`
	Comment      string `json:"comment,omitempty"`
}

// Validate verifica que los campos obligatorios del request sean válidos.
func (r *SubmissionRequest) Validate() error {
	if strings.TrimSpace(r.AssignmentID) == "" {
		return ErrEmptyAssignmentID
	}
	if strings.TrimSpace(r.FilePath) == "" {
		return ErrEmptyFilePath
	}
	info, err := os.Stat(r.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrFileNotFound, r.FilePath)
		}
		return fmt.Errorf("error al verificar archivo: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("la ruta especificada es un directorio, no un archivo: %s", r.FilePath)
	}
	return nil
}

// SubmissionReceipt representa el comprobante criptográfico inmutable de una entrega formal.
type SubmissionReceipt struct {
	Platform       string    `json:"platform"`
	AssignmentID   string    `json:"assignment_id"`
	FileHashSHA256 string    `json:"file_hash_sha256"`
	FileSize       int64     `json:"file_size"`
	FileName       string    `json:"file_name"`
	RemoteDraftID  string    `json:"remote_draft_id"`
	Timestamp      time.Time `json:"timestamp"`
	Status         string    `json:"status"`
	ReceiptPath    string    `json:"receipt_path"`
}
