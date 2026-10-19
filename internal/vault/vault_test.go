package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/e12jassir/atenea/internal/model"
)

func TestVault_New(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	v, err := New()
	if err != nil {
		t.Fatalf("unexpected error from New(): %v", err)
	}
	expectedDir := filepath.Join(tempDir, ".config", "atenea")
	if v.baseDir != expectedDir {
		t.Errorf("expected baseDir %s, got %s", expectedDir, v.baseDir)
	}
}

func TestVault_SaveAndGetCredentials(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	v, err := NewWithDir(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize vault: %v", err)
	}

	// 1. Initial state: no credentials
	if v.HasCredentials(model.PlatformSena) {
		t.Errorf("expected HasCredentials to be false initially")
	}
	_, err = v.GetCredentials(model.PlatformSena)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for non-existent credentials, got %v", err)
	}

	// 2. Save SENA credentials
	senaCreds := model.Credentials{
		Platform: model.PlatformSena,
		DocType:  "CC",
		User:     "100123456",
		Password: "senaSecurePassword#2026",
	}
	if err := v.SaveCredentials(senaCreds); err != nil {
		t.Fatalf("failed to save sena credentials: %v", err)
	}

	if !v.HasCredentials(model.PlatformSena) {
		t.Errorf("expected HasCredentials(sena) to be true after save")
	}

	gotSena, err := v.GetCredentials(model.PlatformSena)
	if err != nil {
		t.Fatalf("failed to get sena credentials: %v", err)
	}
	if gotSena != senaCreds {
		t.Errorf("credentials mismatch: got %+v, want %+v", gotSena, senaCreds)
	}

	// 3. Save SIMA credentials and ensure both coexist
	simaCreds := model.Credentials{
		Platform: model.PlatformSima,
		User:     "sima.user",
		Password: "simaSecretPassword",
		Cookie:   "MoodleSession=abcdef123456",
	}
	if err := v.SaveCredentials(simaCreds); err != nil {
		t.Fatalf("failed to save sima credentials: %v", err)
	}

	gotSima, err := v.GetCredentials(model.PlatformSima)
	if err != nil {
		t.Fatalf("failed to get sima credentials: %v", err)
	}
	if gotSima != simaCreds {
		t.Errorf("credentials mismatch: got %+v, want %+v", gotSima, simaCreds)
	}

	// Re-verify SENA credentials were not overwritten
	gotSenaAgain, err := v.GetCredentials(model.PlatformSena)
	if err != nil {
		t.Fatalf("failed to re-get sena credentials: %v", err)
	}
	if gotSenaAgain != senaCreds {
		t.Errorf("sena credentials corrupted after saving sima: got %+v, want %+v", gotSenaAgain, senaCreds)
	}

	// 4. Persistence across new Vault instance
	v2, err := NewWithDir(tempDir)
	if err != nil {
		t.Fatalf("failed to open vault in same dir: %v", err)
	}

	loadedSena, err := v2.GetCredentials(model.PlatformSena)
	if err != nil || loadedSena != senaCreds {
		t.Errorf("failed to reload sena creds: err=%v, val=%+v", err, loadedSena)
	}
	loadedSima, err := v2.GetCredentials(model.PlatformSima)
	if err != nil || loadedSima != simaCreds {
		t.Errorf("failed to reload sima creds: err=%v, val=%+v", err, loadedSima)
	}
}

func TestVault_SaveCredentials_PreservesDataOnCorruption(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	v, err := NewWithDir(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize vault: %v", err)
	}

	// 1. Save valid SENA credentials first
	senaCreds := model.Credentials{
		Platform: model.PlatformSena,
		DocType:  "CC",
		User:     "100123456",
		Password: "senaSecurePassword#2026",
	}
	if err := v.SaveCredentials(senaCreds); err != nil {
		t.Fatalf("failed to save sena credentials: %v", err)
	}

	// 2. Corrupt the encrypted vault file (invalid ciphertext, longer than nonce)
	corruptPayload := []byte("corrupt_encrypted_payload_that_will_fail_aes_gcm_open_authentication_check_1234567890")
	if err := os.WriteFile(v.dataHost, corruptPayload, 0600); err != nil {
		t.Fatalf("failed to write corrupt vault file: %v", err)
	}

	// 3. Attempt to save SIMA credentials
	simaCreds := model.Credentials{
		Platform: model.PlatformSima,
		User:     "sima.user",
		Password: "simaSecretPassword",
	}
	err = v.SaveCredentials(simaCreds)
	if err == nil {
		t.Fatal("expected SaveCredentials to fail on corrupt vault file, but got nil")
	}

	if !strings.Contains(err.Error(), "cannot load existing vault data") {
		t.Errorf("expected error to contain 'cannot load existing vault data', got: %v", err)
	}

	// 4. Verify existing file on disk was NOT wiped or overwritten
	currentData, readErr := os.ReadFile(v.dataHost)
	if readErr != nil {
		t.Fatalf("failed to read vault file after error: %v", readErr)
	}
	if !bytes.Equal(currentData, corruptPayload) {
		t.Fatalf("vault file was overwritten or wiped despite error! got %s, want %s", string(currentData), string(corruptPayload))
	}

	// 5. Test truncated vault file (< nonce size)
	shortPayload := []byte("tiny")
	if err := os.WriteFile(v.dataHost, shortPayload, 0600); err != nil {
		t.Fatalf("failed to write truncated vault file: %v", err)
	}

	err = v.SaveCredentials(simaCreds)
	if err == nil {
		t.Fatal("expected SaveCredentials to fail on truncated vault file, but got nil")
	}
	if !strings.Contains(err.Error(), "cannot load existing vault data") {
		t.Errorf("expected error to contain 'cannot load existing vault data', got: %v", err)
	}

	currentData, readErr = os.ReadFile(v.dataHost)
	if readErr != nil {
		t.Fatalf("failed to read vault file after error: %v", readErr)
	}
	if !bytes.Equal(currentData, shortPayload) {
		t.Fatalf("truncated vault file was overwritten! got %s, want %s", string(currentData), string(shortPayload))
	}
}

