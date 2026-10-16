// Paket main - Abbrechbare Tastatureingabe für das SSH-Terminal
//
// Hintergrund (Bug "erste Taste nach dem Terminal wird verschluckt"):
// Die stdin-Weiterleitung des SSH-Terminals liest in einer Goroutine von
// stdin. Endete die Sitzung (z.B. mit "exit"), blieb diese Goroutine in einem
// blockierenden Read hängen. Der nächste Tastendruck - etwa "x" zum Trennen -
// landete dort und wurde an die bereits geschlossene Sitzung geschickt, also
// verworfen. Erst der zweite Tastendruck erreichte Bubbletea.
//
// Lösung: Die Eingabe ist abbrechbar (Unix: epoll/select über cancelreader,
// Windows: Abfragen des Konsolenpuffers ohne blockierendes Lesen). Nach dem
// Sitzungsende wird die Weiterleitung gestoppt, BEVOR Bubbletea stdin wieder
// übernimmt.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00
package main

import (
	"errors"
	"io"
	"time"
)

// errInputCanceled wird von Read() geliefert, nachdem Cancel() aufgerufen wurde.
var errInputCanceled = errors.New("Eingabe abgebrochen")

// inputForwarderStopTimeout - so lange wird auf das Ende der Weiterleitung
// gewartet, bevor Bubbletea stdin trotzdem übernimmt.
const inputForwarderStopTimeout = 500 * time.Millisecond

// terminalInput ist eine abbrechbare Eingabequelle.
type terminalInput interface {
	io.Reader
	// Cancel bricht laufende und künftige Reads ab; true wenn erfolgreich
	Cancel() bool
	// Close gibt plattformspezifische Ressourcen frei
	Close() error
}

// consoleKeyEvent ist eine plattformneutrale Sicht auf ein Windows-
// Konsolen-Eingabeereignis (für die testbare Entscheidungslogik).
type consoleKeyEvent struct {
	// Tastaturereignis? (sonst Maus, Fokus, Fenstergröße, Menü)
	isKey bool
	// Taste gedrückt (true) oder losgelassen (false)
	keyDown bool
	// Erzeugtes Zeichen (0 = keines, z.B. reine Shift-Taste)
	char rune
}

// hasCharacterInput prüft ob im Konsolenpuffer mindestens ein gedrückter
// Tastendruck mit Zeichen liegt. Nur dann liefert ReadFile sofort Daten;
// liegen dort nur Loslass-, Fokus- oder Größen-Ereignisse, würde ReadFile
// blockieren und wäre nicht mehr abbrechbar.
//
// @param events - Vorschau des Konsolenpuffers
// @return bool - true wenn ein Lesen sofort Zeichen liefert
// @date   2026-10-16 10:00
func hasCharacterInput(events []consoleKeyEvent) bool {
	for _, e := range events {
		if e.isKey && e.keyDown && e.char != 0 {
			return true
		}
	}
	return false
}

// stopInputForwarder bricht die Eingabe ab und wartet, bis die
// Weiterleitungs-Goroutine beendet ist (höchstens timeout).
//
// @param in - Abbrechbare Eingabe (nil = nur warten)
// @param done - Wird geschlossen, wenn die Goroutine endet
// @param timeout - Maximale Wartezeit
// @return bool - true wenn die Goroutine rechtzeitig beendet wurde
// @date   2026-10-16 10:00
func stopInputForwarder(in terminalInput, done <-chan struct{}, timeout time.Duration) bool {
	if in != nil {
		in.Cancel()
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
