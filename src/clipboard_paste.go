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

// altF4Seq ist die VT-Escape-Sequenz für Alt+F4.
// F4 allein = SS3 S (ESC O S); mit Modifier wird daraus CSI 1;<mod>S,
// wobei Modifier 3 = Alt. Dekodiert: ESC [ 1 ; 3 S
// Alt+F4 soll das Programm auch mitten in einer SSH-Session beenden (wie PuTTY).
var altF4Seq = []byte{0x1b, '[', '1', ';', '3', 'S'}

// readClipboard ist ein Funktionszeiger, damit Tests einen Stub einsetzen können.
// Standardmäßig wird das System-Clipboard über atotto/clipboard gelesen.
//
// @return string - Clipboard-Inhalt
// @return error  - Fehler beim Lesen (z.B. kein xclip/xsel auf Linux)
// @date   2026-04-19 00:00
var readClipboard = func() (string, error) {
	return clipboard.ReadAll()
}

// forwardStdinWithPaste kopiert Bytes von r nach w und fängt dabei zwei
// VT-Sequenzen ab:
//   - Shift+Insert: wird durch den Clipboard-Inhalt ersetzt (Einfügen)
//   - Alt+F4: onAltF4-Callback wird aufgerufen und die Weiterleitung endet
//     sofort (Programm-Beenden wie bei PuTTY, auch mitten in der Session)
//
// Kehrt zurück, sobald r EOF liefert, ein Schreibfehler auf w auftritt
// (z.B. wenn die SSH-Session geschlossen wird) oder Alt+F4 erkannt wurde.
//
// Windows: durch Scroll-Snap springt der Konsolen-Viewport bei jeder
// Tastatureingabe zurück zur Cursor-Zeile (wie PuTTY) - siehe
// snapConsoleToBottom() in console_scroll_windows.go.
//
// @param r - Quelle (typischerweise os.Stdin im Raw-Mode)
// @param w - Ziel (typischerweise session.StdinPipe() der SSH-Session)
// @param onAltF4 - Callback bei Alt+F4 (nil = Alt+F4 wird nur verschluckt)
// @date   2026-07-03 19:30
func forwardStdinWithPaste(r io.Reader, w io.Writer, onAltF4 func()) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			// Windows: Viewport zur Eingabezeile zurückscrollen falls der
			// Nutzer im Scrollback nach oben gescrollt hatte. Unix: No-Op.
			snapConsoleToBottom()

			data := buf[:n]
			// Solange noch eine bekannte Sequenz im Puffer steht: Text davor
			// schreiben, Sequenz behandeln, Rest weiterverarbeiten.
			for {
				idxPaste := bytes.Index(data, shiftInsertSeq)
				idxQuit := bytes.Index(data, altF4Seq)
				if idxPaste < 0 && idxQuit < 0 {
					break
				}

				// Alt+F4 zuerst behandeln wenn es vor der Paste-Sequenz steht
				if idxQuit >= 0 && (idxPaste < 0 || idxQuit < idxPaste) {
					// Bytes vor der Sequenz noch zustellen, dann beenden
					if idxQuit > 0 {
						if _, werr := w.Write(data[:idxQuit]); werr != nil {
							return
						}
					}
					if onAltF4 != nil {
						onAltF4()
					}
					return
				}

				// Shift+Insert: Bytes vor der Sequenz unverändert weiterreichen
				if _, werr := w.Write(data[:idxPaste]); werr != nil {
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
				data = data[idxPaste+len(shiftInsertSeq):]
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
