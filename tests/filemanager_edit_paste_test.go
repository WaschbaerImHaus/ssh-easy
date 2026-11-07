// Paket main - Tests: Einfügen aus der Zwischenablage im F4-Editor (Build 89, Punkt F)
//
// Nutzerwunsch: Im Editor des Datei-Managers soll Einfügen gehen, mit
//   - Strg+V (KeyCtrlV),
//   - Shift+Einfg: Windows = NUL-Rune + gedrücktes Shift und Einfg (Stub
//     keyIsDown), Unix = unbekannte CSI-Sequenz ESC[2;2~ (isShiftInsertSequence),
//   - Strg+Einfg: Windows = NUL-Rune + gedrücktes Strg (VK 0x11) und Einfg,
//     Unix = ESC[2;5~. Bubbletea v1.3.10 kennt "ctrl+insert" NICHT (nur
//     ESC[2~ = Einfg und ESC[3;2~); die Sequenz kommt als unbekannte CSI an.
//
// Die Zwischenablage kommt über den Stub readClipboard (stubClipboard).
//
// Festlegungen dieser Tests (der Lead hat sie offen gelassen):
//   - CRLF und einzelnes CR werden VOR dem Entfernen der Steuerzeichen zu LF.
//   - Entfernt werden alle Steuerzeichen außer \t und \n (ASCII 0-31 und 127
//     sowie C1 0x80-0x9F). "[31m" nach einem ESC bleibt als normaler Text.
//   - Ist nach dem Bereinigen nichts übrig, bleibt der Text unverändert und
//     modified false.
//   - Größengrenze: fmViewMaxBytes (Bytes der gespeicherten Datei). Wird sie
//     überschritten, wird abgelehnt: Text unverändert, modified false, eine
//     Meldung in edit.message. Genau auf der Grenze ist erlaubt. Gezählt wird
//     in Bytes (UTF-8), nicht in Zeichen.
//   - Auch ein Einfügen per Terminal (KeyMsg mit Paste-Flag, Bracketed Paste)
//     entfernt Steuerzeichen - sonst käme ESC auf diesem Weg in den Text.
//   - Einfügen wirkt nur im Textmodus ohne Dialog, Eingabezeile oder Hilfe.
//
// ANMERKUNG zur Hilfe: Die F1-Hilfe des Editors (txtFMEditHelpText in
// src/i18n_filemanager.go) listet Tasten auf; dort gehört eine Zeile
// "Strg+V / Shift+Einfg / Strg+Einfg  einfügen" ergänzt (specialist-i18n).
//
// @author Kurt Ingwer
// @date   2026-11-07 11:00
package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// editTestVKControl - virtueller Tastencode der Strg-Taste (VK_CONTROL) unter Windows
const editTestVKControl = 0x11

// sendEditMsg sendet eine beliebige Nachricht (Taste oder unbekannte CSI-Sequenz)
// an das Modell, ohne das Folgekommando auszuführen.
func sendEditMsg(m AppModel, msg tea.Msg) AppModel {
	updated, _ := m.Update(msg)
	return updated.(AppModel)
}

// editContent liefert den Textinhalt des Editors.
func editContent(m AppModel) string { return string(m.fm.edit.text.bytes()) }

// pasteVariant ist eine Tastenkombination zum Einfügen.
type pasteVariant struct {
	name string
	// send löst die Kombination aus (setzt ggf. den Tastenzustand-Stub)
	send func(t *testing.T, m AppModel) AppModel
}

