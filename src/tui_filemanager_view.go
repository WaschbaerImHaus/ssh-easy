// Paket main - Datei-Manager (MC-Modus): Betrachter (F3)
//
// F3 auf einer Datei öffnet einen bildschirmfüllenden Betrachter wie im mc.
// Die Datei wird per SFTP geladen (höchstens fmViewMaxBytes, sonst gekürzt
// mit Hinweis). Der Inhalt kommt vom Server und ist nicht vertrauenswürdig:
// Steuerzeichen und unsichtbare Formatzeichen werden zu "?", Tabs zu
// Leerzeichen - so kann eine Datei das Terminal nicht steuern.
//
// Tasten wie im mc-Betrachter (unten als F-Tasten-Leiste): F1 Hilfe,
// F2 Zeilenumbruch an/aus (ohne Umbruch ←/→ waagerecht), F3/F10 Beenden,
// F4 Hex/Text, F5 Gehe zu (Zeile bzw. Offset), F6 Suchen mit regulärem
// Ausdruck, F7 Suchen, n weitersuchen, F9 lokal mit dem Standardprogramm
// öffnen (filemanager_open.go); dazu ↑/↓, Bild↑/Bild↓, Pos1/Ende,
// q und Esc. Binärdaten (NUL-Bytes) erscheinen automatisch als Hex-Ansicht.
// Reguläre Ausdrücke nutzen Gos RE2 (lineare Laufzeit, kein Hängen durch
// bösartige Muster).
//
// @author Kurt Ingwer
// @date   2026-10-21 21:36
package main

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// fmViewMaxBytes - höchstens so viel wird geladen (Variable für Tests).
var fmViewMaxBytes int64 = 16 << 20

// fmViewTabWidth - Tabulatorabstand in der Textansicht
const fmViewTabWidth = 8

// fmViewScrollStep - so viele Zeichen springen ←/→ ohne Umbruch
const fmViewScrollStep = 8

// fmPrompt ist die gerade offene Eingabezeile des Betrachters.
type fmPrompt int

const (
	// fmPromptNone - keine Eingabe offen
	fmPromptNone fmPrompt = iota
	// fmPromptGoto - Gehe zu Zeile/Offset (F5)
	fmPromptGoto
	// fmPromptSearch - Suchen (F7)
	fmPromptSearch
	// fmPromptRegex - Suchen mit regulärem Ausdruck (F6)
	fmPromptRegex
)

// fmViewer ist der Zustand des Betrachters.
type fmViewer struct {
	// Anzeigename (user@host:/pfad) und Dateiname (bereinigt)
	title, name string
	// Originalname, Pfad und Dateisystem (für F9 "mit Standardprogramm öffnen")
	rawName, remotePath string
	fs                  remoteFS
	// Geladene Bytes und ob die Datei länger war
	data      []byte
	truncated bool
	// Dateigröße laut Server
	size int64
	// Aufbereitete Textzeilen und die breiteste Zeile (Zellen)
	lines    []string
	maxWidth int
	// Läuft das Laden noch?
	loading bool
	// Hex-Ansicht bzw. Zeilenumbruch
	hex, wrap bool
	// Textansicht: erste sichtbare Zeile und Umbruchstück darin
	top, topSub int
	// Textansicht ohne Umbruch: waagerechte Verschiebung in Zellen
	left int
	// Hex-Ansicht: erste sichtbare Zeile
	hexTop int
	// Offene Eingabezeile und ihr Feld; inputFresh = Vorbelegung noch
	// unberührt (das erste getippte Zeichen ersetzt sie, wie im mc)
	prompt     fmPrompt
	input      textinput.Model
	inputFresh bool
	// Letzte Suche (Text bzw. regulärer Ausdruck) und letzter Treffer
	// (Zeile in der Textansicht, Byte in der Hex-Ansicht; -1 = keiner)
	query       string
	regex       bool
	matchLine   int
	matchOffset int
	// Meldung in der unteren Zeile (bis zum nächsten Tastendruck)
	message string
	// Hilfe (F1) sichtbar?
	showHelp bool
}

