// Tests für die OSC-52-Clipboard-Unterstützung im SSH-Terminal.
//
// Hintergrund: Remote-Programme (z.B. Claude Code, vim, tmux) kopieren Text
// über die Escape-Sequenz OSC 52 in die lokale Zwischenablage. Die Windows-
// Konsole (conhost) kennt OSC 52 nicht und verwirft die Sequenz kommentarlos.
// osc52Writer fängt die Sequenz im Ausgabestrom ab und schreibt den Inhalt
// selbst in die Zwischenablage.
//
// Prüft:
//   - Pass-Through: normaler Text, CSI-Sequenzen und fremde OSC-Sequenzen
//     (Fenstertitel, OSC-8-Hyperlinks) bleiben unverändert.
//   - OSC 52 mit BEL- und mit ST-Terminator wird erkannt und entfernt.
//   - Über mehrere Write-Aufrufe zerstückelte Sequenzen (jeder Schnittpunkt).
//   - Sonderfälle: Query "?", ungültiges Base64, leerer Inhalt, Base64 ohne
//     Padding, Unicode, verschiedene Selektions-Parameter, doppeltes ESC,
//     abgebrochene Sequenz, Größenlimit, mehrere Sequenzen hintereinander.
//   - Clipboard-Fehler: Original-Sequenz wird an das Terminal durchgereicht.
//   - Flush: unvollständige Präfixe werden ausgegeben, unvollständige
//     OSC-52-Nutzdaten verworfen.
//
// @author Kurt Ingwer
// @date   2026-10-14 10:00

package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// stubClipboardWriter ersetzt den echten Clipboard-Writer während eines Tests.
// Alle geschriebenen Texte landen im zurückgegebenen Slice-Zeiger.
//
// @param err - Fehler, den der Stub zurückgeben soll (nil = Erfolg)
// @return *[]string - Mitschnitt aller Schreibversuche
// @return func() - Cleanup-Funktion, via defer aufrufen
// @date   2026-10-14 10:00
func stubClipboardWriter(err error) (*[]string, func()) {
	original := writeClipboard
	written := &[]string{}
	writeClipboard = func(text string) error {
		*written = append(*written, text)
		return err
	}
	return written, func() {
		writeClipboard = original
	}
}

// osc52Seq baut eine OSC-52-Sequenz mit BEL-Terminator.
//
// @param selection - Selektions-Parameter (z.B. "c")
// @param text - Klartext, wird Base64-kodiert
// @return string - komplette Escape-Sequenz
// @date   2026-10-14 10:00
func osc52Seq(selection, text string) string {
	return "\x1b]52;" + selection + ";" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}

// runOSC52 schreibt alle Teile nacheinander in einen neuen osc52Writer,
// ruft Flush auf und liefert die Terminal-Ausgabe.
//
// @param t - Testkontext
// @param parts - einzelne Write-Aufrufe
// @return string - was beim Terminal angekommen ist
// @date   2026-10-14 10:00
func runOSC52(t *testing.T, parts ...string) string {
	t.Helper()
	var out bytes.Buffer
	w := newOSC52Writer(&out)
	for _, p := range parts {
		n, err := w.Write([]byte(p))
		if err != nil {
			t.Fatalf("Write(%q) Fehler: %v", p, err)
		}
		if n != len(p) {
			t.Fatalf("Write(%q) meldet n=%d, erwartet %d", p, n, len(p))
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush Fehler: %v", err)
	}
	return out.String()
}

// TestOSC52PassThroughPlainText prüft, dass normaler Text unverändert bleibt.
func TestOSC52PassThroughPlainText(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	in := "hallo welt\r\nzweite Zeile äöü 🚀\r\n"
	if got := runOSC52(t, in); got != in {
		t.Errorf("Ausgabe = %q, erwartet %q", got, in)
	}
	if len(*written) != 0 {
		t.Errorf("Clipboard darf nicht beschrieben werden, bekam %q", *written)
	}
}

// TestOSC52PassThroughOtherSequences prüft CSI-, Titel- und Hyperlink-Sequenzen.
func TestOSC52PassThroughOtherSequences(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	cases := []string{
		"\x1b[31mrot\x1b[0m",                                  // CSI Farbe
		"\x1b]0;Fenstertitel\x07text",                         // OSC 0 Titel
		"\x1b]8;;https://example.com\x1b\\Link\x1b]8;;\x1b\\", // OSC 8 Hyperlink
		"\x1b]5;kein52\x07",                                   // OSC 5 (Präfix "5" passt, dann nicht)
		"\x1b]521;x\x07",                                      // OSC 521 ist nicht OSC 52
		"\x1b[?1049h\x1b[H\x1b[2J",                            // Alt-Screen + Clear
		"\x1bM\x1b7\x1b8",                                     // einfache ESC-Sequenzen
	}
	for _, in := range cases {
		if got := runOSC52(t, in); got != in {
			t.Errorf("Ausgabe = %q, erwartet %q", got, in)
		}
	}
	if len(*written) != 0 {
		t.Errorf("Clipboard darf nicht beschrieben werden, bekam %q", *written)
	}
}

// TestOSC52WithBEL prüft die Standardform mit BEL-Terminator.
func TestOSC52WithBEL(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	got := runOSC52(t, "vorher"+osc52Seq("c", "hallo")+"nachher")
	if got != "vorhernachher" {
		t.Errorf("Ausgabe = %q, Sequenz muss entfernt werden", got)
	}
	if len(*written) != 1 || (*written)[0] != "hallo" {
		t.Errorf("Clipboard = %q, erwartet [hallo]", *written)
	}
}

// TestOSC52WithST prüft die Form mit String Terminator (ESC \).
func TestOSC52WithST(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("st-text")) + "\x1b\\"
	got := runOSC52(t, "a"+seq+"b")
	if got != "ab" {
		t.Errorf("Ausgabe = %q, erwartet %q", got, "ab")
	}
	if len(*written) != 1 || (*written)[0] != "st-text" {
		t.Errorf("Clipboard = %q, erwartet [st-text]", *written)
	}
}

