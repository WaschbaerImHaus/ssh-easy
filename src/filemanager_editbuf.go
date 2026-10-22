// Paket main - Datei-Manager: Puffer des Editors (Text und Hex)
//
// Reine Bearbeitungslogik ohne Oberfläche, damit sie gründlich testbar ist:
//   - textBuffer: Zeilen aus Zeichen (Runen). Laden und Speichern sind
//     verlustfrei: Windows-Zeilenenden (CRLF) bleiben erhalten, ebenso ein
//     fehlender Zeilenumbruch am Dateiende und einzelne \r mitten im Text
//     (gemischte Dateien werden nicht "repariert").
//   - hexBuffer: Bytes, die nur überschrieben werden (Größe bleibt gleich),
//     mit Merker für geänderte Bytes - für Binärdateien.
//
// Positionen sind (Zeile, Spalte) in Zeichen, nicht in Bytes.
//
// @author Kurt Ingwer
// @date   2026-10-22 00:45
package main

import (
	"encoding/hex"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// editTabWidth - Tabulatorabstand im Editor
const editTabWidth = 8

// textPos ist eine Position im Textpuffer (Zeile, Spalte in Zeichen).
type textPos struct {
	row, col int
}

// before meldet, ob p vor q liegt.
//
// @date   2026-10-22 00:45
func (p textPos) before(q textPos) bool {
	return p.row < q.row || (p.row == q.row && p.col < q.col)
}

// orderPos liefert zwei Positionen in Textreihenfolge.
//
// @date   2026-10-22 00:45
func orderPos(a, b textPos) (textPos, textPos) {
	if b.before(a) {
		return b, a
	}
	return a, b
}

// textBuffer ist der Inhalt einer Textdatei als Zeilen.
type textBuffer struct {
	lines [][]rune
	// Alle Zeilenenden waren \r\n (dann auch beim Speichern)
	crlf bool
	// Datei endete mit einem Zeilenumbruch
	finalNewline bool
}

// newTextBuffer zerlegt einen Dateiinhalt (gültiges UTF-8) in Zeilen.
//
// @param data - Dateiinhalt
// @return *textBuffer - Puffer
// @date   2026-10-22 00:45
func newTextBuffer(data []byte) *textBuffer {
	buf := &textBuffer{}
	text := string(data)
	parts := strings.Split(text, "\n")
	buf.finalNewline = len(text) > 0 && strings.HasSuffix(text, "\n")
	if buf.finalNewline {
		parts = parts[:len(parts)-1]
	}
	// Zeilen mit Zeilenende: alle außer der letzten (ohne Umbruch am Ende)
	terminated := len(parts) - 1
	if buf.finalNewline {
		terminated = len(parts)
	}
	buf.crlf = terminated > 0
	for i := 0; i < terminated; i++ {
		if !strings.HasSuffix(parts[i], "\r") {
			buf.crlf = false
			break
		}
	}
	for i, part := range parts {
		if buf.crlf && i < terminated {
			part = strings.TrimSuffix(part, "\r")
		}
		buf.lines = append(buf.lines, []rune(part))
	}
	if len(buf.lines) == 0 {
		buf.lines = [][]rune{{}}
	}
	return buf
}

// bytes liefert den Inhalt zum Speichern (mit den ursprünglichen Zeilenenden).
//
// @return []byte - Dateiinhalt
// @date   2026-10-22 00:45
func (b *textBuffer) bytes() []byte {
	eol := "\n"
	if b.crlf {
		eol = "\r\n"
	}
	var out strings.Builder
	for i, line := range b.lines {
		if i > 0 {
			out.WriteString(eol)
		}
		out.WriteString(string(line))
	}
	if b.finalNewline {
		out.WriteString(eol)
	}
	return []byte(out.String())
}

// clamp hält eine Position im gültigen Bereich.
//
// @date   2026-10-22 00:45
func (b *textBuffer) clamp(p textPos) textPos {
	p.row = min(max(p.row, 0), len(b.lines)-1)
	p.col = min(max(p.col, 0), len(b.lines[p.row]))
	return p
}

// insert fügt Text ein ('\n' teilt die Zeile) und liefert die Position
// hinter dem eingefügten Text.
//
// @param p - Position
// @param text - Einzufügendes (darf Zeilenumbrüche enthalten)
// @return textPos - Position danach
// @date   2026-10-22 00:45
func (b *textBuffer) insert(p textPos, text []rune) textPos {
	p = b.clamp(p)
	line := b.lines[p.row]
	head := append([]rune{}, line[:p.col]...)
	tail := append([]rune{}, line[p.col:]...)
	segments := splitRunes(text, '\n')
	if len(segments) == 1 {
		b.lines[p.row] = append(append(head, segments[0]...), tail...)
		return textPos{p.row, p.col + len(segments[0])}
	}
	newLines := make([][]rune, 0, len(segments))
	newLines = append(newLines, append(head, segments[0]...))
	for _, segment := range segments[1 : len(segments)-1] {
		newLines = append(newLines, append([]rune{}, segment...))
	}
	last := segments[len(segments)-1]
	newLines = append(newLines, append(append([]rune{}, last...), tail...))
	b.lines = append(b.lines[:p.row], append(newLines, b.lines[p.row+1:]...)...)
	return textPos{p.row + len(segments) - 1, len(last)}
}

// splitRunes zerlegt Runen an einem Trennzeichen.
//
// @date   2026-10-22 00:45
func splitRunes(text []rune, sep rune) [][]rune {
	parts := [][]rune{{}}
	for _, r := range text {
		if r == sep {
			parts = append(parts, []rune{})
			continue
		}
		parts[len(parts)-1] = append(parts[len(parts)-1], r)
	}
	return parts
}

// backspace löscht das Zeichen vor p (am Zeilenanfang: Zeilen verbinden).
//
// @param p - Position
// @return textPos - neue Position
// @date   2026-10-22 00:45
func (b *textBuffer) backspace(p textPos) textPos {
	p = b.clamp(p)
	if p.col > 0 {
		line := b.lines[p.row]
		b.lines[p.row] = append(line[:p.col-1:p.col-1], line[p.col:]...)
		return textPos{p.row, p.col - 1}
	}
	if p.row == 0 {
		return p
	}
	prev := b.lines[p.row-1]
	col := len(prev)
	b.lines[p.row-1] = append(append([]rune{}, prev...), b.lines[p.row]...)
	b.lines = append(b.lines[:p.row], b.lines[p.row+1:]...)
	return textPos{p.row - 1, col}
}

// deleteForward löscht das Zeichen an p (am Zeilenende: nächste Zeile anhängen).
//
// @param p - Position
// @date   2026-10-22 00:45
func (b *textBuffer) deleteForward(p textPos) {
	p = b.clamp(p)
	line := b.lines[p.row]
	if p.col < len(line) {
		b.lines[p.row] = append(line[:p.col:p.col], line[p.col+1:]...)
		return
	}
	if p.row+1 < len(b.lines) {
		b.lines[p.row] = append(append([]rune{}, line...), b.lines[p.row+1]...)
		b.lines = append(b.lines[:p.row+1], b.lines[p.row+2:]...)
	}
}

// deleteLine entfernt eine ganze Zeile (Strg+Y); die letzte verbleibende
// Zeile wird nur geleert.
//
// @param row - Zeile
// @date   2026-10-22 00:45
func (b *textBuffer) deleteLine(row int) {
	if row < 0 || row >= len(b.lines) {
		return
	}
	if len(b.lines) == 1 {
		b.lines[0] = []rune{}
		b.finalNewline = false
		return
	}
	b.lines = append(b.lines[:row], b.lines[row+1:]...)
}

// textBetween liefert den Text zwischen zwei Positionen (Zeilen mit '\n').
//
// @date   2026-10-22 00:45
func (b *textBuffer) textBetween(a, c textPos) []rune {
	start, end := orderPos(b.clamp(a), b.clamp(c))
	if start.row == end.row {
		return append([]rune{}, b.lines[start.row][start.col:end.col]...)
	}
	out := append([]rune{}, b.lines[start.row][start.col:]...)
	for row := start.row + 1; row < end.row; row++ {
		out = append(append(out, '\n'), b.lines[row]...)
	}
	return append(append(out, '\n'), b.lines[end.row][:end.col]...)
}

// deleteBetween löscht den Text zwischen zwei Positionen.
//
// @return textPos - Anfangsposition (Cursor danach)
// @date   2026-10-22 00:45
func (b *textBuffer) deleteBetween(a, c textPos) textPos {
	start, end := orderPos(b.clamp(a), b.clamp(c))
	merged := append(append([]rune{}, b.lines[start.row][:start.col]...), b.lines[end.row][end.col:]...)
	b.lines = append(b.lines[:start.row], append([][]rune{merged}, b.lines[end.row+1:]...)...)
	return start
}

// find sucht ohne Groß-/Kleinschreibung ab einer Position (einschließlich).
//
// @param query - Suchbegriff (eine Zeile)
// @param from - Startposition
// @return textPos - Treffer
// @return bool - gefunden?
// @date   2026-10-22 00:45
func (b *textBuffer) find(query string, from textPos) (textPos, bool) {
	needle := lowerRunes([]rune(query))
	if len(needle) == 0 {
		return textPos{}, false
	}
	from = b.clamp(from)
	for row := from.row; row < len(b.lines); row++ {
		hay := lowerRunes(b.lines[row])
		start := 0
		if row == from.row {
			start = from.col
		}
		for col := start; col+len(needle) <= len(hay); col++ {
			if runesEqual(hay[col:col+len(needle)], needle) {
				return textPos{row, col}, true
			}
		}
	}
	return textPos{}, false
}

// replaceAt ersetzt n Zeichen ab p durch text und liefert die Position dahinter.
//
// @date   2026-10-22 00:45
func (b *textBuffer) replaceAt(p textPos, n int, text []rune) textPos {
	p = b.clamp(p)
	line := b.lines[p.row]
	end := min(p.col+n, len(line))
	b.lines[p.row] = append(append(append([]rune{}, line[:p.col]...), text...), line[end:]...)
	return textPos{p.row, p.col + len(text)}
}

// lowerRunes liefert Kleinbuchstaben (Zeichen für Zeichen, gleiche Länge).
//
// @date   2026-10-22 00:45
func lowerRunes(r []rune) []rune {
	out := make([]rune, len(r))
	for i, c := range r {
		out[i] = unicode.ToLower(c)
	}
	return out
}

// runesEqual vergleicht zwei Runenfolgen.
//
// @date   2026-10-22 00:45
func runesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// editRuneDisplay liefert, wie ein Zeichen an Anzeigespalte col erscheint:
// Tab als Leerzeichen bis zum nächsten Tabstopp, Steuer- und unsichtbare
// Formatzeichen als "?" (Inhalt vom Server - darf das Terminal nicht steuern).
//
// @param r - Zeichen
// @param col - Anzeigespalte davor
// @return string - Darstellung
// @return int - Breite in Zellen
// @date   2026-10-22 00:45
func editRuneDisplay(r rune, col int) (string, int) {
	switch {
	case r == '\t':
		width := editTabWidth - col%editTabWidth
		return strings.Repeat(" ", width), width
	case unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d'):
		return "?", 1
	}
	return string(r), max(runewidth.RuneWidth(r), 0)
}

// displayColumn liefert die Anzeigespalte vor dem Zeichen col einer Zeile.
//
// @date   2026-10-22 00:45
func displayColumn(line []rune, col int) int {
	width := 0
	for i := 0; i < col && i < len(line); i++ {
		_, w := editRuneDisplay(line[i], width)
		width += w
	}
	return width
}

// renderEditLine liefert die sichere Anzeige einer Zeile.
//
// @date   2026-10-22 00:45
func renderEditLine(line []rune) string {
	var out strings.Builder
	width := 0
	for _, r := range line {
		text, w := editRuneDisplay(r, width)
		out.WriteString(text)
		width += w
	}
	return out.String()
}

// hexBuffer ist eine Binärdatei zum Überschreiben einzelner Bytes.
type hexBuffer struct {
	data []byte
	orig []byte
	// Geänderte Bytes (für die Hervorhebung)
	changed []bool
	// Cursor (Byte) und ob als Nächstes das untere Halbbyte kommt
	cursor int
	low    bool
}

// newHexBuffer erzeugt einen Hex-Puffer (die Daten werden kopiert).
//
// @date   2026-10-22 00:45
func newHexBuffer(data []byte) *hexBuffer {
	return &hexBuffer{
		data:    append([]byte{}, data...),
		orig:    append([]byte{}, data...),
		changed: make([]bool, len(data)),
	}
}

// typeHex überschreibt ein Halbbyte; nach dem unteren geht es zum nächsten Byte.
//
// @param r - Hex-Ziffer
// @return bool - angenommen?
// @date   2026-10-22 00:45
func (h *hexBuffer) typeHex(r rune) bool {
	digit, ok := hexDigit(r)
	if !ok || len(h.data) == 0 {
		return false
	}
	b := h.data[h.cursor]
	if !h.low {
		b = digit<<4 | b&0x0f
		h.low = true
	} else {
		b = b&0xf0 | digit
		h.low = false
	}
	h.set(b)
	if !h.low && h.cursor < len(h.data)-1 {
		h.cursor++
	}
	return true
}

// typeASCII überschreibt das Byte mit einem druckbaren ASCII-Zeichen.
//
// @date   2026-10-22 00:45
func (h *hexBuffer) typeASCII(r rune) bool {
	if r < 0x20 || r > 0x7e || len(h.data) == 0 {
		return false
	}
	h.set(byte(r))
	h.low = false
	if h.cursor < len(h.data)-1 {
		h.cursor++
	}
	return true
}

// set schreibt das Byte unter dem Cursor und merkt die Änderung.
//
// @date   2026-10-22 00:45
func (h *hexBuffer) set(b byte) {
	h.data[h.cursor] = b
	h.changed[h.cursor] = b != h.orig[h.cursor]
}

// move bewegt den Cursor um delta Bytes (perRow nur für Aufrufer, die
// zeilenweise rechnen) und hält ihn im Bereich.
//
// @date   2026-10-22 00:45
func (h *hexBuffer) move(delta, perRow int) {
	_ = perRow
	h.low = false
	if len(h.data) == 0 {
		h.cursor = 0
		return
	}
	h.cursor = min(max(h.cursor+delta, 0), len(h.data)-1)
}

// modified meldet, ob sich ein Byte geändert hat.
//
// @date   2026-10-22 00:45
func (h *hexBuffer) modified() bool {
	for _, c := range h.changed {
		if c {
			return true
		}
	}
	return false
}

// hexDigit wandelt eine Hex-Ziffer um.
//
// @date   2026-10-22 00:45
func hexDigit(r rune) (byte, bool) {
	switch {
	case r >= '0' && r <= '9':
		return byte(r - '0'), true
	case r >= 'a' && r <= 'f':
		return byte(r-'a') + 10, true
	case r >= 'A' && r <= 'F':
		return byte(r-'A') + 10, true
	}
	return 0, false
}

// parseHexQuery deutet einen Suchbegriff im Hex-Editor: "ad ff" bzw. "ADFF"
// (gerade Anzahl Hex-Ziffern) als Bytes, alles andere als Text.
//
// @date   2026-10-22 00:45
func parseHexQuery(query string) []byte {
	compact := strings.ReplaceAll(query, " ", "")
	if compact != "" && len(compact)%2 == 0 {
		if decoded, err := hex.DecodeString(compact); err == nil {
			return decoded
		}
	}
	return []byte(query)
}