// fmViewLoadedMsg überbringt die geladene Datei.
type fmViewLoadedMsg struct {
	session, seq int
	data         []byte
	size         int64
	truncated    bool
	err          error
}

// fmOpenViewer öffnet den Betrachter für den Eintrag unter dem Cursor.
// Verzeichnisse und ".." verhalten sich wie Enter (wie im mc).
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Laden im Hintergrund
// @date   2026-10-21 21:36
func (m AppModel) fmOpenViewer() (tea.Model, tea.Cmd) {
	panel := m.fm.panels[m.fm.active]
	entry, ok := panel.current()
	if !ok || panel.fs == nil || panel.loading {
		return m, nil
	}
	if entry.dir {
		return m.fmEnter()
	}
	filePath := path.Join(panel.path, entry.name)
	m.fm.viewSeq++
	m.fm.viewing = true
	m.fm.view = fmViewer{
		title:       sanitizeFileName(panel.conn.User + "@" + panel.conn.Host + ":" + filePath),
		name:        sanitizeFileName(entry.name),
		rawName:     entry.name,
		remotePath:  filePath,
		fs:          panel.fs,
		loading:     true,
		wrap:        true,
		matchLine:   -1,
		matchOffset: -1,
	}
	fsys, session, seq := panel.fs, m.fm.session, m.fm.viewSeq
	return m, func() tea.Msg {
		return loadViewData(fsys, filePath, session, seq)
	}
}

// loadViewData liest eine Datei bis zur Größengrenze.
//
// @param fsys - Dateisystem
// @param filePath - Pfad (Symlinks auf Dateien werden beim Öffnen verfolgt)
// @param session - Sitzungsnummer
// @param seq - Nummer des Betrachters
// @return fmViewLoadedMsg - Ergebnis
// @date   2026-10-21 21:24
func loadViewData(fsys remoteFS, filePath string, session, seq int) fmViewLoadedMsg {
	msg := fmViewLoadedMsg{session: session, seq: seq}
	reader, err := fsys.OpenRead(filePath)
	if err != nil {
		msg.err = err
		return msg
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, fmViewMaxBytes+1))
	if err != nil {
		msg.err = err
		return msg
	}
	if int64(len(data)) > fmViewMaxBytes {
		data = data[:fmViewMaxBytes]
		msg.truncated = true
	}
	msg.data = data
	msg.size = int64(len(data))
	if info, err := fsys.Stat(filePath); err == nil && info.Size() >= 0 {
		msg.size = info.Size()
	}
	return msg
}

// handleFMViewLoaded übernimmt die geladene Datei.
//
// @param msg - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-21 21:24
func (m AppModel) handleFMViewLoaded(msg fmViewLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.session != m.fm.session || msg.seq != m.fm.viewSeq || !m.fm.viewing {
		return m, nil
	}
	if msg.err != nil {
		m.fm.viewing = false
		m.fm.message = fmt.Sprintf(m.tr(txtFMErrView), m.fm.view.name, sanitizeFileName(msg.err.Error()))
		m.fm.view = fmViewer{}
		return m, nil
	}
	v := &m.fm.view
	v.loading = false
	v.data = msg.data
	v.size = msg.size
	v.truncated = msg.truncated
	v.hex = looksBinary(msg.data)
	v.lines = splitViewLines(msg.data)
	for _, line := range v.lines {
		v.maxWidth = max(v.maxWidth, ansi.StringWidth(line))
	}
	return m, nil
}

