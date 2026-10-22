// Paket main - Tests für den Editor des Datei-Managers (F4, Build 62)
//
// Prüft den Ablauf in der Oberfläche mit memFS: Öffnen, Tippen, Speichern
// (Temp-Datei + Umbenennen, Rechte bleiben, CRLF bleibt), Rückfragen beim
// Beenden und bei zwischenzeitlicher Änderung auf dem Server, Symlinks,
// Hex-Bearbeitung von Binärdateien, Blöcke (F3/F5/F6/F8), Suchen (F7),
// Ersetzen (F4), Strg+Y, Größengrenze, Fehler und exakte Bildschirmgröße.
//
// @author Kurt Ingwer
// @date   2026-10-22 02:00
package main

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// openEditorOn öffnet den Editor für eine Datei der Quelle (/home/a).
func openEditorOn(t *testing.T, files map[string]string, name string) (AppModel, *memFS) {
	t.Helper()
	src := newMemFS("/home/a")
	for n, content := range files {
		src.addFile("/home/a/"+n, content, 0o640, copyTestTime)
	}
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	m = cursorTo(t, m, name)
	m = pressRun(t, m, "f4")
	if !m.fm.editing {
		t.Fatalf("Editor nicht geöffnet (Meldung %q)", m.fm.message)
	}
	return m, src
}

// editText liefert den Bildschirm ohne Farbcodes.
func editText(m AppModel) string { return stripANSI(m.View()) }

// typeEdit tippt Text in den Editor (ohne Blink-Kommandos).
func typeEdit(t *testing.T, m AppModel, text string) AppModel {
	t.Helper()
	for _, r := range text {
		switch r {
		case '\n':
			m = press(t, m, "enter")
		case ' ':
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			m = updated.(AppModel)
		default:
			m = press(t, m, string(r))
		}
	}
	return m
}

// TestEditorTypeAndSave - Tippen und Speichern: Inhalt auf dem Server,
// Rechte erhalten, keine Temp-Dateien.
func TestEditorTypeAndSave(t *testing.T) {
	m, src := openEditorOn(t, map[string]string{"notiz.txt": "Welt\n"}, "notiz.txt")
	text := editText(m)
	if !strings.Contains(text, "Welt") || !strings.Contains(strings.Split(text, "\n")[0], "/home/a/notiz.txt") {
		t.Fatalf("Editor zeigt die Datei nicht:\n%s", text)
	}
	m = typeEdit(t, m, "Hallo ")
	if !m.fm.edit.modified {
		t.Error("nicht als geändert markiert")
	}
	if !strings.Contains(strings.Split(editText(m), "\n")[0], "*") {
		t.Error("Kopfzeile ohne Änderungs-Kennzeichen")
	}
	m = pressRun(t, m, "f2")
	if got, _ := src.content("/home/a/notiz.txt"); got != "Hallo Welt\n" {
		t.Errorf("gespeichert: %q", got)
	}
	if n := src.node("/home/a/notiz.txt"); n.mode.Perm() != 0o640 {
		t.Errorf("Rechte %v, erwartet 0640", n.mode.Perm())
	}
	assertNoTempFiles(t, src)
	if m.fm.edit.modified || m.fm.edit.message != fmt.Sprintf(m.tr(txtFMEditSaved), "notiz.txt") {
		t.Errorf("nach Speichern: geändert=%v Meldung %q", m.fm.edit.modified, m.fm.edit.message)
	}
	assertScreenSize(t, m)
}

// TestEditorKeepsLineEndings - CRLF und fehlender Umbruch am Ende bleiben.
func TestEditorKeepsLineEndings(t *testing.T) {
	for _, content := range []string{"a\r\nb\r\n", "ohne Umbruch", "a\nb"} {
		m, src := openEditorOn(t, map[string]string{"f.txt": content}, "f.txt")
		m = press(t, m, "end")
		m = typeEdit(t, m, "!")
		m = pressRun(t, m, "f2")
		want := strings.Replace(content, "a", "a!", 1)
		if content == "ohne Umbruch" {
			want = "ohne Umbruch!"
		}
		if got, _ := src.content("/home/a/f.txt"); got != want {
			t.Errorf("%q gespeichert als %q, erwartet %q", content, got, want)
		}
	}
}