// pasteVariants liefert alle Wege zum Einfügen.
func pasteVariants() []pasteVariant {
	return []pasteVariant{
		{"Strg+V", func(t *testing.T, m AppModel) AppModel {
			return sendEditMsg(m, tea.KeyMsg{Type: tea.KeyCtrlV})
		}},
		{"Windows Shift+Einfg", func(t *testing.T, m AppModel) AppModel {
			stubKeys(t, vkShift, vkInsert)
			return sendEditMsg(m, nulKey)
		}},
		{"Windows Strg+Einfg", func(t *testing.T, m AppModel) AppModel {
			stubKeys(t, editTestVKControl, vkInsert)
			return sendEditMsg(m, nulKey)
		}},
		{"Unix Shift+Einfg ESC[2;2~", func(t *testing.T, m AppModel) AppModel {
			return sendEditMsg(m, fakeCSI("\x1b[2;2~"))
		}},
		{"Unix Strg+Einfg ESC[2;5~", func(t *testing.T, m AppModel) AppModel {
			return sendEditMsg(m, fakeCSI("\x1b[2;5~"))
		}},
	}
}

// forEachPaste führt fn für jede Einfüge-Kombination mit frisch geöffnetem
// Editor (Datei f.txt mit dem Inhalt content) und gesetztem Clipboard-Stub aus.
func forEachPaste(t *testing.T, content, clipboard string, fn func(t *testing.T, m AppModel, paste func(AppModel) AppModel)) {
	t.Helper()
	for _, v := range pasteVariants() {
		t.Run(v.name, func(t *testing.T) {
			defer stubClipboard(clipboard, nil)()
			m, _ := openEditorOn(t, map[string]string{"f.txt": content}, "f.txt")
			fn(t, m, func(m AppModel) AppModel { return v.send(t, m) })
		})
	}
}

// TestEditorPasteShortcutsInsertAtCursor: Jede Kombination fügt den Text an der
// Cursorposition ein - hier mitten in "abcdef" nach "ab", mehrzeilig. Der
// Cursor steht danach hinter dem eingefügten Text (Zeile 2, Spalte 1), die
// Datei ist geändert, und F2 schreibt das Ergebnis auf den Server.
func TestEditorPasteShortcutsInsertAtCursor(t *testing.T) {
	forEachPaste(t, "abcdef\n", "X\nY", func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m = press(t, m, "right", "right")
		m = paste(m)
		if got := editContent(m); got != "abX\nYcdef\n" {
			t.Errorf("Inhalt = %q, erwartet %q", got, "abX\nYcdef\n")
		}
		if cur := m.fm.edit.cur; cur.row != 1 || cur.col != 1 {
			t.Errorf("Cursor = %+v, erwartet Zeile 1, Spalte 1 (hinter dem eingefügten Text)", cur)
		}
		if !m.fm.edit.modified {
			t.Error("modified = false nach dem Einfügen")
		}
		// Weitertippen geht an der Cursorstelle weiter
		m = typeEdit(t, m, "!")
		if got := editContent(m); got != "abX\nY!cdef\n" {
			t.Errorf("nach Tippen: %q", got)
		}
	})
}

// TestEditorPasteSavesToServer: Eingefügter Text wird mit F2 gespeichert
// (Integration bis zum Server), danach ist modified false.
func TestEditorPasteSavesToServer(t *testing.T) {
	for _, v := range pasteVariants() {
		t.Run(v.name, func(t *testing.T) {
			defer stubClipboard("eins\nzwei", nil)()
			m, src := openEditorOn(t, map[string]string{"f.txt": "alt"}, "f.txt")
			m = press(t, m, "end")
			m = v.send(t, m)
			m = pressRun(t, m, "f2")
			if got, _ := src.content("/home/a/f.txt"); got != "alteins\nzwei" {
				t.Errorf("Datei auf dem Server = %q, erwartet %q", got, "alteins\nzwei")
			}
			if m.fm.edit.modified {
				t.Error("modified nach dem Speichern noch true")
			}
		})
	}
}