// handleFMViewerKeys verarbeitet Tasten im Betrachter.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando (Cursor-Blinken der Eingabezeile)
// @date   2026-10-21 21:36
func (m AppModel) handleFMViewerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.fm.view
	// Hilfe: eine beliebige Taste schließt sie
	if v.showHelp {
		v.showHelp = false
		return m, nil
	}
	if v.prompt != fmPromptNone {
		return m.handleFMViewerPrompt(msg)
	}
	v.message = ""
	switch msg.String() {
	case "q", "esc", "f3", "f10":
		m.fm.viewing = false
		m.fm.view = fmViewer{}
		return m, nil
	}
	if v.loading {
		return m, nil
	}
	width, height := m.fmScreenSize()
	rows := max(height-2, 1)
	switch msg.String() {
	case "f1":
		v.showHelp = true
	case "f2":
		v.wrap = !v.wrap
		v.topSub, v.left = 0, 0
	case "f4":
		v.hex = !v.hex
	case "f5":
		return m, v.openPrompt(fmPromptGoto)
	case "f6":
		return m, v.openPrompt(fmPromptRegex)
	case "f7":
		return m, v.openPrompt(fmPromptSearch)
	case "f9":
		return m.fmOpenExternal()
	case "n":
		if v.query != "" {
			v.search(width, true, m.tr)
		}
	case "up", "k":
		v.scroll(-1, width, rows)
	case "down", "j":
		v.scroll(1, width, rows)
	case "pgup":
		v.scroll(-rows, width, rows)
	case "pgdown":
		v.scroll(rows, width, rows)
	case "home":
		v.top, v.topSub, v.left, v.hexTop = 0, 0, 0, 0
	case "end":
		v.toEnd(width, rows)
	case "left":
		v.left = max(v.left-fmViewScrollStep, 0)
	case "right":
		if !v.wrap && !v.hex {
			v.left = min(v.left+fmViewScrollStep, max(v.maxWidth-width, 0))
		}
	}
	return m, nil
}

// fmOpenExternalMsg meldet das Ergebnis von F9 (herunterladen + öffnen).
type fmOpenExternalMsg struct {
	session, seq int
	err          error
}

// fmOpenExternal lädt die angezeigte Datei herunter und öffnet sie mit dem
// Standardprogramm des Rechners (siehe filemanager_open.go). Ausführbare
// Typen und zu große Dateien werden abgelehnt.
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Herunterladen + Öffnen im Hintergrund
// @date   2026-10-22 00:37
func (m AppModel) fmOpenExternal() (tea.Model, tea.Cmd) {
	v := &m.fm.view
	if v.fs == nil {
		return m, nil
	}
	if v.size > fmOpenMaxBytes {
		v.message = fmt.Sprintf(m.tr(txtFMOpenTooLarge), formatSizeWithUnit(fmOpenMaxBytes))
		return m, nil
	}
	local := localOpenName(v.rawName)
	if ext, blocked := blockedOpenExtension(local); blocked {
		// Ohne Endung den Namen nennen, sonst die Endung
		if ext == "" || ext == "." {
			ext = v.name
		}
		v.message = fmt.Sprintf(m.tr(txtFMOpenBlocked), sanitizeFileName(ext))
		return m, nil
	}
	v.message = fmt.Sprintf(m.tr(txtFMOpenDownloading), v.name)
	fsys, remotePath, session, seq := v.fs, v.remotePath, m.fm.session, m.fm.viewSeq
	return m, func() tea.Msg {
		path, err := downloadForOpen(fsys, remotePath, local)
		if err == nil {
			err = externalOpener(path)
		}
		return fmOpenExternalMsg{session: session, seq: seq, err: err}
	}
}

// handleFMOpenExternal zeigt das Ergebnis von F9 an.
//
// @param msg - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-22 00:37
func (m AppModel) handleFMOpenExternal(msg fmOpenExternalMsg) (tea.Model, tea.Cmd) {
	if msg.session != m.fm.session || msg.seq != m.fm.viewSeq || !m.fm.viewing {
		return m, nil
	}
	v := &m.fm.view
	switch {
	case msg.err == errOpenTooLarge:
		v.message = fmt.Sprintf(m.tr(txtFMOpenTooLarge), formatSizeWithUnit(fmOpenMaxBytes))
	case msg.err == errOfficeUnsafe:
		v.message = fmt.Sprintf(m.tr(txtFMOpenMacro), v.name)
	case msg.err != nil:
		v.message = fmt.Sprintf(m.tr(txtFMErrOpen), v.name, sanitizeFileName(msg.err.Error()))
	default:
		v.message = fmt.Sprintf(m.tr(txtFMOpenDone), v.name)
	}
	return m, nil
}

