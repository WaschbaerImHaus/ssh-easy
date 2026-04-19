// Tests für den Terminal-Launcher auf Unix-Systemen.
//
// Zweck: Wenn ssh-easy per Doppelklick aus einem Dateimanager (Nemo, Nautilus etc.)
// gestartet wird, fehlt ein TTY. Bubbletea kann ohne TTY keine UI rendern.
// Lösung: Eigenständiger Re-Launch in einem Terminal-Emulator.
//
// Diese Tests prüfen die Auswahl-Logik und die Kommando-Konstruktion;
// der tatsächliche exec.Start() wird in Integrationstests nicht simuliert.
//
// @author Kurt Ingwer
// @date   2026-04-19 00:00

//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTerminalCandidatesContainsExpected dokumentiert die Reihenfolge der
// Terminal-Emulator-Suche und schützt vor unbeabsichtigten Änderungen.
// x-terminal-emulator muss ganz vorne stehen (Debian/Ubuntu/Mint-Alternative).
func TestTerminalCandidatesContainsExpected(t *testing.T) {
	expectedFirst := "x-terminal-emulator"
	if len(terminalCandidates) == 0 {
		t.Fatal("terminalCandidates ist leer")
	}
	if terminalCandidates[0] != expectedFirst {
		t.Errorf("erster Kandidat sollte %q sein, ist %q", expectedFirst, terminalCandidates[0])
	}
	// Einige wichtige Emulatoren müssen in der Liste sein
	mustContain := []string{"gnome-terminal", "konsole", "xfce4-terminal", "xterm"}
	for _, name := range mustContain {
		found := false
		for _, c := range terminalCandidates {
			if c == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Kandidatenliste enthält %q nicht", name)
		}
	}
}

// TestFindTerminalEmulatorViaEnv prüft, dass die $TERMINAL-Umgebungsvariable
// Vorrang vor der Kandidatenliste hat, solange der Inhalt im PATH auffindbar ist.
func TestFindTerminalEmulatorViaEnv(t *testing.T) {
	// Temporäres Fake-Terminal als ausführbare Datei im PATH ablegen
	tmpDir := t.TempDir()
	fake := filepath.Join(tmpDir, "my-fake-term")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("Fake-Terminal konnte nicht erstellt werden: %v", err)
	}

	// PATH und TERMINAL so setzen, dass NUR das Fake gefunden wird
	origPath := os.Getenv("PATH")
	origTerm := os.Getenv("TERMINAL")
	defer func() {
		os.Setenv("PATH", origPath)
		os.Setenv("TERMINAL", origTerm)
	}()
	os.Setenv("PATH", tmpDir)
	os.Setenv("TERMINAL", "my-fake-term")

	got := findTerminalEmulator()
	if got != fake {
		t.Errorf("findTerminalEmulator = %q, erwartet %q", got, fake)
	}
}

// TestFindTerminalEmulatorEmpty prüft, dass bei leerem PATH und ohne $TERMINAL
// ein leerer String zurückkommt – Aufrufer kann so einen Fehler anzeigen.
func TestFindTerminalEmulatorEmpty(t *testing.T) {
	origPath := os.Getenv("PATH")
	origTerm := os.Getenv("TERMINAL")
	defer func() {
		os.Setenv("PATH", origPath)
		os.Setenv("TERMINAL", origTerm)
	}()
	// Leerer, nicht-existierender PATH und keine TERMINAL-Variable
	os.Setenv("PATH", "/nonexistent/path/xyz")
	os.Unsetenv("TERMINAL")

	got := findTerminalEmulator()
	if got != "" {
		t.Errorf("findTerminalEmulator bei leerem PATH = %q, erwartet leer", got)
	}
}

// TestShouldRelaunchInTerminalNoTTYWithDisplay: klassischer Nemo-Doppelklick –
// kein TTY, aber DISPLAY gesetzt → muss relaunchen.
func TestShouldRelaunchInTerminalNoTTYWithDisplay(t *testing.T) {
	if !shouldRelaunchInTerminal(false, "(:0", "") {
		t.Error("shouldRelaunchInTerminal(hasTTY=false, DISPLAY=:0) = false, erwartet true")
	}
}

// TestShouldRelaunchInTerminalNoTTYWithWayland: Wayland-Session ohne TTY → relaunch.
func TestShouldRelaunchInTerminalNoTTYWithWayland(t *testing.T) {
	if !shouldRelaunchInTerminal(false, "", "wayland-0") {
		t.Error("shouldRelaunchInTerminal(hasTTY=false, WAYLAND_DISPLAY=wayland-0) = false, erwartet true")
	}
}

// TestShouldRelaunchInTerminalHasTTY: normale Terminal-Session → nicht relaunchen.
func TestShouldRelaunchInTerminalHasTTY(t *testing.T) {
	if shouldRelaunchInTerminal(true, ":0", "") {
		t.Error("shouldRelaunchInTerminal(hasTTY=true) = true, erwartet false")
	}
}

// TestShouldRelaunchInTerminalHeadless: SSH ohne TTY und ohne Display
// (z.B. Cronjob, Pipe) → NICHT relaunchen; Fehler soll auf stderr landen.
func TestShouldRelaunchInTerminalHeadless(t *testing.T) {
	if shouldRelaunchInTerminal(false, "", "") {
		t.Error("shouldRelaunchInTerminal(headless) = true, erwartet false")
	}
}