// TestEditorPasteLineEndings: CRLF (Windows-Zwischenablage) und einzelnes CR
// (alte Macs) werden zu LF, auch gemischt und am Ende. Das CR darf nicht als
// Steuerzeichen einfach verschwinden (dann klebten die Zeilen zusammen).
func TestEditorPasteLineEndings(t *testing.T) {
	cases := []struct{ name, clip, want string }{
		{"CRLF", "a\r\nb\r\n", "a\nb\n"},
		{"CR", "a\rb\rc", "a\nb\nc"},
		{"gemischt", "a\r\nb\rc\nd", "a\nb\nc\nd"},
		{"nur CRLF", "\r\n", "\n"},
		{"nur CR", "\r", "\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachPaste(t, "", c.clip, func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
				m = paste(m)
				if got := editContent(m); got != c.want {
					t.Errorf("Inhalt = %q, erwartet %q", got, c.want)
				}
				if strings.Contains(editContent(m), "\r") {
					t.Error("CR im Text")
				}
			})
		})
	}
}

// TestEditorPasteWithCRLFFileKeepsFileEnding: Eine CRLF-Datei bleibt CRLF; der
// eingefügte Zeilenumbruch (LF im Puffer) wird beim Speichern zu CRLF wie jeder
// getippte. Der Puffer selbst enthält kein CR.
func TestEditorPasteWithCRLFFileKeepsFileEnding(t *testing.T) {
	forEachPaste(t, "a\r\nb\r\n", "x\ny", func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m = paste(m)
		if got := editContent(m); got != "x\r\nya\r\nb\r\n" {
			t.Errorf("Inhalt = %q, erwartet %q", got, "x\r\nya\r\nb\r\n")
		}
	})
}

// TestEditorPasteStripsControlCharacters: Steuerzeichen außer Tab und LF werden
// entfernt: ESC, NUL, BEL, DEL, C1 (U+0085), Backspace, Formfeed. Ein Tab und
// der Zeilenumbruch bleiben. Von einer Escape-Sequenz bleibt nur das druckbare
// "[31m" (kein ESC) - sie kann das Terminal beim Anzeigen und die Datei nicht
// steuern. Die Zeichen entstehen per Escape im Quelltext, nicht als Zeichen.
func TestEditorPasteStripsControlCharacters(t *testing.T) {
	clip := "a\x1b[31mb\x00c\x07d\x7fe\u0085f\x08g\x0ch\tx\ny"
	want := "a[31mbcdefgh\tx\ny"
	forEachPaste(t, "", clip, func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m = paste(m)
		got := editContent(m)
		if got != want {
			t.Errorf("Inhalt = %q, erwartet %q", got, want)
		}
		for _, r := range got {
			if (r < 0x20 && r != '\t' && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("Steuerzeichen %U im Text", r)
			}
		}
		if strings.Contains(m.View(), "\x1b[31m") {
			t.Error("Escape-Sequenz erreicht die Anzeige")
		}
	})
}

// TestEditorPasteOnlyControlCharsChangesNothing: Besteht die Zwischenablage nur
// aus Steuerzeichen, bleibt nach dem Bereinigen nichts: Text unverändert,
// modified false (sonst fragte der Editor beim Beenden grundlos nach).
func TestEditorPasteOnlyControlCharsChangesNothing(t *testing.T) {
	forEachPaste(t, "unverändert", "\x1b\x07\x00\x7f", func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m = paste(m)
		if got := editContent(m); got != "unverändert" {
			t.Errorf("Inhalt = %q", got)
		}
		if m.fm.edit.modified {
			t.Error("modified = true, obwohl nichts eingefügt wurde")
		}
	})
}

