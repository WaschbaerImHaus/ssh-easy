// Tests für Shift+Insert Paste-Handling im SSH-Terminal.
//
// Prüft:
//   - Pass-Through ohne Sequenz: Eingaben werden unverändert durchgereicht.
//   - Shift+Insert erkannt: VT-Sequenz ESC[2;2~ wird durch Clipboard-Inhalt ersetzt.
//   - Mehrfach-Sequenz in einem Read-Buffer.
//   - Zeilenenden-Normalisierung (\r\n und \n → \r, damit Remote-Shell Enter sieht).
//   - Fehlender Clipboard-Inhalt (Reader liefert Fehler) darf nicht abstürzen.
//   - Nicht-Shift-Insert Escape-Sequenzen (z.B. Pfeiltasten) werden unverändert weitergereicht.
//
// @author Kurt Ingwer
// @date   2026-04-19 00:00

package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// stubClipboard ersetzt den echten Clipboard-Reader während eines Tests und
// gibt am Ende eine Cleanup-Funktion zurück, die den Original-Reader wiederherstellt.
//
// @param text - Text, den der Stub liefern soll
// @param err  - optionaler Fehler statt Text
// @return func() - Cleanup-Funktion, via defer aufrufen
// @date   2026-04-19 00:00
func stubClipboard(text string, err error) func() {
	original := readClipboard
	readClipboard = func() (string, error) {
		return text, err
	}
	return func() {
		readClipboard = original
	}
}

// TestForwardStdinPassthrough stellt sicher, dass Bytes ohne Shift+Insert unverändert
// durchgereicht werden. Basis-Fall: normale Tastatureingaben dürfen nicht verändert werden.
func TestForwardStdinPassthrough(t *testing.T) {
	restore := stubClipboard("should-not-be-used", nil)
	defer restore()

	input := []byte("hello world\rexit\r")
	r := bytes.NewReader(input)
	var w bytes.Buffer

	forwardStdinWithPaste(r, &w)

	if !bytes.Equal(w.Bytes(), input) {
		t.Errorf("Passthrough fehlgeschlagen: erwartet %q, erhalten %q", input, w.Bytes())
	}
}

// TestForwardStdinShiftInsertReplacement prüft, dass die VT-Sequenz ESC[2;2~
// durch den Clipboard-Inhalt ersetzt wird, umliegende Bytes bleiben erhalten.
func TestForwardStdinShiftInsertReplacement(t *testing.T) {
	restore := stubClipboard("pasted", nil)
	defer restore()

	// Eingabe: "ab" + Shift+Insert + "cd"
	input := append([]byte("ab"), shiftInsertSeq...)
	input = append(input, []byte("cd")...)

	r := bytes.NewReader(input)
	var w bytes.Buffer

	forwardStdinWithPaste(r, &w)

	expected := "abpastedcd"
	if w.String() != expected {
		t.Errorf("Paste-Ersetzung fehlgeschlagen: erwartet %q, erhalten %q", expected, w.String())
	}
}

// TestForwardStdinMultipleShiftInsert prüft zwei aufeinanderfolgende Shift+Insert
// Sequenzen im selben Read-Buffer.
func TestForwardStdinMultipleShiftInsert(t *testing.T) {
	restore := stubClipboard("X", nil)
	defer restore()

	// "a" + Shift+Insert + "b" + Shift+Insert + "c"
	var input []byte
	input = append(input, 'a')
	input = append(input, shiftInsertSeq...)
	input = append(input, 'b')
	input = append(input, shiftInsertSeq...)
	input = append(input, 'c')

	r := bytes.NewReader(input)
	var w bytes.Buffer

	forwardStdinWithPaste(r, &w)

	expected := "aXbXc"
	if w.String() != expected {
		t.Errorf("Mehrfach-Paste fehlgeschlagen: erwartet %q, erhalten %q", expected, w.String())
	}
}