// TestEditorQTypesLetter - q ist im Editor ein Buchstabe.
func TestEditorQTypesLetter(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": ""}, "f.txt")
	m = typeEdit(t, m, "q")
	if !m.fm.editing || string(m.fm.edit.text.lines[0]) != "q" {
		t.Errorf("q: editing=%v Zeile %q", m.fm.editing, string(m.fm.edit.text.lines[0]))
	}
}

// TestEditorQuitDialog - Beenden mit Änderungen fragt nach.
func TestEditorQuitDialog(t *testing.T) {
	m, src := openEditorOn(t, map[string]string{"f.txt": "alt"}, "f.txt")
	m = typeEdit(t, m, "X")
	m = pressRun(t, m, "f10")
	if m.fm.edit.dialog != edDialogSaveQuestion || !strings.Contains(editText(m), m.tr(txtFMEditSaveTitle)) {
		t.Fatalf("keine Rückfrage beim Beenden:\n%s", editText(m))
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "esc")
	if !m.fm.editing || m.fm.edit.dialog != edDialogNone {
		t.Fatal("Esc in der Rückfrage schließt den Editor")
	}
	m = pressRun(t, m, "f10")
	m = pressRun(t, m, "n")
	if m.fm.editing {
		t.Fatal("n schließt nicht")
	}
	if got, _ := src.content("/home/a/f.txt"); got != "alt" {
		t.Errorf("verworfen, aber gespeichert: %q", got)
	}
	// Mit j: speichern und schließen
	m = cursorTo(t, m, "f.txt")
	m = pressRun(t, m, "f4")
	m = typeEdit(t, m, "Y")
	m = pressRun(t, m, "esc")
	m = pressRun(t, m, "j")
	if m.fm.editing {
		t.Fatal("j schließt nicht")
	}
	if got, _ := src.content("/home/a/f.txt"); got != "Yalt" {
		t.Errorf("gespeichert: %q", got)
	}
	// Fenster zeigt die neue Größe
	m = cursorTo(t, m, "f.txt")
	if e, _ := m.fm.panels[0].current(); e.size != 4 {
		t.Errorf("Größe im Fenster %d, erwartet 4 (nicht neu eingelesen)", e.size)
	}
}

// TestEditorQuitUnmodified - ohne Änderungen schließen F10 und Esc sofort.
func TestEditorQuitUnmodified(t *testing.T) {
	for _, key := range []string{"f10", "esc"} {
		m, _ := openEditorOn(t, map[string]string{"f.txt": "x"}, "f.txt")
		m = pressRun(t, m, key)
		if m.fm.editing {
			t.Errorf("%s schließt nicht", key)
		}
	}
}

// TestEditorConflict - wurde die Datei auf dem Server geändert, fragt F2 nach.
func TestEditorConflict(t *testing.T) {
	m, src := openEditorOn(t, map[string]string{"f.txt": "eins"}, "f.txt")
	m = typeEdit(t, m, "A")
	// Jemand anderes ändert die Datei
	src.mu.Lock()
	src.nodes["/home/a/f.txt"].data = []byte("fremd geändert")
	src.nodes["/home/a/f.txt"].modTime = copyTestTime.Add(time.Hour)
	src.mu.Unlock()
	m = pressRun(t, m, "f2")
	if m.fm.edit.dialog != edDialogConflict {
		t.Fatalf("keine Rückfrage bei Konflikt (Dialog %v)", m.fm.edit.dialog)
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "n")
	if got, _ := src.content("/home/a/f.txt"); got != "fremd geändert" {
		t.Errorf("trotz Nein überschrieben: %q", got)
	}
	m = pressRun(t, m, "f2")
	m = pressRun(t, m, "j")
	if got, _ := src.content("/home/a/f.txt"); got != "Aeins" {
		t.Errorf("nach Ja: %q", got)
	}
	// Danach ist der neue Stand die Grundlage: erneutes Speichern ohne Rückfrage
	m = typeEdit(t, m, "B")
	m = pressRun(t, m, "f2")
	if m.fm.edit.dialog != edDialogNone {
		t.Error("zweites Speichern fragt erneut")
	}
}

