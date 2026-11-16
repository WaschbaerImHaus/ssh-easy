// Paket main - Tests: Einfügen in die Eingabezeilen des F4-Editors (Build 90)
//
// Nutzerwunsch: Auch in den Eingabezeilen des Editors (Suchen F7, Ersetzen F4
// mit Suchbegriff und Ersatz) soll Einfügen gehen - mit denselben Tasten wie im
// Text (Strg+V, Shift+Einfg, Strg+Einfg; Unix als ESC[2;2~ / ESC[2;5~, Windows
// als NUL-Rune plus Tastenzustand) sowie als vom Terminal gemeldetes Einfügen
// (Bracketed Paste, KeyMsg mit Paste-Flag).
//
// Festlegungen (Eingabezeile ist einzeilig, 256 Zeichen lang):
//   - Nur die erste Zeile der Zwischenablage zählt (CR oder LF beenden sie).
//   - Steuerzeichen außer Tab fallen weg (C0, DEL, C1, ungültiges UTF-8);
//     ein Tab wird vom Eingabefeld wie beim Tippen zum Leerzeichen.
//   - Höchstens so viele Zeichen, wie bis zur Grenze (CharLimit 256) noch Platz
//     ist. Eine noch unberührte Vorbelegung (inputFresh) zählt nicht, sie wird
//     ersetzt - wie beim ersten getippten Zeichen.
//   - Bleibt nach dem Bereinigen nichts übrig, ändert sich nichts: Wert,
//     Vorbelegung und inputFresh bleiben.
//   - Eingefügt wird an der Cursorposition der Zeile; der Cursor steht
//     danach hinter dem eingefügten Text.
//   - Der Editortext bleibt bei alledem unverändert (modified false).
//   - Rückfragen (Dialoge mit j/n) nehmen NIE Einfügen an.
//   - Die NUL-Rune ohne Einfüge-Kombination (Windows: Einfg, Alt, Strg allein)
//     tippt nichts und löscht die Vorbelegung nicht.
//
// @author Kurt Ingwer
// @date   2026-11-12 20:24
package main

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// openEditPrompt öffnet die Eingabezeile des Editors über eine Funktionstaste
// (f7 = Suchen, f4 = Ersetzen) und prüft, dass sie offen ist.
func openEditPrompt(t *testing.T, m AppModel, key string) AppModel {
	t.Helper()
	m = pressRun(t, m, key)
	if m.fm.edit.prompt == edPromptNone {
		t.Fatalf("Eingabezeile nach %s nicht offen", key)
	}
	return m
}

// promptValue liefert den Wert der offenen Eingabezeile.
func promptValue(m AppModel) string { return m.fm.edit.input.Value() }

// typeIntoPrompt tippt Zeichen in die Eingabezeile (ohne Blink-Kommandos).
func typeIntoPrompt(t *testing.T, m AppModel, text string) AppModel {
	t.Helper()
	for _, r := range text {
		m = press(t, m, string(r))
	}
	return m
}

// assertTextUntouched prüft, dass der Editortext und modified unverändert sind
// und die Eingabezeile noch offen ist.
func assertTextUntouched(t *testing.T, m AppModel, wantText string) {
	t.Helper()
	if got := editContent(m); got != wantText {
		t.Errorf("Editortext = %q, erwartet unverändert %q", got, wantText)
	}
	if m.fm.edit.modified {
		t.Error("modified = true, obwohl nur die Eingabezeile gefüllt wurde")
	}
	if m.fm.edit.prompt == edPromptNone {
		t.Error("Eingabezeile wurde durch das Einfügen geschlossen")
	}
}

// TestEditorPromptPasteSearchFirstLineOnly: F7 offen (leer), Zwischenablage
// "foo\nbar" - nur die erste Zeile "foo" landet in der Eingabezeile. Der Prompt
// bleibt offen, der Text bleibt, modified false. Enter sucht "foo" und springt
// auf den Treffer. Esc schließt die Zeile, ohne dass sich Suchbegriff oder Text
// ändern (Abbruch mitten im Vorgang).
func TestEditorPromptPasteSearchFirstLineOnly(t *testing.T) {
	const file = "xx\nfoo hier\n"
	forEachPaste(t, file, "foo\nbar", func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m = openEditPrompt(t, m, "f7")
		m = paste(m)
		if got := promptValue(m); got != "foo" {
			t.Fatalf("Eingabezeile = %q, erwartet %q", got, "foo")
		}
		if m.fm.edit.prompt != edPromptSearch {
			t.Errorf("prompt = %v, erwartet Suchen", m.fm.edit.prompt)
		}
		assertTextUntouched(t, m, file)

		// Abbruch: Esc, danach ist nichts gemerkt und nichts verändert
		esc := press(t, m, "esc")
		if esc.fm.edit.prompt != edPromptNone || esc.fm.edit.query != "" {
			t.Errorf("nach Esc: prompt=%v query=%q", esc.fm.edit.prompt, esc.fm.edit.query)
		}
		if got := editContent(esc); got != file || esc.fm.edit.modified {
			t.Errorf("nach Esc: Text %q, modified %v", got, esc.fm.edit.modified)
		}

		// Enter sucht den eingefügten Begriff
		m = press(t, m, "enter")
		if m.fm.edit.query != "foo" {
			t.Errorf("query = %q, erwartet foo", m.fm.edit.query)
		}
		if m.fm.edit.prompt != edPromptNone {
			t.Error("Eingabezeile nach Enter noch offen")
		}
		if cur := m.fm.edit.cur; cur.row != 1 || cur.col != 0 {
			t.Errorf("Cursor = %+v, erwartet auf dem Treffer (Zeile 1, Spalte 0)", cur)
		}
		if m.fm.edit.modified {
			t.Error("modified = true nach der Suche")
		}
	})
}