// TestOSC52SplitAtEveryOffset zerlegt den Strom an jeder möglichen Stelle in
// zwei Write-Aufrufe - SSH-Pakete können überall getrennt werden.
func TestOSC52SplitAtEveryOffset(t *testing.T) {
	stream := "x\x1b[1m" + osc52Seq("c", "https://claude.ai/oauth?code=abc") + "y" +
		"\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("zwei")) + "\x1b\\z"
	for i := 0; i <= len(stream); i++ {
		written, cleanup := stubClipboardWriter(nil)
		got := runOSC52(t, stream[:i], stream[i:])
		cleanup()
		if got != "x\x1b[1myz" {
			t.Fatalf("Schnitt bei %d: Ausgabe = %q", i, got)
		}
		if len(*written) != 2 || (*written)[0] != "https://claude.ai/oauth?code=abc" || (*written)[1] != "zwei" {
			t.Fatalf("Schnitt bei %d: Clipboard = %q", i, *written)
		}
	}
}

// TestOSC52ByteByByte schreibt jedes Byte einzeln (Extremfall der Zerstückelung).
func TestOSC52ByteByByte(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	stream := "a\x1b]0;titel\x07" + osc52Seq("c", "einzeln") + "b"
	parts := make([]string, 0, len(stream))
	for i := 0; i < len(stream); i++ {
		parts = append(parts, stream[i:i+1])
	}
	got := runOSC52(t, parts...)
	if got != "a\x1b]0;titel\x07b" {
		t.Errorf("Ausgabe = %q", got)
	}
	if len(*written) != 1 || (*written)[0] != "einzeln" {
		t.Errorf("Clipboard = %q", *written)
	}
}

// TestOSC52QueryIsIgnored: Eine Leseanfrage ("?") darf weder beantwortet noch
// an das Terminal durchgereicht werden - sonst könnte ein Server die lokale
// Zwischenablage auslesen.
func TestOSC52QueryIsIgnored(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	got := runOSC52(t, "a\x1b]52;c;?\x07b")
	if got != "ab" {
		t.Errorf("Ausgabe = %q, Query muss verschluckt werden", got)
	}
	if len(*written) != 0 {
		t.Errorf("Clipboard darf nicht beschrieben werden, bekam %q", *written)
	}
}

// TestOSC52InvalidBase64 verwirft kaputte Nutzdaten ohne Absturz.
func TestOSC52InvalidBase64(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	got := runOSC52(t, "a\x1b]52;c;!!!kein base64!!!\x07b")
	if got != "ab" {
		t.Errorf("Ausgabe = %q", got)
	}
	if len(*written) != 0 {
		t.Errorf("Clipboard darf nicht beschrieben werden, bekam %q", *written)
	}
}