// TestEditorSymlinkSavesTarget - über einen Symlink geöffnet: die Zieldatei
// wird gespeichert, der Symlink bleibt.
func TestEditorSymlinkSavesTarget(t *testing.T) {
	src := newMemFS("/home/a")
	src.addFile("/home/a/echt.txt", "Inhalt", 0o600, copyTestTime)
	src.addLink("/home/a/link.txt", "echt.txt")
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	m = cursorTo(t, m, "link.txt")
	m = pressRun(t, m, "f4")
	if !m.fm.editing {
		t.Fatalf("nicht geöffnet: %q", m.fm.message)
	}
	m = typeEdit(t, m, "Neu ")
	m = pressRun(t, m, "f2")
	if got, _ := src.content("/home/a/echt.txt"); got != "Neu Inhalt" {
		t.Errorf("Zieldatei: %q", got)
	}
	if n := src.node("/home/a/link.txt"); n == nil || n.mode&fs.ModeSymlink == 0 {
		t.Error("Symlink durch Datei ersetzt")
	}
	if n := src.node("/home/a/echt.txt"); n.mode.Perm() != 0o600 {
		t.Errorf("Rechte der Zieldatei %v", n.mode.Perm())
	}
}

// TestEditorBinaryHexEdit - Binärdatei: Hex-Ziffern überschreiben Bytes.
func TestEditorBinaryHexEdit(t *testing.T) {
	m, src := openEditorOn(t, map[string]string{"prog.bin": "\xad\x00\x01\x02"}, "prog.bin")
	if !m.fm.edit.hexMode {
		t.Fatal("Binärdatei nicht als Hex geöffnet")
	}
	text := editText(m)
	if !strings.Contains(text, "ad 00 01 02") {
		t.Errorf("Hex-Ansicht fehlt:\n%s", text)
	}
	m = typeEdit(t, m, "af")
	m = typeEdit(t, m, "80")
	m = typeEdit(t, m, "xz") // ungültig: nichts passiert
	m = pressRun(t, m, "f2")
	if got, _ := src.content("/home/a/prog.bin"); got != "\xaf\x80\x01\x02" {
		t.Errorf("gespeichert: % x", got)
	}
	assertNoTempFiles(t, src)
	assertScreenSize(t, m)
}

// TestEditorInvalidUTF8OpensHex - ungültiges UTF-8 wird byte-genau als Hex
// bearbeitet (Textbearbeitung könnte Bytes verfälschen).
func TestEditorInvalidUTF8OpensHex(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"latin1.txt": "Gr\xfc\xdfe"}, "latin1.txt")
	if !m.fm.edit.hexMode {
		t.Error("ungültiges UTF-8 nicht als Hex geöffnet")
	}
}

// TestEditorHexASCIIColumn - Tab wechselt in die Zeichenspalte.
func TestEditorHexASCIIColumn(t *testing.T) {
	m, src := openEditorOn(t, map[string]string{"x.bin": "\x00ab"}, "x.bin")
	m = press(t, m, "tab")
	m = typeEdit(t, m, "Z")
	m = pressRun(t, m, "f2")
	if got, _ := src.content("/home/a/x.bin"); got != "Zab" {
		t.Errorf("gespeichert: %q", got)
	}
}

// TestEditorHexBlockFunctionsUnavailable - Blockfunktionen melden sich in Hex.
func TestEditorHexBlockFunctionsUnavailable(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"x.bin": "\x00"}, "x.bin")
	for _, key := range []string{"f3", "f4", "f5", "f6", "f8"} {
		m = pressRun(t, m, key)
		if m.fm.edit.message != m.tr(txtFMEditNotInHex) {
			t.Errorf("%s: Meldung %q", key, m.fm.edit.message)
		}
	}
}

// TestEditorHexSearch - F7 sucht Hex-Bytes bzw. Text.
func TestEditorHexSearch(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"x.bin": "\x00\x00\x00MARKE\xad\xff"}, "x.bin")
	m = pressRun(t, m, "f7")
	m = typeEdit(t, m, "ad ff")
	m = press(t, m, "enter")
	if m.fm.edit.hexb.cursor != 8 {
		t.Errorf("Hex-Suche: Cursor %d, erwartet 8", m.fm.edit.hexb.cursor)
	}
	m = press(t, m, "home")
	m = pressRun(t, m, "f7")
	m = typeEdit(t, m, "MARKE")
	m = press(t, m, "enter")
	if m.fm.edit.hexb.cursor != 3 {
		t.Errorf("Text-Suche in Hex: Cursor %d, erwartet 3", m.fm.edit.hexb.cursor)
	}
}