// TestEditorPromptPasteReplacesFreshPrefill: Die Eingabezeile ist mit dem
// letzten Suchbegriff "alt" vorbelegt (inputFresh). Einfügen ersetzt die
// Vorbelegung - wie das erste getippte Zeichen - statt anzuhängen. Der Cursor
// steht hinter dem eingefügten Text, und weiteres Einfügen hängt nun an.
func TestEditorPromptPasteReplacesFreshPrefill(t *testing.T) {
	forEachPaste(t, "alt neu\n", "neu", func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m = openEditPrompt(t, m, "f7")
		m = typeIntoPrompt(t, m, "alt")
		m = press(t, m, "enter")
		if m.fm.edit.query != "alt" {
			t.Fatalf("query = %q, erwartet alt", m.fm.edit.query)
		}
		m = openEditPrompt(t, m, "f7")
		if promptValue(m) != "alt" || !m.fm.edit.inputFresh {
			t.Fatalf("Vorbelegung = %q, inputFresh = %v", promptValue(m), m.fm.edit.inputFresh)
		}
		m = paste(m)
		if got := promptValue(m); got != "neu" {
			t.Errorf("Eingabezeile = %q, erwartet %q (Vorbelegung ersetzt)", got, "neu")
		}
		if pos := m.fm.edit.input.Position(); pos != 3 {
			t.Errorf("Cursor in der Zeile = %d, erwartet 3", pos)
		}
		if m.fm.edit.inputFresh {
			t.Error("inputFresh nach dem Einfügen noch true")
		}
		// Ein zweites Einfügen hängt an (die Vorbelegung ist nicht mehr frisch)
		m = paste(m)
		if got := promptValue(m); got != "neuneu" {
			t.Errorf("nach zweitem Einfügen = %q, erwartet %q", got, "neuneu")
		}
	})
}

// TestEditorPromptPasteInsertsAtCursor: Mitten in der Zeile einfügen: "ab",
// Cursor eine Stelle zurück, Einfügen von "XY" ergibt "aXYb" mit dem Cursor
// hinter dem eingefügten Text (Position 3), nicht am Zeilenende.
func TestEditorPromptPasteInsertsAtCursor(t *testing.T) {
	forEachPaste(t, "text\n", "XY", func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m = openEditPrompt(t, m, "f7")
		m = typeIntoPrompt(t, m, "ab")
		m = press(t, m, "left")
		m = paste(m)
		if got := promptValue(m); got != "aXYb" {
			t.Errorf("Eingabezeile = %q, erwartet %q", got, "aXYb")
		}
		if pos := m.fm.edit.input.Position(); pos != 3 {
			t.Errorf("Cursor in der Zeile = %d, erwartet 3", pos)
		}
		// Weitertippen geht an der Cursorstelle weiter
		m = typeIntoPrompt(t, m, "!")
		if got := promptValue(m); got != "aXY!b" {
			t.Errorf("nach Tippen = %q, erwartet %q", got, "aXY!b")
		}
		assertTextUntouched(t, m, "text\n")
	})
}

// TestEditorPromptPasteControlChars: Steuerzeichen fallen weg (ESC, DEL, C1
// U+0085), vom Escape bleibt nur das druckbare "[31m"; der Tab wird wie beim
// Tippen zum Leerzeichen. Die Zeichen entstehen als Escape im Quelltext.
func TestEditorPromptPasteControlChars(t *testing.T) {
	clip := "a\x1b[31mb\tc\x7f" + string(rune(0x85)) + "d"
	forEachPaste(t, "x\n", clip, func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m = openEditPrompt(t, m, "f7")
		m = paste(m)
		got := promptValue(m)
		if got != "a[31mb cd" {
			t.Errorf("Eingabezeile = %q, erwartet %q", got, "a[31mb cd")
		}
		for _, r := range got {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("Steuerzeichen %U in der Eingabezeile", r)
			}
		}
		if strings.Contains(m.View(), "\x1b[31m") {
			t.Error("Escape-Sequenz aus der Zwischenablage erreicht die Anzeige")
		}
		assertTextUntouched(t, m, "x\n")
	})
}

