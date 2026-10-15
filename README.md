# 🏛️ Atenea (Go Native Edition)

**Suite de Inteligencia y Gestión Académica Unificada**  
*SENA (Zajuna LMS ADSO) + Universidad de Cartagena (SIMA e Inglés Institucional)*

Construida 100% en Go nativo con el framework TUI **Charmbracelet (Bubble Tea + Lip Gloss + Bubbles)**.

---

## ⚡ Características Principales

- **Cero Dependencias en Runtime**: Un único binario compilado estáticamente. No requiere Python, pip ni módulos externos.
- **Bóveda Criptográfica Segura**: Cifrado local **AES-256-GCM** para credenciales de acceso (`~/.config/atenea/vault_go.enc`) con permisos estrictos `0600`.
- **TUI Moderna a 60 FPS**: Componentes reactivos, spinners fluidos, listas interactivas y formularios de autenticación con contraseñas enmascaradas.
- **Sincronización Concurrente**: Clientes HTTP nativos optimizados para Zajuna LMS y Moodle SIMA UDC.

---

## 🚀 Compilación e Instalación

```bash
# Compilar binario optimizado
make build

# Instalar en ~/.local/bin/atenea
make install
```

---

## 🎮 Uso

```bash
# Iniciar la interfaz visual interactiva
atenea

# Ayuda y comandos
atenea --help
```

---

## 📁 Estructura del Proyecto

```
atenea/
├── cmd/
│   └── atenea/
│       └── main.go       # Punto de entrada y CLI
├── internal/
│   ├── model/           # Modelos de dominio
│   ├── vault/           # Bóveda cifrada AES-256-GCM
│   ├── sena/            # Cliente HTTP Zajuna LMS
│   ├── sima/            # Cliente HTTP SIMA UDC e Inglés
│   └── ui/              # TUI Bubble Tea con estilos Lip Gloss
├── Makefile             # Automatización de build e instalación
└── go.mod
```
