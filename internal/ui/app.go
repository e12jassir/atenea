package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/sena"
	"github.com/e12jassir/atenea/internal/sima"
	"github.com/e12jassir/atenea/internal/vault"
)

type sessionState int

const (
	stateMenu sessionState = iota
	stateSenaMenu
	stateSimaMenu
	stateInglesMenu
	stateLoading
	stateList
	stateAuth
	stateDetail
)

type dataLoadedMsg struct {
	title string
	items []listItem
	err   error
}

type authValidatedMsg struct {
	ok  bool
	msg string
}

type listItem struct {
	title    string
	subtitle string
	status   string
	details  string
}

type menuItem struct {
	category string
	label    string
	badge    string
	action   int
}

type Model struct {
	state        sessionState
	prevState    sessionState
	vault        *vault.Vault
	spinner      spinner.Model
	loadingText  string
	errorMessage string
	successMsg   string

	// Menu
	menuCursor int
	menuList   []menuItem

	// Submenu
	subCursor int
	subItems  []string

	// Lista de datos
	listTitle  string
	listItems  []listItem
	listCursor int
	detailItem *listItem

	// Formulario Auth
	authPlatform model.Platform
	authDocTypes []string
	authDocIdx   int
	authInputs   []textinput.Model
	authFocus    int
	authLoading  bool

	width  int
	height int
}

func NewModel(v *vault.Vault) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ColorAccent)

	m := Model{
		state:        stateMenu,
		vault:        v,
		spinner:      s,
		authDocTypes: []string{"TI", "CC", "CE", "PAS"},
	}

	m.refreshMainMenu()
	m.initAuthInputs()
	return m
}

func (m *Model) initAuthInputs() {
	m.authInputs = make([]textinput.Model, 2)

	m.authInputs[0] = textinput.New()
	m.authInputs[0].Placeholder = "Numero de Documento / Usuario"
	m.authInputs[0].Prompt = "› "
	m.authInputs[0].PromptStyle = lipgloss.NewStyle().Foreground(ColorAccent)
	m.authInputs[0].TextStyle = lipgloss.NewStyle().Foreground(ColorHighlight)
	m.authInputs[0].CharLimit = 35

	m.authInputs[1] = textinput.New()
	m.authInputs[1].Placeholder = "Contrasena de la plataforma"
	m.authInputs[1].Prompt = "› "
	m.authInputs[1].PromptStyle = lipgloss.NewStyle().Foreground(ColorAccent)
	m.authInputs[1].TextStyle = lipgloss.NewStyle().Foreground(ColorHighlight)
	m.authInputs[1].EchoMode = textinput.EchoPassword
	m.authInputs[1].EchoCharacter = '•'
	m.authInputs[1].CharLimit = 60
}

func (m *Model) refreshMainMenu() {
	senaBadge := BadgeSuccess.Render("[CONFIGURADO]")
	if !m.vault.HasCredentials(model.PlatformSena) {
		senaBadge = BadgeError.Render("[SIN CONFIGURAR]")
	}

	simaBadge := BadgeSuccess.Render("[CONFIGURADO]")
	if !m.vault.HasCredentials(model.PlatformSima) {
		simaBadge = BadgeError.Render("[SIN CONFIGURAR]")
	}

	inglesBadge := BadgeSuccess.Render("[CONFIGURADO]")
	if !m.vault.HasCredentials(model.PlatformIngles) {
		inglesBadge = BadgeError.Render("[SIN CONFIGURAR]")
	}

	m.menuList = []menuItem{
		{category: "UNIVERSIDAD DE CARTAGENA", label: "SIMA UDC (Ingenieria de Software)", badge: simaBadge, action: 0},
		{category: "UNIVERSIDAD DE CARTAGENA", label: "SIMA INGLES (Plataforma Bilinguismo)", badge: inglesBadge, action: 1},
		{category: "SENA ZAJUNA", label: "ANALISIS Y DESARROLLO DE SOFTWARE (Ficha 3545922)", badge: senaBadge, action: 2},
		{category: "CONFIGURACION", label: "Boveda de Credenciales Cifradas (AES-256-GCM)", badge: "", action: 3},
		{category: "CONFIGURACION", label: "Salir", badge: "", action: 4},
	}
}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case dataLoadedMsg:
		m.state = stateList
		m.listCursor = 0
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Error al consultar: %v", msg.err)
			m.listItems = nil
		} else {
			m.listTitle = msg.title
			m.listItems = msg.items
			m.errorMessage = ""
		}
		return m, nil

	case authValidatedMsg:
		m.authLoading = false
		if msg.ok {
			m.successMsg = msg.msg
			m.errorMessage = ""
			m.refreshMainMenu()
			m.state = stateMenu
		} else {
			m.errorMessage = msg.msg
			m.successMsg = ""
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}

		switch m.state {
		case stateMenu:
			return m.updateMenu(msg)
		case stateSenaMenu, stateSimaMenu, stateInglesMenu:
			return m.updateSubmenu(msg)
		case stateList:
			return m.updateList(msg)
		case stateDetail:
			if msg.String() == "esc" || msg.String() == "q" || msg.String() == "enter" {
				m.state = stateList
			}
			return m, nil
		case stateAuth:
			return m.updateAuth(msg)
		}
	}

	return m, nil
}