// TestEditorPromptPasteLineBreaks: Ein einzelnes CR, CRLF und LF beenden die
// Zeile - nur der Teil davor zählt (kein Zusammenkleben der Zeilen).
func TestEditorPromptPasteLineBreaks(t *testing.T) {
	cases := []struct{ name, clip, want string }{
		{"nur CR", "a\rb", "a"},
		{"CRLF", "x\r\ny", "x"},
		{"LF", "p\nq", "p"},
		{"Umbruch am Ende", "ende\n", "ende"},
		{"CRLF am Ende", "ende\r\n", "ende"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachPaste(t, "x\n", c.clip, func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
				m = openEditPrompt(t, m, "f7")
				m = paste(m)
				if got := promptValue(m); got != c.want {
					t.Errorf("Eingabezeile = %q, erwartet %q", got, c.want)
				}
				if strings.ContainsAny(promptValue(m), "\r\n") {
					t.Error("Zeilenumbruch in der Eingabezeile")
				}
			})
		})
	}
}

// TestEditorPromptPasteEmptyFirstLineKeepsPrefill: Ist die erste Zeile leer
// ("\nzweite") oder besteht sie nur aus Steuerzeichen, ändert sich nichts. Eine
// Vorbelegung bleibt samt inputFresh erhalten (das nächste getippte Zeichen
// ersetzt sie weiterhin); eine leere Zeile bleibt leer.
func TestEditorPromptPasteEmptyFirstLineKeepsPrefill(t *testing.T) {
	clips := []struct{ name, clip string }{
		{"erste Zeile leer", "\nzweite"},
		{"CR zuerst", "\rzweite"},
		{"nur Steuerzeichen", "\x1b\x07\x00\x7f\nzweite"},
		{"nur Steuerzeichen ohne Umbruch", "\x1b\x07\x00\x7f"},
	}
	for _, c := range clips {
		t.Run(c.name, func(t *testing.T) {
			forEachPaste(t, "alt\n", c.clip, func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
				// leere Zeile bleibt leer
				m = openEditPrompt(t, m, "f7")
				m = paste(m)
				if got := promptValue(m); got != "" {
					t.Errorf("leere Zeile: Eingabezeile = %q, erwartet leer", got)
				}
				m = press(t, m, "esc")

				// Vorbelegung bleibt samt inputFresh
				m = openEditPrompt(t, m, "f7")
				m = typeIntoPrompt(t, m, "alt")
				m = press(t, m, "enter")
				m = openEditPrompt(t, m, "f7")
				m = paste(m)
				if got := promptValue(m); got != "alt" {
					t.Errorf("Vorbelegung: Eingabezeile = %q, erwartet alt", got)
				}
				if !m.fm.edit.inputFresh {
					t.Error("inputFresh nach wirkungslosem Einfügen false (Vorbelegung gilt als berührt)")
				}
				// Das nächste getippte Zeichen ersetzt die Vorbelegung weiterhin
				m = typeIntoPrompt(t, m, "z")
				if got := promptValue(m); got != "z" {
					t.Errorf("nach Tippen = %q, erwartet z", got)
				}
			})
		})
	}
}

