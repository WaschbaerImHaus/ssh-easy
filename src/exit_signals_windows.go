// Paket main - Schließ-Signale unter Windows (Build 87)
//
// Go meldet CTRL_CLOSE_EVENT (Schließkreuz, in Windows Terminal auch
// Alt+F4), CTRL_LOGOFF_EVENT und CTRL_SHUTDOWN_EVENT als SIGTERM. Solange
// ein Handler registriert ist, hält die Laufzeit den Prozess nach dem
// Ereignis am Leben, bis os.Exit kommt - höchstens aber ca. 5 s, dann
// beendet Windows hart. SIGINT (Strg+C) wird NICHT abgefangen.
//
// @author Kurt Ingwer
// @date   2026-10-31 14:40

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