// openPrompt öffnet eine Eingabezeile (Suchen ist mit dem letzten Begriff
// vorbelegt, wie im mc).
//
// @param kind - Art der Eingabe
// @return tea.Cmd - Cursor-Blinken
// @date   2026-10-21 21:36
func (v *fmViewer) openPrompt(kind fmPrompt) tea.Cmd {
	v.prompt = kind
	v.input = textinput.New()
	v.input.Prompt = ""
	v.input.CharLimit = 256
	v.inputFresh = false
	if kind != fmPromptGoto && v.query != "" && v.regex == (kind == fmPromptRegex) {
		v.input.SetValue(v.query)
		v.inputFresh = true
	}
	v.input.Focus()
	return textinput.Blink
}

// handleFMViewerPrompt verarbeitet Tasten in der Eingabezeile: Enter führt
// aus, Esc bricht ab (q ist hier ein normales Zeichen).
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando der Eingabezeile
// @date   2026-10-21 21:36
func (m AppModel) handleFMViewerPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.fm.view
	switch msg.String() {
	case "esc":
		v.prompt = fmPromptNone
		return m, nil
	case "enter":
		kind, value := v.prompt, strings.TrimSpace(v.input.Value())
		v.prompt = fmPromptNone
		if value == "" {
			return m, nil
		}
		width, _ := m.fmScreenSize()
		if kind == fmPromptGoto {
			v.gotoPosition(value, width, m.tr)
			return m, nil
		}
		v.query, v.regex = value, kind == fmPromptRegex
		v.matchLine, v.matchOffset = -1, -1
		v.search(width, false, m.tr)
		return m, nil
	}
	// Erstes getipptes Zeichen bzw. Rücktaste ersetzt die Vorbelegung
	if v.inputFresh && (msg.Type == tea.KeyRunes || msg.Type == tea.KeyBackspace) {
		v.input.SetValue("")
		if msg.Type == tea.KeyBackspace {
			v.inputFresh = false
			return m, nil
		}
	}
	v.inputFresh = false
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return m, cmd
}

// gotoPosition springt zu einer Zeile (Text) bzw. einem Offset (Hex,
// dezimal oder mit 0x).
//
// @param value - Eingabe
// @param width - Bildschirmbreite
// @param tr - Übersetzungsfunktion
// @date   2026-10-21 21:36
func (v *fmViewer) gotoPosition(value string, width int, tr func(textKey) string) {
	if v.hex {
		offset, err := strconv.ParseInt(value, 0, 64)
		if err != nil || offset < 0 {
			v.message = fmt.Sprintf(tr(txtFMBadNumber), sanitizeFileName(value))
			return
		}
		v.hexTop = int(min(offset, int64(len(v.data)))) / hexBytesPerRow(width)
		return
	}
	line, err := strconv.Atoi(value)
	if err != nil || line < 1 {
		v.message = fmt.Sprintf(tr(txtFMBadNumber), sanitizeFileName(value))
		return
	}
	v.top, v.topSub = min(line-1, max(len(v.lines)-1, 0)), 0
}

// search sucht den letzten Suchbegriff (ohne Groß-/Kleinschreibung) ab der
// aktuellen Position bzw. hinter dem letzten Treffer (next) und zeigt den
// Treffer oben an. In der Hex-Ansicht wird in den Rohdaten gesucht.
//
// @param width - Bildschirmbreite
// @param next - hinter dem letzten Treffer weitersuchen
// @param tr - Übersetzungsfunktion
// @date   2026-10-21 21:36
func (v *fmViewer) search(width int, next bool, tr func(textKey) string) {
	var re *regexp.Regexp
	if v.regex {
		var err error
		if re, err = regexp.Compile("(?i)" + v.query); err != nil {
			v.message = fmt.Sprintf(tr(txtFMBadRegex), sanitizeFileName(err.Error()))
			return
		}
	}
	notFound := fmt.Sprintf(tr(txtFMNotFound), sanitizeFileName(v.query))

	if v.hex {
		start := v.hexTop * hexBytesPerRow(width)
		if next && v.matchOffset >= 0 {
			start = v.matchOffset + 1
		}
		if start >= len(v.data) {
			v.message = notFound
			return
		}
		var index int
		if re != nil {
			loc := re.FindIndex(v.data[start:])
			index = -1
			if loc != nil {
				index = loc[0]
			}
		} else {
			index = bytes.Index(bytes.ToLower(v.data[start:]), bytes.ToLower([]byte(v.query)))
		}
		if index < 0 {
			v.message = notFound
			return
		}
		v.matchOffset = start + index
		v.hexTop = v.matchOffset / hexBytesPerRow(width)
		return
	}

	start := v.top
	if next && v.matchLine >= 0 {
		start = v.matchLine + 1
	}
	lowerQuery := strings.ToLower(v.query)
	for i := start; i < len(v.lines); i++ {
		column := -1
		if re != nil {
			if loc := re.FindStringIndex(v.lines[i]); loc != nil {
				column = loc[0]
			}
		} else {
			column = strings.Index(strings.ToLower(v.lines[i]), lowerQuery)
		}
		if column < 0 {
			continue
		}
		v.matchLine = i
		v.top, v.topSub = i, 0
		if !v.wrap {
			// Treffer waagerecht sichtbar machen
			visual := ansi.StringWidth(v.lines[i][:min(column, len(v.lines[i]))])
			v.left = max(0, min(visual-width/3, max(v.maxWidth-width, 0)))
		}
		return
	}
	v.message = notFound
}