// TestOSC52MissingSelectionSeparator: "ESC]52;abc BEL" ohne zweites Semikolon.
func TestOSC52MissingSelectionSeparator(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	got := runOSC52(t, "a\x1b]52;aGFsbG8=\x07b")
	if got != "ab" {
		t.Errorf("Ausgabe = %q", got)
	}
	if len(*written) != 0 {
		t.Errorf("Clipboard darf nicht beschrieben werden, bekam %q", *written)
	}
}

// TestOSC52EmptyPayloadIgnored: leere Nutzdaten überschreiben nichts.
func TestOSC52EmptyPayloadIgnored(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	got := runOSC52(t, "a\x1b]52;c;\x07b")
	if got != "ab" {
		t.Errorf("Ausgabe = %q", got)
	}
	if len(*written) != 0 {
		t.Errorf("Clipboard darf nicht beschrieben werden, bekam %q", *written)
	}
}

// TestOSC52UnpaddedBase64 akzeptiert Base64 ohne "="-Padding.
func TestOSC52UnpaddedBase64(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	runOSC52(t, "\x1b]52;c;aGFsbG8\x07") // "hallo" ohne Padding
	if len(*written) != 1 || (*written)[0] != "hallo" {
		t.Errorf("Clipboard = %q, erwartet [hallo]", *written)
	}
}

// TestOSC52Unicode prüft UTF-8-Inhalte inklusive Emoji und Zeilenumbrüchen.
func TestOSC52Unicode(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	text := "Grüße aus Köln 🚀\nzweite Zeile"
	runOSC52(t, osc52Seq("c", text))
	if len(*written) != 1 || (*written)[0] != text {
		t.Errorf("Clipboard = %q, erwartet %q", *written, text)
	}
}

// TestOSC52SelectionParameters: tmux sendet oft leere Selektion, andere "p"/"s0".
func TestOSC52SelectionParameters(t *testing.T) {
	for _, sel := range []string{"c", "", "p", "s0", "pc"} {
		written, cleanup := stubClipboardWriter(nil)
		got := runOSC52(t, osc52Seq(sel, "sel-"+sel))
		cleanup()
		if got != "" {
			t.Errorf("Selektion %q: Ausgabe = %q", sel, got)
		}
		if len(*written) != 1 || (*written)[0] != "sel-"+sel {
			t.Errorf("Selektion %q: Clipboard = %q", sel, *written)
		}
	}
}

// TestOSC52DoubleEscape: Ein einzelnes ESC vor der Sequenz wird durchgereicht,
// die eigentliche Sequenz trotzdem erkannt.
func TestOSC52DoubleEscape(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	got := runOSC52(t, "\x1b"+osc52Seq("c", "doppelt"))
	if got != "\x1b" {
		t.Errorf("Ausgabe = %q, erwartet einzelnes ESC", got)
	}
	if len(*written) != 1 || (*written)[0] != "doppelt" {
		t.Errorf("Clipboard = %q", *written)
	}
}

// TestOSC52AbortedByNewSequence: ESC mitten in den Nutzdaten (nicht gefolgt
// von "\") bricht die OSC-Sequenz ab wie in xterm. Die neue Sequenz bleibt
// erhalten, die angefangenen Nutzdaten werden verworfen.
func TestOSC52AbortedByNewSequence(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	got := runOSC52(t, "\x1b]52;c;aGFs\x1b[31mX")
	if got != "\x1b[31mX" {
		t.Errorf("Ausgabe = %q", got)
	}
	if len(*written) != 0 {
		t.Errorf("Clipboard darf nicht beschrieben werden, bekam %q", *written)
	}
}

// TestOSC52AbortedByNewOSC52: Abbruch durch eine neue, vollständige OSC 52.
func TestOSC52AbortedByNewOSC52(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	got := runOSC52(t, "\x1b]52;c;aGFs"+osc52Seq("c", "neu"))
	if got != "" {
		t.Errorf("Ausgabe = %q", got)
	}
	if len(*written) != 1 || (*written)[0] != "neu" {
		t.Errorf("Clipboard = %q, erwartet [neu]", *written)
	}
}

// TestOSC52OversizedPayloadDropped: Nutzdaten über dem Limit werden komplett
// verworfen (Speicherschutz), nachfolgender Text bleibt erhalten.
func TestOSC52OversizedPayloadDropped(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	huge := strings.Repeat("A", osc52MaxPayload+10)
	got := runOSC52(t, "a\x1b]52;c;", huge, "\x07b")
	if got != "ab" {
		t.Errorf("Ausgabe-Länge = %d, erwartet \"ab\"", len(got))
	}
	if len(*written) != 0 {
		t.Errorf("Clipboard darf bei Übergröße nicht beschrieben werden")
	}

	// Danach muss der Writer wieder normal arbeiten
	written2, cleanup2 := stubClipboardWriter(nil)
	defer cleanup2()
	runOSC52(t, osc52Seq("c", "ok"))
	if len(*written2) != 1 || (*written2)[0] != "ok" {
		t.Errorf("Clipboard nach Übergröße = %q", *written2)
	}
}