// TestEditorPromptPasteLengthLimit: Die Zeile hält ihre Grenze von 256 Zeichen.
//   - 300 x "x" in leere Zeile: genau 256.
//   - 250 getippte Zeichen + 10 eingefügte: 256, die ersten 6 eingefügten
//     stehen am Ende (der Rest fällt weg).
//   - volle Zeile (256): Einfügen ändert nichts.
//   - Mehrbyte: 300 x "ä" ergeben 256 Zeichen (Runen, nicht Bytes).
//   - Vorbelegung (200 Zeichen) zählt nicht, sie wird ersetzt: 256 neue.
func TestEditorPromptPasteLengthLimit(t *testing.T) {
	t.Run("300 Zeichen in leere Zeile", func(t *testing.T) {
		forEachPaste(t, "x\n", strings.Repeat("x", 300), func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
			m = openEditPrompt(t, m, "f7")
			m = paste(m)
			if n := utf8.RuneCountInString(promptValue(m)); n != 256 {
				t.Errorf("Länge = %d, erwartet 256", n)
			}
			if pos := m.fm.edit.input.Position(); pos != 256 {
				t.Errorf("Cursor = %d, erwartet 256", pos)
			}
		})
	})
	t.Run("250 getippt plus 10 eingefügt", func(t *testing.T) {
		forEachPaste(t, "x\n", "0123456789", func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
			m = openEditPrompt(t, m, "f7")
			m = typeIntoPrompt(t, m, strings.Repeat("x", 250))
			m = paste(m)
			want := strings.Repeat("x", 250) + "012345"
			if got := promptValue(m); got != want {
				t.Errorf("Eingabezeile endet auf %q (Länge %d), erwartet Länge 256 und Ende 012345",
					got[max(0, len(got)-10):], len(got))
			}
		})
	})
	t.Run("volle Zeile bleibt", func(t *testing.T) {
		forEachPaste(t, "x\n", "abc", func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
			m = openEditPrompt(t, m, "f7")
			full := strings.Repeat("v", 256)
			m = typeIntoPrompt(t, m, full)
			if promptValue(m) != full {
				t.Fatalf("Zeile nach dem Tippen = %d Zeichen", len(promptValue(m)))
			}
			m = paste(m)
			if promptValue(m) != full {
				t.Errorf("volle Zeile verändert: %d Zeichen, Ende %q", len(promptValue(m)), promptValue(m)[250:])
			}
		})
	})
	t.Run("Mehrbyte 300 x ä", func(t *testing.T) {
		forEachPaste(t, "x\n", strings.Repeat("ä", 300), func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
			m = openEditPrompt(t, m, "f7")
			m = paste(m)
			if n := utf8.RuneCountInString(promptValue(m)); n != 256 {
				t.Errorf("Länge = %d Zeichen, erwartet 256", n)
			}
			if promptValue(m) != strings.Repeat("ä", 256) {
				t.Error("Inhalt ist nicht 256 x ä")
			}
			if !utf8.ValidString(promptValue(m)) {
				t.Error("ungültiges UTF-8 in der Eingabezeile")
			}
		})
	})
	t.Run("Vorbelegung zählt nicht", func(t *testing.T) {
		forEachPaste(t, "x\n", strings.Repeat("y", 300), func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
			m = openEditPrompt(t, m, "f7")
			m = typeIntoPrompt(t, m, strings.Repeat("a", 200))
			m = press(t, m, "enter")
			m = openEditPrompt(t, m, "f7")
			if !m.fm.edit.inputFresh || len(promptValue(m)) != 200 {
				t.Fatalf("Vorbelegung = %d Zeichen, inputFresh %v", len(promptValue(m)), m.fm.edit.inputFresh)
			}
			m = paste(m)
			if got := promptValue(m); got != strings.Repeat("y", 256) {
				t.Errorf("Eingabezeile = %d Zeichen (Anfang %q), erwartet 256 x y", len(got), got[:min(10, len(got))])
			}
		})
	})
}

// TestEditorPromptPasteHugeClipboard: Eine riesige einzeilige Zwischenablage
// (20 MiB "x") ergibt 256 Zeichen und kostet kaum Zeit (die Bereinigung bricht
// an der Grenze ab). Als Hänger gilt mehr als 5 s je Einfügen.
func TestEditorPromptPasteHugeClipboard(t *testing.T) {
	forEachPaste(t, "x\n", strings.Repeat("x", 20<<20), func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m = openEditPrompt(t, m, "f7")
		start := time.Now()
		m = paste(m)
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("Einfügen dauerte %v", d)
		}
		if got := promptValue(m); got != strings.Repeat("x", 256) {
			t.Errorf("Eingabezeile hat %d Zeichen, erwartet 256", len(got))
		}
		assertTextUntouched(t, m, "x\n")
	})
}

// TestEditorPromptPasteReplaceFlow: Ersetzen (F4) hat zwei Eingabezeilen.
// Einfügen in den Suchbegriff "foo", Enter öffnet die Ersatz-Zeile, dort Einfügen
// von "bar", Enter öffnet die Rückfrage "Diesen Treffer ersetzen?" (die Datei
// enthält foo). Es wird noch nichts ersetzt.
func TestEditorPromptPasteReplaceFlow(t *testing.T) {
	const file = "ein foo da\n"
	forEachPaste(t, file, "foo", func(t *testing.T, m AppModel, paste func(AppModel) AppModel) {
		m = openEditPrompt(t, m, "f4")
		if m.fm.edit.prompt != edPromptReplaceFind {
			t.Fatalf("prompt = %v, erwartet Suchbegriff", m.fm.edit.prompt)
		}
		m = paste(m)
		if got := promptValue(m); got != "foo" {
			t.Fatalf("Suchbegriff = %q, erwartet foo", got)
		}
		m = press(t, m, "enter")
		if m.fm.edit.prompt != edPromptReplaceWith || m.fm.edit.query != "foo" {
			t.Fatalf("prompt = %v, query = %q, erwartet Ersatz-Zeile und foo", m.fm.edit.prompt, m.fm.edit.query)
		}

		defer stubClipboard("bar\nbaz", nil)()
		m = paste(m)
		if got := promptValue(m); got != "bar" {
			t.Fatalf("Ersatz = %q, erwartet bar", got)
		}
		assertTextUntouched(t, m, file)
		m = press(t, m, "enter")
		if m.fm.edit.replaceWith != "bar" {
			t.Errorf("replaceWith = %q, erwartet bar", m.fm.edit.replaceWith)
		}
		if m.fm.edit.dialog != edDialogReplace {
			t.Errorf("dialog = %v, erwartet Rückfrage zum Ersetzen", m.fm.edit.dialog)
		}
		if got := editContent(m); got != file {
			t.Errorf("Text vor der Bestätigung verändert: %q", got)
		}
	})
}

