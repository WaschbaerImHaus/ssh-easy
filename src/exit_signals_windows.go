// Paket main - Schließ-Signale unter Windows (Build 87, ergänzt in Build 89 und 91)
//
// CTRL_CLOSE_EVENT (Schließkreuz, in Windows Terminal auch Alt+F4),
// CTRL_LOGOFF_EVENT und CTRL_SHUTDOWN_EVENT gelten als SIGTERM.
//
// KORREKTUR Build 91: Bis Build 90 stand hier, Go melde diese Ereignisse
// über signal.Notify an main.go und halte den Prozess am Leben. Das stimmt
// für ssh-easy NICHT: Go meldet seinen Handler vor init() an, und
// AllocConsole (icon_windows.go) setzt die Handler-Tabelle zurück. Das
// Signal erreichte main.go deshalb nie; Windows beendete den Prozess beim
// Kreuz sofort. Seit Build 91 meldet init() einen eigenen Handler an
// (registerConsoleCtrlHandler). Er legt SIGTERM in consoleCloseRelay, das
// watchCloseSignals liest, und hält den Prozess bis os.Exit am Leben -
// höchstens aber ca. 5 s, dann beendet Windows hart. Der Kanal aus
// signal.Notify bleibt angemeldet (schadet nicht, falls Gos Handler doch
// noch greift). SIGINT (Strg+C) wird NICHT abgefangen.
//
// @author Kurt Ingwer
// @date   2026-11-16 16:45

//go:build windows

package main

import (
	"os"
	"syscall"
)

// closeSignals liefert die Signale, die das Schließen des Fensters melden.
//
// @return []os.Signal - {SIGTERM}
// @date   2026-10-31 14:40
func closeSignals() []os.Signal {
	return []os.Signal{syscall.SIGTERM}
}

// emergencyTerminalResetEnabled - unter Windows schreibt das letzte
// Zurücksetzen im Notfall KEINE Terminalfolge (Build 89, Runde 2): Die
// Konsole wird mit dem Prozess ohnehin abgebaut.
const emergencyTerminalResetEnabled = false
