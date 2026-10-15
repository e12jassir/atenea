package model

import "time"

// Platform identifica las instituciones soportadas
type Platform string

const (
	PlatformSena   Platform = "sena"
	PlatformSima   Platform = "sima"
	PlatformIngles Platform = "ingles"
)

// Credentials representa los datos de autenticación cifrados en la bóveda
type Credentials struct {
	Platform Platform `json:"platform"`
	DocType  string   `json:"doc_type,omitempty"` // TI, CC, CE, PAS (para SENA)
	User     string   `json:"user"`
	Password string   `json:"password"`
	Cookie   string   `json:"cookie,omitempty"`
}

// Evidence representa una actividad, foro o tarea de Zajuna LMS
type Evidence struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Course    string    `json:"course"`
	DueDate   string    `json:"due_date"`
	Status    string    `json:"status"` // "Aprobado (A)", "Pendiente", "Entregado"
	Link      string    `json:"link"`
	Grade     string    `json:"grade,omitempty"`
	Feedback  string    `json:"feedback,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// Announcement representa un comunicado oficial en el tablón
type Announcement struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Content string `json:"content"`
	Link    string `json:"link"`
}

// SimaEvent representa una entrega, parcial o foro de SIMA UDC
type SimaEvent struct {
	ID        string `json:"id"`
	Course    string `json:"course"`
	Title     string `json:"title"`
	DueDate   string `json:"due_date"`
	Status    string `json:"status"` // "YA REALIZADO", "YA ENVIADO", "PENDIENTE"
	Link      string `json:"link"`
	Timestamp int64  `json:"timestamp"`
}

// CourseGrade representa la calificación consolidada de una asignatura
type CourseGrade struct {
	Course string `json:"course"`
	Grade  string `json:"grade"`
	Status string `json:"status"`
}