// TestEditorBracketedPasteStripsControlCharacters: Auch ein vom Terminal
// gemeldetes Einfügen (Bubbletea, KeyMsg mit Paste-Flag) entfernt Steuerzeichen,
// behält Tab und LF und macht aus CRLF/CR ein LF. Sonst käme ESC auf diesem
// Weg in die Datei.
func TestEditorBracketedPasteStripsControlCharacters(t *testing.T) {
	m, _ := openEditorOn(t, map[string]string{"f.txt": ""}, "f.txt")
	m = sendEditMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\x1b[2Jb\x00\tc\r\nd\re\x7f"), Paste: true})
	if got := editContent(m); got != "a[2Jb\tc\nd\ne" {
		t.Errorf("Inhalt = %q, erwartet %q", got, "a[2Jb\tc\nd\ne")
	}
}

// TestEditorPasteEmptyOrUnreadableClipboard: Leere oder nicht lesbare Zwischen-
// ablage (kein xclip, Fehler): Text unverändert, modified false, kein Absturz.
func TestEditorPasteEmptyOrUnreadableClipboard(t *testing.T) {
	cases := []struct {
		name string
		clip string
		err  error
	}{
		{"leer", "", nil},
		{"Fehler", "wird nicht übernommen", errors.New("kein Clipboard")},
		{"Fehler und leer", "", errors.New("kein Clipboard")},
	}
	for _, c := range cases {
		for _, v := range pasteVariants() {
			t.Run(c.name+"/"+v.name, func(t *testing.T) {
				defer stubClipboard(c.clip, c.err)()
				m, _ := openEditorOn(t, map[string]string{"f.txt": "bleibt"}, "f.txt")
				m = v.send(t, m)
				if got := editContent(m); got != "bleibt" {
					t.Errorf("Inhalt = %q, erwartet unverändert", got)
				}
				if m.fm.edit.modified {
					t.Error("modified = true bei leerer/nicht lesbarer Zwischenablage")
				}
				if !m.fm.editing {
					t.Error("Editor wurde geschlossen")
				}
			})
		}
	}
}

// TestEditorPasteClearsBlock: Ein markierter Block (fertig oder noch im Aufbau)
// wird beim Einfügen aufgehoben - seine Positionen stimmen danach nicht mehr
// (wie bei jeder Änderung über e.edit).
func TestEditorPasteClearsBlock(t *testing.T) {
	for _, state := range []string{"Block fertig", "Markieren läuft"} {
		t.Run(state, func(t *testing.T) {
			forEachPaste(t, "eins\nzwei\ndrei\n", "NEU\nNEU", func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
				m = pressRun(t, m, "f3")
				m = press(t, m, "down")
				if state == "Block fertig" {
					m = pressRun(t, m, "f3")
					if !m.fm.edit.blockSet {
						t.Fatal("Block nicht gesetzt")
					}
				} else if !m.fm.edit.markActive {
					t.Fatal("Markieren läuft nicht")
				}
				m = paste(m)
				if m.fm.edit.blockSet || m.fm.edit.markActive {
					t.Errorf("Block nach dem Einfügen noch da: blockSet=%v markActive=%v", m.fm.edit.blockSet, m.fm.edit.markActive)
				}
				if !m.fm.edit.modified {
					t.Error("modified = false")
				}
			})
		})
	}
}

// TestEditorPasteSizeLimit: Die Grenze fmViewMaxBytes (größte Datei, die der
// Editor überhaupt öffnet) darf durch Einfügen nicht überschritten werden;
// sonst ließe sich die gespeicherte Datei nicht mehr öffnen. Hier 20 Bytes.
// Genau auf der Grenze ist erlaubt, ein Byte mehr wird abgelehnt: Text
// unverändert, modified false, Meldung in edit.message. Gezählt wird in Bytes
// ("ä" = 2). Nach einer Ablehnung geht ein kleineres Einfügen wieder.
func TestEditorPasteSizeLimit(t *testing.T) {
	defer func(old int64) { fmViewMaxBytes = old }(fmViewMaxBytes)
	fmViewMaxBytes = 20
	const base = "0123456789" // 10 Bytes
	cases := []struct {
		name, clip string
		accepted   bool
	}{
		{"genau auf der Grenze", "abcdefghij", true},
		{"ein Byte darüber", "abcdefghijk", false},
		{"Mehrbyte genau auf der Grenze (5 x ä = 10 Bytes)", strings.Repeat("ä", 5), true},
		{"Mehrbyte darüber (6 Zeichen = 12 Bytes)", strings.Repeat("ä", 6), false},
		{"sehr groß", strings.Repeat("x", 5<<20), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachPaste(t, base, c.clip, func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
				m = press(t, m, "end")
				m = paste(m)
				got := editContent(m)
				if c.accepted {
					if got != base+c.clip || !m.fm.edit.modified {
						t.Errorf("Einfügen auf der Grenze abgelehnt: Inhalt %d Bytes, modified=%v", len(got), m.fm.edit.modified)
					}
					return
				}
				if got != base {
					t.Errorf("Inhalt hat %d Bytes, erwartet unverändert (%d)", len(got), len(base))
				}
				if m.fm.edit.modified {
					t.Error("modified = true nach abgelehntem Einfügen")
				}
				if m.fm.edit.message == "" {
					t.Error("keine Meldung beim Ablehnen")
				}
				// Danach funktioniert ein passendes Einfügen weiter
				defer stubClipboard("ok", nil)()
				m = paste(m)
				if got := editContent(m); got != base+"ok" {
					t.Errorf("Einfügen nach Ablehnung: %q", got)
				}
			})
		})
	}
}