// TestEditorBlockCopyMoveDelete prüft F3 (markieren), F5, F6, F8.
func TestEditorBlockCopyMoveDelete(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": "eins\nzwei\ndrei"}, "f.txt")
	// "eins" markieren
	m = pressRun(t, m, "f3")
	for i := 0; i < 4; i++ {
		m = press(t, m, "right")
	}
	m = pressRun(t, m, "f3")
	if !m.fm.edit.blockSet {
		t.Fatal("Block nicht gesetzt")
	}
	// ans Ende von "drei" und kopieren
	m = press(t, m, "down")
	m = press(t, m, "down")
	m = press(t, m, "end")
	m = pressRun(t, m, "f5")
	if got := string(m.fm.edit.text.bytes()); got != "eins\nzwei\ndreieins" {
		t.Errorf("F5: %q", got)
	}
	// Block "eins" (Zeile 1) ans Zeilenende von "zwei" verschieben
	m = press(t, m, "up")
	m = press(t, m, "end")
	m = pressRun(t, m, "f6")
	if got := string(m.fm.edit.text.bytes()); got != "\nzweieins\ndreieins" {
		t.Errorf("F6: %q", got)
	}
	// verschobener Block ist markiert -> F8 löscht ihn
	m = pressRun(t, m, "f8")
	if got := string(m.fm.edit.text.bytes()); got != "\nzwei\ndreieins" {
		t.Errorf("F8: %q", got)
	}
	if m.fm.edit.blockSet {
		t.Error("Block nach F8 noch gesetzt")
	}
}

// TestEditorNoBlockMessage - F5/F6/F8 ohne Block melden sich.
func TestEditorNoBlockMessage(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": "x"}, "f.txt")
	for _, key := range []string{"f5", "f6", "f8"} {
		m = pressRun(t, m, key)
		if m.fm.edit.message != m.tr(txtFMEditNoBlock) {
			t.Errorf("%s: Meldung %q", key, m.fm.edit.message)
		}
	}
}

// TestEditorMoveBlockIntoItself - Verschieben in den eigenen Block ist
// nicht möglich (nichts passiert).
func TestEditorMoveBlockIntoItself(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": "abcdef"}, "f.txt")
	m = pressRun(t, m, "f3")
	for i := 0; i < 4; i++ {
		m = press(t, m, "right")
	}
	m = pressRun(t, m, "f3")
	m = press(t, m, "left") // Cursor mitten im Block
	m = pressRun(t, m, "f6")
	if got := string(m.fm.edit.text.bytes()); got != "abcdef" {
		t.Errorf("Text verändert: %q", got)
	}
}

// TestEditorSearch - F7 springt zum Treffer, erneut F7+Enter weiter.
func TestEditorSearch(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": "eins\nZwei zwei\ndrei"}, "f.txt")
	m = pressRun(t, m, "f7")
	m = typeEdit(t, m, "zwei")
	m = press(t, m, "enter")
	if m.fm.edit.cur != (textPos{1, 0}) {
		t.Errorf("erster Treffer: %+v", m.fm.edit.cur)
	}
	m = pressRun(t, m, "f7")
	if m.fm.edit.input.Value() != "zwei" {
		t.Errorf("Suchfeld nicht vorbelegt: %q", m.fm.edit.input.Value())
	}
	m = press(t, m, "enter")
	if m.fm.edit.cur != (textPos{1, 5}) {
		t.Errorf("zweiter Treffer: %+v", m.fm.edit.cur)
	}
	m = pressRun(t, m, "f7")
	m = press(t, m, "enter")
	if m.fm.edit.message != fmt.Sprintf(m.tr(txtFMNotFound), "zwei") {
		t.Errorf("Meldung %q", m.fm.edit.message)
	}
}