func TestVault_SaveCredentials_PreservesDataOnCorruptKey(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	v, err := NewWithDir(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize vault: %v", err)
	}

	// 1. Save valid credentials
	senaCreds := model.Credentials{
		Platform: model.PlatformSena,
		DocType:  "CC",
		User:     "100123456",
		Password: "senaSecurePassword#2026",
	}
	if err := v.SaveCredentials(senaCreds); err != nil {
		t.Fatalf("failed to save sena credentials: %v", err)
	}

	validVaultData, err := os.ReadFile(v.dataHost)
	if err != nil {
		t.Fatalf("failed to read valid vault data: %v", err)
	}

	// 2. Corrupt key file to an invalid size (< 32 bytes)
	badKey := []byte("short_16_byte_key")
	if err := os.WriteFile(v.keyPath, badKey, 0600); err != nil {
		t.Fatalf("failed to corrupt key file: %v", err)
	}

	// 3. Attempt to save new credentials
	simaCreds := model.Credentials{
		Platform: model.PlatformSima,
		User:     "sima.user",
		Password: "simaSecretPassword",
	}
	err = v.SaveCredentials(simaCreds)
	if err == nil {
		t.Fatal("expected SaveCredentials to fail when key file is corrupted, but got nil")
	}

	if !strings.Contains(err.Error(), "cannot load existing vault data") {
		t.Errorf("expected error to contain 'cannot load existing vault data', got: %v", err)
	}

	// 4. Verify key file was NOT overwritten with a newly generated key
	currentKey, err := os.ReadFile(v.keyPath)
	if err != nil {
		t.Fatalf("failed to read key file: %v", err)
	}
	if !bytes.Equal(currentKey, badKey) {
		t.Fatalf("key file was overwritten/regenerated! got %d bytes, want %d bytes", len(currentKey), len(badKey))
	}

	// 5. Verify vault data was NOT overwritten
	currentVaultData, err := os.ReadFile(v.dataHost)
	if err != nil {
		t.Fatalf("failed to read vault data: %v", err)
	}
	if !bytes.Equal(currentVaultData, validVaultData) {
		t.Fatal("vault data was modified despite corrupt key error!")
	}
}

func TestVault_GetOrCreateKey_FailsClosed(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	v, err := NewWithDir(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize vault: %v", err)
	}

	// 1. If key file exists but is invalid size (e.g. 10 bytes), must fail closed
	badKey := []byte("0123456789")
	if err := os.WriteFile(v.keyPath, badKey, 0600); err != nil {
		t.Fatalf("failed to write bad key: %v", err)
	}

	_, err = v.getOrCreateKey()
	if err == nil {
		t.Fatal("expected getOrCreateKey to return error for invalid key length, got nil")
	}
	if !strings.Contains(err.Error(), "invalid key length") {
		t.Errorf("expected 'invalid key length' error, got %v", err)
	}

	// Ensure the key file on disk was NOT replaced
	keyOnDisk, _ := os.ReadFile(v.keyPath)
	if !bytes.Equal(keyOnDisk, badKey) {
		t.Errorf("bad key was modified/overwritten: got %s, want %s", string(keyOnDisk), string(badKey))
	}

	// 2. If valid 32-byte key exists, it must be returned without modification
	validKey := bytes.Repeat([]byte("k"), 32)
	if err := os.WriteFile(v.keyPath, validKey, 0600); err != nil {
		t.Fatalf("failed to write valid key: %v", err)
	}

	key, err := v.getOrCreateKey()
	if err != nil {
		t.Fatalf("unexpected error getting key: %v", err)
	}
	if !bytes.Equal(key, validKey) {
		t.Errorf("key returned does not match key on disk: got %x, want %x", key, validKey)
	}

	// 3. If key file does not exist, it should generate a fresh 32-byte key
	_ = os.Remove(v.keyPath)
	generatedKey, err := v.getOrCreateKey()
	if err != nil {
		t.Fatalf("unexpected error generating key: %v", err)
	}
	if len(generatedKey) != 32 {
		t.Errorf("expected 32 byte key, got %d", len(generatedKey))
	}

	fi, err := os.Stat(v.keyPath)
	if err != nil {
		t.Fatalf("expected key file to exist: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected key file permissions 0600, got %#o", fi.Mode().Perm())
	}
}