func (m Model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case "down", "j":
		if m.menuCursor < len(m.menuList)-1 {
			m.menuCursor++
		}
	case "enter":
		m.errorMessage = ""
		m.successMsg = ""
		action := m.menuList[m.menuCursor].action
		switch action {
		case 0: // SIMA
			if !m.vault.HasCredentials(model.PlatformSima) {
				m.startAuth(model.PlatformSima)
				return m, nil
			}
			m.state = stateSimaMenu
			m.subCursor = 0
			m.subItems = []string{
				"1. Entregas y examenes proximos (Con estado en vivo)",
				"2. Calificaciones del semestre actual",
				"3. Volver al menu principal",
			}
		case 1: // Inglés
			if !m.vault.HasCredentials(model.PlatformIngles) {
				m.startAuth(model.PlatformIngles)
				return m, nil
			}
			m.state = stateInglesMenu
			m.subCursor = 0
			m.subItems = []string{
				"1. Actividades y entregas proximas",
				"2. Volver al menu principal",
			}
		case 2: // SENA
			if !m.vault.HasCredentials(model.PlatformSena) {
				m.startAuth(model.PlatformSena)
				return m, nil
			}
			m.state = stateSenaMenu
			m.subCursor = 0
			m.subItems = []string{
				"1. Auditoria de entregas (Estado real en vivo)",
				"2. Evidencias pendientes por calificar",
				"3. Juicios evaluativos aprobados [A]",
				"4. Tablon de anuncios oficiales",
				"5. Retroalimentacion de instructores",
				"6. Volver al menu principal",
			}
		case 3: // Vault
			m.startAuth(model.PlatformSena)
		case 4: // Salir
			return m, tea.Quit
		}
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateSubmenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.subCursor > 0 {
			m.subCursor--
		}
	case "down", "j":
		if m.subCursor < len(m.subItems)-1 {
			m.subCursor++
		}
	case "esc", "q":
		m.state = stateMenu
		return m, nil
	case "enter":
		if m.state == stateSenaMenu {
			switch m.subCursor {
			case 0: // Auditoria check
				m.state = stateLoading
				m.loadingText = "Auditando evidencias en vivo en Zajuna..."
				return m, m.fetchSenaAudit()
			case 1: // Pendientes
				m.state = stateLoading
				m.loadingText = "Consultando evidencias pendientes..."
				return m, m.fetchSenaPending()
			case 2: // Notas aprobadas
				m.state = stateLoading
				m.loadingText = "Consultando juicios evaluativos aprobados..."
				return m, m.fetchSenaGrades()
			case 3: // Anuncios
				m.state = stateLoading
				m.loadingText = "Descargando anuncios de instructores..."
				return m, m.fetchSenaAnnouncements()
			case 4: // Feedback
				m.state = stateLoading
				m.loadingText = "Descargando comentarios de instructores..."
				return m, m.fetchSenaFeedback()
			case 5:
				m.state = stateMenu
			}
		} else if m.state == stateSimaMenu {
			switch m.subCursor {
			case 0: // Upcoming
				m.state = stateLoading
				m.loadingText = "Sincronizando entregas con SIMA UDC..."
				return m, m.fetchSimaUpcoming(model.PlatformSima)
			case 1: // Grades overview
				m.state = stateLoading
				m.loadingText = "Consultando calificaciones del semestre..."
				return m, m.fetchSimaGrades(model.PlatformSima)
			case 2:
				m.state = stateMenu
			}
		} else if m.state == stateInglesMenu {
			switch m.subCursor {
			case 0:
				m.state = stateLoading
				m.loadingText = "Sincronizando plataforma de Ingles..."
				return m, m.fetchSimaUpcoming(model.PlatformIngles)
			case 1:
				m.state = stateMenu
			}
		}
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.listCursor > 0 {
			m.listCursor--
		}
	case "down", "j":
		if m.listCursor < len(m.listItems)-1 {
			m.listCursor++
		}
	case "enter":
		if len(m.listItems) > 0 {
			m.detailItem = &m.listItems[m.listCursor]
			m.state = stateDetail
		}
	case "esc", "q":
		m.state = stateMenu
	}
	return m, nil
}

