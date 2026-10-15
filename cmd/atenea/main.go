package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/atenea/internal/ui"
	"github.com/e12jassir/atenea/internal/vault"
)

func printHelp() {
	fmt.Println(`🏛️ ATENEA — Suite de Inteligencia Académica (Edición Go)
SENA Zajuna ADSO + Universidad de Cartagena (SIMA e Inglés)

Uso:
  atenea                Inicia la TUI visual interactiva (Bubble Tea)
  atenea auth [modulo]  Configura credenciales cifradas (sena, sima, ingles)
  atenea -h, --help     Muestra esta ayuda
  atenea version        Muestra la versión compilada

Características:
  • Binario único compilado nativamente en Go (Cero dependencias)
  • Cifrado AES-256-GCM para credenciales académicas (Permisos 0600)
  • Interfaz visual sin parpadeos a 60 FPS con temas modernos
  • Conexión HTTP nativa concurrente con gestión de cookies de sesión
`)
}

func main() {
	v, err := vault.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error inicializando bóveda segura: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		arg := os.Args[1]
		switch arg {
		case "-h", "--help", "help":
			printHelp()
			return
		case "version", "-v", "--version":
			fmt.Println("Atenea v2.0.0 (Go Native • Charm Bubble Tea)")
			return
		}
	}

	// Iniciar la TUI interactiva en pantalla alterna
	p := tea.NewProgram(ui.NewModel(v), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error al ejecutar Atenea: %v\n", err)
		os.Exit(1)
	}
}