// TestEditorReplace - F4: Suchen, Ersetzen-durch, Rückfrage je Treffer.
func TestEditorReplace(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": "a-a-a-a"}, "f.txt")
	m = pressRun(t, m, "f4")
	m = typeEdit(t, m, "a")
	m = press(t, m, "enter")
	if m.fm.edit.prompt != edPromptReplaceWith {
		t.Fatalf("keine Eingabe für den Ersatz (prompt %v)", m.fm.edit.prompt)
	}
	m = typeEdit(t, m, "OO")
	m = press(t, m, "enter")
	if m.fm.edit.dialog != edDialogReplace {
		t.Fatalf("keine Rückfrage je Treffer (Dialog %v)", m.fm.edit.dialog)
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "j") // erstes ersetzen
	m = pressRun(t, m, "n") // zweites überspringen
	m = pressRun(t, m, "a") // Rest ersetzen
	if got := string(m.fm.edit.text.bytes()); got != "OO-a-OO-OO" {
		t.Errorf("Ergebnis %q", got)
	}
	if m.fm.edit.message != fmt.Sprintf(m.tr(txtFMEditReplaced), 3) {
		t.Errorf("Meldung %q", m.fm.edit.message)
	}
	// Esc bricht ab
	m = pressRun(t, m, "home")
	m = pressRun(t, m, "f4")
	m = typeEdit(t, m, "OO")
	m = press(t, m, "enter")
	m = typeEdit(t, m, "x")
	m = press(t, m, "enter")
	m = pressRun(t, m, "esc")
	if got := string(m.fm.edit.text.bytes()); got != "OO-a-OO-OO" || m.fm.edit.dialog != edDialogNone {
		t.Errorf("nach Esc: %q Dialog %v", got, m.fm.edit.dialog)
	}
}