func (m *Model) startAuth(p model.Platform) {
	m.authPlatform = p
	m.state = stateAuth
	m.authFocus = 0
	m.errorMessage = ""
	m.successMsg = ""

	creds, _ := m.vault.GetCredentials(p)
	if creds.User != "" {
		m.authInputs[0].SetValue(creds.User)
	} else {
		m.authInputs[0].SetValue("")
	}
	m.authInputs[1].SetValue("")
	m.authInputs[0].Focus()
	m.authInputs[1].Blur()
}

func (m Model) updateAuth(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.authLoading {
		return m, nil
	}

	isSena := (m.authPlatform == model.PlatformSena)
	totalFields := 4
	if isSena {
		totalFields = 5
	}

	switch msg.String() {
	case "tab", "down":
		m.authFocus = (m.authFocus + 1) % totalFields
		m.updateFocus()
		return m, nil
	case "shift+tab", "up":
		m.authFocus = (m.authFocus - 1 + totalFields) % totalFields
		m.updateFocus()
		return m, nil
	case "esc":
		m.state = stateMenu
		return m, nil
	}

	if isSena && m.authFocus == 0 {
		if msg.String() == " " || msg.String() == "right" {
			m.authDocIdx = (m.authDocIdx + 1) % len(m.authDocTypes)
		} else if msg.String() == "left" {
			m.authDocIdx = (m.authDocIdx - 1 + len(m.authDocTypes)) % len(m.authDocTypes)
		} else if msg.String() == "enter" {
			m.authFocus = 1
			m.updateFocus()
		}
		return m, nil
	}

	userFieldIdx := 0
	passFieldIdx := 1
	saveBtnIdx := 2
	cancelBtnIdx := 3
	if isSena {
		userFieldIdx = 1
		passFieldIdx = 2
		saveBtnIdx = 3
		cancelBtnIdx = 4
	}

	if m.authFocus == userFieldIdx {
		if msg.String() == "enter" {
			m.authFocus = passFieldIdx
			m.updateFocus()
			return m, nil
		}
		var cmd tea.Cmd
		m.authInputs[0], cmd = m.authInputs[0].Update(msg)
		return m, cmd
	} else if m.authFocus == passFieldIdx {
		if msg.String() == "enter" {
			m.authFocus = saveBtnIdx
			m.updateFocus()
			return m, nil
		}
		var cmd tea.Cmd
		m.authInputs[1], cmd = m.authInputs[1].Update(msg)
		return m, cmd
	} else if m.authFocus == saveBtnIdx {
		if msg.String() == "enter" || msg.String() == " " {
			user := strings.TrimSpace(m.authInputs[0].Value())
			pass := strings.TrimSpace(m.authInputs[1].Value())
			if user == "" || pass == "" {
				m.errorMessage = "El usuario y contrasena son requeridos"
				return m, nil
			}

			m.authLoading = true
			m.errorMessage = ""
			docType := "CC"
			if isSena {
				docType = m.authDocTypes[m.authDocIdx]
			}

			return m, m.validateAndSaveCreds(m.authPlatform, docType, user, pass)
		}
	} else if m.authFocus == cancelBtnIdx {
		if msg.String() == "enter" || msg.String() == " " {
			m.state = stateMenu
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) updateFocus() {
	isSena := (m.authPlatform == model.PlatformSena)
	userFieldIdx := 0
	passFieldIdx := 1
	if isSena {
		userFieldIdx = 1
		passFieldIdx = 2
	}

	if m.authFocus == userFieldIdx {
		m.authInputs[0].Focus()
		m.authInputs[1].Blur()
	} else if m.authFocus == passFieldIdx {
		m.authInputs[1].Focus()
		m.authInputs[0].Blur()
	} else {
		m.authInputs[0].Blur()
		m.authInputs[1].Blur()
	}
}

func (m Model) validateAndSaveCreds(p model.Platform, docType, user, pass string) tea.Cmd {
	return func() tea.Msg {
		if p == model.PlatformSena {
			c, err := sena.NewClient()
			if err != nil {
				return authValidatedMsg{ok: false, msg: err.Error()}
			}
			if err := c.Login(docType, user, pass); err != nil {
				return authValidatedMsg{ok: false, msg: err.Error()}
			}
		} else {
			baseURL := "https://sima.unicartagena.edu.co"
			if p == model.PlatformIngles {
				baseURL = "https://ingles.unicartagena.edu.co"
			}
			c, err := sima.NewClient(baseURL)
			if err != nil {
				return authValidatedMsg{ok: false, msg: err.Error()}
			}
			if err := c.Login(user, pass); err != nil {
				return authValidatedMsg{ok: false, msg: err.Error()}
			}
		}

		creds := model.Credentials{
			Platform: p,
			DocType:  docType,
			User:     user,
			Password: pass,
		}
		if err := m.vault.SaveCredentials(creds); err != nil {
			return authValidatedMsg{ok: false, msg: fmt.Sprintf("Error guardando en boveda: %v", err)}
		}

		return authValidatedMsg{ok: true, msg: "[OK] Credenciales verificadas en vivo y cifradas en boveda"}
	}
}

func (m Model) fetchSenaAudit() tea.Cmd {
	return func() tea.Msg {
		creds, err := m.vault.GetCredentials(model.PlatformSena)
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		client, err := sena.NewClient()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		if err := client.Login(creds.DocType, creds.User, creds.Password); err != nil {
			return dataLoadedMsg{err: err}
		}
		evidences, err := client.AuditPendingEvidences()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		var items []listItem
		for _, e := range evidences {
			items = append(items, listItem{
				title:    e.Title,
				subtitle: e.Course,
				status:   e.Status,
				details:  fmt.Sprintf("Materia: %s\nEstado de entrega: %s\nCalificacion: %s", e.Course, e.Status, e.Grade),
			})
		}
		return dataLoadedMsg{title: "SENA ZAJUNA - AUDITORIA DE ENTREGAS", items: items}
	}
}

func (m Model) fetchSenaPending() tea.Cmd {
	return func() tea.Msg {
		creds, err := m.vault.GetCredentials(model.PlatformSena)
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		client, err := sena.NewClient()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		if err := client.Login(creds.DocType, creds.User, creds.Password); err != nil {
			return dataLoadedMsg{err: err}
		}
		evidences, err := client.GetPendingEvidences()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		var items []listItem
		for _, e := range evidences {
			items = append(items, listItem{
				title:    e.Title,
				subtitle: e.Course,
				status:   e.Status,
				details:  fmt.Sprintf("Materia: %s\nEstado: %s\nCalificacion: %s", e.Course, e.Status, e.Grade),
			})
		}
		return dataLoadedMsg{title: "SENA ZAJUNA - EVIDENCIAS PENDIENTES", items: items}
	}
}

func (m Model) fetchSenaGrades() tea.Cmd {
	return func() tea.Msg {
		creds, err := m.vault.GetCredentials(model.PlatformSena)
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		client, err := sena.NewClient()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		if err := client.Login(creds.DocType, creds.User, creds.Password); err != nil {
			return dataLoadedMsg{err: err}
		}
		grades, err := client.GetApprovedGrades()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		var items []listItem
		for _, g := range grades {
			items = append(items, listItem{
				title:    g.Title,
				subtitle: fmt.Sprintf("Juicio Evaluativo: %s", g.Grade),
				status:   g.Status,
				details:  fmt.Sprintf("Materia: %s\nResultado: %s\nRetroalimentacion: %s", g.Course, g.Status, g.Feedback),
			})
		}
		return dataLoadedMsg{title: "SENA ZAJUNA - JUICIOS EVALUATIVOS APROBADOS", items: items}
	}
}

func (m Model) fetchSenaAnnouncements() tea.Cmd {
	return func() tea.Msg {
		creds, err := m.vault.GetCredentials(model.PlatformSena)
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		client, err := sena.NewClient()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		if err := client.Login(creds.DocType, creds.User, creds.Password); err != nil {
			return dataLoadedMsg{err: err}
		}
		announcements, err := client.GetAnnouncements()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		var items []listItem
		for _, a := range announcements {
			body, _ := client.GetAnnouncementDetail(a.ID)
			items = append(items, listItem{
				title:    a.Title,
				subtitle: a.Author,
				status:   "OFICIAL",
				details:  fmt.Sprintf("Titulo: %s\nPublicado por: %s\nEnlace: %s\n\nContenido:\n%s", a.Title, a.Author, a.Link, body),
			})
		}
		return dataLoadedMsg{title: "SENA ZAJUNA - TABLON DE ANUNCIOS", items: items}
	}
}

func (m Model) fetchSenaFeedback() tea.Cmd {
	return func() tea.Msg {
		creds, err := m.vault.GetCredentials(model.PlatformSena)
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		client, err := sena.NewClient()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		if err := client.Login(creds.DocType, creds.User, creds.Password); err != nil {
			return dataLoadedMsg{err: err}
		}
		feedbackList, err := client.GetFeedback()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		var items []listItem
		for _, f := range feedbackList {
			items = append(items, listItem{
				title:    f.Title,
				subtitle: fmt.Sprintf("Nota: %s", f.Grade),
				status:   "FEEDBACK",
				details:  fmt.Sprintf("Materia: %s\nNota: %s\nComentario del Instructor:\n\"%s\"", f.Course, f.Grade, f.Feedback),
			})
		}
		return dataLoadedMsg{title: "SENA ZAJUNA - RETROALIMENTACION DE INSTRUCTORES", items: items}
	}
}

func (m Model) fetchSimaUpcoming(p model.Platform) tea.Cmd {
	return func() tea.Msg {
		creds, err := m.vault.GetCredentials(p)
		if err != nil {
			return dataLoadedMsg{err: err}
		}

		baseURL := "https://sima.unicartagena.edu.co"
		title := "SIMA UDC - ENTREGAS Y EXAMENES"
		if p == model.PlatformIngles {
			baseURL = "https://ingles.unicartagena.edu.co"
			title = "SIMA INGLES - ACTIVIDADES PROXIMAS"
		}

		client, err := sima.NewClient(baseURL)
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		if err := client.Login(creds.User, creds.Password); err != nil {
			return dataLoadedMsg{err: err}
		}

		events, err := client.GetUpcomingEvents()
		if err != nil {
			return dataLoadedMsg{err: err}
		}

		var items []listItem
		for _, ev := range events {
			items = append(items, listItem{
				title:    ev.Title,
				subtitle: fmt.Sprintf("%s · %s", ev.Course, ev.DueDate),
				status:   ev.Status,
				details:  fmt.Sprintf("Actividad: %s\nAsignatura: %s\nFecha limite: %s\nEstado: %s\nURL: %s", ev.Title, ev.Course, ev.DueDate, ev.Status, ev.Link),
			})
		}

		return dataLoadedMsg{title: title, items: items}
	}
}

func (m Model) fetchSimaGrades(p model.Platform) tea.Cmd {
	return func() tea.Msg {
		creds, err := m.vault.GetCredentials(p)
		if err != nil {
			return dataLoadedMsg{err: err}
		}

		client, err := sima.NewClient("https://sima.unicartagena.edu.co")
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		if err := client.Login(creds.User, creds.Password); err != nil {
			return dataLoadedMsg{err: err}
		}

		grades, err := client.GetGradesOverview()
		if err != nil {
			return dataLoadedMsg{err: err}
		}

		var items []listItem
		for _, g := range grades {
			items = append(items, listItem{
				title:    g.Course,
				subtitle: fmt.Sprintf("Calificacion: %s", g.Grade),
				status:   g.Status,
				details:  fmt.Sprintf("Asignatura: %s\nNota Acumulada: %s\nEstado: %s", g.Course, g.Grade, g.Status),
			})
		}

		return dataLoadedMsg{title: "SIMA UDC - CALIFICACIONES DEL SEMESTRE", items: items}
	}
}

// Renderizado Centralizado
func (m Model) View() string {
	w := m.width
	h := m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	var content string
	switch m.state {
	case stateMenu:
		content = m.renderMainMenu()
	case stateSenaMenu, stateSimaMenu, stateInglesMenu:
		content = m.renderSubmenu()
	case stateLoading:
		content = m.renderLoading()
	case stateList:
		content = m.renderList()
	case stateDetail:
		content = m.renderDetail()
	case stateAuth:
		content = m.renderAuth()
	}

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) renderMainMenu() string {
	logo := AsciiLogoStyle.Render(AsciiLogo)
	subtitle := SubtitleStyle.Render("Terminal Academic Intelligence Suite · ADSO & UDC")

	var lines []string
	var lastCategory string

	for i, item := range m.menuList {
		if item.category != "" && item.category != lastCategory {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, CategoryHeaderStyle.Render(item.category))
			lastCategory = item.category
		}

		prefix := "   "
		if i == m.menuCursor {
			prefix = " › "
		}

		itemText := fmt.Sprintf("%s%-50s %s", prefix, item.label, item.badge)
		if i == m.menuCursor {
			lines = append(lines, MenuItemSelected.Render(itemText))
		} else {
			lines = append(lines, MenuItemNormal.Render(itemText))
		}
	}

	menuBox := MenuBoxStyle.Render(strings.Join(lines, "\n"))
	footer := FooterHelpStyle.Render("↑/k  ↓/j: navegar  ·  enter: entrar  ·  q: salir")

	var elements []string
	elements = append(elements, logo, subtitle)

	if m.successMsg != "" {
		elements = append(elements, "", StatusBarSuccess.Render("[OK] "+m.successMsg))
	}
	if m.errorMessage != "" {
		elements = append(elements, "", StatusBarError.Render("[ERROR] "+m.errorMessage))
	}

	elements = append(elements, "", menuBox, "", footer)
	return lipgloss.JoinVertical(lipgloss.Center, elements...)
}

func (m Model) renderSubmenu() string {
	title := "MODULO SELECCIONADO"
	if m.state == stateSenaMenu {
		title = "SENA ZAJUNA · ADSO FICHA 3545922"
	} else if m.state == stateSimaMenu {
		title = "SIMA UDC · INGENIERIA DE SOFTWARE"
	} else if m.state == stateInglesMenu {
		title = "SIMA INGLES · UDC BILINGUISMO"
	}

	header := AppTitleStyle.Render(title)

	var lines []string
	for i, item := range m.subItems {
		prefix := "   "
		if i == m.subCursor {
			prefix = " › "
		}
		itemText := fmt.Sprintf("%s%s", prefix, item)
		if i == m.subCursor {
			lines = append(lines, MenuItemSelected.Render(itemText))
		} else {
			lines = append(lines, MenuItemNormal.Render(itemText))
		}
	}

	menuBox := MenuBoxStyle.Render(strings.Join(lines, "\n"))
	footer := FooterHelpStyle.Render("↑/k  ↓/j: navegar  ·  enter: seleccionar  ·  esc: volver")

	elements := []string{header, "", menuBox, "", footer}
	return lipgloss.JoinVertical(lipgloss.Center, elements...)
}

func (m Model) renderLoading() string {
	spinnerView := m.spinner.View()
	text := SubtitleStyle.Render(m.loadingText)
	box := ModalBoxStyle.Render(fmt.Sprintf("\n  %s  %s\n", spinnerView, text))
	return box
}

func (m Model) renderList() string {
	header := AppTitleStyle.Render(fmt.Sprintf("%s · %d REGISTROS", m.listTitle, len(m.listItems)))

	if len(m.listItems) == 0 {
		empty := CardNormal.Render(SubtitleStyle.Render("No se encontraron registros."))
		footer := FooterHelpStyle.Render("esc / q: volver")
		return lipgloss.JoinVertical(lipgloss.Center, header, "", empty, "", footer)
	}

	start := 0
	if m.listCursor > 4 {
		start = m.listCursor - 4
	}
	end := start + 5
	if end > len(m.listItems) {
		end = len(m.listItems)
	}

	var cardElements []string
	for i := start; i < end; i++ {
		item := m.listItems[i]

		statusTag := BadgeWarning.Render(fmt.Sprintf("[%s]", item.status))
		if strings.Contains(item.status, "Aprobado") || strings.Contains(item.status, "ENVIADO") || strings.Contains(item.status, "REALIZADO") {
			statusTag = BadgeSuccess.Render(fmt.Sprintf("[%s]", item.status))
		} else if strings.Contains(item.status, "NO ENVIADO") || strings.Contains(item.status, "Deficiente") {
			statusTag = BadgeError.Render(fmt.Sprintf("[%s]", item.status))
		}

		titleStr := item.title
		if len(titleStr) > 52 {
			titleStr = titleStr[:49] + "..."
		}

		cardContent := fmt.Sprintf("%s  %s\n%s", statusTag, CardTitleStyle.Render(titleStr), CardSubtitleStyle.Render("  | "+item.subtitle))

		if i == m.listCursor {
			cardElements = append(cardElements, CardSelected.Render(" › "+cardContent))
		} else {
			cardElements = append(cardElements, CardNormal.Render("   "+cardContent))
		}
	}

	listColumn := lipgloss.JoinVertical(lipgloss.Left, cardElements...)
	footer := FooterHelpStyle.Render("↑/k  ↓/j: navegar  ·  enter: ver detalles  ·  esc / q: volver")

	elements := []string{header, "", listColumn, "", footer}
	return lipgloss.JoinVertical(lipgloss.Center, elements...)
}

func (m Model) renderDetail() string {
	if m.detailItem == nil {
		return ""
	}

	header := ModalTitleStyle.Render("DETALLE DEL REGISTRO")
	divider := ModalDivider.Render(strings.Repeat("─", 58))
	body := CardSubtitleStyle.Render(m.detailItem.details)
	footer := FooterHelpStyle.Render("enter / esc / q: volver a la lista")

	box := ModalBoxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, divider, "", body, "", footer))
	return box
}