// TestEditorPasteSingleKeysDoNotPaste: Abgrenzung. Einfg allein, die NUL-Rune
// ohne Einfüge-Kombination, die Alt-Taste allein (Alt+Tab), Shift allein, Strg
// allein, Einfg mit Alt sowie fremde CSI-Sequenzen fügen NICHT ein - der Text
// bleibt, modified false.
func TestEditorPasteSingleKeysDoNotPaste(t *testing.T) {
	cases := []struct {
		name string
		down []int
		msg  tea.Msg
	}{
		{"Einfg (KeyInsert)", nil, tea.KeyMsg{Type: tea.KeyInsert}},
		{"Einfg mit Alt", nil, tea.KeyMsg{Type: tea.KeyInsert, Alt: true}},
		{"NUL ohne Taste", nil, nulKey},
		{"NUL nur Einfg gedrückt", []int{vkInsert}, nulKey},
		{"NUL nur Shift gedrückt", []int{vkShift}, nulKey},
		{"NUL nur Strg gedrückt", []int{editTestVKControl}, nulKey},
		{"NUL Shift+Strg ohne Einfg", []int{vkShift, editTestVKControl}, nulKey},
		{"Alt-NUL mit Shift+Einfg", []int{vkShift, vkInsert}, fmKey("alt+nul")},
		{"Alt-NUL mit Strg+Einfg", []int{editTestVKControl, vkInsert}, fmKey("alt+nul")},
		{"CSI Alt+Einfg ESC[2;3~", nil, fakeCSI("\x1b[2;3~")},
		{"CSI Einfg ESC[2~ als Bytes", nil, fakeCSI("\x1b[2~")},
		{"CSI Shift+Entf ESC[3;2~", nil, fakeCSI("\x1b[3;2~")},
		{"Buchstabe v (Kontrolle: normales Tippen)", nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer stubClipboard("EINGEFÜGT", nil)()
			stubKeys(t, c.down...)
			m, _ := openEditorOn(t, map[string]string{"f.txt": "x"}, "f.txt")
			want := "x"
			if r, ok := c.msg.(tea.KeyMsg); ok && r.Type == tea.KeyRunes && len(r.Runes) == 1 && r.Runes[0] == 'v' {
				want = "vx" // normales Tippen, kein Einfügen
			}
			m = sendEditMsg(m, c.msg)
			if got := editContent(m); got != want {
				t.Errorf("Inhalt = %q, erwartet %q", got, want)
			}
			if want == "x" && m.fm.edit.modified {
				t.Error("modified = true")
			}
		})
	}
}

