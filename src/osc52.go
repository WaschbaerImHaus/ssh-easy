// Paket main - OSC-52-Clipboard-Unterstützung im SSH-Terminal
//
// Problem: Programme auf dem Remote-Server (z.B. Claude Code, vim, tmux)
// kopieren Text in die Zwischenablage, indem sie die Escape-Sequenz OSC 52
// ausgeben:
//
//	ESC ] 52 ; <Selektion> ; <Base64-Text> BEL      (oder ESC \ statt BEL)
//
// Der lokale Terminal-Emulator soll den Text dann in die System-Zwischen-
// ablage legen. Die Windows-Konsole (conhost), die ssh-easy per AllocConsole
// öffnet, kennt OSC 52 aber nicht und verwirft die Sequenz kommentarlos -
// "Kopieren" im Remote-Programm hatte dadurch keine Wirkung (z.B. der
// Login-Link von Claude Code). Unter Linux betrifft das alle VTE-basierten
// Terminals (GNOME Terminal & Co.).
//
// Lösung: osc52Writer sitzt zwischen SSH-Session und Konsole. Er reicht alle
// Bytes unverändert durch, fängt aber OSC-52-Sequenzen ab, dekodiert den
// Base64-Inhalt und schreibt ihn selbst in die Zwischenablage.
//
// Sicherheit:
//   - Steuerzeichen (außer Tab/CR/LF) werden aus dem Inhalt entfernt, damit
//     ein Server kein "ESC[201~" für Paste-Jacking unterschieben kann.
//   - Leseanfragen ("?") werden nie beantwortet - ein Server darf die lokale
//     Zwischenablage nicht auslesen können.
//   - Nutzdaten sind auf osc52MaxPayload Bytes begrenzt (Speicherschutz).
//
// @author Kurt Ingwer
// @date   2026-10-14 15:50

package main

import (
	"encoding/base64"
	"io"
	"strings"
	"sync"

	"github.com/atotto/clipboard"
	"golang.org/x/crypto/ssh"
)

// osc52MaxPayload ist die maximale Größe der Nutzdaten ("<Selektion>;<Base64>")
// einer OSC-52-Sequenz in Bytes. 1 MiB Base64 entspricht ca. 768 KiB Text -
// mehr als genug für Links, Code-Schnipsel und Logausschnitte. Größere
// Sequenzen werden komplett verworfen.
const osc52MaxPayload = 1 << 20

// Steuerzeichen, die das Ende einer OSC-Sequenz markieren.
const (
	asciiBEL = 0x07 // Bell - kurzer OSC-Terminator (xterm-Stil)
	asciiESC = 0x1b // Escape - Beginn jeder Escape-Sequenz
)

// osc52Intro ist das feste Präfix jeder OSC-52-Sequenz.
var osc52Intro = []byte("\x1b]52;")

// osc52TerminatorBEL und osc52TerminatorST werden beim Durchreichen einer
// Sequenz (Clipboard-Fehler) wieder angehängt.
var (
	osc52TerminatorBEL = []byte{asciiBEL}
	osc52TerminatorST  = []byte{asciiESC, '\\'}
)

// writeClipboard ist ein Funktionszeiger, damit Tests einen Stub einsetzen
// können. Standardmäßig wird das System-Clipboard über atotto/clipboard
// beschrieben (Windows: Win32-API, Linux: xclip/xsel/wl-copy).
//
// @param text - Text für die Zwischenablage
// @return error - Fehler, wenn keine Zwischenablage verfügbar ist
// @date   2026-10-14 10:20
var writeClipboard = func(text string) error {
	return clipboard.WriteAll(text)
}

// osc52State beschreibt, an welcher Stelle einer möglichen Sequenz der
// Parser gerade steht.
type osc52State int

