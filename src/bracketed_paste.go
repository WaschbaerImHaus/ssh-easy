// Paket main - Bracketed Paste für Shift+Einfg im SSH-Terminal
//
// Problem: ssh-easy fügt beim Drücken von Shift+Einfg den Inhalt der
// Zwischenablage selbst in die SSH-Session ein (siehe clipboard_paste.go).
// Bisher kam der Text roh an - für die Remote-Shell sah das aus, als hätte
// der Nutzer jede Zeile getippt und Enter gedrückt. Eine mehrzeilige
// Zwischenablage (z.B. von einer präparierten Webseite kopiert) wurde so
// Zeile für Zeile sofort ausgeführt.
//
// Lösung (wie in xterm, Windows Terminal, kitty usw.):
//   - Remote-Programme (bash/readline, zsh, vim ...) schalten mit der
//     Sequenz ESC[?2004h den Modus "Bracketed Paste" ein, mit ESC[?2004l
//     wieder aus. bracketedPasteTracker beobachtet dafür die Session-Ausgabe.
//   - Ist der Modus aktiv, wird eingefügter Text in ESC[200~ ... ESC[201~
//     eingeschlossen. Die Shell erkennt ihn so als eingefügt und führt ihn
//     erst aus, wenn der Nutzer selbst Enter drückt.
//   - Eingebettete Marker werden aus dem Text entfernt, damit der Inhalt den
//     Modus nicht vorzeitig beenden kann (Paste-Jacking).
//
// @author Kurt Ingwer
// @date   2026-10-14 17:10

package main

import (
	"io"
	"strings"
	"sync/atomic"
)

// Marker, die eingefügten Text bei aktivem Bracketed Paste einschließen.
const (
	pasteStartMarker = "\x1b[200~"
	pasteEndMarker   = "\x1b[201~"
)

// bracketedPasteModeNumber ist die Nummer des privaten DEC-Modus
// "Bracketed Paste" (ESC[?2004h = ein, ESC[?2004l = aus).
const bracketedPasteModeNumber = 2004

// trackerMaxParamBytes begrenzt die Parameterlänge einer beobachteten
// Sequenz. Echte Sequenzen sind wenige Bytes lang; längere werden ignoriert,
// damit ein Server keinen unbegrenzten Zustand erzeugen kann.
const trackerMaxParamBytes = 64

// bracketedPasteMode merkt sich, ob die Remote-Seite Bracketed Paste
// eingeschaltet hat. Wird von der Ausgabe-Goroutine geschrieben und von der
// Eingabe-Goroutine gelesen - daher atomar.
//
// Ein nil-Zeiger ist erlaubt und gilt als "nicht aktiv".
type bracketedPasteMode struct {
	active atomic.Bool
}

// IsActive meldet, ob eingefügter Text eingeschlossen werden muss.
//
// @return bool - true, wenn die Remote-Seite ESC[?2004h gesendet hat
// @date   2026-10-14 17:10
func (m *bracketedPasteMode) IsActive() bool {
	if m == nil {
		return false
	}
	return m.active.Load()
}

// set schaltet den Modus ein oder aus. Bei nil-Zeiger passiert nichts.
//
// @param on - neuer Zustand
// @date   2026-10-14 17:10
func (m *bracketedPasteMode) set(on bool) {
	if m == nil {
		return
	}
	m.active.Store(on)
}

// trackerState beschreibt, an welcher Stelle einer möglichen Sequenz
// ESC [ ? <Parameter> h|l der Tracker gerade steht.
type trackerState int

const (
	// trackerText: normaler Text
	trackerText trackerState = iota
	// trackerEsc: ESC gesehen
	trackerEsc
	// trackerCSI: "ESC[" gesehen, "?" wird erwartet
	trackerCSI
	// trackerParams: "ESC[?" gesehen, Parameter (Ziffern und ";") werden gelesen
	trackerParams
)

// bracketedPasteTracker ist ein io.Writer, der die Session-Ausgabe
// unverändert weiterreicht und dabei die Modus-Sequenzen für Bracketed Paste
// erkennt. Sequenzen dürfen über mehrere Write-Aufrufe verteilt sein und
// mehrere Modi enthalten (z.B. ESC[?1049;2004h).
//
// Jeder Ausgabestrom (stdout, stderr) bekommt einen eigenen Tracker, alle
// teilen sich denselben bracketedPasteMode.
type bracketedPasteTracker struct {
	out        io.Writer           // Ziel (Konsole)
	mode       *bracketedPasteMode // gemeinsamer Modus der Session
	state      trackerState        // aktueller Parser-Zustand
	paramBytes int                 // Länge der bisher gelesenen Parameter
	current    int                 // Wert des gerade gelesenen Parameters
	found      bool                // Parameter 2004 in dieser Sequenz enthalten
}

