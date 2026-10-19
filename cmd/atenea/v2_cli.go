package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/e12jassir/atenea/internal/forum"
	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/sena"
	"github.com/e12jassir/atenea/internal/session"
	"github.com/e12jassir/atenea/internal/sima"
	"github.com/e12jassir/atenea/internal/store"
	"github.com/e12jassir/atenea/internal/submit"
	"github.com/e12jassir/atenea/internal/vault"
	"github.com/e12jassir/atenea/internal/watcher"
)

func runSubmitCLI(v *vault.Vault, args []string) {
	if len(args) < 3 {
		fmt.Println("Uso: atenea submit <sena|sima|ingles> <assignment_id> <ruta_archivo> [comentario]")
		fmt.Println()
		fmt.Println("Ejemplo:")
		fmt.Println("  atenea submit sena 123456 ./evidencia.pdf \"Entrega de taller GA1\"")
		return
	}

	platform := strings.ToLower(args[0])
	assignID := args[1]
	filePath := args[2]
	comment := ""
	if len(args) > 3 {
		comment = strings.Join(args[3:], " ")
	}

	if _, err := os.Stat(filePath); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Archivo no encontrado: %s\n", filePath)
		return
	}

	sessionStore, err := session.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] No se pudo inicializar sesión caché: %v\n", err)
	}

	httpClient, baseURL, err := getAuthenticatedClient(v, sessionStore, platform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		return
	}

	cfg := submit.Config{
		HTTPClient:   httpClient,
		BaseURL:      baseURL,
		SessionStore: sessionStore,
	}

	engine, err := submit.NewEngine(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Inicializando motor de entregas: %v\n", err)
		return
	}

	req := submit.SubmissionRequest{
		Platform:     platform,
		AssignmentID: assignID,
		FilePath:     filePath,
		Comment:      comment,
	}

	fmt.Printf("Enviando evidencia a %s (Asignación #%s)...\n", strings.ToUpper(platform), assignID)
	receipt, err := engine.Submit(context.Background(), req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Falló la entrega: %v\n", err)
		return
	}

	fmt.Println("\n[OK] Evidencia enviada exitosamente a Moodle/Zajuna.")
	fmt.Println(strings.Repeat("-", 75))
	fmt.Printf("  Archivo:        %s\n", receipt.FileName)
	fmt.Printf("  Tamaño:         %d bytes\n", receipt.FileSize)
	fmt.Printf("  Hash SHA-256:   %s\n", receipt.FileHashSHA256)
	if receipt.RemoteDraftID != "" {
		fmt.Printf("  Remote Draft:   %s\n", receipt.RemoteDraftID)
	}
	fmt.Printf("  Fecha y Hora:   %s\n", receipt.Timestamp.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("  Comprobante:    %s\n", receipt.ReceiptPath)
	fmt.Println(strings.Repeat("-", 75))
}

