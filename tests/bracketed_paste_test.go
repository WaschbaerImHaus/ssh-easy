// Tests für Bracketed Paste beim Einfügen per Shift+Einfg.
//
// Hintergrund: Remote-Programme (bash/readline, zsh, vim ...) schalten mit
// ESC[?2004h den Modus "Bracketed Paste" ein. Eingefügter Text muss dann in
// ESC[200~ ... ESC[201~ eingeschlossen werden, damit die Shell ihn als Text
// erkennt und mehrzeilige Inhalte nicht Zeile für Zeile sofort ausführt.
//
// Prüft:
//   - Erkennung von ESC[?2004h/l im Ausgabestrom (auch kombiniert und zerteilt)
//   - Ausgabe wird vom Tracker nie verändert
//   - Keine Fehlerkennung bei ähnlichen Sequenzen (ohne "?", 20040, 1049 ...)
//   - Einschließen des Clipboard-Inhalts nur bei aktivem Modus
//   - Entfernen eingebetteter Start-/Endmarker (auch verschachtelt)
//   - Einbindung in attachOutputFilters (stdout und stderr)
//
// @author Kurt Ingwer
// @date   2026-10-14 17:00

package main

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// trackOutput schreibt die Teile nacheinander durch einen neuen Tracker und
// liefert die Ausgabe sowie den Modus danach.
//
// @param t - Testkontext
// @param parts - einzelne Write-Aufrufe (simuliert zerteilte SSH-Pakete)
// @return string - unveränderte Ausgabe am Terminal
// @return *bracketedPasteMode - Modus nach allen Writes
// @date   2026-10-14 17:00
func trackOutput(t *testing.T, parts ...string) (string, *bracketedPasteMode) {
	t.Helper()
	var out bytes.Buffer
	mode := &bracketedPasteMode{}
	tracker := newBracketedPasteTracker(&out, mode)
	for _, part := range parts {
		n, err := tracker.Write([]byte(part))
		if err != nil {
			t.Fatalf("Write(%q) Fehler: %v", part, err)
		}
		if n != len(part) {
			t.Fatalf("Write(%q) = %d, erwartet %d", part, n, len(part))
		}
	}
	return out.String(), mode
}

// TestBracketedPasteModeInitiallyInactive: Eine neue Session startet ohne Modus.
func TestBracketedPasteModeInitiallyInactive(t *testing.T) {
	mode := &bracketedPasteMode{}
	if mode.IsActive() {
		t.Error("Neuer Modus darf nicht aktiv sein")
	}
}

// TestBracketedPasteModeNilSafe: nil-Modus (kein Tracker) gilt als inaktiv.
func TestBracketedPasteModeNilSafe(t *testing.T) {
	var mode *bracketedPasteMode
	if mode.IsActive() {
		t.Error("nil-Modus muss als inaktiv gelten")
	}
}

// TestTrackerDetectsEnableAndDisable: ESC[?2004h schaltet ein, ESC[?2004l aus.
func TestTrackerDetectsEnableAndDisable(t *testing.T) {
	_, mode := trackOutput(t, "prompt$ \x1b[?2004h")
	if !mode.IsActive() {
		t.Fatal("ESC[?2004h muss den Modus aktivieren")
	}

	var out bytes.Buffer
	tracker := newBracketedPasteTracker(&out, mode)
	_, _ = tracker.Write([]byte("\x1b[?2004l"))
	if mode.IsActive() {
		t.Error("ESC[?2004l muss den Modus deaktivieren")
	}
}

// TestTrackerPassesOutputUnchanged: Der Tracker beobachtet nur - jedes Byte
// muss unverändert am Terminal ankommen, auch die Modus-Sequenzen selbst.
func TestTrackerPassesOutputUnchanged(t *testing.T) {
	in := "a\x1b[?2004hb\x1b[31mrot\x1b[0m\x1b]0;Titel\x07\x1b[?2004lc"
	out, _ := trackOutput(t, in)
	if out != in {
		t.Errorf("Ausgabe = %q, erwartet %q", out, in)
	}
}

// TestTrackerSplitAcrossWrites: SSH-Pakete können überall getrennt sein -
// die Sequenz wird an jeder Trennstelle erkannt.
func TestTrackerSplitAcrossWrites(t *testing.T) {
	seq := "x\x1b[?2004hy"
	for cut := 1; cut < len(seq); cut++ {
		out, mode := trackOutput(t, seq[:cut], seq[cut:])
		if !mode.IsActive() {
			t.Errorf("Schnitt bei %d: Modus nicht erkannt", cut)
		}
		if out != seq {
			t.Errorf("Schnitt bei %d: Ausgabe = %q", cut, out)
		}
	}
}

