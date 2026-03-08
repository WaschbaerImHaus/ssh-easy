// Paket main - Tests fuer den Logger
//
// Testet Logger-Erstellung und Log-Ausgaben.
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 22:00
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewLogger prueft die Logger-Erstellung.
func TestNewLogger(t *testing.T) {
	logger := NewLogger()
	if logger == nil {
		t.Fatal("NewLogger sollte nicht nil zurueckgeben")
	}
}

// TestLoggerInfo prueft die Info-Log-Funktion.
func TestLoggerInfo(t *testing.T) {
	logger := NewLogger()
	// Sollte keinen Panic ausloesen
	logger.Info("Test-Nachricht: %s", "Hallo")
}

// TestLoggerError prueft die Error-Log-Funktion.
func TestLoggerError(t *testing.T) {
	logger := NewLogger()
	// Sollte keinen Panic ausloesen
	logger.Error("Fehler-Nachricht: %d", 42)
}

// TestLoggerWarn prueft die Warn-Log-Funktion.
func TestLoggerWarn(t *testing.T) {
	logger := NewLogger()
	// Sollte keinen Panic ausloesen
	logger.Warn("Warnung: %v", true)
}

// TestLoggerWritesToFile prueft ob der Logger in eine Datei schreibt.
func TestLoggerWritesToFile(t *testing.T) {
	logger := NewLogger()
	logger.Info("Test-Eintrag fuer Datei-Pruefung")

	// Log-Datei pruefen
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Home-Verzeichnis nicht ermittelbar")
	}

	logPath := filepath.Join(home, ".ssh-easy", "ssh-easy.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Skipf("Log-Datei nicht lesbar: %v", err)
	}

	if !strings.Contains(string(data), "Test-Eintrag fuer Datei-Pruefung") {
		t.Error("Log-Datei enthaelt nicht den erwarteten Eintrag")
	}
}
