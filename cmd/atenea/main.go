package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/sena"
	"github.com/e12jassir/atenea/internal/sima"
	"github.com/e12jassir/atenea/internal/ui"
	"github.com/e12jassir/atenea/internal/vault"
)

func printHelp() {
	fmt.Println(`ATENEA - Suite de Inteligencia Academica (Edicion Go)
SENA Zajuna ADSO + Universidad de Cartagena (SIMA e Ingles)

Uso Interactivo:
  atenea                     Inicia la TUI interactiva (Bubble Tea)

Uso CLI Directo:
  atenea sena check          Auditoria en vivo de estado de evidencias
  atenea sena pending        Lista de evidencias pendientes
  atenea sena notas          Juicios evaluativos aprobados [A]
  atenea sena anuncios       Tablon de anuncios oficiales
  atenea sena anuncios <id>  Leer cuerpo completo del anuncio
  atenea sena feedback       Comentarios y retroalimentacion de instructores

  atenea udc upcoming        Entregas y examenes proximos en SIMA
  atenea udc grades          Calificaciones del semestre actual

  atenea ingles upcoming     Actividades proximas de ingles

  atenea auth [modulo]       Configurar y cifrar credenciales
  atenea version             Version compilada
`)
}

func main() {
	v, err := vault.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error inicializando boveda segura: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		cmd := strings.ToLower(os.Args[1])
		subArgs := os.Args[2:]

		switch cmd {
		case "-h", "--help", "help":
			printHelp()
			return
		case "version", "-v", "--version":
			fmt.Println("Atenea v2.0.0 (Go Native • Charm Bubble Tea)")
			return
		case "sena":
			runSenaCLI(v, subArgs)
			return
		case "udc", "sima":
			runSimaCLI(v, subArgs)
			return
		case "ingles":
			runInglesCLI(v, subArgs)
			return
		}
	}

	p := tea.NewProgram(ui.NewModel(v), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error al ejecutar Atenea: %v\n", err)
		os.Exit(1)
	}
}

func runSenaCLI(v *vault.Vault, args []string) {
	creds, err := v.GetCredentials(model.PlatformSena)
	if err != nil {
		fmt.Println("[ERROR] No se encontraron credenciales para SENA Zajuna.")
		fmt.Println("        Ejecuta 'atenea' para configurarlas en la boveda.")
		return
	}

	client, err := sena.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	if err := client.Login(creds.DocType, creds.User, creds.Password); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Autenticacion fallida en Zajuna: %v\n", err)
		return
	}

	action := "pending"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}

	switch action {
	case "check":
		fmt.Println("Auditando estado real de evidencias en Zajuna...")
		evs, err := client.AuditPendingEvidences()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nEvidencias en SENA Zajuna (Total: %d)\n", len(evs))
		fmt.Println(strings.Repeat("-", 75))
		for i, e := range evs {
			fmt.Printf("[%02d] [%s] %s\n", i+1, e.Status, e.Title)
		}
		fmt.Println(strings.Repeat("-", 75))

	case "pending":
		evs, err := client.GetPendingEvidences()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nEvidencias Pendientes en SENA Zajuna (Total: %d)\n", len(evs))
		fmt.Println(strings.Repeat("-", 75))
		for i, e := range evs {
			fmt.Printf("[%02d] %s\n", i+1, e.Title)
		}
		fmt.Println(strings.Repeat("-", 75))

	case "notas":
		rep, err := client.GetGradeReport()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nJuicios Evaluativos en SENA Zajuna (Aprobadas: %d)\n", len(rep.Graded))
		fmt.Println(strings.Repeat("-", 75))
		for i, g := range rep.Graded {
			fmt.Printf("[%02d] [%s] %s\n", i+1, g.Grade, g.Title)
		}
		fmt.Println(strings.Repeat("-", 75))

	case "anuncios":
		if len(args) > 1 {
			body, err := client.GetAnnouncementDetail(args[1])
			if err != nil {
				fmt.Println("Error:", err)
				return
			}
			fmt.Printf("\nAnuncio #%s\n%s\n%s\n%s\n", args[1], strings.Repeat("-", 75), body, strings.Repeat("-", 75))
			return
		}
		ans, err := client.GetAnnouncements()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nTablon de Anuncios SENA Zajuna (Total: %d)\n", len(ans))
		fmt.Println(strings.Repeat("-", 75))
		for i, a := range ans {
			fmt.Printf("[%02d] ID: %s | %s\n     %s\n", i+1, a.ID, a.Title, a.Author)
		}
		fmt.Println(strings.Repeat("-", 75))

	case "feedback":
		fb, err := client.GetFeedback()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nRetroalimentacion de Instructores (Total: %d)\n", len(fb))
		fmt.Println(strings.Repeat("-", 75))
		for _, f := range fb {
			fmt.Printf("%s\n  Nota: %s\n  Comentario: \"%s\"\n\n", f.Title, f.Grade, f.Feedback)
		}
		fmt.Println(strings.Repeat("-", 75))
	}
}

func runSimaCLI(v *vault.Vault, args []string) {
	creds, err := v.GetCredentials(model.PlatformSima)
	if err != nil {
		fmt.Println("[ERROR] No se encontraron credenciales para SIMA UDC.")
		return
	}

	client, err := sima.NewClient("https://sima.unicartagena.edu.co")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	if err := client.Login(creds.User, creds.Password); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Autenticacion fallida en SIMA: %v\n", err)
		return
	}

	action := "upcoming"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}

	switch action {
	case "upcoming":
		evs, err := client.GetUpcomingEvents()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nEventos y Entregas SIMA UDC (Total: %d)\n", len(evs))
		fmt.Println(strings.Repeat("-", 75))
		for i, e := range evs {
			fmt.Printf("[%02d] %s\n     %s\n     Limite: %s | Estado: %s\n", i+1, e.Course, e.Title, e.DueDate, e.Status)
		}
		fmt.Println(strings.Repeat("-", 75))

	case "grades":
		grades, err := client.GetGradesOverview()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nCalificaciones Semestre Actual - SIMA UDC\n")
		fmt.Println(strings.Repeat("-", 75))
		for i, g := range grades {
			fmt.Printf("[%02d] %-40s Nota: %s\n", i+1, g.Course, g.Grade)
		}
		fmt.Println(strings.Repeat("-", 75))
	}
}

func runInglesCLI(v *vault.Vault, args []string) {
	creds, err := v.GetCredentials(model.PlatformIngles)
	if err != nil {
		fmt.Println("[ERROR] No se encontraron credenciales para SIMA Ingles.")
		return
	}

	client, err := sima.NewClient("https://ingles.unicartagena.edu.co")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	if err := client.Login(creds.User, creds.Password); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Autenticacion fallida en Ingles: %v\n", err)
		return
	}

	evs, err := client.GetUpcomingEvents()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Printf("\nActividades Proximas - SIMA Ingles UDC (Total: %d)\n", len(evs))
	fmt.Println(strings.Repeat("-", 75))
	for i, e := range evs {
		fmt.Printf("[%02d] %s\n     %s\n     Limite: %s | Estado: %s\n", i+1, e.Course, e.Title, e.DueDate, e.Status)
	}
	fmt.Println(strings.Repeat("-", 75))
}