// TestOSC52PayloadAtLimitAccepted: genau am Limit wird noch akzeptiert.
func TestOSC52PayloadAtLimitAccepted(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	// "c;" zählt mit zu den Nutzdaten - Base64-Länge muss Vielfaches von 4 sein
	b64Len := (osc52MaxPayload - 2) / 4 * 4
	payload := strings.Repeat("QUFB", b64Len/4) // "AAA" pro Block
	runOSC52(t, "\x1b]52;c;"+payload+"\x07")
	if len(*written) != 1 || len((*written)[0]) != b64Len/4*3 {
		t.Errorf("Clipboard am Limit nicht beschrieben (Einträge: %d)", len(*written))
	}
}

// TestOSC52ClipboardErrorPassesThrough: Kann die Zwischenablage nicht
// beschrieben werden (z.B. Linux ohne xclip), geht die Original-Sequenz an
// das Terminal - vielleicht kann der Terminal-Emulator OSC 52 selbst.
func TestOSC52ClipboardErrorPassesThrough(t *testing.T) {
	_, cleanup := stubClipboardWriter(errors.New("kein Clipboard"))
	defer cleanup()

	seqBEL := osc52Seq("c", "hallo")
	if got := runOSC52(t, "a"+seqBEL+"b"); got != "a"+seqBEL+"b" {
		t.Errorf("Ausgabe = %q, Sequenz muss durchgereicht werden", got)
	}
	seqST := "\x1b]52;c;aGFsbG8=\x1b\\"
	if got := runOSC52(t, seqST); got != seqST {
		t.Errorf("Ausgabe = %q, Sequenz mit ST muss durchgereicht werden", got)
	}
}

// TestOSC52MultipleSequences: mehrere Sequenzen in einem Write.
func TestOSC52MultipleSequences(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	got := runOSC52(t, osc52Seq("c", "eins")+"|"+osc52Seq("c", "zwei")+"|")
	if got != "||" {
		t.Errorf("Ausgabe = %q", got)
	}
	if len(*written) != 2 || (*written)[0] != "eins" || (*written)[1] != "zwei" {
		t.Errorf("Clipboard = %q", *written)
	}
}

// TestOSC52LongLoginURL: realistischer Fall - langer OAuth-Login-Link.
func TestOSC52LongLoginURL(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	url := "https://claude.ai/oauth/authorize?code=true&client_id=" +
		strings.Repeat("abcdef0123456789", 40) + "&state=xyz"
	stream := osc52Seq("c", url)
	// In 7-Byte-Häppchen zerlegen (wie kleine SSH-Pakete)
	var parts []string
	for i := 0; i < len(stream); i += 7 {
		end := i + 7
		if end > len(stream) {
			end = len(stream)
		}
		parts = append(parts, stream[i:end])
	}
	runOSC52(t, parts...)
	if len(*written) != 1 || (*written)[0] != url {
		t.Errorf("Login-URL nicht korrekt kopiert")
	}
}

// TestOSC52FlushIncompletePrefix: Ein abgeschnittenes Präfix am Stromende
// ist kein OSC 52 - die Bytes müssen beim Flush ausgegeben werden.
func TestOSC52FlushIncompletePrefix(t *testing.T) {
	for _, in := range []string{"\x1b", "\x1b]", "\x1b]5", "\x1b]52"} {
		if got := runOSC52(t, "a"+in); got != "a"+in {
			t.Errorf("Flush bei %q: Ausgabe = %q", in, got)
		}
	}
}

// TestOSC52FlushUnterminatedPayload: unterminierte OSC-52-Nutzdaten werden
// beim Flush verworfen und nie ins Clipboard geschrieben.
func TestOSC52FlushUnterminatedPayload(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	for _, in := range []string{"\x1b]52;", "\x1b]52;c;aGFsbG8=", "\x1b]52;c;aGFsbG8=\x1b"} {
		if got := runOSC52(t, "a"+in); got != "a" {
			t.Errorf("Flush bei %q: Ausgabe = %q, erwartet \"a\"", in, got)
		}
	}
	if len(*written) != 0 {
		t.Errorf("Clipboard darf nicht beschrieben werden, bekam %q", *written)
	}
}

