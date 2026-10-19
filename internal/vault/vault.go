package vault

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/e12jassir/atenea/internal/model"
)

var (
	ErrNotFound = errors.New("credenciales no encontradas")
)

type Vault struct {
	baseDir  string
	keyPath  string
	dataHost string
}

type vaultData struct {
	Platforms map[string]model.Credentials `json:"platforms"`
}

func New() (*Vault, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	baseDir := filepath.Join(home, ".config", "atenea")
	return NewWithDir(baseDir)
}

func NewWithDir(baseDir string) (*Vault, error) {
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, err
	}
	_ = os.Chmod(baseDir, 0700)

	v := &Vault{
		baseDir:  baseDir,
		keyPath:  filepath.Join(baseDir, ".vault_go.key"),
		dataHost: filepath.Join(baseDir, "vault_go.enc"),
	}

	return v, nil
}

func (v *Vault) getOrCreateKey() ([]byte, error) {
	if _, err := os.Stat(v.keyPath); err == nil {
		key, err := os.ReadFile(v.keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file %s: %w", v.keyPath, err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid key length in %s: expected 32 bytes, got %d", v.keyPath, len(key))
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to check key file %s: %w", v.keyPath, err)
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %w", err)
	}

	tmpKeyPath := v.keyPath + ".tmp"
	f, err := os.OpenFile(tmpKeyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}

	if _, err := f.Write(key); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpKeyPath)
		return nil, err
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpKeyPath)
		return nil, err
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpKeyPath)
		return nil, err
	}

	if err := os.Chmod(tmpKeyPath, 0600); err != nil {
		_ = os.Remove(tmpKeyPath)
		return nil, err
	}

	if err := os.Rename(tmpKeyPath, v.keyPath); err != nil {
		_ = os.Remove(tmpKeyPath)
		return nil, err
	}
	_ = os.Chmod(v.keyPath, 0600)

	return key, nil
}

func (v *Vault) loadData() (*vaultData, error) {
	if _, err := os.Stat(v.dataHost); os.IsNotExist(err) {
		// Intentar migrar desde archivos existentes
		data := v.migrateExisting()
		if len(data.Platforms) > 0 {
			if err := v.saveData(data); err != nil {
				return nil, err
			}
			return data, nil
		}
		return nil, err
	} else if err != nil {
		return nil, err
	}

	key, err := v.getOrCreateKey()
	if err != nil {
		return nil, err
	}

	ciphertext, err := os.ReadFile(v.dataHost)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("archivo de bóveda dañado")
	}

	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, err
	}

	var data vaultData
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return nil, err
	}

	if data.Platforms == nil {
		data.Platforms = make(map[string]model.Credentials)
	}

	return &data, nil
}

func (v *Vault) saveData(data *vaultData) error {
	key, err := v.getOrCreateKey()
	if err != nil {
		return err
	}

	plaintext, err := json.Marshal(data)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	tmpPath := filepath.Join(filepath.Dir(v.dataHost), filepath.Base(v.dataHost)+".tmp")
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	if _, err := f.Write(ciphertext); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Chmod(tmpPath, 0600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, v.dataHost); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	_ = os.Chmod(v.dataHost, 0600)

	return nil
}

// GetCredentials retorna las credenciales de una plataforma
func (v *Vault) GetCredentials(platform model.Platform) (model.Credentials, error) {
	data, err := v.loadData()
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, os.ErrNotExist) {
			return model.Credentials{}, ErrNotFound
		}
		return model.Credentials{}, err
	}

	creds, ok := data.Platforms[string(platform)]
	if !ok || creds.User == "" || creds.Password == "" {
		return model.Credentials{}, ErrNotFound
	}

	return creds, nil
}

// SaveCredentials guarda o actualiza credenciales cifradas
func (v *Vault) SaveCredentials(creds model.Credentials) error {
	data, err := v.loadData()
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, os.ErrNotExist) {
			data = &vaultData{Platforms: make(map[string]model.Credentials)}
		} else {
			return fmt.Errorf("cannot load existing vault data: %w", err)
		}
	}

	data.Platforms[string(creds.Platform)] = creds
	return v.saveData(data)
}

// HasCredentials verifica si la plataforma tiene credenciales registradas
func (v *Vault) HasCredentials(platform model.Platform) bool {
	creds, err := v.GetCredentials(platform)
	return err == nil && creds.User != "" && creds.Password != ""
}

// migrateExisting importa configuraciones heredadas automáticamente
func (v *Vault) migrateExisting() *vaultData {
	home, _ := os.UserHomeDir()
	data := &vaultData{Platforms: make(map[string]model.Credentials)}

	// SENA .env
	senaEnv := filepath.Join(home, ".config", "sena", "credentials.env")
	if file, err := os.Open(senaEnv); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		var docType, user, pass string
		docType = "CC"
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "SENA_DOC_TYPE=") {
				docType = strings.Trim(strings.TrimPrefix(line, "SENA_DOC_TYPE="), "\"")
			} else if strings.HasPrefix(line, "SENA_USER=") {
				user = strings.Trim(strings.TrimPrefix(line, "SENA_USER="), "\"")
			} else if strings.HasPrefix(line, "SENA_PASS=") {
				pass = strings.Trim(strings.TrimPrefix(line, "SENA_PASS="), "\"")
			}
		}
		if user != "" && pass != "" {
			data.Platforms[string(model.PlatformSena)] = model.Credentials{
				Platform: model.PlatformSena,
				DocType:  docType,
				User:     user,
				Password: pass,
			}
		}
	}

	// SIMA .env
	simaEnv := filepath.Join(home, ".config", "sima", "credentials.env")
	if file, err := os.Open(simaEnv); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		var user, pass, cookie string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "SIMA_USER=") {
				user = strings.Trim(strings.TrimPrefix(line, "SIMA_USER="), "\"")
			} else if strings.HasPrefix(line, "SIMA_PASS=") {
				pass = strings.Trim(strings.TrimPrefix(line, "SIMA_PASS="), "\"")
			} else if strings.HasPrefix(line, "SIMA_COOKIE=") {
				cookie = strings.Trim(strings.TrimPrefix(line, "SIMA_COOKIE="), "\"")
			}
		}
		if user != "" && pass != "" {
			data.Platforms[string(model.PlatformSima)] = model.Credentials{
				Platform: model.PlatformSima,
				User:     user,
				Password: pass,
				Cookie:   cookie,
			}
			data.Platforms[string(model.PlatformIngles)] = model.Credentials{
				Platform: model.PlatformIngles,
				User:     user,
				Password: pass,
				Cookie:   cookie,
			}
		}
	}

	return data
}
