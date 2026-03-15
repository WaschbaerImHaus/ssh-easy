// Paket main - Tests für known_hosts-Verwaltung
//
// Testet removeKnownHost und appendKnownHost inkl. dem kritischen
// Port-22-Normalisierungsfall: SSH-Callbacks liefern "host:22",
// OpenSSH schreibt aber "host" (ohne Port) in known_hosts.
//
// @author Kurt Ingwer
// @date   2026-03-15 02:00
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemoveKnownHost_StandardPort prüft den häufigsten Fehlerfall:
// Hostname kommt als "192.168.1.1:22" vom SSH-Callback,
// ist in known_hosts aber als "192.168.1.1" (ohne Port) gespeichert.
//
// @date 2026-03-15 02:00
func TestRemoveKnownHost_StandardPort(t *testing.T) {
	// known_hosts mit Eintrag im OpenSSH-Format (Port 22 ohne Portangabe)
	content := "192.168.1.1 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest\n"
	path := writeTempKnownHosts(t, content)

	// hostname wie er vom SSH-Callback kommt (immer mit Port)
	if err := removeKnownHost(path, "192.168.1.1:22"); err != nil {
		t.Fatalf("removeKnownHost fehlgeschlagen: %v", err)
	}

	result := readKnownHosts(t, path)
	if strings.Contains(result, "192.168.1.1") {
		t.Errorf("Eintrag wurde NICHT entfernt (Port-22-Normalisierungsbug).\nKnown_hosts: %q", result)
	}
}

// TestRemoveKnownHost_NonStandardPort prüft Nicht-Standard-Ports.
// known_hosts speichert "[host]:2222", Callback liefert "host:2222".
//
// @date 2026-03-15 02:00
func TestRemoveKnownHost_NonStandardPort(t *testing.T) {
	content := "[192.168.1.1]:2222 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest\n"
	path := writeTempKnownHosts(t, content)

	if err := removeKnownHost(path, "192.168.1.1:2222"); err != nil {
		t.Fatalf("removeKnownHost fehlgeschlagen: %v", err)
	}

	result := readKnownHosts(t, path)
	if strings.Contains(result, "192.168.1.1") {
		t.Errorf("Eintrag wurde NICHT entfernt.\nKnown_hosts: %q", result)
	}
}

// TestRemoveKnownHost_OtherEntriesUnchanged stellt sicher, dass
// andere Einträge beim Entfernen eines Hosts erhalten bleiben.
//
// @date 2026-03-15 02:00
func TestRemoveKnownHost_OtherEntriesUnchanged(t *testing.T) {
	content := "192.168.1.1 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest\n" +
		"192.168.1.2 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOther\n"
	path := writeTempKnownHosts(t, content)

	if err := removeKnownHost(path, "192.168.1.1:22"); err != nil {
		t.Fatalf("removeKnownHost fehlgeschlagen: %v", err)
	}

	result := readKnownHosts(t, path)
	if strings.Contains(result, "192.168.1.1") {
		t.Errorf("Ziel-Eintrag wurde nicht entfernt. Known_hosts: %q", result)
	}
	if !strings.Contains(result, "192.168.1.2") {
		t.Errorf("Anderer Eintrag wurde fälschlicherweise entfernt. Known_hosts: %q", result)
	}
}

// TestRemoveKnownHost_CommentAndEmptyLines stellt sicher, dass
// Kommentare und Leerzeilen erhalten bleiben.
//
// @date 2026-03-15 02:00
func TestRemoveKnownHost_CommentAndEmptyLines(t *testing.T) {
	content := "# Diese Datei wird von ssh-easy verwaltet\n\n" +
		"192.168.1.1 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest\n"
	path := writeTempKnownHosts(t, content)

	if err := removeKnownHost(path, "192.168.1.1:22"); err != nil {
		t.Fatalf("removeKnownHost fehlgeschlagen: %v", err)
	}

	result := readKnownHosts(t, path)
	if !strings.Contains(result, "# Diese Datei") {
		t.Errorf("Kommentar wurde fälschlicherweise entfernt. Known_hosts: %q", result)
	}
}

// TestRemoveKnownHost_NotFound prüft dass kein Fehler entsteht,
// wenn der Hostname gar nicht in known_hosts steht.
//
// @date 2026-03-15 02:00
func TestRemoveKnownHost_NotFound(t *testing.T) {
	content := "192.168.1.2 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOther\n"
	path := writeTempKnownHosts(t, content)

	if err := removeKnownHost(path, "192.168.1.1:22"); err != nil {
		t.Errorf("removeKnownHost sollte keinen Fehler liefern wenn Host nicht gefunden: %v", err)
	}
}

// writeTempKnownHosts erstellt eine temporäre known_hosts-Datei für Tests.
//
// @param t - Test-Kontext
// @param content - Dateiinhalt
// @return string - Pfad zur temporären Datei
// @date 2026-03-15 02:00
func writeTempKnownHosts(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("Temp known_hosts konnte nicht erstellt werden: %v", err)
	}
	return path
}

// readKnownHosts liest eine known_hosts-Datei und gibt den Inhalt zurück.
//
// @param t - Test-Kontext
// @param path - Pfad zur Datei
// @return string - Dateiinhalt
// @date 2026-03-15 02:00
func readKnownHosts(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("known_hosts konnte nicht gelesen werden: %v", err)
	}
	return string(data)
}
