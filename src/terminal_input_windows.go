// Abbrechbare Tastatureingabe für die Windows-Konsole.
//
// Ein blockierendes ReadFile auf der Konsole lässt sich nicht zuverlässig
// abbrechen. Deshalb wird der Eingabepuffer zuerst per PeekConsoleInput
// angesehen: Nur wenn ein Zeichen bereitliegt, wird gelesen (dann kehrt
// ReadFile sofort zurück). Liegen nur Ereignisse ohne Zeichen vor (Taste
// losgelassen, Fokus, Fenstergröße), werden sie verworfen. Dazwischen wird
// kurz gewartet und der Abbruch-Status geprüft - wie Bubbletea selbst es
// unter Windows macht (peekAndReadConsInput).
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00

//go:build windows

package main

import (
	"os"
	"sync/atomic"
	"time"

	"github.com/erikgeiser/coninput"
	"golang.org/x/sys/windows"
)

// consolePollInterval - Wartezeit zwischen zwei Blicken in den Eingabepuffer
const consolePollInterval = 10 * time.Millisecond

// consoleInput liest abbrechbar von der Windows-Konsole.
type consoleInput struct {
	// Datei für das eigentliche Lesen (UTF-8-Umwandlung durch Go)
	file *os.File
	// Konsolen-Handle für Peek/Read der Eingabeereignisse
	handle windows.Handle
	// Abbruch angefordert?
	canceled atomic.Bool
	// Hat der letzte Read den Puffer komplett gefüllt? Dann liegt sehr
	// wahrscheinlich ein Rest in Gos internem Konsolenpuffer (os.File wandelt
	// UTF-16 in UTF-8 um und hält Überhang zurück). Dieser Rest ist im
	// Konsolenpuffer NICHT mehr sichtbar - Peek fände nichts und der Rest
	// bliebe bis zum nächsten Tastendruck hängen. Deshalb direkt weiterlesen.
	maybeBuffered bool
}

// newTerminalInput erstellt eine abbrechbare Eingabe über der Konsole.
// Ist f keine Konsole (z.B. umgeleitet), wird ein Fehler geliefert und der
// Aufrufer nutzt die Datei direkt.
//
// @param f - Eingabedatei (typischerweise os.Stdin)
// @return terminalInput - Abbrechbare Eingabe
// @return error - Fehler wenn f keine Konsole ist
// @date   2026-10-16 10:00
func newTerminalInput(f *os.File) (terminalInput, error) {
	handle := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return nil, err
	}
	return &consoleInput{file: f, handle: handle}, nil
}

// Read wartet auf Zeichen im Konsolenpuffer und liest sie dann.
//
// @param p - Zielpuffer
// @return int - Anzahl gelesener Bytes
// @return error - errInputCanceled nach Cancel(), sonst Lesefehler
// @date   2026-10-16 10:00
func (c *consoleInput) Read(p []byte) (int, error) {
	for {
		if c.canceled.Load() {
			return 0, errInputCanceled
		}
		if c.maybeBuffered {
			return c.readFile(p)
		}
		records, err := coninput.PeekNConsoleInputs(c.handle, 16)
		if err != nil {
			return 0, err
		}
		if len(records) == 0 {
			time.Sleep(consolePollInterval)
			continue
		}
		events := make([]consoleKeyEvent, len(records))
		for i, rec := range records {
			if key, ok := rec.Unwrap().(coninput.KeyEventRecord); ok {
				events[i] = consoleKeyEvent{isKey: true, keyDown: key.KeyDown, char: key.Char}
			}
		}
		if hasCharacterInput(events) {
			// Zeichen liegt bereit: ReadFile kehrt sofort zurück.
			// os.File.Read übernimmt die UTF-16 -> UTF-8-Umwandlung.
			return c.readFile(p)
		}
		// Nur Ereignisse ohne Zeichen: verwerfen, sonst würde ReadFile
		// blockieren und ließe sich nicht mehr abbrechen
		_, _ = coninput.ReadNConsoleInputs(c.handle, uint32(len(records)))
	}
}

// readFile liest über os.File und merkt sich, ob der Puffer voll wurde.
//
// @param p - Zielpuffer
// @return int - Anzahl gelesener Bytes
// @return error - Lesefehler
// @date   2026-10-16 10:00
func (c *consoleInput) readFile(p []byte) (int, error) {
	n, err := c.file.Read(p)
	c.maybeBuffered = len(p) > 0 && n == len(p)
	return n, err
}

// Cancel bricht laufende und künftige Reads ab. Da Read nie blockierend
// liest, endet es spätestens nach consolePollInterval.
//
// @return bool - immer true
// @date   2026-10-16 10:00
func (c *consoleInput) Cancel() bool {
	c.canceled.Store(true)
	return true
}

// Close gibt nichts frei - das Konsolen-Handle gehört os.Stdin.
//
// @return error - immer nil
// @date   2026-10-16 10:00
func (c *consoleInput) Close() error {
	return nil
}