// TestForwardStdinLineEndingNormalization stellt sicher, dass CRLF und LF in
// Clipboard-Inhalten zu CR konvertiert werden (entspricht Enter in Raw-Mode).
func TestForwardStdinLineEndingNormalization(t *testing.T) {
	restore := stubClipboard("line1\r\nline2\nline3", nil)
	defer restore()

	r := bytes.NewReader(shiftInsertSeq)
	var w bytes.Buffer

	forwardStdinWithPaste(r, &w)

	expected := "line1\rline2\rline3"
	if w.String() != expected {
		t.Errorf("Zeilenenden-Normalisierung fehlgeschlagen: erwartet %q, erhalten %q", expected, w.String())
	}
}

// TestForwardStdinClipboardError prüft, dass ein Clipboard-Fehler nicht abstürzt
// und die umliegenden Bytes trotzdem durchgereicht werden (Shift+Insert wird schlicht ignoriert).
func TestForwardStdinClipboardError(t *testing.T) {
	restore := stubClipboard("", errors.New("clipboard not available"))
	defer restore()

	input := append([]byte("before"), shiftInsertSeq...)
	input = append(input, []byte("after")...)

	r := bytes.NewReader(input)
	var w bytes.Buffer

	forwardStdinWithPaste(r, &w)

	// Bei Fehler wird die Sequenz einfach verschluckt (keine Paste, kein Crash).
	expected := "beforeafter"
	if w.String() != expected {
		t.Errorf("Clipboard-Fehler-Handling fehlgeschlagen: erwartet %q, erhalten %q", expected, w.String())
	}
}

// TestForwardStdinUnrelatedEscapeSequence stellt sicher, dass andere VT-Sequenzen
// (z.B. Pfeil-hoch ESC[A) unverändert weitergereicht werden.
func TestForwardStdinUnrelatedEscapeSequence(t *testing.T) {
	restore := stubClipboard("SHOULD-NOT-APPEAR", nil)
	defer restore()

	// Pfeil-hoch-Sequenz + Text
	arrowUp := []byte{0x1b, '[', 'A'}
	input := append([]byte{}, arrowUp...)
	input = append(input, []byte("ls\r")...)

	r := bytes.NewReader(input)
	var w bytes.Buffer

	forwardStdinWithPaste(r, &w)

	if !bytes.Equal(w.Bytes(), input) {
		t.Errorf("Fremde Escape-Sequenz wurde verändert: erwartet %q, erhalten %q", input, w.Bytes())
	}
	if strings.Contains(w.String(), "SHOULD-NOT-APPEAR") {
		t.Errorf("Clipboard-Inhalt versehentlich eingefügt")
	}
}

// TestShiftInsertSequenceBytes dokumentiert die erwartete VT-Sequenz und dient
// als Regression-Schutz falls jemand die Konstante versehentlich ändert.
func TestShiftInsertSequenceBytes(t *testing.T) {
	expected := []byte{0x1b, '[', '2', ';', '2', '~'}
	if !bytes.Equal(shiftInsertSeq, expected) {
		t.Errorf("Shift+Insert-Sequenz ist falsch: erwartet %q, erhalten %q", expected, shiftInsertSeq)
	}
}

// writerAtEOF simuliert einen Writer, der beim ersten Schreiben Fehler liefert.
// Wird genutzt um zu prüfen, dass forwardStdinWithPaste bei Schreibfehlern sauber abbricht.
type writerAtEOF struct{}

// Write liefert immer io.ErrClosedPipe um das Verhalten einer geschlossenen SSH-Pipe zu simulieren.
//
// @return int - immer 0
// @return error - immer io.ErrClosedPipe
// @date   2026-04-19 00:00
func (writerAtEOF) Write(_ []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

// TestForwardStdinWriterError prüft, dass die Funktion bei geschlossenem Ziel
// sauber abbricht statt in einer Schleife zu hängen.
func TestForwardStdinWriterError(t *testing.T) {
	restore := stubClipboard("x", nil)
	defer restore()

	input := []byte("hello")
	r := bytes.NewReader(input)
	w := writerAtEOF{}

	// Darf nicht blockieren oder panicken.
	forwardStdinWithPaste(r, w)
}
