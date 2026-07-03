// Paket main - Tests fuer Security-Fixes aus Build 41
//
// Prueft die in Build 41 umgesetzten Haertungsmassnahmen:
//  - HostKeyChangedError als typisierter Fehler (mit errors.As)
//  - Path-Traversal-Schutz in expandTilde
//  - Symlink-Filter + PEM-Whitelist in discoverSSHKeys
//  - clearPassword ueberschreibt Byte-Slice mit Nullen
//  - looksLikePEMKey erkennt nur echte PEM-Dateien
//
// @author Kurt Ingwer
// @date   2026-04-19 12:00
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestHostKeyChangedErrorType prueft dass der Fehler typisiert erkannt wird
// auch wenn er mit fmt.Errorf("...%w", ...) umhuellt wurde.
func TestHostKeyChangedErrorType(t *testing.T) {
	inner := &HostKeyChangedError{Hostname: "example.com"}
	wrapped := fmt.Errorf("SSH-Verbindung fehlgeschlagen: %w", inner)

	if !IsHostKeyChangedError(wrapped) {
		t.Error("IsHostKeyChangedError sollte auch bei umhuellten Fehlern true liefern")
	}
	if got := parseHostKeyChangedHostname(wrapped); got != "example.com" {
		t.Errorf("parseHostKeyChangedHostname = %q, erwartet %q", got, "example.com")
	}

	// Kontrolltest: Fremder Fehler wird NICHT faelschlich erkannt
	other := errors.New("anderer Fehler ohne Host-Key-Thema")
	if IsHostKeyChangedError(other) {
		t.Error("Fremder Fehler darf nicht als HostKeyChangedError erkannt werden")
	}
	if got := parseHostKeyChangedHostname(other); got != "" {
		t.Errorf("parseHostKeyChangedHostname = %q, erwartet leeren String", got)
	}
}

// TestHostKeyChangedErrorMessage prueft das Format der Fehlermeldung
// (Rueckwaertskompatibilitaet fuer Log-Scanner).
func TestHostKeyChangedErrorMessage(t *testing.T) {
	err := &HostKeyChangedError{Hostname: "192.168.1.1"}
	msg := err.Error()

	if !strings.Contains(msg, "HOST-KEY GEÄNDERT") {
		t.Errorf("Fehlermeldung fehlt Marker: %q", msg)
	}
	if !strings.Contains(msg, "192.168.1.1") {
		t.Errorf("Fehlermeldung fehlt Hostname: %q", msg)
	}
}

// TestExpandTildeNormal prueft den normalen Fall (Tilde am Anfang).
func TestExpandTildeNormal(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Home-Verzeichnis nicht ermittelbar")
	}

	got := expandTilde("~/.ssh/id_ed25519")
	want := filepath.Join(home, ".ssh", "id_ed25519")
	if got != want {
		t.Errorf("expandTilde = %q, erwartet %q", got, want)
	}
}

// TestExpandTildeWithoutTilde prueft dass Pfade ohne Tilde unveraendert bleiben.
func TestExpandTildeWithoutTilde(t *testing.T) {
	in := "/etc/ssh/ssh_config"
	if got := expandTilde(in); got != in {
		t.Errorf("expandTilde = %q, erwartet %q", got, in)
	}
}

// TestExpandTildePathTraversal prueft dass ~/../ Escapes blockiert werden.
func TestExpandTildePathTraversal(t *testing.T) {
	// "~/../../etc/passwd" wuerde expandiert aus Home ausbrechen -
	// muss den Original-Pfad zurueckgeben (nicht das gefaehrliche Ergebnis)
	input := "~/../../etc/passwd"
	got := expandTilde(input)
	if got != input {
		t.Errorf("expandTilde sollte Path-Traversal blockieren: got %q, erwartet %q", got, input)
	}

	// "~/." ist okay (expandiert zu Home)
	home, _ := os.UserHomeDir()
	if home != "" {
		got = expandTilde("~/.")
		// filepath.Join(home, ".") = home (normalisiert)
		if got != home {
			t.Errorf("expandTilde(~/.) = %q, erwartet %q", got, home)
		}

		// "~/subdir/../other" bleibt unter Home - okay
		got = expandTilde("~/subdir/../other")
		want := filepath.Join(home, "other")
		if got != want {
			t.Errorf("expandTilde(~/subdir/../other) = %q, erwartet %q", got, want)
		}
	}
}