// TestTrackerByteByByte: Extremfall - jedes Byte in eigenem Write.
func TestTrackerByteByByte(t *testing.T) {
	seq := "\x1b[?2004h"
	parts := make([]string, len(seq))
	for i := range seq {
		parts[i] = seq[i : i+1]
	}
	_, mode := trackOutput(t, parts...)
	if !mode.IsActive() {
		t.Error("Byteweise geschriebene Sequenz nicht erkannt")
	}
}

// TestTrackerCombinedParameters: Mehrere Modi in einer Sequenz, z.B.
// ESC[?1049;2004h (Alternativ-Bildschirm + Bracketed Paste).
func TestTrackerCombinedParameters(t *testing.T) {
	for _, seq := range []string{"\x1b[?1049;2004h", "\x1b[?2004;1h", "\x1b[?1;2004;25h"} {
		if _, mode := trackOutput(t, seq); !mode.IsActive() {
			t.Errorf("%q: Modus nicht erkannt", seq)
		}
	}
	_, mode := trackOutput(t, "\x1b[?2004h", "\x1b[?1049;2004l")
	if mode.IsActive() {
		t.Error("ESC[?1049;2004l muss den Modus deaktivieren")
	}
}

// TestTrackerIgnoresSimilarSequences: Nur der private Modus 2004 zählt.
func TestTrackerIgnoresSimilarSequences(t *testing.T) {
	for _, seq := range []string{
		"\x1b[2004h",      // ohne "?" = ANSI-Modus, nicht DEC-privat
		"\x1b[?20040h",    // anderer Modus
		"\x1b[?200h",      // anderer Modus
		"\x1b[?12004h",    // anderer Modus
		"\x1b[?1049h",     // Alternativ-Bildschirm
		"\x1b[?2004$p",    // DECRQM-Abfrage, kein Setzen
		"\x1b]?2004h\x07", // OSC statt CSI
		"?2004h",          // ohne ESC
	} {
		if _, mode := trackOutput(t, seq); mode.IsActive() {
			t.Errorf("%q darf den Modus nicht aktivieren", seq)
		}
	}
}

// TestTrackerOtherModesKeepState: Andere Modi ändern den Zustand nicht.
func TestTrackerOtherModesKeepState(t *testing.T) {
	_, mode := trackOutput(t, "\x1b[?2004h", "\x1b[?1049l\x1b[?25l\x1b[2J")
	if !mode.IsActive() {
		t.Error("Andere Modi dürfen Bracketed Paste nicht abschalten")
	}
}

// TestTrackerInterruptedByEscape: Ein ESC mitten in der Sequenz startet eine
// neue Sequenz - die neue wird korrekt erkannt.
func TestTrackerInterruptedByEscape(t *testing.T) {
	_, mode := trackOutput(t, "\x1b[?20\x1b[?2004h")
	if !mode.IsActive() {
		t.Error("Sequenz nach abgebrochener Sequenz nicht erkannt")
	}
}

// TestTrackerOverlongParameters: Überlange Parameter werden verworfen (kein
// unbegrenzter Speicher), der Tracker funktioniert danach weiter.
func TestTrackerOverlongParameters(t *testing.T) {
	long := "\x1b[?" + strings.Repeat("1;", 10000) + "2004h"
	out, mode := trackOutput(t, long)
	if out != long {
		t.Error("Überlange Sequenz muss unverändert durchgereicht werden")
	}
	if mode.IsActive() {
		t.Error("Überlange Sequenz darf den Modus nicht setzen")
	}
	_, mode2 := trackOutput(t, long, "\x1b[?2004h")
	if !mode2.IsActive() {
		t.Error("Nach überlanger Sequenz muss die Erkennung weiter funktionieren")
	}
}

// TestTrackerPropagatesOutputError: Schreibfehler des Terminals werden gemeldet.
func TestTrackerPropagatesOutputError(t *testing.T) {
	tracker := newBracketedPasteTracker(errWriter{}, &bracketedPasteMode{})
	if _, err := tracker.Write([]byte("text")); err == nil {
		t.Error("Schreibfehler des Terminals muss zurückgegeben werden")
	}
}

// TestPreparePasteInactive: Ohne Modus wird der Text nur normalisiert (CR als
// Zeilenende) - Verhalten wie bisher.
func TestPreparePasteInactive(t *testing.T) {
	got := preparePaste("ls\npwd\n", &bracketedPasteMode{})
	if string(got) != "ls\rpwd\r" {
		t.Errorf("preparePaste = %q", got)
	}
	if got := preparePaste("ls\n", nil); string(got) != "ls\r" {
		t.Errorf("preparePaste mit nil-Modus = %q", got)
	}
}