// TestEditorEditingKeys prüft Enter, Rücktaste, Entf, Tab und Strg+Y.
func TestEditorEditingKeys(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": "abc\nxyz"}, "f.txt")
	m = press(t, m, "right")
	m = press(t, m, "enter")
	if got := string(m.fm.edit.text.bytes()); got != "a\nbc\nxyz" {
		t.Errorf("Enter: %q", got)
	}
	m = press(t, m, "backspace")
	if got := string(m.fm.edit.text.bytes()); got != "abc\nxyz" {
		t.Errorf("Rücktaste: %q", got)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	m = updated.(AppModel)
	if got := string(m.fm.edit.text.bytes()); got != "ac\nxyz" {
		t.Errorf("Entf: %q", got)
	}
	m = press(t, m, "tab")
	if got := string(m.fm.edit.text.bytes()); got != "a\tc\nxyz" {
		t.Errorf("Tab: %q", got)
	}
	if !strings.Contains(editText(m), "a       c") {
		t.Errorf("Tab nicht erweitert angezeigt:\n%s", editText(m))
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	m = updated.(AppModel)
	if got := string(m.fm.edit.text.bytes()); got != "xyz" {
		t.Errorf("Strg+Y: %q", got)
	}
}

// TestEditorPaste - eingefügter Text (auch mit CRLF) wird übernommen.
func TestEditorPaste(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": ""}, "f.txt")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("eins\r\nzwei"), Paste: true})
	m = updated.(AppModel)
	if got := string(m.fm.edit.text.bytes()); got != "eins\nzwei" {
		t.Errorf("Einfügen: %q", got)
	}
}

// TestEditorSanitizesDisplay - Steuerzeichen aus der Datei steuern das
// Terminal nicht; sie bleiben aber unverändert in der Datei.
func TestEditorSanitizesDisplay(t *testing.T) {
	m, src := openEditorOn(t, map[string]string{"f.txt": "a\x1b[2Jb"}, "f.txt")
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Error("Steuersequenz in der Anzeige")
	}
	if !strings.Contains(editText(m), "a?[2Jb") {
		t.Errorf("Anzeige:\n%s", editText(m))
	}
	m = press(t, m, "end")
	m = typeEdit(t, m, "!")
	m = pressRun(t, m, "f2")
	if got, _ := src.content("/home/a/f.txt"); got != "a\x1b[2Jb!" {
		t.Errorf("Datei verändert: %q", got)
	}
}

// TestEditorTooLarge - zu große Dateien werden nicht geöffnet.
func TestEditorTooLarge(t *testing.T) {
	defer func(old int64) { fmViewMaxBytes = old }(fmViewMaxBytes)
	fmViewMaxBytes = 10
	src := newMemFS("/home/a")
	src.addFile("/home/a/gross.txt", strings.Repeat("x", 50), 0o644, copyTestTime)
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	m = cursorTo(t, m, "gross.txt")
	m = pressRun(t, m, "f4")
	if m.fm.editing {
		t.Error("zu große Datei geöffnet")
	}
	if m.fm.message != fmt.Sprintf(m.tr(txtFMEditTooLarge), "gross.txt", formatSizeWithUnit(10)) {
		t.Errorf("Meldung %q", m.fm.message)
	}
}

// TestEditorSaveError - Speichern scheitert (kein Schreibrecht im
// Verzeichnis): Meldung, Änderungen bleiben im Editor.
func TestEditorSaveError(t *testing.T) {
	m, src := openEditorOn(t, map[string]string{"f.txt": "x"}, "f.txt")
	src.denied["/home/a"] = true
	m = typeEdit(t, m, "Y")
	m = pressRun(t, m, "f2")
	if !m.fm.edit.modified || !strings.Contains(m.fm.edit.message, "f.txt") {
		t.Errorf("geändert=%v Meldung %q", m.fm.edit.modified, m.fm.edit.message)
	}
	src.denied["/home/a"] = false
	if got, _ := src.content("/home/a/f.txt"); got != "x" {
		t.Errorf("Datei trotz Fehler verändert: %q", got)
	}
	assertNoTempFiles(t, src)
}

// TestEditorF4OnDirectoryDoesNothing - F4 auf Verzeichnis öffnet nichts.
func TestEditorF4OnDirectoryDoesNothing(t *testing.T) {
	src := newCopySource()
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	for _, name := range []string{"projekt", ".."} {
		m = cursorTo(t, m, name)
		m = pressRun(t, m, "f4")
		if m.fm.editing || m.fm.panels[0].path != "/home/a" {
			t.Errorf("F4 auf %s: editing=%v Pfad %q", name, m.fm.editing, m.fm.panels[0].path)
		}
	}
}

// TestEditorKeyBarLikeMC - F-Tasten-Leiste wie mcedit.
func TestEditorKeyBarLikeMC(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": "x"}, "f.txt")
	m.termWidth = 120
	lines := strings.Split(editText(m), "\n")
	bar := lines[len(lines)-1]
	labels := strings.Split(m.tr(txtFMEditFKeys), "|")
	if len(labels) != 10 {
		t.Fatalf("%d Beschriftungen", len(labels))
	}
	for i, label := range labels {
		if want := fmt.Sprintf("%d%s", i+1, label); !strings.Contains(bar, want) {
			t.Errorf("Leiste ohne %q: %q", want, bar)
		}
	}
	for _, opt := range AvailableLanguages {
		if n := len(strings.Split(extraText(opt.Code, txtFMEditFKeys), "|")); n != 10 {
			t.Errorf("%s: %d Teile", opt.Code, n)
		}
	}
}

// TestEditorHelp - F1 zeigt die Hilfe (Text und Hex verschieden).
func TestEditorHelp(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": "x", "b.bin": "\x00"}, "f.txt")
	m = pressRun(t, m, "f1")
	if !strings.Contains(editText(m), m.tr(txtFMHelpTitle)) {
		t.Error("Hilfe fehlt")
	}
	m = pressRun(t, m, "z")
	if m.fm.edit.showHelp || string(m.fm.edit.text.lines[0]) != "x" {
		t.Error("Taste zum Schließen der Hilfe wurde getippt")
	}
}

// TestEditorScrollAndScreenSize - lange Dateien, Cursor bleibt sichtbar,
// exakte Größe in allen Farbmodi und Größen (Text und Hex).
func TestEditorScrollAndScreenSize(t *testing.T) {
	withColorProfile(t)
	var b strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&b, "Zeile %03d %s\n", i, strings.Repeat("breit日本", 15))
	}
	m, _ := openEditorOn(t, map[string]string{"lang.txt": b.String(), "bin.dat": strings.Repeat("\x00\xff", 500)}, "lang.txt")
	m.termWidth, m.termHeight = 80, 24
	m = press(t, m, "pgdown")
	m = press(t, m, "pgdown")
	m = press(t, m, "end")
	if !strings.Contains(editText(m), fmt.Sprintf("Zeile %03d", m.fm.edit.cur.row+1)) && m.fm.edit.left == 0 {
		t.Error("Cursorzeile nicht sichtbar")
	}
	for theme := range fmThemes {
		m.fm.theme = theme
		for _, size := range [][2]int{{100, 30}, {80, 24}, {40, 12}, {20, 5}} {
			m.termWidth, m.termHeight = size[0], size[1]
			assertScreenSize(t, m)
		}
	}
	m = pressRun(t, m, "f10")
	m = cursorTo(t, m, "bin.dat")
	m = pressRun(t, m, "f4")
	for _, size := range [][2]int{{100, 30}, {80, 24}, {40, 12}, {20, 5}} {
		m.termWidth, m.termHeight = size[0], size[1]
		m = press(t, m, "pgdown")
		assertScreenSize(t, m)
	}
}