// TestLooksLikePEMKeyAcceptsPEM prueft dass echte PEM-Dateien erkannt werden.
func TestLooksLikePEMKeyAcceptsPEM(t *testing.T) {
	tmpDir := t.TempDir()

	cases := map[string]string{
		"openssh-key":  "-----BEGIN OPENSSH PRIVATE KEY-----\nfakedata\n-----END OPENSSH PRIVATE KEY-----\n",
		"rsa-key":      "-----BEGIN RSA PRIVATE KEY-----\nfakedata\n-----END RSA PRIVATE KEY-----\n",
		"ec-key":       "-----BEGIN EC PRIVATE KEY-----\nfakedata\n-----END EC PRIVATE KEY-----\n",
		"private-key":  "-----BEGIN PRIVATE KEY-----\nfakedata\n-----END PRIVATE KEY-----\n",
	}

	for name, content := range cases {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("Setup %s: %v", name, err)
		}
		if !looksLikePEMKey(path) {
			t.Errorf("looksLikePEMKey(%s) = false, erwartet true", name)
		}
	}
}

// TestLooksLikePEMKeyRejectsNonPEM prueft dass Nicht-PEM-Dateien abgelehnt werden.
func TestLooksLikePEMKeyRejectsNonPEM(t *testing.T) {
	tmpDir := t.TempDir()

	cases := map[string]string{
		"config":    "Host example\n    User foo\n",
		"empty":     "",
		"binary":    "\x00\x01\x02\x03",
		"text":      "Dies ist ein zufaelliger Text",
		"short":     "AB",
		"json":      `{"key": "value"}`,
	}

	for name, content := range cases {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("Setup %s: %v", name, err)
		}
		if looksLikePEMKey(path) {
			t.Errorf("looksLikePEMKey(%s) = true, erwartet false", name)
		}
	}

	// Nicht existierende Datei
	if looksLikePEMKey(filepath.Join(tmpDir, "gibts-nicht")) {
		t.Error("looksLikePEMKey(fehlende Datei) sollte false sein")
	}
}

// TestDiscoverSSHKeysSkipsSymlinks prueft dass Symlinks ignoriert werden.
// Wird unter Windows uebersprungen (Symlinks brauchen Admin-Rechte).
func TestDiscoverSSHKeysSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Symlinks unter Windows brauchen Admin-Rechte")
	}

	// Wir testen discoverSSHKeys indirekt, indem wir einen temporaeren
	// Fake-Home setzen. Da discoverSSHKeys os.UserHomeDir() nutzt, setzen
	// wir HOME explizit um, damit wir einen kontrollierbaren ~/.ssh/ bekommen.
	tmpHome := t.TempDir()
	sshDir := filepath.Join(tmpHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("SSH-Dir Setup: %v", err)
	}

	// Echte PEM-Datei (soll gefunden werden)
	realKey := filepath.Join(sshDir, "id_real")
	if err := os.WriteFile(realKey, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nx\n"), 0600); err != nil {
		t.Fatalf("Real-Key Setup: %v", err)
	}

	// Symlink auf eine sensitive Datei ausserhalb (soll NICHT gefolgt werden)
	sensitive := filepath.Join(tmpHome, "secret.txt")
	if err := os.WriteFile(sensitive, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nx\n"), 0600); err != nil {
		t.Fatalf("Secret Setup: %v", err)
	}
	symlinkPath := filepath.Join(sshDir, "id_symlink")
	if err := os.Symlink(sensitive, symlinkPath); err != nil {
		t.Fatalf("Symlink Setup: %v", err)
	}

	// HOME umleiten damit discoverSSHKeys unseren Fake-Home findet
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv: %v", err)
	}

	found := discoverSSHKeys("", nil)

	// Der Symlink darf NICHT in der Liste auftauchen
	for _, p := range found {
		if filepath.Base(p) == "id_symlink" {
			t.Errorf("discoverSSHKeys hat Symlink gefunden: %s", p)
		}
	}

	// Die echte Datei muss vorhanden sein (Sanity-Check)
	hasReal := false
	for _, p := range found {
		if filepath.Base(p) == "id_real" {
			hasReal = true
		}
	}
	if !hasReal {
		t.Errorf("Echter Key wurde nicht gefunden. Liste: %v", found)
	}
}

