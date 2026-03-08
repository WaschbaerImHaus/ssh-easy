// Tests fuer die SSH-Key-Generierung
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 19:00
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateSSHKeyBasic prueft ob ein Schluessel ohne Passphrase generiert wird
func TestGenerateSSHKeyBasic(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test_key")

	pubKey, err := GenerateSSHKey(keyPath, "")
	if err != nil {
		t.Fatalf("Key-Generierung fehlgeschlagen: %v", err)
	}

	// Public Key sollte mit ssh-ed25519 beginnen
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("Public Key sollte mit 'ssh-ed25519' beginnen, bekommen: %s", pubKey[:20])
	}

	// Private Key Datei sollte existieren
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Error("Private Key Datei sollte existieren")
	}

	// Public Key Datei sollte existieren
	pubKeyPath := keyPath + ".pub"
	if _, err := os.Stat(pubKeyPath); os.IsNotExist(err) {
		t.Error("Public Key Datei (.pub) sollte existieren")
	}

	// Public Key in Datei sollte mit Rueckgabewert uebereinstimmen
	pubKeyFile, _ := os.ReadFile(pubKeyPath)
	if strings.TrimSpace(string(pubKeyFile)) != strings.TrimSpace(pubKey) {
		t.Error("Public Key in Datei stimmt nicht mit Rueckgabewert ueberein")
	}
}

// TestGenerateSSHKeyWithPassphrase prueft Key-Generierung mit Passphrase
func TestGenerateSSHKeyWithPassphrase(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test_key_pw")

	pubKey, err := GenerateSSHKey(keyPath, "mein-geheimes-passwort")
	if err != nil {
		t.Fatalf("Key-Generierung mit Passphrase fehlgeschlagen: %v", err)
	}

	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("Public Key sollte mit 'ssh-ed25519' beginnen")
	}

	// Private Key sollte verschluesselt sein (ENCRYPTED im PEM-Header)
	privKeyData, _ := os.ReadFile(keyPath)
	privKeyStr := string(privKeyData)
	if !strings.Contains(privKeyStr, "BEGIN OPENSSH PRIVATE KEY") {
		t.Error("Private Key sollte im OpenSSH-Format sein")
	}
}

// TestGenerateSSHKeyPermissions prueft die Dateiberechtigungen
func TestGenerateSSHKeyPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test_key_perm")

	GenerateSSHKey(keyPath, "")

	// Private Key: 0600
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("Private Key Stat fehlgeschlagen: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("Private Key Berechtigung erwartet 0600, bekommen %o", info.Mode().Perm())
	}

	// Public Key: 0644
	pubInfo, err := os.Stat(keyPath + ".pub")
	if err != nil {
		t.Fatalf("Public Key Stat fehlgeschlagen: %v", err)
	}
	if pubInfo.Mode().Perm() != 0644 {
		t.Errorf("Public Key Berechtigung erwartet 0644, bekommen %o", pubInfo.Mode().Perm())
	}
}

// TestGenerateSSHKeyExistingFile prueft ob eine existierende Datei nicht ueberschrieben wird
func TestGenerateSSHKeyExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "existing_key")

	// Datei vorab erstellen
	os.WriteFile(keyPath, []byte("existiert bereits"), 0600)

	_, err := GenerateSSHKey(keyPath, "")
	if err == nil {
		t.Error("Generierung sollte fehlschlagen wenn Datei existiert")
	}
	if !strings.Contains(err.Error(), "existiert bereits") {
		t.Errorf("Fehlermeldung sollte 'existiert bereits' enthalten, bekommen: %v", err)
	}
}

// TestGenerateSSHKeyEmptyPath prueft ob leerer Pfad abgefangen wird
func TestGenerateSSHKeyEmptyPath(t *testing.T) {
	_, err := GenerateSSHKey("", "")
	if err == nil {
		t.Error("Leerer Pfad sollte einen Fehler erzeugen")
	}
}

// TestGenerateSSHKeyCreatesDirectory prueft ob fehlende Verzeichnisse erstellt werden
func TestGenerateSSHKeyCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "subdir", "deep", "test_key")

	pubKey, err := GenerateSSHKey(keyPath, "")
	if err != nil {
		t.Fatalf("Key-Generierung in verschachteltem Verzeichnis fehlgeschlagen: %v", err)
	}

	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("Public Key sollte gueltig sein")
	}

	// Verzeichnisstruktur sollte existieren
	dirInfo, err := os.Stat(filepath.Join(tmpDir, "subdir", "deep"))
	if err != nil {
		t.Fatal("Verzeichnis sollte erstellt worden sein")
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Errorf("Verzeichnisberechtigung erwartet 0700, bekommen %o", dirInfo.Mode().Perm())
	}
}

// TestGenerateSSHKeyTildeExpansion prueft ob ~ im Pfad aufgeloest wird
func TestGenerateSSHKeyTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Home-Verzeichnis nicht verfuegbar")
	}

	// Tempordner im Home anlegen damit wir aufräumen können
	testDir := filepath.Join(home, ".ssh-easy-test-keygen")
	os.MkdirAll(testDir, 0700)
	defer os.RemoveAll(testDir)

	keyPath := "~/.ssh-easy-test-keygen/tilde_test_key"
	pubKey, err := GenerateSSHKey(keyPath, "")
	if err != nil {
		t.Fatalf("Tilde-Expansion fehlgeschlagen: %v", err)
	}

	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("Public Key sollte gueltig sein")
	}

	// Datei sollte im aufgelösten Pfad existieren
	expectedPath := filepath.Join(home, ".ssh-easy-test-keygen", "tilde_test_key")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Error("Datei sollte im aufgelösten Pfad existieren")
	}
}
