// Plattform-spezifische Terminal-Resize-Behandlung für Windows.
//
// Windows hat kein SIGWINCH-Äquivalent. Stattdessen wird im kurzen Intervall
// gepollt ob sich die Konsolengröße geändert hat.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00

//go:build windows

package main

import "time"

// waitForResizeSignal wartet auf Windows durch kurzes Polling (250ms Intervall).
// Windows bietet kein SIGWINCH, daher ist regelmäßiges Prüfen die einzige Option.
//
// @param stop - Kanal zum Beenden der Wartefunktion
// @date   2026-03-08 00:00
func waitForResizeSignal(stop <-chan struct{}) {
	select {
	case <-time.After(250 * time.Millisecond):
		// Nach 250ms zurückkehren damit watchTerminalResize die Größe prüft
	case <-stop:
		// Session beendet → Goroutine beenden
	}
}