// TestPreparePasteActiveWraps: Bei aktivem Modus wird der Text eingeschlossen.
func TestPreparePasteActiveWraps(t *testing.T) {
	mode := &bracketedPasteMode{}
	mode.set(true)
	got := preparePaste("ls\npwd\n", mode)
	want := "\x1b[200~ls\rpwd\r\x1b[201~"
	if string(got) != want {
		t.Errorf("preparePaste = %q, erwartet %q", got, want)
	}
}

// TestPreparePasteActiveStripsMarkers: Eingebettete Marker würden den
// Einfüge-Modus vorzeitig beenden (Paste-Jacking) - sie werden entfernt,
// auch wenn erst das Entfernen einen neuen Marker erzeugt.
func TestPreparePasteActiveStripsMarkers(t *testing.T) {
	mode := &bracketedPasteMode{}
	mode.set(true)
	cases := map[string]string{
		"echo ok\x1b[201~rm -rf ~\n":      "echo okrm -rf ~\r",
		"a\x1b[200~b":                     "ab",
		"a\x1b[20\x1b[201~1~b":            "ab",
		"harmlos\x1b[31mfarbig\x1b[0m":    "harmlos\x1b[31mfarbig\x1b[0m",
		"x\x1b[2\x1b[200~01~\x1b[201~y\n": "xy\r",
	}
	for in, inner := range cases {
		got := string(preparePaste(in, mode))
		want := "\x1b[200~" + inner + "\x1b[201~"
		if got != want {
			t.Errorf("preparePaste(%q) = %q, erwartet %q", in, got, want)
		}
		if strings.Count(got, "\x1b[201~") != 1 {
			t.Errorf("preparePaste(%q): Endmarker muss genau einmal vorkommen", in)
		}
	}
}

// TestPreparePasteEmpty: Leere Zwischenablage (oder nur Marker) erzeugt keine
// Ausgabe - auch keine leeren Marker.
func TestPreparePasteEmpty(t *testing.T) {
	mode := &bracketedPasteMode{}
	mode.set(true)
	for _, in := range []string{"", "\x1b[201~\x1b[200~", "\x1b[20\x1b[201~1~"} {
		if got := preparePaste(in, mode); len(got) != 0 {
			t.Errorf("preparePaste(%q) = %q, erwartet leer", in, got)
		}
	}
}

// TestForwardStdinBracketedPaste: Shift+Einfg bei aktivem Modus liefert den
// eingeschlossenen Clipboard-Inhalt an die Remote-Shell.
func TestForwardStdinBracketedPaste(t *testing.T) {
	restore := stubClipboard("echo eins\necho zwei\n", nil)
	defer restore()

	mode := &bracketedPasteMode{}
	mode.set(true)
	r := bytes.NewReader([]byte("a" + string(shiftInsertSeq) + "b"))
	var w bytes.Buffer
	forwardStdinWithPaste(r, &w, nil, mode)

	want := "a\x1b[200~echo eins\recho zwei\r\x1b[201~b"
	if w.String() != want {
		t.Errorf("Ausgabe = %q, erwartet %q", w.String(), want)
	}
}

// TestForwardStdinNoBracketedPasteWhenInactive: Ohne aktivierten Modus bleibt
// das bisherige Verhalten (roher Text) - Programme ohne Bracketed Paste
// würden die Marker sonst als Müll anzeigen.
func TestForwardStdinNoBracketedPasteWhenInactive(t *testing.T) {
	restore := stubClipboard("ls\n", nil)
	defer restore()

	r := bytes.NewReader(shiftInsertSeq)
	var w bytes.Buffer
	forwardStdinWithPaste(r, &w, nil, &bracketedPasteMode{})
	if w.String() != "ls\r" {
		t.Errorf("Ausgabe = %q, erwartet %q", w.String(), "ls\r")
	}
}

// TestAttachOutputFiltersTracksBracketedPaste: Die Session-Ausgabe (stdout
// und stderr) läuft durch den Tracker, OSC 52 wird weiterhin gefiltert.
func TestAttachOutputFiltersTracksBracketedPaste(t *testing.T) {
	written, cleanup := stubClipboardWriter(nil)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	session := &ssh.Session{}
	mode := &bracketedPasteMode{}
	flush := attachOutputFilters(session, &stdout, &stderr, mode)

	_, _ = session.Stdout.Write([]byte("$ \x1b[?2004h" + osc52Seq("c", "kopiert")))
	if !mode.IsActive() {
		t.Error("Modus über stdout nicht erkannt")
	}
	_, _ = session.Stderr.Write([]byte("\x1b[?2004l"))
	if mode.IsActive() {
		t.Error("Modus über stderr nicht abgeschaltet")
	}
	flush()

	if stdout.String() != "$ \x1b[?2004h" {
		t.Errorf("stdout = %q", stdout.String())
	}
	if len(*written) != 1 || (*written)[0] != "kopiert" {
		t.Errorf("Clipboard = %q", *written)
	}
}
