// Paket main - Schließ-Signale unter Linux/macOS (Build 87, ergänzt in Build 89)
//
// SIGHUP: Das Terminalfenster wurde geschlossen (Kreuz, Alt+F4).
// SIGTERM: Abmelden, kill, Systemende.
// SIGINT (Strg+C) wird NICHT abgefangen - es wirkt weiter wie bisher.
//
// @author Kurt Ingwer
// @date   2026-11-07 14:00

//go:build !windows

package main

import (
	"os"
	"syscall"
)

// closeSignals liefert die Signale, die das Schließen des Fensters melden.
//
// @return []os.Signal - {SIGHUP, SIGTERM}
// @date   2026-10-31 14:40
func closeSignals() []os.Signal {
	return []os.Signal{syscall.SIGHUP, syscall.SIGTERM}
}

// emergencyTerminalResetEnabled - unter Linux/macOS schreibt das letzte
// Zurücksetzen im Notfall die Terminalfolge (Build 89, Runde 2).
const emergencyTerminalResetEnabled = true