// newBracketedPasteTracker erzeugt einen Tracker vor dem Ausgabeziel.
//
// @param out - Ziel für alle Bytes (typischerweise os.Stdout)
// @param mode - gemeinsamer Modus der Session (nil = nur durchreichen)
// @return *bracketedPasteTracker - einsatzbereiter Tracker
// @date   2026-10-14 17:10
func newBracketedPasteTracker(out io.Writer, mode *bracketedPasteMode) *bracketedPasteTracker {
	return &bracketedPasteTracker{out: out, mode: mode}
}

// Write beobachtet die Bytes und reicht sie unverändert weiter.
//
// @param p - Bytes vom Remote-Server
// @return int - Anzahl geschriebener Bytes des Ausgabeziels
// @return error - Schreibfehler des Ausgabeziels
// @date   2026-10-14 17:10
func (t *bracketedPasteTracker) Write(p []byte) (int, error) {
	for _, b := range p {
		t.observe(b)
	}
	return t.out.Write(p)
}

// observe verarbeitet ein einzelnes Byte der Zustandsmaschine.
//
// @param b - nächstes Byte der Ausgabe
// @date   2026-10-14 17:10
func (t *bracketedPasteTracker) observe(b byte) {
	// Ein ESC beginnt in jedem Zustand eine neue Sequenz (bricht die alte ab)
	if b == asciiESC {
		t.state = trackerEsc
		return
	}

	switch t.state {
	case trackerEsc:
		if b == '[' {
			t.state = trackerCSI
		} else {
			t.state = trackerText
		}

	case trackerCSI:
		if b == '?' {
			t.state = trackerParams
			t.paramBytes = 0
			t.current = 0
			t.found = false
		} else {
			// Keine private Modus-Sequenz (z.B. Farbe, Cursor)
			t.state = trackerText
		}

	case trackerParams:
		t.observeParam(b)
	}
}

// observeParam liest Parameter und Abschlussbyte einer Sequenz ESC[?...
//
// @param b - nächstes Byte nach "ESC[?"
// @date   2026-10-14 17:10
func (t *bracketedPasteTracker) observeParam(b byte) {
	t.paramBytes++
	if t.paramBytes > trackerMaxParamBytes {
		// Überlange Sequenz: ignorieren, Rest gilt als normaler Text
		t.state = trackerText
		return
	}

	switch {
	case b >= '0' && b <= '9':
		// Wert begrenzen, damit große Zahlen nicht überlaufen - alles über
		// 99999 ist ohnehin nicht 2004
		if t.current <= 99999 {
			t.current = t.current*10 + int(b-'0')
		}
	case b == ';':
		t.finishParam()
	case b == 'h' || b == 'l':
		t.finishParam()
		if t.found {
			t.mode.set(b == 'h')
		}
		t.state = trackerText
	default:
		// Anderes Abschlussbyte (z.B. "$p" = Abfrage) - kein Setzen
		t.state = trackerText
	}
}

// finishParam schließt den aktuellen Parameter ab und merkt sich, ob es der
// Bracketed-Paste-Modus war.
//
// @date 2026-10-14 17:10
func (t *bracketedPasteTracker) finishParam() {
	if t.current == bracketedPasteModeNumber {
		t.found = true
	}
	t.current = 0
}

// preparePaste wandelt Clipboard-Text in die Bytes um, die an die
// Remote-Shell gehen. Zeilenumbrüche werden immer zu CR normalisiert. Bei
// aktivem Bracketed Paste wird der Text zusätzlich von eingebetteten Markern
// befreit und in ESC[200~ ... ESC[201~ eingeschlossen.
//
// @param text - Inhalt der Zwischenablage
// @param mode - Modus der Session (nil = nicht aktiv)
// @return []byte - zu sendende Bytes (leer, wenn nichts einzufügen ist)
// @date   2026-10-14 17:10
func preparePaste(text string, mode *bracketedPasteMode) []byte {
	if !mode.IsActive() {
		return normalizeLineEndings(text)
	}
	text = stripPasteMarkers(text)
	if text == "" {
		return nil
	}
	body := normalizeLineEndings(text)
	result := make([]byte, 0, len(pasteStartMarker)+len(body)+len(pasteEndMarker))
	result = append(result, pasteStartMarker...)
	result = append(result, body...)
	result = append(result, pasteEndMarker...)
	return result
}

// stripPasteMarkers entfernt alle Start- und Endmarker aus dem Text. Das
// Entfernen wird wiederholt, bis keine Marker mehr vorhanden sind: Aus
// "ESC[20" + "ESC[201~" + "1~" würde sonst nach einem Durchgang ein neuer
// Endmarker entstehen.
//
// @param text - Inhalt der Zwischenablage
// @return string - Text ohne Marker
// @date   2026-10-14 17:10
func stripPasteMarkers(text string) string {
	for {
		cleaned := strings.ReplaceAll(text, pasteEndMarker, "")
		cleaned = strings.ReplaceAll(cleaned, pasteStartMarker, "")
		// Jeder Durchgang macht den Text kürzer - die Schleife endet sicher
		if cleaned == text {
			return cleaned
		}
		text = cleaned
	}
}