// TestEditorPromptPasteNotInQuestions: Rückfragen (Ja/Nein) nehmen nie
// Einfügen an. Zwischenablage "y" bzw. "j" darf weder "Ersetzen" noch
// "Speichern" bestätigen: Text unverändert, nichts ersetzt, der Dialog bleibt.
// Auch ein vom Terminal gemeldetes Einfügen von "y" bestätigt nichts.
func TestEditorPromptPasteNotInQuestions(t *testing.T) {
	setups := []struct {
		name   string
		dialog edDialog
		open   func(t *testing.T) AppModel
		text   string
	}{
		{"Ersetzen", edDialogReplace, func(t *testing.T) AppModel {
			m, _ := openEditorOn(t, map[string]string{"f.txt": "foo\n"}, "f.txt")
			m = openEditPrompt(t, m, "f4")
			m = typeIntoPrompt(t, m, "foo")
			m = press(t, m, "enter")
			return press(t, m, "enter")
		}, "foo\n"},
		{"Speichern?", edDialogSaveQuestion, func(t *testing.T) AppModel {
			m, _ := openEditorOn(t, map[string]string{"f.txt": "x"}, "f.txt")
			m = typeEdit(t, m, "a")
			return press(t, m, "esc")
		}, "ax"},
	}
	for _, s := range setups {
		for _, clip := range []string{"y", "j"} {
			for _, v := range pasteVariants() {
				t.Run(s.name+"/"+clip+"/"+v.name, func(t *testing.T) {
					defer stubClipboard(clip, nil)()
					m := s.open(t)
					if m.fm.edit.dialog != s.dialog {
						t.Fatalf("dialog = %v, erwartet %v", m.fm.edit.dialog, s.dialog)
					}
					wasModified := m.fm.edit.modified
					m = v.send(t, m)
					if m.fm.edit.dialog != s.dialog {
						t.Errorf("Dialog nach dem Einfügen = %v, erwartet unverändert", m.fm.edit.dialog)
					}
					if got := editContent(m); got != s.text {
						t.Errorf("Text = %q, erwartet %q", got, s.text)
					}
					if m.fm.edit.replaceCount != 0 || m.fm.edit.modified != wasModified {
						t.Errorf("replaceCount = %d, modified = %v (vorher %v)", m.fm.edit.replaceCount, m.fm.edit.modified, wasModified)
					}
					if !m.fm.editing {
						t.Error("Editor wurde geschlossen")
					}
				})
			}
		}
		t.Run(s.name+"/Bracketed Paste", func(t *testing.T) {
			m := s.open(t)
			m = sendEditMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y"), Paste: true})
			if m.fm.edit.dialog != s.dialog {
				t.Errorf("Dialog = %v, erwartet %v (Einfügen von y darf nicht bestätigen)", m.fm.edit.dialog, s.dialog)
			}
			if got := editContent(m); got != s.text {
				t.Errorf("Text = %q, erwartet %q", got, s.text)
			}
			if m.fm.edit.replaceCount != 0 {
				t.Errorf("replaceCount = %d, erwartet 0", m.fm.edit.replaceCount)
			}
		})
	}
}