// scroll bewegt die Ansicht um delta Bildschirmzeilen; am Ende bleibt die
// letzte Bildschirmseite voll.
//
// @param delta - Zeilen (negativ = nach oben)
// @param width - Bildschirmbreite
// @param rows - sichtbare Zeilen
// @date   2026-10-21 21:24
func (v *fmViewer) scroll(delta, width, rows int) {
	if v.hex {
		total := hexRowCount(len(v.data), hexBytesPerRow(width))
		v.hexTop = min(max(v.hexTop+delta, 0), max(total-rows, 0))
		return
	}
	if !v.wrap {
		v.top = min(max(v.top+delta, 0), max(len(v.lines)-rows, 0))
		v.topSub = 0
		return
	}
	for ; delta < 0; delta++ {
		if v.topSub > 0 {
			v.topSub--
		} else if v.top > 0 {
			v.top--
			v.topSub = len(segmentLine(v.lines[v.top], width)) - 1
		}
	}
	for ; delta > 0 && !v.atEnd(width, rows); delta-- {
		if v.topSub+1 < len(segmentLine(v.lines[v.top], width)) {
			v.topSub++
		} else if v.top+1 < len(v.lines) {
			v.top++
			v.topSub = 0
		}
	}
}

// toEnd springt ans Ende, sodass die letzte Bildschirmseite voll ist.
// Im Umbruch-Modus vom letzten Stück aus rückwärts - nicht Zeile für
// Zeile von vorn (bei großen Dateien zu langsam).
//
// @param width - Bildschirmbreite
// @param rows - sichtbare Zeilen
// @date   2026-10-21 21:24
func (v *fmViewer) toEnd(width, rows int) {
	if v.hex || !v.wrap {
		v.scroll(len(v.data)+len(v.lines)+1, width, rows)
		return
	}
	if len(v.lines) == 0 {
		return
	}
	v.top = len(v.lines) - 1
	v.topSub = len(segmentLine(v.lines[v.top], width)) - 1
	v.scroll(-(rows - 1), width, rows)
}

// atEnd meldet (Umbruch-Modus), ob ab der aktuellen Position höchstens noch
// eine Bildschirmseite folgt.
//
// @date   2026-10-21 21:24
func (v *fmViewer) atEnd(width, rows int) bool {
	remaining := 0
	for i := v.top; i < len(v.lines); i++ {
		segments := len(segmentLine(v.lines[i], width))
		if i == v.top {
			segments -= v.topSub
		}
		remaining += segments
		if remaining > rows {
			return false
		}
	}
	return true
}