func TestVault_AtomicWrite(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	v, err := NewWithDir(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize vault: %v", err)
	}

	creds := model.Credentials{
		Platform: model.PlatformSena,
		DocType:  "CC",
		User:     "user1",
		Password: "password1",
	}
	if err := v.SaveCredentials(creds); err != nil {
		t.Fatalf("failed to save credentials: %v", err)
	}

	// Verify vault file exists
	fi, err := os.Stat(v.dataHost)
	if err != nil {
		t.Fatalf("vault file does not exist: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected vault file mode 0600, got %#o", fi.Mode().Perm())
	}

	// Verify temporary file does not remain
	tmpPath := filepath.Join(filepath.Dir(v.dataHost), filepath.Base(v.dataHost)+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("temporary file %s still exists after save", tmpPath)
	}

	// Check key file mode
	keyFi, err := os.Stat(v.keyPath)
	if err != nil {
		t.Fatalf("key file does not exist: %v", err)
	}
	if keyFi.Mode().Perm() != 0600 {
		t.Errorf("expected key file mode 0600, got %#o", keyFi.Mode().Perm())
	}

	// Check directory mode
	dirFi, err := os.Stat(v.baseDir)
	if err != nil {
		t.Fatalf("directory does not exist: %v", err)
	}
	if dirFi.Mode().Perm() != 0700 {
		t.Errorf("expected dir mode 0700, got %#o", dirFi.Mode().Perm())
	}
}

func TestVault_MigrateExisting(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	// Create legacy SENA and SIMA credentials files
	senaDir := filepath.Join(tempDir, ".config", "sena")
	if err := os.MkdirAll(senaDir, 0700); err != nil {
		t.Fatalf("failed to create fake sena dir: %v", err)
	}
	senaContent := `SENA_DOC_TYPE="TI"
SENA_USER="987654321"
SENA_PASS="legacyPassword123"
`
	if err := os.WriteFile(filepath.Join(senaDir, "credentials.env"), []byte(senaContent), 0600); err != nil {
		t.Fatalf("failed to write fake sena credentials: %v", err)
	}

	simaDir := filepath.Join(tempDir, ".config", "sima")
	if err := os.MkdirAll(simaDir, 0700); err != nil {
		t.Fatalf("failed to create fake sima dir: %v", err)
	}
	simaContent := `SIMA_USER="legacy.sima"
SIMA_PASS="legacySimaPass"
SIMA_COOKIE="legacyCookie123"
`
	if err := os.WriteFile(filepath.Join(simaDir, "credentials.env"), []byte(simaContent), 0600); err != nil {
		t.Fatalf("failed to write fake sima credentials: %v", err)
	}

	vaultDir := filepath.Join(tempDir, ".config", "atenea")
	v, err := NewWithDir(vaultDir)
	if err != nil {
		t.Fatalf("failed to initialize vault: %v", err)
	}

	// When GetCredentials is first called, migration should happen automatically
	senaCreds, err := v.GetCredentials(model.PlatformSena)
	if err != nil {
		t.Fatalf("failed to get migrated sena credentials: %v", err)
	}
	if senaCreds.DocType != "TI" || senaCreds.User != "987654321" || senaCreds.Password != "legacyPassword123" {
		t.Errorf("unexpected migrated sena creds: %+v", senaCreds)
	}

	simaCreds, err := v.GetCredentials(model.PlatformSima)
	if err != nil {
		t.Fatalf("failed to get migrated sima credentials: %v", err)
	}
	if simaCreds.User != "legacy.sima" || simaCreds.Password != "legacySimaPass" || simaCreds.Cookie != "legacyCookie123" {
		t.Errorf("unexpected migrated sima creds: %+v", simaCreds)
	}

	// English platform should also be populated from SIMA
	inglesCreds, err := v.GetCredentials(model.PlatformIngles)
	if err != nil {
		t.Fatalf("failed to get migrated ingles credentials: %v", err)
	}
	if inglesCreds.User != "legacy.sima" || inglesCreds.Password != "legacySimaPass" {
		t.Errorf("unexpected migrated ingles creds: %+v", inglesCreds)
	}
}