const (
	// osc52StateText: normaler Text, keine Sequenz angefangen
	osc52StateText osc52State = iota
	// osc52StateIntro: ESC gesehen, Präfix "ESC]52;" wird verglichen
	osc52StateIntro
	// osc52StatePayload: Präfix vollständig, Nutzdaten werden gesammelt
	osc52StatePayload
	// osc52StatePayloadEsc: ESC in den Nutzdaten - folgt "\" ist es das Ende (ST)
	osc52StatePayloadEsc
)

// osc52Writer ist ein io.Writer, der OSC-52-Sequenzen aus dem Datenstrom
// filtert und in die Zwischenablage schreibt. Alles andere geht unverändert
// an out. Sequenzen dürfen beliebig über mehrere Write-Aufrufe verteilt sein
// (SSH-Pakete können überall getrennt werden).
//
// Hinweis: Endet ein Write mit einem angefangenen Präfix (z.B. nur "ESC"),
// werden diese wenigen Bytes bis zum nächsten Write oder Flush zurückgehalten.
type osc52Writer struct {
	mu        sync.Mutex
	out       io.Writer  // Ziel (Konsole)
	state     osc52State // aktueller Parser-Zustand
	introLen  int        // Anzahl bereits passender Präfix-Bytes
	payload   []byte     // gesammelte Nutzdaten "<Selektion>;<Base64>"
	oversized bool       // true = Limit überschritten, Sequenz wird verworfen
	pending   []byte     // Ausgabe-Puffer des laufenden Write-Aufrufs
}

// newOSC52Writer erzeugt einen Filter vor dem angegebenen Ausgabeziel.
//
// @param out - Ziel für alle Bytes außer OSC 52 (typischerweise os.Stdout)
// @return *osc52Writer - einsatzbereiter Filter
// @date   2026-10-14 10:20
func newOSC52Writer(out io.Writer) *osc52Writer {
	return &osc52Writer{out: out}
}