func (m Model) renderAuth() string {
	isSena := (m.authPlatform == model.PlatformSena)
	platName := "SENA ZAJUNA"
	if m.authPlatform == model.PlatformSima {
		platName = "SIMA UNIVERSIDAD DE CARTAGENA"
	} else if m.authPlatform == model.PlatformIngles {
		platName = "SIMA INGLES UDC"
	}

	header := ModalTitleStyle.Render(fmt.Sprintf("CONFIGURACION SEGURA · %s", platName))
	subtitle := SubtitleStyle.Render("Cifrado AES-256-GCM nativo · Permisos restringidos 0600")
	divider := ModalDivider.Render(strings.Repeat("─", 58))

	var fields []string

	if isSena {
		docStr := fmt.Sprintf("< %s >  (Espacio para alternar)", m.authDocTypes[m.authDocIdx])
		if m.authFocus == 0 {
			fields = append(fields, fmt.Sprintf("Tipo Documento : %s", MenuItemSelected.Render(docStr)))
		} else {
			fields = append(fields, fmt.Sprintf("Tipo Documento : %s", docStr))
		}
		fields = append(fields, "")
	}

	fields = append(fields, "Usuario / Numero de Documento:")
	fields = append(fields, m.authInputs[0].View(), "")

	fields = append(fields, "Contrasena:")
	fields = append(fields, m.authInputs[1].View(), "")

	saveBtnIdx := 2
	cancelBtnIdx := 3
	if isSena {
		saveBtnIdx = 3
		cancelBtnIdx = 4
	}

	saveBtn := ButtonNormalStyle.Render("Guardar y Probar en Vivo")
	if m.authFocus == saveBtnIdx {
		saveBtn = ButtonActiveStyle.Render("Guardar y Probar en Vivo")
	}

	cancelBtn := ButtonNormalStyle.Render("Cancelar")
	if m.authFocus == cancelBtnIdx {
		cancelBtn = ButtonActiveStyle.Render("Cancelar")
	}

	btnRow := fmt.Sprintf("%s   %s", saveBtn, cancelBtn)
	if m.authLoading {
		btnRow = fmt.Sprintf("%s  %s", m.spinner.View(), SubtitleStyle.Render("Validando sesion contra el servidor..."))
	}

	var alert string
	if m.errorMessage != "" {
		alert = StatusBarError.Render("[ERROR] " + m.errorMessage)
	}

	elements := []string{header, subtitle, divider, ""}
	if alert != "" {
		elements = append(elements, alert, "")
	}
	elements = append(elements, fields...)
	elements = append(elements, btnRow)

	box := ModalBoxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, elements...))
	return box
}