// TestEditorPromptBracketedPaste: Ein vom Terminal gemeldetes Einfügen (KeyMsg
// mit Paste-Flag) landet bereinigt und einzeilig in der Eingabezeile: ESC und
// alles nach dem Umbruch fallen weg ("x\x1by\nz" ergibt "xy"). Auch hier wird
// eine Vorbelegung ersetzt, an der Cursorstelle eingefügt und die Grenze
// gehalten; ein wirkungsloses Einfügen lässt die Vorbelegung stehen.
func TestEditorPromptBracketedPaste(t *testing.T) {
	paste := func(m AppModel, text string) AppModel {
		return sendEditMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	}
	t.Run("Steuerzeichen und Umbruch", func(t *testing.T) {
		m, _ := openEditorOn(t, map[string]string{"f.txt": "x\n"}, "f.txt")
		m = openEditPrompt(t, m, "f7")
		m = paste(m, "x\x1by\nz")
		if got := promptValue(m); got != "xy" {
			t.Errorf("Eingabezeile = %q, erwartet xy", got)
		}
		assertTextUntouched(t, m, "x\n")
	})
	t.Run("ersetzt Vorbelegung", func(t *testing.T) {
		m, _ := openEditorOn(t, map[string]string{"f.txt": "x\n"}, "f.txt")
		m = openEditPrompt(t, m, "f7")
		m = typeIntoPrompt(t, m, "alt")
		m = press(t, m, "enter")
		m = openEditPrompt(t, m, "f7")
		m = paste(m, "neu")
		if got := promptValue(m); got != "neu" {
			t.Errorf("Eingabezeile = %q, erwartet neu", got)
		}
	})
	t.Run("wirkungslos lässt Vorbelegung", func(t *testing.T) {
		m, _ := openEditorOn(t, map[string]string{"f.txt": "x\n"}, "f.txt")
		m = openEditPrompt(t, m, "f7")
		m = typeIntoPrompt(t, m, "alt")
		m = press(t, m, "enter")
		m = openEditPrompt(t, m, "f7")
		m = paste(m, "\x1b\nrest")
		if promptValue(m) != "alt" || !m.fm.edit.inputFresh {
			t.Errorf("Eingabezeile = %q, inputFresh = %v", promptValue(m), m.fm.edit.inputFresh)
		}
	})
	t.Run("mitten einfügen", func(t *testing.T) {
		m, _ := openEditorOn(t, map[string]string{"f.txt": "x\n"}, "f.txt")
		m = openEditPrompt(t, m, "f7")
		m = typeIntoPrompt(t, m, "ab")
		m = press(t, m, "left")
		m = paste(m, "XY")
		if got := promptValue(m); got != "aXYb" || m.fm.edit.input.Position() != 3 {
			t.Errorf("Eingabezeile = %q, Position %d", got, m.fm.edit.input.Position())
		}
	})
	t.Run("Grenze", func(t *testing.T) {
		m, _ := openEditorOn(t, map[string]string{"f.txt": "x\n"}, "f.txt")
		m = openEditPrompt(t, m, "f7")
		m = paste(m, strings.Repeat("q", 1000))
		if n := utf8.RuneCountInString(promptValue(m)); n != 256 {
			t.Errorf("Länge = %d, erwartet 256", n)
		}
	})
}

// TestEditorPromptPasteHexMode: Auch im Hex-Modus einer Binärdatei geht Einfügen
// in die Eingabezeile (etwa "ad ff" für die Bytesuche). Die Daten bleiben
// unverändert, modified false, der Hex-Modus bleibt.
func TestEditorPromptPasteHexMode(t *testing.T) {
	const data = "\x00\x01\xff\xad"
	for _, v := range pasteVariants() {
		t.Run(v.name, func(t *testing.T) {
			defer stubClipboard("ad ff\nzweite", nil)()
			m, _ := openEditorOn(t, map[string]string{"b.bin": data}, "b.bin")
			if !m.fm.edit.hexMode {
				t.Fatal("Hex-Modus nicht aktiv")
			}
			m = openEditPrompt(t, m, "f7")
			m = v.send(t, m)
			if got := promptValue(m); got != "ad ff" {
				t.Errorf("Eingabezeile = %q, erwartet %q", got, "ad ff")
			}
			if string(m.fm.edit.hexb.data) != data || m.fm.edit.modified {
				t.Errorf("Hex: Daten = %q, modified = %v", m.fm.edit.hexb.data, m.fm.edit.modified)
			}
			if !m.fm.edit.hexMode || m.fm.edit.prompt == edPromptNone {
				t.Errorf("hexMode = %v, prompt = %v", m.fm.edit.hexMode, m.fm.edit.prompt)
			}
		})
	}
	t.Run("Bracketed Paste", func(t *testing.T) {
		m, _ := openEditorOn(t, map[string]string{"b.bin": data}, "b.bin")
		m = openEditPrompt(t, m, "f7")
		m = sendEditMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ad ff\n"), Paste: true})
		if got := promptValue(m); got != "ad ff" {
			t.Errorf("Eingabezeile = %q, erwartet %q", got, "ad ff")
		}
		if string(m.fm.edit.hexb.data) != data {
			t.Errorf("Daten verändert: %q", m.fm.edit.hexb.data)
		}
	})
}

// TestEditorPromptPasteEmptyOrUnreadableClipboard: Leere oder nicht lesbare
// Zwischenablage (kein xclip, Fehler): Eingabezeile, Vorbelegung und inputFresh
// bleiben, kein Absturz, der Prompt bleibt offen.
func TestEditorPromptPasteEmptyOrUnreadableClipboard(t *testing.T) {
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
				m = openEditPrompt(t, m, "f7")
				m = typeIntoPrompt(t, m, "alt")
				m = press(t, m, "enter")
				m = openEditPrompt(t, m, "f7")
				m = v.send(t, m)
				if promptValue(m) != "alt" || !m.fm.edit.inputFresh {
					t.Errorf("Eingabezeile = %q, inputFresh = %v", promptValue(m), m.fm.edit.inputFresh)
				}
				assertTextUntouched(t, m, "bleibt")
			})
		}
	}
}