// Write verarbeitet einen Block des Ausgabestroms.
//
// Liefert bei Erfolg immer len(p) - auch wenn Bytes (die OSC-52-Sequenz)
// verschluckt wurden. Das erwartet io.Copy in der SSH-Bibliothek.
//
// @param p - Bytes vom Remote-Server
// @return int - len(p) bei Erfolg, 0 bei Schreibfehler
// @return error - Schreibfehler des Ausgabeziels
// @date   2026-10-14 10:20
func (w *osc52Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending = w.pending[:0]
	for i := 0; i < len(p); {
		switch w.state {
		case osc52StateText:
			i += w.consumeText(p[i:])

		case osc52StateIntro:
			// Byte gehört zum Präfix? Dann weiter vergleichen.
			if p[i] == osc52Intro[w.introLen] {
				w.introLen++
				i++
				if w.introLen == len(osc52Intro) {
					w.startPayload()
				}
				continue
			}
			// Keine OSC 52: zurückgehaltene Präfix-Bytes ausgeben und das
			// aktuelle Byte als normalen Text neu bewerten (i bleibt stehen).
			// Es könnte selbst ein ESC sein, das eine neue Sequenz beginnt.
			w.pending = append(w.pending, osc52Intro[:w.introLen]...)
			w.state = osc52StateText

		case osc52StatePayload:
			i += w.consumePayload(p[i:])

		case osc52StatePayloadEsc:
			if p[i] == '\\' {
				// ESC \ = String Terminator: Sequenz vollständig
				i++
				w.finishPayload(osc52TerminatorST)
				continue
			}
			// ESC ohne "\" bricht die OSC-Sequenz ab (Verhalten wie xterm).
			// Das ESC beginnt eine neue Sequenz, das aktuelle Byte wird im
			// Präfix-Zustand neu bewertet (i bleibt stehen).
			w.resetPayload()
			w.state = osc52StateIntro
			w.introLen = 1
		}
	}

	if len(w.pending) > 0 {
		if _, err := w.out.Write(w.pending); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// consumeText übernimmt normalen Text bis zum nächsten ESC in den
// Ausgabepuffer. Ein gefundenes ESC wird verbraucht und startet den
// Präfix-Vergleich.
//
// @param p - restliche Bytes des Write-Aufrufs
// @return int - Anzahl verbrauchter Bytes (mindestens 1)
// @date   2026-10-14 10:20
func (w *osc52Writer) consumeText(p []byte) int {
	for i, b := range p {
		if b == asciiESC {
			w.pending = append(w.pending, p[:i]...)
			w.state = osc52StateIntro
			w.introLen = 1
			return i + 1
		}
	}
	w.pending = append(w.pending, p...)
	return len(p)
}

// consumePayload sammelt Nutzdaten bis zum Terminator (BEL oder ESC).
//
// @param p - restliche Bytes des Write-Aufrufs
// @return int - Anzahl verbrauchter Bytes (mindestens 1)
// @date   2026-10-14 10:20
func (w *osc52Writer) consumePayload(p []byte) int {
	for i, b := range p {
		switch b {
		case asciiBEL:
			w.appendPayload(p[:i])
			w.finishPayload(osc52TerminatorBEL)
			return i + 1
		case asciiESC:
			// Könnte der Anfang von ESC \ (ST) sein - nächstes Byte entscheidet
			w.appendPayload(p[:i])
			w.state = osc52StatePayloadEsc
			return i + 1
		}
	}
	w.appendPayload(p)
	return len(p)
}

// startPayload wechselt nach vollständigem Präfix in den Sammel-Zustand.
//
// @date 2026-10-14 10:20
func (w *osc52Writer) startPayload() {
	w.state = osc52StatePayload
	w.payload = w.payload[:0]
	w.oversized = false
}

// appendPayload hängt Nutzdaten an und überwacht das Größenlimit. Nach einer
// Überschreitung wird nichts mehr gespeichert, aber bis zum Terminator
// weitergelesen, damit der Rest der Sequenz nicht als Text erscheint.
//
// @param chunk - Teil der Nutzdaten
// @date 2026-10-14 10:20
func (w *osc52Writer) appendPayload(chunk []byte) {
	if w.oversized {
		return
	}
	if len(w.payload)+len(chunk) > osc52MaxPayload {
		w.oversized = true
		w.payload = nil // großen Puffer sofort freigeben
		return
	}
	w.payload = append(w.payload, chunk...)
}

// resetPayload verwirft die gesammelten Nutzdaten. Puffer über 64 KiB
// werden freigegeben, damit ein einmaliger großer Kopiervorgang nicht
// dauerhaft Speicher belegt.
//
// @date 2026-10-14 10:20
func (w *osc52Writer) resetPayload() {
	if cap(w.payload) > 64*1024 {
		w.payload = nil
	} else {
		w.payload = w.payload[:0]
	}
	w.oversized = false
}

// finishPayload wertet eine vollständige OSC-52-Sequenz aus und schreibt den
// Inhalt in die Zwischenablage. Ungültige Sequenzen und Leseanfragen werden
// verworfen. Schlägt das Schreiben in die Zwischenablage fehl, wird die
// Original-Sequenz an das Terminal durchgereicht - vielleicht beherrscht der
// Terminal-Emulator OSC 52 selbst.
//
// @param terminator - verwendeter Terminator (für das Durchreichen)
// @date 2026-10-14 10:20
func (w *osc52Writer) finishPayload(terminator []byte) {
	defer func() {
		w.resetPayload()
		w.state = osc52StateText
	}()

	if w.oversized {
		return
	}
	text, ok := decodeOSC52Payload(w.payload)
	if !ok {
		return
	}
	if err := writeClipboard(text); err != nil {
		w.pending = append(w.pending, osc52Intro...)
		w.pending = append(w.pending, w.payload...)
		w.pending = append(w.pending, terminator...)
	}
}

// decodeOSC52Payload zerlegt "<Selektion>;<Base64>" und dekodiert den Text.
// Der Selektions-Parameter (c, p, s0, leer ...) wird nicht ausgewertet: Es
// gibt nur eine System-Zwischenablage, in die alles geschrieben wird.
//
// @param payload - Nutzdaten zwischen Präfix und Terminator
// @return string - dekodierter Text
// @return bool - false bei Leseanfrage ("?"), leerem oder ungültigem Inhalt
// @date 2026-10-14 15:50
func decodeOSC52Payload(payload []byte) (string, bool) {
	sep := -1
	for i, b := range payload {
		if b == ';' {
			sep = i
			break
		}
	}
	if sep < 0 {
		return "", false
	}
	encoded := string(payload[sep+1:])
	// "?" = Leseanfrage - wird aus Sicherheitsgründen nie beantwortet
	if encoded == "" || encoded == "?" {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// Manche Programme lassen das "="-Padding weg
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return "", false
		}
	}
	text := sanitizeClipboardText(string(decoded))
	if text == "" {
		return "", false
	}
	return text, true
}

// sanitizeClipboardText entfernt Steuerzeichen aus Text, den der Remote-Server
// in die Zwischenablage legen will. Erhalten bleiben Tab, CR und LF (für
// mehrzeilige Code-Schnipsel). Entfernt werden alle übrigen C0-Zeichen, DEL
// und C1-Zeichen (U+0080-U+009F).
//
// Hintergrund (Paste-Jacking): Ein bösartiger Server könnte sonst z.B.
// "ESC[201~" (Ende von Bracketed Paste) in die Zwischenablage legen. Beim
// späteren Einfügen in eine lokale Shell würde der folgende Text dann als
// getippte Eingabe gelten und sofort ausgeführt.
//
// @param text - dekodierter Text vom Server
// @return string - Text ohne gefährliche Steuerzeichen
// @date 2026-10-14 15:50
func sanitizeClipboardText(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return -1 // Zeichen entfernen
		default:
			return r
		}
	}, text)
}