// TestEditorPasteNotInOtherModes: Die Kombinationen fügen NICHT in den Text ein,
// wenn der Editor in einem anderen Modus ist: Rückfrage "Änderungen speichern?",
// Eingabezeile (Suchen F7), Hilfe (F1), Hex-Modus einer Binärdatei. Die
// Eingabezeile und der Dialog bleiben, der Text bleibt, modified ändert sich nicht.
func TestEditorPasteNotInOtherModes(t *testing.T) {
	for _, v := range pasteVariants() {
		t.Run(v.name, func(t *testing.T) {
			defer stubClipboard("EINGEFÜGT", nil)()

			// Rückfrage beim Beenden (Text vorher geändert)
			m, _ := openEditorOn(t, map[string]string{"f.txt": "x"}, "f.txt")
			m = typeEdit(t, m, "a")
			m = press(t, m, "esc")
			if m.fm.edit.dialog != edDialogSaveQuestion {
				t.Fatalf("Dialog = %v, erwartet Rückfrage", m.fm.edit.dialog)
			}
			m = v.send(t, m)
			if got := editContent(m); got != "ax" {
				t.Errorf("Rückfrage: Inhalt = %q, erwartet %q", got, "ax")
			}
			if m.fm.edit.dialog != edDialogSaveQuestion {
				t.Errorf("Rückfrage: Dialog = %v, erwartet unverändert", m.fm.edit.dialog)
			}

			// Eingabezeile (Suchen)
			m, _ = openEditorOn(t, map[string]string{"f.txt": "x"}, "f.txt")
			m = pressRun(t, m, "f7")
			if m.fm.edit.prompt == edPromptNone {
				t.Fatal("Eingabezeile nicht offen")
			}
			m = v.send(t, m)
			if got := editContent(m); got != "x" || m.fm.edit.modified {
				t.Errorf("Eingabezeile: Inhalt = %q, modified = %v", got, m.fm.edit.modified)
			}
			if m.fm.edit.prompt == edPromptNone {
				t.Error("Eingabezeile wurde durch das Einfügen geschlossen")
			}

			// Hilfe
			m, _ = openEditorOn(t, map[string]string{"f.txt": "x"}, "f.txt")
			m = pressRun(t, m, "f1")
			m = v.send(t, m)
			if got := editContent(m); got != "x" || m.fm.edit.modified {
				t.Errorf("Hilfe: Inhalt = %q, modified = %v", got, m.fm.edit.modified)
			}

			// Hex-Modus (Binärdatei): Bytes bleiben
			m, _ = openEditorOn(t, map[string]string{"b.bin": "\x00\x01\xff"}, "b.bin")
			if !m.fm.edit.hexMode {
				t.Fatal("Hex-Modus nicht aktiv")
			}
			m = v.send(t, m)
			if string(m.fm.edit.hexb.data) != "\x00\x01\xff" || m.fm.edit.modified {
				t.Errorf("Hex: Daten = %q, modified = %v", m.fm.edit.hexb.data, m.fm.edit.modified)
			}
		})
	}
}

// TestViewerAndPanelsIgnorePasteShortcuts: Im F3-Betrachter und in den
// Datei-Fenstern (kein Editor offen) passiert beim Einfügen nichts: kein Editor
// öffnet sich, der Betrachter bleibt offen, die Datei auf dem Server bleibt
// unverändert, kein Absturz.
func TestViewerAndPanelsIgnorePasteShortcuts(t *testing.T) {
	for _, v := range pasteVariants() {
		t.Run(v.name, func(t *testing.T) {
			defer stubClipboard("EINGEFÜGT", nil)()
			m, src := openViewManager(t, map[string]string{"hallo.txt": "erste Zeile\n"})
			m = cursorTo(t, m, "hallo.txt")

			// Datei-Fenster: es öffnet sich nichts. (Windows Strg+Einfg ist dort
			// wie bisher Einfg und markiert den Eintrag - das bleibt unverändert,
			// deshalb wird der Cursor nicht geprüft.)
			m = v.send(t, m)
			if m.fm.editing || m.fm.viewing || m.state != ViewFileManager {
				t.Errorf("Fenster: editing=%v viewing=%v state=%v", m.fm.editing, m.fm.viewing, m.state)
			}

			// Betrachter
			m = cursorTo(t, m, "hallo.txt")
			m = pressRun(t, m, "f3")
			if !m.fm.viewing {
				t.Fatal("Betrachter nicht geöffnet")
			}
			m = v.send(t, m)
			if !m.fm.viewing || m.fm.editing {
				t.Errorf("Betrachter: viewing=%v editing=%v", m.fm.viewing, m.fm.editing)
			}
			if !strings.Contains(viewText(m), "erste Zeile") || strings.Contains(viewText(m), "EINGEFÜGT") {
				t.Errorf("Betrachter zeigt:\n%s", viewText(m))
			}
			if got, _ := src.content("/home/a/hallo.txt"); got != "erste Zeile\n" {
				t.Errorf("Datei verändert: %q", got)
			}
		})
	}
}

