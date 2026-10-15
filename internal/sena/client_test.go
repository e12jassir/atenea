package sena

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/e12jassir/atenea/internal/model"
	"github.com/e12jassir/atenea/internal/vault"
)

func TestSenaLogin(t *testing.T) {
	v, err := vault.New()
	if err != nil {
		t.Skip("Vault no disponible:", err)
	}
	creds, err := v.GetCredentials(model.PlatformSena)
	if err != nil {
		t.Skip("Credenciales SENA no configuradas:", err)
	}

	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}

	err = client.Login(creds.DocType, creds.User, creds.Password)
	if err != nil {
		uRoot, _ := url.Parse("https://zajuna.sena.edu.co")
		uZajuna, _ := url.Parse("https://zajuna.sena.edu.co/zajuna/")
		fmt.Printf("Cookies en root (%s): %v\n", uRoot, client.jar.Cookies(uRoot))
		fmt.Printf("Cookies en /zajuna/ (%s): %v\n", uZajuna, client.jar.Cookies(uZajuna))
		t.Fatalf("Login falló: %v", err)
	}
	t.Log("Login exitoso!")
}
