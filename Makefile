BINARY_NAME=atenea
SRC_DIR=./cmd/atenea
INSTALL_PATH=$(HOME)/.local/bin/$(BINARY_NAME)

.PHONY: all build install clean test

all: build

build:
	@echo "==> Compilando $(BINARY_NAME) optimizado (sin símbolos de depuración)..."
	@mkdir -p bin
	go build -ldflags="-s -w" -o bin/$(BINARY_NAME) $(SRC_DIR)
	@echo "✓ Binario generado en bin/$(BINARY_NAME)"

install: build
	@echo "==> Instalando en $(INSTALL_PATH)..."
	@mkdir -p $(HOME)/.local/bin
	cp bin/$(BINARY_NAME) $(INSTALL_PATH)
	chmod +x $(INSTALL_PATH)
	@echo "✓ Instalación completa. Ya puedes ejecutar '$(BINARY_NAME)' en cualquier terminal."

clean:
	@echo "==> Limpiando..."
	rm -rf bin/

test:
	@echo "==> Ejecutando pruebas..."
	go test -v ./...