// Flush schreibt zurückgehaltene Präfix-Bytes aus (sie waren keine OSC 52)
// und verwirft eine unvollständige OSC-52-Sequenz. Danach beginnt der Writer
// in sauberem Zustand. Aufrufen, wenn die SSH-Session beendet ist.
//
// @return error - Schreibfehler des Ausgabeziels
// @date 2026-10-14 10:20
func (w *osc52Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	state, introLen := w.state, w.introLen
	w.state = osc52StateText
	w.introLen = 0
	w.resetPayload()

	if state == osc52StateIntro && introLen > 0 {
		_, err := w.out.Write(osc52Intro[:introLen])
		return err
	}
	return nil
}

// attachOutputFilters hängt je einen OSC-52-Filter vor stdout und stderr der
// SSH-Session. Hinter jedem Filter sitzt ein Bracketed-Paste-Tracker, der den
// gemeinsamen pasteMode aktualisiert (siehe bracketed_paste.go). Die
// zurückgegebene Funktion muss nach session.Wait() aufgerufen werden: Sie
// gibt zurückgehaltene Präfix-Bytes aus und verwirft unvollständige
// OSC-52-Sequenzen.
//
// @param session - SSH-Session vor dem Start der Shell
// @param stdout - Ziel für die Standardausgabe (typischerweise os.Stdout)
// @param stderr - Ziel für die Fehlerausgabe (typischerweise os.Stderr)
// @param pasteMode - Bracketed-Paste-Modus der Session (nil = nicht verfolgen)
// @return func() - Flush-Funktion, nach Session-Ende aufrufen
// @date 2026-10-14 17:10
func attachOutputFilters(session *ssh.Session, stdout, stderr io.Writer, pasteMode *bracketedPasteMode) func() {
	outFilter := newOSC52Writer(newBracketedPasteTracker(stdout, pasteMode))
	errFilter := newOSC52Writer(newBracketedPasteTracker(stderr, pasteMode))
	session.Stdout = outFilter
	session.Stderr = errFilter
	return func() {
		_ = outFilter.Flush()
		_ = errFilter.Flush()
	}
}