// TestEditorPromptNulKeysKeepPrefill: Unter Windows kommen Einfg, Alt, Strg und
// Shift allein als NUL-Rune. Ohne Einfüge-Kombination (Shift+Einfg bzw.
// Strg+Einfg) darf das weder etwas einfügen noch die Vorbelegung löschen:
// Wert bleibt "alt", inputFresh bleibt true. Danach ersetzt das erste echte
// Zeichen sie wie gewohnt. Auch die NUL-Rune mit Alt-Kennzeichen (Alt allein)
// und mit gedrückter Einfüge-Kombination zählt nicht als Einfügen.
func TestEditorPromptNulKeysKeepPrefill(t *testing.T) {
	altNul := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}, Alt: true}
	cases := []struct {
		name string
		down []int
		msg  tea.Msg
	}{
		{"NUL ohne Taste", nil, nulKey},
		{"NUL nur Einfg gedrückt", []int{vkInsert}, nulKey},
		{"NUL nur Shift gedrückt", []int{vkShift}, nulKey},
		{"NUL nur Strg gedrückt", []int{editTestVKControl}, nulKey},
		{"NUL Shift+Strg ohne Einfg", []int{vkShift, editTestVKControl}, nulKey},
		{"Alt-NUL ohne Taste", nil, altNul},
		{"Alt-NUL mit Shift+Einfg", []int{vkShift, vkInsert}, altNul},
		{"Alt-NUL mit Strg+Einfg", []int{editTestVKControl, vkInsert}, altNul},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer stubClipboard("EINGEFÜGT", nil)()
			stubKeys(t)
			m, _ := openEditorOn(t, map[string]string{"f.txt": "x"}, "f.txt")
			m = openEditPrompt(t, m, "f7")
			m = typeIntoPrompt(t, m, "alt")
			m = press(t, m, "enter")
			m = openEditPrompt(t, m, "f7")
			stubKeys(t, c.down...)
			m = sendEditMsg(m, c.msg)
			if got := promptValue(m); got != "alt" {
				t.Errorf("Eingabezeile = %q, erwartet alt", got)
			}
			if !m.fm.edit.inputFresh {
				t.Error("inputFresh = false: die NUL-Rune hat die Vorbelegung als berührt markiert")
			}
			assertTextUntouched(t, m, "x")
			// Das erste echte Zeichen ersetzt die Vorbelegung weiterhin
			m = typeIntoPrompt(t, m, "q")
			if got := promptValue(m); got != "q" {
				t.Errorf("nach Tippen = %q, erwartet q", got)
			}
		})
	}
}

// TestCleanPromptPaste: Tabellentest der reinen Funktion cleanPromptPaste -
// erste Zeile, Steuerzeichen, ungültiges UTF-8, Grenze (maxRunes <= 0 heißt
// unbegrenzt, Steuerzeichen zählen nicht mit, Mehrbyte zählt in Zeichen).
func TestCleanPromptPaste(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"leer", "", 0, ""},
		{"leer mit Grenze", "", 5, ""},
		{"einfach unbegrenzt", "abc", 0, "abc"},
		{"negative Grenze = unbegrenzt", "abc", -5, "abc"},
		{"genau auf der Grenze", "abc", 3, "abc"},
		{"eins unter der Grenze", "abc", 2, "ab"},
		{"Grenze 1", "abc", 1, "a"},
		{"über der Grenze liegt nichts", "abc", 99, "abc"},
		{"LF", "a\nb", 0, "a"},
		{"CR", "a\rb", 0, "a"},
		{"CRLF", "a\r\nb", 0, "a"},
		{"erste Zeile leer", "\nb", 0, ""},
		{"nur Umbruch", "\r\n", 0, ""},
		{"Tab bleibt", "a\tb", 0, "a\tb"},
		{"ESC fällt weg", "a\x1b[0mb", 0, "a[0mb"},
		{"NUL fällt weg", "a\x00b", 0, "ab"},
		{"DEL fällt weg", "a\x7fb", 0, "ab"},
		{"C1 fällt weg", "a" + string(rune(0x85)) + "b" + string(rune(0x9f)) + "c", 0, "abc"},
		{"U+00A0 bleibt (kein C1)", "a" + string(rune(0xa0)) + "b", 0, "a" + string(rune(0xa0)) + "b"},
		{"Steuerzeichen zählen nicht zur Grenze", "\x00\x00ab", 2, "ab"},
		{"nur Steuerzeichen", "\x00\x07\x1b", 0, ""},
		{"ungültiges UTF-8 fällt weg", "a\xffb", 0, "ab"},
		{"U+FFFD fällt weg", "a�b", 0, "ab"},
		{"abgeschnittenes Mehrbyte-Zeichen", "ab\xc3", 0, "ab"},
		{"Mehrbyte nach Zeichen gezählt", "äöüß", 2, "äö"},
		{"Mehrbyte unbegrenzt", "äöüß", 0, "äöüß"},
		{"japanisch", "日本語テキスト", 3, "日本語"},
		{"Zeile endet vor dem Rest", "ab\ncd", 99, "ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanPromptPaste(c.in, c.max); got != c.want {
				t.Errorf("cleanPromptPaste(%q, %d) = %q, erwartet %q", c.in, c.max, got, c.want)
			}
		})
	}
	// Riesiger Text mit kleiner Grenze: schnell und genau auf der Grenze
	start := time.Now()
	got := cleanPromptPaste(strings.Repeat("x", 20<<20), 256)
	if len(got) != 256 {
		t.Errorf("riesiger Text: Länge %d, erwartet 256", len(got))
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("riesiger Text dauerte %v", d)
	}
}

