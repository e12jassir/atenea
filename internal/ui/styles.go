package ui

import "github.com/charmbracelet/lipgloss"

// AsciiLogo en fuente ANSI Shadow / Block Font (estilo Kickoff / Neovim)
const AsciiLogo = `█████╗ ████████╗███████╗███╗   ██╗███████╗ █████╗ 
██╔══██╗╚══██╔══╝██╔════╝████╗  ██║██╔════╝██╔══██╗
███████║   ██║   █████╗  ██╔██╗ ██║█████╗  ███████║
██╔══██║   ██║   ██╔══╝  ██║╚██╗██║██╔══╝  ██╔══██║
██║  ██║   ██║   ███████╗██║ ╚████║███████╗██║  ██║
╚═╝  ╚═╝   ╚═╝   ╚══════╝╚═╝  ╚═══╝╚══════╝╚═╝  ╚═╝`

var (
	// Neutral Theme-Agnostic Dark Palette (Zinc / Dark Theme estilo Kickoff)
	ColorHighlight      = lipgloss.Color("#FFFFFF") // Blanco nítido
	ColorText           = lipgloss.Color("#F4F4F5") // Zinc 100
	ColorTextMuted      = lipgloss.Color("#D4D4D8") // Zinc 300
	ColorCategory       = lipgloss.Color("#A1A1AA") // Zinc 400
	ColorMuted          = lipgloss.Color("#71717A") // Zinc 500
	ColorBorder         = lipgloss.Color("#3F3F46") // Zinc 700 (borde sutil)
	ColorBorderSelected = lipgloss.Color("#E4E4E7") // Zinc 200 (borde de selección nítido)
	ColorError          = lipgloss.Color("#EF4444") // Coral Red
	ColorSuccess        = lipgloss.Color("#22C55E") // Bright Green (Badges/Éxito)
	ColorAccent         = lipgloss.Color("#38BDF8") // Sky Blue / Cyan
	ColorWarning        = lipgloss.Color("#F59E0B") // Amber
	ColorBgCard         = lipgloss.Color("#18181B") // Zinc 900

	// Tipografía y Cabeceras
	AsciiLogoStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorHighlight)

	AppTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorHighlight).
			Padding(0, 1)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	// Contenedor de Menú Principal (Idéntico al estilo Kickoff)
	MenuBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2).
			Width(64)

	CategoryHeaderStyle = lipgloss.NewStyle().
				Foreground(ColorCategory).
				Bold(true).
				MarginTop(1).
				MarginBottom(0)

	MenuItemNormal = lipgloss.NewStyle().
			Foreground(ColorTextMuted)

	MenuItemSelected = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorHighlight)

	// Tarjetas de Lista (Evidencias y Eventos)
	CardBoxStyle = lipgloss.NewStyle().
			Width(70)

	CardNormal = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1).
			Width(70).
			MarginBottom(1)

	CardSelected = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorderSelected).
			Padding(0, 1).
			Width(70).
			MarginBottom(1)

	CardTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText)

	CardSubtitleStyle = lipgloss.NewStyle().
				Foreground(ColorMuted)

	// Píldoras y Badges
	BadgeSuccess = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	BadgeWarning = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)

	BadgeError = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	ActivePillTab = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorHighlight).
			Background(ColorBorder).
			Padding(0, 1).
			MarginRight(1)

	InactivePillTab = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Padding(0, 1).
			MarginRight(1)

	TabsRowStyle = lipgloss.NewStyle().
			MarginBottom(1)

	// Modales flotantes (Auth y Detalle)
	ModalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorderSelected).
			Padding(1, 2).
			Width(64)

	ModalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorHighlight).
			MarginBottom(1)

	ModalDivider = lipgloss.NewStyle().
			Foreground(ColorBorder)

	// Botones
	ButtonActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorHighlight).
				Background(ColorAccent).
				Padding(0, 2)

	ButtonNormalStyle = lipgloss.NewStyle().
				Foreground(ColorMuted).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorBorder).
				Padding(0, 1)

	// Pie de página con atajos limpios
	FooterHelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			MarginTop(1)

	FooterKeyStyle = lipgloss.NewStyle().
			Foreground(ColorText).
			Bold(true)

	StatusBarSuccess = lipgloss.NewStyle().
				Foreground(ColorSuccess).
				Bold(true)

	StatusBarError = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)
)