func runForumCLI(v *vault.Vault, args []string) {
	if len(args) < 2 {
		fmt.Println("Uso: atenea forum <sena|sima|ingles> <list|read|reply> [opciones]")
		fmt.Println()
		fmt.Println("Comandos disponibles:")
		fmt.Println("  atenea forum <plat> list <forum_id>                             Listar temas del foro")
		fmt.Println("  atenea forum <plat> read <discussion_id>                       Leer hilo de discusión")
		fmt.Println("  atenea forum <plat> reply <disc_id> <post_id> <asunto> <msg>   Responder a un post")
		return
	}

	platform := strings.ToLower(args[0])
	action := strings.ToLower(args[1])

	sessionStore, _ := session.New()
	httpClient, baseURL, err := getAuthenticatedClient(v, sessionStore, platform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		return
	}

	engine := forum.NewEngine()

	switch action {
	case "list":
		if len(args) < 3 {
			fmt.Println("Uso: atenea forum <plat> list <forum_id>")
			return
		}
		forumID := args[2]
		discussions, err := engine.ListDiscussions(httpClient, baseURL, forumID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Consultando discusiones del foro: %v\n", err)
			return
		}
		fmt.Printf("\nDiscusiones del Foro #%s (%s) - Total: %d\n", forumID, strings.ToUpper(platform), len(discussions))
		fmt.Println(strings.Repeat("-", 75))
		for i, d := range discussions {
			lockStatus := ""
			if d.IsLocked {
				lockStatus = " [CERRADO]"
			}
			fmt.Printf("[%02d] ID: %-8s | Respuestas: %-3d | %s%s\n     Autor: %-25s | Fecha: %s\n",
				i+1, d.ID, d.ReplyCount, d.Title, lockStatus, d.Author, d.Date)
		}
		fmt.Println(strings.Repeat("-", 75))

	case "read":
		if len(args) < 3 {
			fmt.Println("Uso: atenea forum <plat> read <discussion_id>")
			return
		}
		discussionID := args[2]
		posts, err := engine.GetDiscussionPosts(httpClient, baseURL, discussionID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Leyendo tema de discusión: %v\n", err)
			return
		}
		fmt.Printf("\nHilo de Discusión #%s (%s) - Mensajes: %d\n", discussionID, strings.ToUpper(platform), len(posts))
		fmt.Println(strings.Repeat("=", 75))
		for i, p := range posts {
			indent := ""
			if p.ParentID != "" && p.ParentID != "0" {
				indent = "  ↳ "
			}
			fmt.Printf("%s[%02d] Post #%s por %s (%s)\n", indent, i+1, p.ID, p.Author, p.Date)
			fmt.Printf("%sAsunto: %s\n", indent, p.Subject)
			fmt.Println(strings.Repeat("-", 75))
			content := strings.TrimSpace(p.ContentText)
			if content == "" {
				content = strings.TrimSpace(p.ContentHTML)
			}
			fmt.Printf("%s\n\n", content)
		}

	case "reply":
		if len(args) < 6 {
			fmt.Println("Uso: atenea forum <plat> reply <discussion_id> <parent_post_id> <asunto> <mensaje>")
			return
		}
		discID := args[2]
		parentID := args[3]
		subject := args[4]
		message := strings.Join(args[5:], " ")

		req := forum.ReplyRequest{
			DiscussionID: discID,
			ParentPostID: parentID,
			Subject:      subject,
			Message:      message,
		}

		fmt.Printf("Publicando respuesta en discusión #%s (Post #%s)...\n", discID, parentID)
		if err := engine.ReplyToPost(httpClient, baseURL, "", req); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Publicando respuesta: %v\n", err)
			return
		}
		fmt.Println("[OK] Respuesta publicada exitosamente en el foro.")

	default:
		fmt.Printf("[ERROR] Acción '%s' desconocida. Usa list, read o reply.\n", action)
	}
}

func runWatchCLI(v *vault.Vault, args []string) {
	interval := 5 * time.Minute
	platform := "all"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--interval", "-i":
			if i+1 < len(args) {
				d, err := time.ParseDuration(args[i+1])
				if err == nil && d >= 30*time.Second {
					interval = d
				}
				i++
			}
		case "--platform", "-p":
			if i+1 < len(args) {
				platform = strings.ToLower(args[i+1])
				i++
			}
		}
	}

	sessionStore, _ := session.New()

	var senaClient *sena.Client
	var simaClient *sima.Client
	var inglesClient *sima.Client

	if platform == "all" || platform == "sena" {
		if sCreds, err := v.GetCredentials(model.PlatformSena); err == nil {
			if sc, err := sena.NewClient(); err == nil {
				_ = sc.EnsureAuthenticated(sessionStore, sCreds.DocType, sCreds.User, sCreds.Password)
				senaClient = sc
			}
		}
	}

	if platform == "all" || platform == "sima" || platform == "udc" {
		if simaCreds, err := v.GetCredentials(model.PlatformSima); err == nil {
			if smc, err := sima.NewClient("https://sima.unicartagena.edu.co"); err == nil {
				_ = smc.EnsureAuthenticated(sessionStore, "sima", simaCreds.User, simaCreds.Password)
				simaClient = smc
			}
		}
	}

	if platform == "all" || platform == "ingles" {
		if ingCreds, err := v.GetCredentials(model.PlatformIngles); err == nil {
			if ic, err := sima.NewClient("https://ingles.unicartagena.edu.co"); err == nil {
				_ = ic.EnsureAuthenticated(sessionStore, "ingles", ingCreds.User, ingCreds.Password)
				inglesClient = ic
			}
		}
	}

	fetcher := watcher.NewDefaultFetcher(senaClient, simaClient, inglesClient)
	notifier := watcher.NewLinuxNotifier()

	cfg := watcher.WatcherConfig{
		PollInterval:        interval,
		Platform:            platform,
		NotifyDeadlines:     true,
		NotifyGrades:        true,
		NotifyAnnouncements: true,
	}

	d := watcher.NewDaemon(cfg, notifier, fetcher)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	fmt.Println("==> Atenea Watcher Daemon iniciado")
	fmt.Printf("    Intervalo: %v | Plataforma: %s\n", interval, strings.ToUpper(platform))
	fmt.Println("    Notificaciones de escritorio: activas (notify-send)")
	fmt.Println("    Presiona Ctrl+C para detener el daemon.")
	fmt.Println(strings.Repeat("-", 75))

	if err := d.Start(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error en watcher daemon: %v\n", err)
	}
	fmt.Println("\nAtenea Watcher Daemon detenido correctamente.")
}