// TestFmEditorAcceptsPasteStates: fmEditorAcceptsPaste ist true bei offener
// Eingabezeile (im Text- und im Hex-Modus) und im Textmodus ohne alles; false
// bei Dialog, Hilfe, Laden, Speichern, im Hex-Modus ohne Eingabezeile und wenn
// gar kein Editor offen ist - auch dann, wenn zugleich eine Eingabezeile offen ist.
func TestFmEditorAcceptsPasteStates(t *testing.T) {
	open := func(t *testing.T, name string) AppModel {
		m, _ := openEditorOn(t, map[string]string{"f.txt": "x", "b.bin": "\x00\x01\xff"}, name)
		return m
	}
	t.Run("Text ohne Eingabezeile", func(t *testing.T) {
		if !open(t, "f.txt").fmEditorAcceptsPaste() {
			t.Error("false im Textmodus")
		}
	})
	t.Run("Hex ohne Eingabezeile", func(t *testing.T) {
		if open(t, "b.bin").fmEditorAcceptsPaste() {
			t.Error("true im Hex-Modus ohne Eingabezeile")
		}
	})
	t.Run("Text mit Eingabezeile", func(t *testing.T) {
		for _, key := range []string{"f7", "f4"} {
			if !openEditPrompt(t, open(t, "f.txt"), key).fmEditorAcceptsPaste() {
				t.Errorf("false bei offener Eingabezeile (%s)", key)
			}
		}
	})
	t.Run("Hex mit Eingabezeile", func(t *testing.T) {
		if !openEditPrompt(t, open(t, "b.bin"), "f7").fmEditorAcceptsPaste() {
			t.Error("false bei offener Eingabezeile im Hex-Modus")
		}
	})
	t.Run("zweite Zeile (Ersatz)", func(t *testing.T) {
		m := openEditPrompt(t, open(t, "f.txt"), "f4")
		m = typeIntoPrompt(t, m, "x")
		m = press(t, m, "enter")
		if m.fm.edit.prompt != edPromptReplaceWith || !m.fmEditorAcceptsPaste() {
			t.Errorf("prompt = %v, akzeptiert = %v", m.fm.edit.prompt, m.fmEditorAcceptsPaste())
		}
	})
	t.Run("Dialog", func(t *testing.T) {
		m := open(t, "f.txt")
		m = typeEdit(t, m, "a")
		m = press(t, m, "esc")
		if m.fm.edit.dialog == edDialogNone {
			t.Fatal("Dialog nicht offen")
		}
		if m.fmEditorAcceptsPaste() {
			t.Error("true bei offener Rückfrage")
		}
		// Auch mit zugleich gesetzter Eingabezeile: Dialog hat Vorrang
		m.fm.edit.prompt = edPromptSearch
		if m.fmEditorAcceptsPaste() {
			t.Error("true bei Dialog und gesetzter Eingabezeile")
		}
	})
	t.Run("Hilfe", func(t *testing.T) {
		m := pressRun(t, open(t, "f.txt"), "f1")
		if !m.fm.edit.showHelp {
			t.Fatal("Hilfe nicht offen")
		}
		if m.fmEditorAcceptsPaste() {
			t.Error("true bei offener Hilfe")
		}
	})
	t.Run("Laden und Speichern mit Eingabezeile", func(t *testing.T) {
		m := openEditPrompt(t, open(t, "f.txt"), "f7")
		m.fm.edit.loading = true
		if m.fmEditorAcceptsPaste() {
			t.Error("true beim Laden")
		}
		m.fm.edit.loading, m.fm.edit.saving = false, true
		if m.fmEditorAcceptsPaste() {
			t.Error("true beim Speichern")
		}
	})
	t.Run("Editor geschlossen", func(t *testing.T) {
		m := openEditPrompt(t, open(t, "f.txt"), "f7")
		m.fm.editing = false
		if m.fmEditorAcceptsPaste() {
			t.Error("true ohne offenen Editor")
		}
	})
}