// renderFMViewer rendert den Betrachter bildschirmfüllend.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-21 21:36
func (m AppModel) renderFMViewer(s *strings.Builder) {
	width, height := m.fmScreenSize()
	st := fmThemeStyles(m.fm.theme)
	v := m.fm.view
	rows := max(height-2, 0)

	// Kopfzeile: Datei, Größe, Position, ggf. Hinweis auf Kürzung
	header := " " + v.title + "  " + formatSizeWithUnit(v.size)
	if !v.loading {
		if v.hex {
			header += fmt.Sprintf("  %08x", v.hexTop*hexBytesPerRow(width))
		} else {
			header += "  " + fmt.Sprintf(m.tr(txtFMViewLine), min(v.top+1, max(len(v.lines), 1)), len(v.lines))
		}
		if v.truncated {
			header += "  " + fmt.Sprintf(m.tr(txtFMViewTruncated), formatSizeWithUnit(int64(len(v.data))))
		}
	}
	lines := []string{st.menu.Render(fitWidth(header, width))}

	// Inhalt; hit[i] = Zeile i gehört zum Suchtreffer (hervorheben)
	var content []string
	var hit []bool
	switch {
	case v.loading:
		content = []string{m.tr(txtFMViewLoading)}
	case v.hex:
		content, hit = v.hexRows(width, rows)
	case v.wrap:
		content, hit = v.wrappedRows(width, rows)
	default:
		for i := v.top; i < len(v.lines) && len(content) < rows; i++ {
			content = append(content, ansi.TruncateLeft(v.lines[i], v.left, ""))
			hit = append(hit, i == v.matchLine)
		}
	}
	for i := 0; i < rows; i++ {
		text, style := "", st.panel
		if i < len(content) {
			text = content[i]
		}
		if i < len(hit) && hit[i] {
			style = st.cursor
		}
		lines = append(lines, style.Render(fitWidth(text, width)))
	}

	// Untere Zeile: Eingabe, Meldung oder F-Tasten-Leiste wie im mc
	switch {
	case v.prompt != fmPromptNone:
		label := map[fmPrompt]string{
			fmPromptGoto:   m.tr(txtFMGotoLine),
			fmPromptSearch: m.tr(txtFMSearchPrompt),
			fmPromptRegex:  m.tr(txtFMRegexPrompt),
		}[v.prompt]
		if v.prompt == fmPromptGoto && v.hex {
			label = m.tr(txtFMGotoOffset)
		}
		lines = append(lines, st.keyLabel.Render(fitWidth(" "+label+v.input.View(), width)))
	case v.message != "":
		lines = append(lines, st.message.Render(fitWidth(" "+v.message, width)))
	default:
		lines = append(lines, renderKeyBar(st, width, strings.Split(m.tr(txtFMViewFKeys), "|")))
	}

	// Hilfe (F1) als Kasten über dem Inhalt
	if v.showHelp && len(lines) > 2 {
		boxWidth := dialogBoxWidth(width)
		help := strings.Split(m.tr(txtFMViewHelpText), "\n")
		box := renderDialogBox(st, boxWidth, height, m.tr(txtFMHelpTitle), help, m.tr(txtFMHelpClose))
		overlayDialog(lines, box, boxWidth, width)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	s.WriteString(strings.Join(lines, "\n"))
}

// wrappedRows liefert die sichtbaren Bildschirmzeilen im Umbruch-Modus.
//
// @date   2026-10-21 21:36
func (v fmViewer) wrappedRows(width, rows int) ([]string, []bool) {
	var out []string
	var hit []bool
	for i := v.top; i < len(v.lines) && len(out) < rows; i++ {
		segments := segmentLine(v.lines[i], width)
		start := 0
		if i == v.top {
			start = min(v.topSub, len(segments)-1)
		}
		for _, segment := range segments[start:] {
			if len(out) == rows {
				break
			}
			out = append(out, segment)
			hit = append(hit, i == v.matchLine)
		}
	}
	return out, hit
}

// hexRows liefert die sichtbaren Zeilen der Hex-Ansicht.
//
// @date   2026-10-21 21:36
func (v fmViewer) hexRows(width, rows int) ([]string, []bool) {
	perRow := hexBytesPerRow(width)
	total := hexRowCount(len(v.data), perRow)
	top := min(v.hexTop, max(total-rows, 0))
	var out []string
	var hit []bool
	for row := top; row < total && len(out) < rows; row++ {
		start := row * perRow
		out = append(out, hexLine(v.data[start:min(start+perRow, len(v.data))], start, perRow))
		hit = append(hit, v.matchOffset >= start && v.matchOffset < start+perRow)
	}
	return out, hit
}

// hexBytesPerRow wählt die Bytes pro Hex-Zeile passend zur Breite:
// "OOOOOOOO  hh hh …  ascii" braucht 11 + 4 Zellen je Byte; höchstens 16.
//
// @param width - Bildschirmbreite
// @return int - Bytes pro Zeile (mindestens 1)
// @date   2026-10-21 21:24
func hexBytesPerRow(width int) int {
	return min(max((width-11)/4, 1), 16)
}

// hexRowCount liefert die Zahl der Hex-Zeilen.
//
// @date   2026-10-21 21:24
func hexRowCount(size, perRow int) int {
	return (size + perRow - 1) / perRow
}

// hexLine formatiert eine Hex-Zeile: Offset, Bytes, druckbare Zeichen.
//
// @param chunk - Bytes der Zeile
// @param offset - Position des ersten Bytes
// @param perRow - Bytes pro Zeile (für gleichmäßige Spalten)
// @return string - Zeile
// @date   2026-10-21 21:24
func hexLine(chunk []byte, offset, perRow int) string {
	var hexPart, asciiPart strings.Builder
	for i := 0; i < perRow; i++ {
		if i > 0 {
			hexPart.WriteByte(' ')
		}
		if i < len(chunk) {
			fmt.Fprintf(&hexPart, "%02x", chunk[i])
			if chunk[i] >= 0x20 && chunk[i] < 0x7f {
				asciiPart.WriteByte(chunk[i])
			} else {
				asciiPart.WriteByte('.')
			}
		} else {
			hexPart.WriteString("  ")
		}
	}
	return fmt.Sprintf("%08x  %s  %s", offset, hexPart.String(), asciiPart.String())
}

// splitViewLines zerlegt den Inhalt in aufbereitete Zeilen; ein
// abschließender Zeilenumbruch erzeugt keine leere letzte Zeile.
//
// @param data - Dateiinhalt
// @return []string - Zeilen
// @date   2026-10-21 21:24
func splitViewLines(data []byte) []string {
	raw := strings.Split(string(data), "\n")
	if len(raw) > 1 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	lines := make([]string, len(raw))
	for i, line := range raw {
		lines[i] = prepareViewLine(line)
	}
	return lines
}

// prepareViewLine macht eine Zeile anzeigbar: CR am Ende entfernen, Tabs zu
// Leerzeichen, Steuer- und Formatzeichen zu "?".
//
// @param line - Rohzeile
// @return string - sichere Zeile
// @date   2026-10-21 21:24
func prepareViewLine(line string) string {
	line = strings.TrimSuffix(line, "\r")
	if strings.Contains(line, "\t") {
		var out strings.Builder
		column := 0
		for _, r := range line {
			if r == '\t' {
				spaces := fmViewTabWidth - column%fmViewTabWidth
				out.WriteString(strings.Repeat(" ", spaces))
				column += spaces
				continue
			}
			out.WriteRune(r)
			column += max(runewidth.RuneWidth(r), 0)
		}
		line = out.String()
	}
	return sanitizeFileName(line)
}

// segmentLine bricht eine Zeile nach Zellbreite um (breite Zeichen zählen
// doppelt und werden nicht geteilt). Eine leere Zeile ergibt ein leeres Stück.
//
// @param line - aufbereitete Zeile
// @param width - Breite (≤ 0 = nicht umbrechen)
// @return []string - Stücke
// @date   2026-10-21 21:24
func segmentLine(line string, width int) []string {
	if width <= 0 || ansi.StringWidth(line) <= width {
		return []string{line}
	}
	var segments []string
	var current strings.Builder
	used := 0
	for _, r := range line {
		w := runewidth.RuneWidth(r)
		if used+w > width && used > 0 {
			segments = append(segments, current.String())
			current.Reset()
			used = 0
		}
		current.WriteRune(r)
		used += w
	}
	return append(segments, current.String())
}

// looksBinary erkennt Binärdaten an NUL-Bytes in den ersten 8 KiB.
//
// @param data - Inhalt
// @return bool - true bei Binärdaten
// @date   2026-10-21 21:24
func looksBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8192)], 0) >= 0
}
