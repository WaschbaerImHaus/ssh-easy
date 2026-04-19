// Paket main - Clipboard-Paste im SSH-Terminal (Shift+Insert)
//
// Hintergrund:
//   Auf Windows (conhost/Windows Terminal) wird die Taste Shift+Insert als
//   VT-Escape-Sequenz "ESC[2;2~" an die Anwendung geliefert (weil
//   ENABLE_VIRTUAL_TERMINAL_INPUT gesetzt ist). Ohne eigene Behandlung wird
//   diese Sequenz stumpf durch die SSH-Session zur Remote-Shell geschickt,
//   die damit nichts anfangen kann - "Einfügen" funktioniert nicht.
//
// Lösung:
//   Eigene stdin-Weiterleitung statt session.Stdin = os.Stdin. Wir scannen den
//   Eingabestrom auf die Shift+Insert-Sequenz und ersetzen sie durch den
//   aktuellen Clipboard-Inhalt. Alle anderen Bytes (inkl. anderer VT-Sequenzen
//   wie Pfeiltasten) bleiben unverändert.
//
// @author Kurt Ingwer
// @date   2026-04-19 00:00
package main

import (
	"bytes"
	"io"
	"strings"

	"github.com/atotto/clipboard"
)

// shiftInsertSeq ist die VT-Escape-Sequenz, die Windows mit aktivem
// ENABLE_VIRTUAL_TERMINAL_INPUT für Shift+Insert liefert.
// Dekodiert: ESC [ 2 ; 2 ~
var shiftInsertSeq = []byte{0x1b, '[', '2', ';', '2', '~'}

// readClipboard ist ein Funktionszeiger, damit Tests einen Stub einsetzen können.
// Standardmäßig wird das System-Clipboard über atotto/clipboard gelesen.
//
// @return string - Clipboard-Inhalt
// @return error  - Fehler beim Lesen (z.B. kein xclip/xsel auf Linux)
// @date   2026-04-19 00:00
var readClipboard = func() (string, error) {
	return clipboard.ReadAll()
}

// forwardStdinWithPaste kopiert Bytes von r nach w und fängt dabei die
// Shift+Insert-VT-Sequenz ab. Wird die Sequenz erkannt, wird stattdessen
// der aktuelle Clipboard-Inhalt geschrieben (mit normalisierten Zeilenenden).
//
// Kehrt zurück, sobald r EOF liefert oder ein Schreibfehler auf w auftritt
// (z.B. wenn die SSH-Session geschlossen wird).
//
// @param r - Quelle (typischerweise os.Stdin im Raw-Mode)
// @param w - Ziel (typischerweise session.StdinPipe() der SSH-Session)
// @date   2026-04-19 00:00
func forwardStdinWithPaste(r io.Reader, w io.Writer) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			data := buf[:n]
			// Solange noch eine Shift+Insert-Sequenz im Puffer steht: Text davor
			// schreiben, Sequenz durch Clipboard-Inhalt ersetzen, Rest weiterverarbeiten.
			for {
				idx := bytes.Index(data, shiftInsertSeq)
				if idx < 0 {
					break
				}
				// Bytes vor der Sequenz unverändert weiterreichen
				if _, werr := w.Write(data[:idx]); werr != nil {
					return
				}
				// Clipboard lesen und einfügen - Fehler wird ignoriert, dann wird
				// die Sequenz einfach verschluckt (besser als abstürzen).
				if text, cerr := readClipboard(); cerr == nil {
					if _, werr := w.Write(normalizeLineEndings(text)); werr != nil {
						return
					}
				}
				// Hinter der Sequenz weitermachen
				data = data[idx+len(shiftInsertSeq):]
			}
			// Verbleibende Bytes (ohne weitere Sequenz) schreiben
			if len(data) > 0 {
				if _, werr := w.Write(data); werr != nil {
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// normalizeLineEndings wandelt CRLF und einzelne LF zu CR um. Hintergrund:
// Im Raw-Terminal-Modus sendet die Enter-Taste ein CR (0x0D). Damit eingefügter
// mehrzeiliger Text sich genauso verhält als hätte der Nutzer jede Zeile
// getippt und Enter gedrückt, werden alle Zeilenumbrüche auf CR normalisiert.
//
// @param text - zu normalisierender Text aus dem Clipboard
// @return []byte - normalisierter Text
// @date   2026-04-19 00:00
func normalizeLineEndings(text string) []byte {
	// Zuerst CRLF zu CR (sonst würde das LF anschließend zu einem zweiten CR werden)
	text = strings.ReplaceAll(text, "\r\n", "\r")
	// Dann einzelne LF zu CR
	text = strings.ReplaceAll(text, "\n", "\r")
	return []byte(text)
}