func runScanCLI(v *vault.Vault, args []string) {
	isLive := false
	platform := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--live", "-l":
			isLive = true
		case "--platform", "-p":
			if i+1 < len(args) {
				platform = strings.ToLower(args[i+1])
				i++
			}
		}
	}

	st, err := store.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Error abriendo base de datos SQLite: %v\n", err)
		return
	}
	defer st.Close()

	if !isLive {
		assignments, err := st.GetAssignments(platform)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Leyendo asignaciones de SQLite: %v\n", err)
			return
		}

		announcements, err := st.GetAnnouncements(platform)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Leyendo anuncios de SQLite: %v\n", err)
			return
		}

		if len(assignments) == 0 && len(announcements) == 0 {
			fmt.Println("\n[INFO] La base de datos local SQLite (~/.local/share/atenea/state.db) está vacía.")
			fmt.Println("       Ejecuta 'atenea scan --live' para sincronizar con los servidores por primera vez.")
			return
		}

		fmt.Printf("\n[0ms] Instantánea Local SQLite - Estado Académico (%s)\n", st.DBPath())
		fmt.Println(strings.Repeat("-", 75))
		fmt.Printf("Asignaciones y Entregas Registradas (Total: %d)\n", len(assignments))
		for i, a := range assignments {
			gradeStr := ""
			if a.Grade != "" && a.Grade != "-" {
				gradeStr = fmt.Sprintf(" | Nota: %s", a.Grade)
			}
			fmt.Printf("[%02d] [%s] %s\n     Curso: %s | Vence: %s%s\n",
				i+1, a.Status, a.Title, a.CourseID, a.DueDate, gradeStr)
		}

		if len(announcements) > 0 {
			fmt.Printf("\nAnuncios Registrados (Total: %d)\n", len(announcements))
			for i, an := range announcements {
				fmt.Printf("[%02d] %s (%s - %s)\n", i+1, an.Title, an.Author, an.Date)
			}
		}
		fmt.Println(strings.Repeat("-", 75))
		fmt.Println("Tip: Usa 'atenea scan --live' para actualizar la instantánea desde Zajuna/SIMA.")
		return
	}

	// Modo Live: consultar servidores, calcular deltas y actualizar SQLite
	fmt.Println("Sincronizando estado académico en vivo desde Zajuna y SIMA...")
	sessionStore, _ := session.New()

	var newAssignments []store.AssignmentRecord
	var newAnnouncements []store.AnnouncementRecord
	var newCourses []store.CourseRecord

	// Sincronizar SENA Zajuna
	if platform == "" || platform == "sena" {
		if sCreds, err := v.GetCredentials(model.PlatformSena); err == nil {
			if sc, err := sena.NewClient(); err == nil {
				if err := sc.EnsureAuthenticated(sessionStore, sCreds.DocType, sCreds.User, sCreds.Password); err == nil {
					newCourses = append(newCourses, store.CourseRecord{
						ID:       "sena-adso",
						Platform: "sena",
						Code:     "3545922",
						Name:     "ADSO Tecnólogo - SENA Zajuna",
					})

					if evs, err := sc.AuditPendingEvidences(); err == nil {
						for _, ev := range evs {
							newAssignments = append(newAssignments, store.AssignmentRecord{
								ID:        ev.ID,
								CourseID:  "sena-adso",
								Code:      ev.ID,
								Title:     ev.Title,
								DueDate:   ev.DueDate,
								Status:    ev.Status,
								Grade:     ev.Grade,
								UpdatedAt: time.Now(),
							})
						}
					}

					if ans, err := sc.GetAnnouncements(); err == nil {
						for _, a := range ans {
							newAnnouncements = append(newAnnouncements, store.AnnouncementRecord{
								ID:        a.ID,
								CourseID:  "sena-adso",
								Title:     a.Title,
								Author:    a.Author,
								Date:      a.Date,
								Content:   a.Content,
								UpdatedAt: time.Now(),
							})
						}
					}
				}
			}
		}
	}

	// Sincronizar UDC SIMA
	if platform == "" || platform == "sima" || platform == "udc" {
		if simaCreds, err := v.GetCredentials(model.PlatformSima); err == nil {
			if smc, err := sima.NewClient("https://sima.unicartagena.edu.co"); err == nil {
				if err := smc.EnsureAuthenticated(sessionStore, "sima", simaCreds.User, simaCreds.Password); err == nil {
					newCourses = append(newCourses, store.CourseRecord{
						ID:       "sima-academic",
						Platform: "sima",
						Code:     "UDC-ING",
						Name:     "Ingeniería de Software - SIMA UDC",
					})

					if events, err := smc.GetUpcomingEvents(); err == nil {
						for _, ev := range events {
							newAssignments = append(newAssignments, store.AssignmentRecord{
								ID:        fmt.Sprintf("sima-%s", ev.ID),
								CourseID:  "sima-academic",
								Title:     ev.Title,
								DueDate:   ev.DueDate,
								Status:    ev.Status,
								UpdatedAt: time.Now(),
							})
						}
					}
				}
			}
		}
	}

	// Calcular diferencias (deltas)
	diffTarget := platform
	if diffTarget == "" {
		diffTarget = "all"
	}
	deltas, err := st.ComputeDiff(diffTarget, newAssignments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] Error calculando deltas: %v\n", err)
	}

	// Persistir en SQLite
	if len(newCourses) > 0 {
		_ = st.SaveCourses(newCourses)
	}
	if len(newAssignments) > 0 {
		_ = st.SaveAssignments(newAssignments)
	}
	if len(newAnnouncements) > 0 {
		_ = st.SaveAnnouncements(newAnnouncements)
	}
	if len(deltas) > 0 {
		_ = st.SaveEvents(deltas)
	}

	fmt.Printf("\n[OK] Sincronización completa: %d asignaciones, %d anuncios guardados en SQLite.\n",
		len(newAssignments), len(newAnnouncements))

	if len(deltas) > 0 {
		fmt.Printf("\n⚡ Cambios Detectados (Deltas: %d):\n", len(deltas))
		fmt.Println(strings.Repeat("-", 75))
		for _, d := range deltas {
			fmt.Printf("  • [%s] %s\n", d.Type, d.Description)
		}
		fmt.Println(strings.Repeat("-", 75))
	} else {
		fmt.Println("✓ Sin cambios respecto a la última instantánea.")
	}
}