// TestEditorPasteScreenSize: Nach dem Einfügen (viele Zeilen, breite Zeichen,
// lange Zeilen) stimmt die Bildschirmgröße in allen Farbmodi und kleinen
// Größen, und der Cursor ist sichtbar: Die letzte eingefügte Zeile steht im
// Bild (80x24), auch wenn der Text länger als das Fenster ist. Die Steuer-
// zeichen im Clipboard erreichen die Anzeige nicht.
func TestEditorPasteScreenSize(t *testing.T) {
	withColorProfile(t)
	var b strings.Builder
	for i := 1; i <= 80; i++ {
		b.WriteString("Zeile ")
		b.WriteString(strings.Repeat("breit日本", 20))
		b.WriteString("\x1b[2J\r\n")
	}
	b.WriteString("ENDE")
	forEachPaste(t, "", b.String(), func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m.termWidth, m.termHeight = 80, 24
		m = paste(m)
		if m.fm.edit.cur.row != 80 {
			t.Fatalf("Cursor in Zeile %d, erwartet 80", m.fm.edit.cur.row)
		}
		if !strings.Contains(editText(m), "ENDE") {
			t.Errorf("Cursorzeile (ENDE) nach dem Einfügen nicht sichtbar:\n%s", editText(m))
		}
		if strings.Contains(m.View(), "\x1b[2J") {
			t.Error("Steuersequenz aus der Zwischenablage in der Anzeige")
		}
		for theme := range fmThemes {
			m.fm.theme = theme
			for _, size := range [][2]int{{100, 30}, {80, 24}, {40, 12}, {20, 5}} {
				m.termWidth, m.termHeight = size[0], size[1]
				assertScreenSize(t, m)
			}
		}
	})
}

// =============================================================================
// Build 89, Runde 2 (Fable-Review, Funde 2 und 4)
// =============================================================================

// TestFileManagerInsertKeysStillMarkInPanels (Fund 2, Testlücke): In den
// Datei-Fenstern (kein Editor offen) markieren Einfg allein und - wie bisher -
// Windows Strg+Einfg den Eintrag unter dem Cursor und springen eine Zeile
// weiter; ein zweiter Druck hebt die Markierung auf. Strg+Einfg ist dort NICHT
// Einfügen. Gegenprobe: Windows Shift+Einfg markiert nicht und bewegt den Cursor
// nicht (dort ist Shift+Einfg "einfügen", im Fenster ohne Wirkung).
func TestFileManagerInsertKeysStillMarkInPanels(t *testing.T) {
	cases := []struct {
		name string
		down []int
		msg  tea.Msg
		mark bool
	}{
		{"Einfg (KeyInsert)", nil, tea.KeyMsg{Type: tea.KeyInsert}, true},
		{"Windows Einfg allein (NUL, Einfg gedrückt)", []int{vkInsert}, nulKey, true},
		{"Windows Einfg ohne Tastenzustand (NUL)", nil, nulKey, true},
		{"Windows Strg+Einfg (NUL, Strg und Einfg gedrückt)", []int{editTestVKControl, vkInsert}, nulKey, true},
		{"Windows Shift+Einfg (NUL, Shift und Einfg gedrückt)", []int{vkShift, vkInsert}, nulKey, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer stubClipboard("EINGEFÜGT", nil)()
			stubKeys(t, c.down...)
			m, _ := openFileManagerFor(t)
			m = cursorTo(t, m, "geheim")
			start := m.fm.panels[0].cursor
			m = sendEditMsg(m, c.msg)
			p := m.fm.panels[0]
			if !c.mark {
				if got := markedNames(p); len(got) != 0 || p.cursor != start {
					t.Errorf("markiert %v, Cursor %d (Start %d), erwartet keine Wirkung", got, p.cursor, start)
				}
				return
			}
			if got := strings.Join(markedNames(p), ","); got != "geheim" {
				t.Errorf("markiert = %q, erwartet geheim", got)
			}
			if p.cursor != start+1 {
				t.Errorf("Cursor = %d, erwartet %d (eine Zeile weiter)", p.cursor, start+1)
			}
			m = cursorTo(t, m, "geheim")
			m = sendEditMsg(m, c.msg)
			if got := markedNames(m.fm.panels[0]); len(got) != 0 {
				t.Errorf("nach zweitem Druck noch markiert: %v", got)
			}
			if m.fm.editing {
				t.Error("ein Editor hat sich geöffnet")
			}
		})
	}
}