// TestOSC52WriterStateResetsAfterFlush: Nach Flush beginnt der Writer frisch.
func TestOSC52WriterStateResetsAfterFlush(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	var out bytes.Buffer
	w := newOSC52Writer(&out)
	_, _ = w.Write([]byte("\x1b]52;c;aGFs"))
	_ = w.Flush()
	_, _ = w.Write([]byte("normal"))
	_ = w.Flush()
	if out.String() != "normal" {
		t.Errorf("Ausgabe = %q, erwartet \"normal\"", out.String())
	}
	if len(*written) != 0 {
		t.Errorf("Clipboard = %q", *written)
	}
}

// errWriter liefert bei jedem Write einen Fehler (simuliert geschlossenes Terminal).
type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) { return 0, errors.New("terminal weg") }

// TestOSC52PropagatesOutputError: Schreibfehler des Terminals werden gemeldet.
func TestOSC52PropagatesOutputError(t *testing.T) {
	w := newOSC52Writer(errWriter{})
	if _, err := w.Write([]byte("text")); err == nil {
		t.Error("Schreibfehler des Terminals muss zurückgegeben werden")
	}
}

// TestAttachOutputFilters prüft die Einbindung in die SSH-Session: stdout und
// stderr laufen beide durch einen eigenen OSC-52-Filter, die Flush-Funktion
// gibt zurückgehaltene Präfix-Bytes beider Ströme aus.
func TestAttachOutputFilters(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	session := &ssh.Session{}
	flush := attachOutputFilters(session, &stdout, &stderr)

	_, _ = session.Stdout.Write([]byte("out" + osc52Seq("c", "von-stdout") + "\x1b"))
	_, _ = session.Stderr.Write([]byte("err" + osc52Seq("c", "von-stderr") + "\x1b]"))
	flush()

	if stdout.String() != "out\x1b" {
		t.Errorf("stdout = %q, erwartet %q", stdout.String(), "out\x1b")
	}
	if stderr.String() != "err\x1b]" {
		t.Errorf("stderr = %q, erwartet %q", stderr.String(), "err\x1b]")
	}
	if len(*written) != 2 || (*written)[0] != "von-stdout" || (*written)[1] != "von-stderr" {
		t.Errorf("Clipboard = %q", *written)
	}
}

// TestOSC52StripsEscape: ESC aus Remote-Inhalten wird entfernt. Sonst könnte
// ein Server z.B. "ESC[201~" (Ende von Bracketed Paste) in die Zwischenablage
// legen und beim späteren Einfügen in eine lokale Shell Befehle sofort
// ausführen lassen (Paste-Jacking).
func TestOSC52StripsEscape(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	runOSC52(t, osc52Seq("c", "harmlos\x1b[201~rm -rf ~\r"))
	if len(*written) != 1 || (*written)[0] != "harmlos[201~rm -rf ~\r" {
		t.Errorf("Clipboard = %q, ESC muss entfernt sein", *written)
	}
}

// TestOSC52KeepsTabAndLineBreaks: Tab, CR und LF bleiben erhalten - mehrzeilige
// Code-Schnipsel müssen korrekt kopiert werden.
func TestOSC52KeepsTabAndLineBreaks(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	text := "func main() {\r\n\tfmt.Println(\"hi\")\n}"
	runOSC52(t, osc52Seq("c", text))
	if len(*written) != 1 || (*written)[0] != text {
		t.Errorf("Clipboard = %q, erwartet %q", *written, text)
	}
}

// TestOSC52StripsOtherControlChars: übrige C0-Steuerzeichen, DEL und
// C1-Steuerzeichen (z.B. U+009B = 8-Bit-CSI) werden entfernt.
func TestOSC52StripsOtherControlChars(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	runOSC52(t, osc52Seq("c", "a\x00b\x07c\x08d\x7fe\u009bf\u0085g"))
	if len(*written) != 1 || (*written)[0] != "abcdefg" {
		t.Errorf("Clipboard = %q, erwartet [abcdefg]", *written)
	}
}

// TestOSC52OnlyControlCharsIgnored: Bleibt nach dem Filtern nichts übrig,
// wird die Zwischenablage nicht angefasst.
func TestOSC52OnlyControlCharsIgnored(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	runOSC52(t, osc52Seq("c", "\x1b\x00\x07"))
	if len(*written) != 0 {
		t.Errorf("Clipboard darf nicht beschrieben werden, bekam %q", *written)
	}
}