func getAuthenticatedClient(v *vault.Vault, sessionStore session.SessionStore, platform string) (*http.Client, string, error) {
	switch platform {
	case "sena":
		creds, err := v.GetCredentials(model.PlatformSena)
		if err != nil {
			return nil, "", fmt.Errorf("no hay credenciales configuradas para SENA Zajuna (usa 'atenea auth sena')")
		}
		sc, err := sena.NewClient()
		if err != nil {
			return nil, "", fmt.Errorf("creando cliente SENA: %w", err)
		}
		if sessionStore != nil {
			_ = sc.EnsureAuthenticated(sessionStore, creds.DocType, creds.User, creds.Password)
		} else {
			_ = sc.Login(creds.DocType, creds.User, creds.Password)
		}
		return sc.HTTPClient(), "https://zajuna.sena.edu.co/zajuna", nil

	case "sima", "udc":
		creds, err := v.GetCredentials(model.PlatformSima)
		if err != nil {
			return nil, "", fmt.Errorf("no hay credenciales configuradas para SIMA UDC (usa 'atenea auth sima')")
		}
		smc, err := sima.NewClient("https://sima.unicartagena.edu.co")
		if err != nil {
			return nil, "", fmt.Errorf("creando cliente SIMA: %w", err)
		}
		if sessionStore != nil {
			_ = smc.EnsureAuthenticated(sessionStore, "sima", creds.User, creds.Password)
		} else {
			_ = smc.Login(creds.User, creds.Password)
		}
		return smc.HTTPClient(), "https://sima.unicartagena.edu.co", nil

	case "ingles":
		creds, err := v.GetCredentials(model.PlatformIngles)
		if err != nil {
			return nil, "", fmt.Errorf("no hay credenciales configuradas para SIMA Inglés (usa 'atenea auth ingles')")
		}
		ic, err := sima.NewClient("https://ingles.unicartagena.edu.co")
		if err != nil {
			return nil, "", fmt.Errorf("creando cliente Inglés: %w", err)
		}
		if sessionStore != nil {
			_ = ic.EnsureAuthenticated(sessionStore, "ingles", creds.User, creds.Password)
		} else {
			_ = ic.Login(creds.User, creds.Password)
		}
		return ic.HTTPClient(), "https://ingles.unicartagena.edu.co", nil

	default:
		return nil, "", fmt.Errorf("plataforma no soportada: '%s' (usa sena, sima o ingles)", platform)
	}
}
