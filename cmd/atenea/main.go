package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/atenea/internal/doctor"
	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/sena"
	"github.com/e12jassir/atenea/internal/session"
	"github.com/e12jassir/atenea/internal/sima"
	"github.com/e12jassir/atenea/internal/ui"
	"github.com/e12jassir/atenea/internal/vault"
)

func printHelp() {
	fmt.Print(`ATENEA - Suite de Inteligencia Academica (Edicion Go)
SENA Zajuna ADSO + Universidad de Cartagena (SIMA e Ingles)

Uso Interactivo:
  atenea                     Inicia la TUI interactiva (Bubble Tea)

Uso CLI Directo:
  atenea scan [--live]       Instantanea local SQLite (0ms) o sync en vivo
  atenea watch [--interval]  Daemon en background con alertas notify-send
  atenea submit <plat> <id> <file> [comentario] Entrega con recibo criptografico SHA-256
  atenea forum <plat> <list|read|reply> ...     Interaccion completa con foros Moodle

  atenea sena check          Auditoria en vivo de estado de evidencias
  atenea sena pending        Lista de evidencias pendientes
  atenea sena notas          Juicios evaluativos aprobados [A]
  atenea sena anuncios       Tablon de anuncios oficiales
  atenea sena anuncios <id>  Leer cuerpo completo del anuncio
  atenea sena feedback       Comentarios y retroalimentacion de instructores

  atenea udc upcoming        Entregas y examenes proximos en SIMA
  atenea udc grades          Calificaciones del semestre actual
  atenea udc breakdown <id>  Notas por unidad y PCC de una asignatura
  atenea udc anuncios        Anuncios y foros de asignaturas

  atenea ingles upcoming     Actividades proximas de ingles
  atenea ingles grades       Calificacion consolidada de ingles
  atenea ingles breakdown    Desglose por unidades (Unit 1-4 y Assessments)
  atenea ingles anuncios     Anuncios del curso de bilinguismo

  atenea doctor              Diagnostico de conectividad, boveda y sesiones
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
		case "doctor":
			runDoctorCLI(subArgs)
			return
		case "auth":
			runAuthCLI(v, subArgs)
			return
		case "submit":
			runSubmitCLI(v, subArgs)
			return
		case "forum":
			runForumCLI(v, subArgs)
			return
		case "watch":
			runWatchCLI(v, subArgs)
			return
		case "scan":
			runScanCLI(v, subArgs)
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

	store, err := session.New()
	if err == nil {
		err = client.EnsureAuthenticated(store, creds.DocType, creds.User, creds.Password)
	} else {
		err = client.Login(creds.DocType, creds.User, creds.Password)
	}
	if err != nil {
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
			fmt.Printf("[%02d] %-42s Nota: %-6s [ID: %s]\n", i+1, g.Course, g.Grade, g.ID)
		}
		fmt.Println(strings.Repeat("-", 75))


	case "breakdown":
		if len(args) < 2 {
			fmt.Println("Uso: atenea udc breakdown <id_curso>")
			return
		}
		items, err := client.GetCourseGradeBreakdown(args[1])
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nDesglose de Calificaciones (Total: %d)\n", len(items))
		fmt.Println(strings.Repeat("-", 75))
		for i, item := range items {
			fmt.Printf("[%02d] [%s] %-38s Nota: %-6s (Pond: %s, Rango: %s)\n", i+1, item.Category, item.Name, item.Grade, item.Weight, item.Range)
		}
		fmt.Println(strings.Repeat("-", 75))

	case "anuncios":
		ans, err := client.GetAnnouncements()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nAnuncios y Foros SIMA UDC (Total: %d)\n", len(ans))
		fmt.Println(strings.Repeat("-", 75))
		for i, a := range ans {
			fmt.Printf("[%02d] %s\n     %s | %s\n", i+1, a.Title, a.Course, a.Author)
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
		fmt.Printf("\nActividades Proximas - SIMA Ingles UDC (Total: %d)\n", len(evs))
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
		fmt.Printf("\nCalificacion Consolidada - SIMA Ingles UDC\n")
		fmt.Println(strings.Repeat("-", 75))
		for i, g := range grades {
			fmt.Printf("[%02d] %-40s Nota: %s\n", i+1, g.Course, g.Grade)
		}
		fmt.Println(strings.Repeat("-", 75))

	case "breakdown":
		items, err := client.GetCourseGradeBreakdown("310")
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nDesglose de Calificaciones por Unidad - SIMA Ingles UDC (Total: %d)\n", len(items))
		fmt.Println(strings.Repeat("-", 75))
		for i, item := range items {
			fmt.Printf("[%02d] [%s] %-38s Nota: %-6s (Pond: %s, Rango: %s)\n", i+1, item.Category, item.Name, item.Grade, item.Weight, item.Range)
		}
		fmt.Println(strings.Repeat("-", 75))

	case "anuncios":
		ans, err := client.GetAnnouncements()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Printf("\nAnuncios y Foros - SIMA Ingles UDC (Total: %d)\n", len(ans))
		fmt.Println(strings.Repeat("-", 75))
		for i, a := range ans {
			fmt.Printf("[%02d] %s\n     %s | %s\n", i+1, a.Title, a.Course, a.Author)
		}
		fmt.Println(strings.Repeat("-", 75))
	}
}

func runDoctorCLI(args []string) {
	fmt.Println("Ejecutando diagnóstico integral de Atenea...")
	fmt.Println()
	report, err := doctor.RunDiagnostics("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error ejecutando diagnóstico: %v\n", err)
		os.Exit(1)
	}

	report.PrintSummary(os.Stdout)
	if report.OverallStatus == doctor.StatusFail {
		os.Exit(1)
	}
}

func runAuthCLI(v *vault.Vault, args []string) {
	if len(args) == 0 || strings.ToLower(args[0]) == "status" {
		printAuthStatus(v)
		return
	}

	platformName := strings.ToLower(args[0])
	var targetPlatform model.Platform
	switch platformName {
	case "sena":
		targetPlatform = model.PlatformSena
	case "sima", "udc":
		targetPlatform = model.PlatformSima
	case "ingles":
		targetPlatform = model.PlatformIngles
	default:
		fmt.Printf("[ERROR] Módulo '%s' desconocido.\n", platformName)
		fmt.Println("Módulos disponibles: sena, sima (o udc), ingles, status")
		return
	}

	// Check if --cli flag was provided
	useCLI := false
	for _, arg := range args[1:] {
		if arg == "--cli" {
			useCLI = true
			break
		}
	}

	if useCLI {
		runAuthPromptCLI(v, targetPlatform)
		return
	}

	// Launch Bubble Tea TUI directly in authentication view for this platform
	p := tea.NewProgram(ui.NewModelWithAuth(v, targetPlatform), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error al iniciar interfaz interactiva: %v\n", err)
		fmt.Println("Cambiando a modo terminal directo...")
		runAuthPromptCLI(v, targetPlatform)
	}
}

func printAuthStatus(v *vault.Vault) {
	fmt.Println("\nEstado de Credenciales en Bóveda Cifrada (AES-256-GCM)")
	fmt.Println(strings.Repeat("-", 75))

	platforms := []struct {
		platform model.Platform
		name     string
	}{
		{model.PlatformSena, "SENA Zajuna ADSO"},
		{model.PlatformSima, "UDC SIMA"},
		{model.PlatformIngles, "UDC Inglés Institucional"},
	}

	for _, p := range platforms {
		creds, err := v.GetCredentials(p.platform)
		if err == nil && creds.User != "" && creds.Password != "" {
			docInfo := ""
			if creds.DocType != "" {
				docInfo = fmt.Sprintf("[%s] ", creds.DocType)
			}
			maskedUser := creds.User
			if len(maskedUser) > 4 {
				maskedUser = maskedUser[:4] + strings.Repeat("*", len(maskedUser)-4)
			}
			fmt.Printf("[OK]   %-26s Configurado (%s%s)\n", p.name+":", docInfo, maskedUser)
		} else {
			fmt.Printf("[WARN] %-26s No configurado\n", p.name+":")
		}
	}

	fmt.Println(strings.Repeat("-", 75))
	fmt.Println("Para configurar o actualizar credenciales:")
	fmt.Println("  atenea auth <sena|sima|ingles>")
	fmt.Println("Para verificar conectividad y diagnósticos completos:")
	fmt.Println("  atenea doctor")
	fmt.Println()
}

func runAuthPromptCLI(v *vault.Vault, p model.Platform) {
	reader := bufio.NewReader(os.Stdin)
	var docType, user, pass string

	fmt.Printf("\n--- Configuración de Credenciales: %s ---\n", p)
	if p == model.PlatformSena {
		fmt.Print("Tipo de Documento (TI, CC, CE, PAS) [CC]: ")
		dt, _ := reader.ReadString('\n')
		dt = strings.TrimSpace(dt)
		if dt == "" {
			dt = "CC"
		}
		docType = strings.ToUpper(dt)
	}

	fmt.Print("Número de Documento / Usuario: ")
	u, _ := reader.ReadString('\n')
	user = strings.TrimSpace(u)

	fmt.Print("Contraseña: ")
	pwd, _ := reader.ReadString('\n')
	pass = strings.TrimSpace(pwd)

	if user == "" || pass == "" {
		fmt.Println("[ERROR] El usuario y contraseña son requeridos.")
		return
	}

	fmt.Println("\nVerificando credenciales en vivo...")
	if p == model.PlatformSena {
		client, err := sena.NewClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creando cliente SENA: %v\n", err)
			return
		}
		if err := client.Login(docType, user, pass); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Autenticación fallida en SENA Zajuna: %v\n", err)
			return
		}
	} else {
		baseURL := "https://sima.unicartagena.edu.co"
		if p == model.PlatformIngles {
			baseURL = "https://ingles.unicartagena.edu.co"
		}
		client, err := sima.NewClient(baseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creando cliente SIMA: %v\n", err)
			return
		}
		if err := client.Login(user, pass); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Autenticación fallida en %s: %v\n", baseURL, err)
			return
		}
	}

	creds := model.Credentials{
		Platform: p,
		DocType:  docType,
		User:     user,
		Password: pass,
	}

	if err := v.SaveCredentials(creds); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Error al guardar en la bóveda: %v\n", err)
		return
	}

	fmt.Println("[OK] Credenciales verificadas exitosamente y cifradas en la bóveda segura.")
}