// TestDiscoverSSHKeysSkipsNonPEM prueft dass Nicht-PEM-Dateien nicht
// faelschlich als Keys erkannt werden (vs. alte Blacklist-Logik).
func TestDiscoverSSHKeysSkipsNonPEM(t *testing.T) {
	tmpHome := t.TempDir()
	sshDir := filepath.Join(tmpHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Nicht-PEM-Datei mit einem Namen der nicht in der alten Blacklist stand
	// (z.B. "rc", "environment.old", "my_notes.txt")
	if err := os.WriteFile(filepath.Join(sshDir, "rc"), []byte("# shell rc\n"), 0600); err != nil {
		t.Fatalf("Setup rc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "my_notes.txt"), []byte("Notizen zum SSH\n"), 0600); err != nil {
		t.Fatalf("Setup notes: %v", err)
	}

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpHome)
	if runtime.GOOS == "windows" {
		origUserProfile := os.Getenv("USERPROFILE")
		defer os.Setenv("USERPROFILE", origUserProfile)
		os.Setenv("USERPROFILE", tmpHome)
	}

	found := discoverSSHKeys("", nil)
	if len(found) != 0 {
		t.Errorf("discoverSSHKeys fand Nicht-PEM-Dateien: %v", found)
	}
}

// TestClearPasswordZeroesBytes prueft dass clearPassword das Byte-Slice
// tatsaechlich mit Nullen ueberschreibt und nicht nur die Referenz loescht.
func TestClearPasswordZeroesBytes(t *testing.T) {
	// Wir behalten eine eigene Referenz auf das interne Slice, um pruefen
	// zu koennen dass die Bytes selbst ueberschrieben wurden.
	original := []byte("supergeheim")
	mc := &ManagedConnection{password: original}

	// Kopie der Referenz VOR dem Clear merken - nach dem Clear muessten
	// die Bytes an dieser Speicherstelle 0 sein.
	backingRef := mc.password

	mc.clearPassword()

	// Das Slice im Struct sollte jetzt nil sein
	if mc.password != nil {
		t.Error("clearPassword sollte die Slice-Referenz auf nil setzen")
	}

	// Die Bytes im urspruenglichen Backing-Array muessen 0 sein
	for i, b := range backingRef {
		if b != 0 {
			t.Errorf("Byte %d nach clearPassword = %d, erwartet 0 (backingRef=%v)", i, b, backingRef)
			break
		}
	}
}

// TestClearPasswordSafeOnNil prueft dass clearPassword auf einem leeren
// ManagedConnection nicht panikt.
func TestClearPasswordSafeOnNil(t *testing.T) {
	mc := &ManagedConnection{} // password = nil
	// Darf nicht paniken
	mc.clearPassword()
	if mc.password != nil {
		t.Error("clearPassword auf nil-Passwort sollte nil lassen")
	}
}

// TestRestrictFilePermissionsNoError prueft dass restrictFilePermissions
// auf einer existierenden Datei fehlerfrei ausgefuehrt werden kann.
// Der tatsaechliche Effekt (0600 bzw. DACL) wird plattformspezifisch nicht
// tief geprueft - nur der Smoke-Test dass keine Panik/kein Fehler auftritt.
func TestRestrictFilePermissionsNoError(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sensitive.txt")
	if err := os.WriteFile(path, []byte("geheim"), 0644); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if err := restrictFilePermissions(path); err != nil {
		t.Errorf("restrictFilePermissions schlug fehl: %v", err)
	}

	// Unter Unix: Mode muss jetzt 0600 sein
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if mode := info.Mode().Perm(); mode != 0600 {
			t.Errorf("Permissions nach restrictFilePermissions = %o, erwartet 0600", mode)
		}
	}
}