// TestEditorPasteHugeClipboardRejectedBeforeCleaning (Fund 4): Ist die
// Zwischenablage größer als 4 * fmViewMaxBytes Bytes, wird SOFORT abgelehnt - vor
// dem Bereinigen (sonst würde eine riesige Zwischenablage erst vollständig
// durchlaufen). Meldung wie beim Überschreiten der Dateigrenze
// (txtFMEditPasteTooLarge), Text und modified unverändert.
// Unterscheidend ist eine Zwischenablage aus lauter Steuerzeichen: Sie wäre nach
// dem Bereinigen leer (altes Verhalten: stillschweigend nichts), ist aber zu
// groß und wird jetzt mit Meldung abgelehnt. Genau 4 * Grenze ist noch erlaubt.
// Die Grenze wird klein gesetzt (100), damit der Test schnell bleibt.
func TestEditorPasteHugeClipboardRejectedBeforeCleaning(t *testing.T) {
	defer func(old int64) { fmViewMaxBytes = old }(fmViewMaxBytes)
	fmViewMaxBytes = 100
	cases := []struct {
		name string
		clip string
		// wantTooLarge - Ablehnung mit Meldung erwartet
		wantTooLarge bool
		// wantText - erwarteter Inhalt nach dem Einfügen
		wantText string
	}{
		{"401 Steuerzeichen (über 4 x Grenze)", strings.Repeat("\x00", 401), true, "alt"},
		{"401 ESC-Zeichen", strings.Repeat("\x1b", 401), true, "alt"},
		{"401 normale Zeichen", strings.Repeat("x", 401), true, "alt"},
		{"1000 Steuerzeichen und ein Wort", strings.Repeat("\x07", 1000) + "wort", true, "alt"},
		{"genau 400 Steuerzeichen (erlaubt, danach leer)", strings.Repeat("\x00", 400), false, "alt"},
		{"400 Bytes: 395 ESC und hello (erlaubt, bereinigt)", strings.Repeat("\x1b", 395) + "hello", false, "althello"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachPaste(t, "alt", c.clip, func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
				m = press(t, m, "end")
				m = paste(m)
				if got := editContent(m); got != c.wantText {
					t.Errorf("Inhalt = %q, erwartet %q", got, c.wantText)
				}
				wantMsg := fmt.Sprintf(m.tr(txtFMEditPasteTooLarge), formatSizeWithUnit(fmViewMaxBytes))
				if c.wantTooLarge {
					if m.fm.edit.message != wantMsg {
						t.Errorf("Meldung = %q, erwartet %q", m.fm.edit.message, wantMsg)
					}
					if m.fm.edit.modified {
						t.Error("modified = true nach abgelehntem Einfügen")
					}
					return
				}
				if m.fm.edit.message == wantMsg {
					t.Errorf("Zu-groß-Meldung bei %d Bytes (erlaubt bis 4 x Grenze)", len(c.clip))
				}
				if wantModified := c.wantText != "alt"; m.fm.edit.modified != wantModified {
					t.Errorf("modified = %v, erwartet %v", m.fm.edit.modified, wantModified)
				}
			})
		})
	}
}
