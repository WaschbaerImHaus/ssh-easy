// Paket main - Datei-Manager (MC-Modus): Ansichten und Tastensteuerung
//
// Ablauf: Eintrag "Datei-Manager" ganz unten in der Verbindungsliste ->
// linke Verbindung wählen -> rechte Verbindung wählen -> beide Verbindungen
// werden nacheinander aufgebaut (eigene SSH-Verbindungen ohne Tunnel,
// bei Bedarf mit Passwortabfrage) -> Zwei-Fenster-Ansicht wie im Midnight
// Commander: oben Menüleiste, unten F-Tasten-Leiste, keine Kommandozeile.
//
// q (bzw. Esc im Passwortfeld) bricht jederzeit ab, F10 beendet den
// Datei-Manager; beides führt zurück zur Liste mit dem Cursor auf dem
// Datei-Manager-Eintrag. Verzeichnisse werden im Hintergrund gelesen
// (tea.Cmd), damit die Oberfläche bei langsamen Servern nicht hängt.
//
// Rohfassung (Build 53): Navigation und Seitenwechsel funktionieren,
// Menü und F1-F9 sind nur optisch vorhanden.
//
// @author Kurt Ingwer
// @date   2026-10-21 20:04
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Ersatzgröße, solange das Terminal noch keine Größe gemeldet hat
const (
	fmDefaultWidth  = 80
	fmDefaultHeight = 24
)

// fmPanelChromeLines - Zeilen eines Fensters außer den Einträgen
// (Rahmen oben, Spaltenköpfe, Trennlinie, Infozeile, Rahmen unten)
const fmPanelChromeLines = 5

// fmScreenChromeLines - Zeilen des Bildschirms außer den Fenstern
// (Menüleiste, F-Tasten-Leiste)
const fmScreenChromeLines = 2

// Spaltenbreiten der Fenster
const (
	fmSizeColumnWidth = 7
	fmDateColumnWidth = 16
	fmDateFormat      = "2006-01-02 15:04"
)

// fileManager ist der Zustand des Datei-Managers.
type fileManager struct {
	// Laufende Nummer der Sitzung: Ergebnisse älterer Sitzungen werden verworfen
	session int
	// Verbindungsauswahl: welche Seite wird gerade gewählt (0 links, 1 rechts)
	pickSide int
	// Verbindungsauswahl: Cursor und erste sichtbare Zeile
	pickCursor int
	pickOffset int
	// Linkes (0) und rechtes (1) Dateifenster
	panels [2]filePanel
	// Aktives Fenster (0 links, 1 rechts)
	active int
	// Farbmodus (Index in fmThemes)
	theme int
	// Meldung (verschwindet beim nächsten Tastendruck)
	message string
	// Seite, deren Verbindung gerade aufgebaut wird bzw. ein Passwort braucht
	connectSide  int
	passwordSide int
	// Passwortfeld
	passwordInput textinput.Model
}

// fmConnectedMsg meldet das Ergebnis eines Verbindungsaufbaus.
type fmConnectedMsg struct {
	session int
	side    int
	// Verbundenes Dateisystem (nil bei Fehler)
	fs remoteFS
	// Startverzeichnis und dessen Einträge
	home    string
	entries []fmEntry
	// Fehler beim Lesen des Startverzeichnisses (Verbindung steht trotzdem)
	listErr error
	// Fehler beim Verbinden
	err error
	// War es ein Versuch mit Passwort?
	withPassword bool
}

// fmDirLoadedMsg meldet das Ergebnis eines Verzeichniswechsels.
type fmDirLoadedMsg struct {
	session int
	side    int
	path    string
	entries []fmEntry
	// Name, auf den der Cursor danach soll
	focus string
	err   error
}

// openFileManager startet den Datei-Manager mit der Auswahl der linken
// Verbindung. Der Farbmodus bleibt über Sitzungen hinweg erhalten.
//
// @return tea.Model - Modell in ViewFMPick
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-21 19:45
func (m AppModel) openFileManager() (tea.Model, tea.Cmd) {
	m.fm = fileManager{session: m.fm.session + 1, theme: m.fm.theme}
	m.state = ViewFMPick
	m.errorMsg = ""
	m.successMsg = ""
	return m, nil
}

// closeFileManager schließt alle Verbindungen des Datei-Managers und
// kehrt zur Liste zurück (Cursor auf dem Datei-Manager-Eintrag).
// Die Sitzungsnummer wird erhöht, damit noch laufende Hintergrund-
// ergebnisse verworfen werden.
//
// @return AppModel - Modell in ViewList
// @date   2026-10-21 19:45
func (m AppModel) closeFileManager() AppModel {
	for i := range m.fm.panels {
		if m.fm.panels[i].fs != nil {
			m.fm.panels[i].fs.Close()
		}
	}
	m.fm = fileManager{session: m.fm.session + 1, theme: m.fm.theme}
	m.state = ViewList
	m.cursor = len(m.listRows()) - 1
	m.clampListCursor()
	return m
}

// fmPickConnections liefert die wählbaren Verbindungen: flach (ohne
// Gruppen), alphabetisch wie in der Konfiguration. Bei der Wahl der
// rechten Seite fehlt die links gewählte (keine Verbindung mit sich selbst).
// Davor steht in der Anzeige immer der Platzhalter "(nicht ausgewählt)"
// (Zeile 0), Verbindung i steht also in Zeile i+1.
//
// @return []Connection - Verbindungen
// @date   2026-10-21 20:03
func (m AppModel) fmPickConnections() []Connection {
	if m.fm.pickSide == 0 {
		return m.connections
	}
	leftID := m.fm.panels[0].conn.ID
	conns := make([]Connection, 0, len(m.connections))
	for _, conn := range m.connections {
		if conn.ID != leftID {
			conns = append(conns, conn)
		}
	}
	return conns
}

// handleFMPickKeys verarbeitet Tasten in der Verbindungsauswahl.
// Der Cursor startet auf dem Platzhalter "(nicht ausgewählt)"; Enter darauf
// bewirkt nichts - so landet man nicht mit Enter-Enter versehentlich im
// Datei-Manager, sondern muss beide Verbindungen bewusst wählen.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando (Verbindungsaufbau nach der 2. Wahl)
// @date   2026-10-21 20:03
func (m AppModel) handleFMPickKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	conns := m.fmPickConnections()
	// Zeilen: Platzhalter + Verbindungen
	rows := len(conns) + 1
	limit := visibleRowLimit(m.termHeight)
	switch msg.String() {
	case "q", "esc":
		return m.closeFileManager(), nil
	case "up", "k":
		m.fm.pickCursor--
	case "down", "j":
		m.fm.pickCursor++
	case "pgup":
		m.fm.pickCursor -= limit
	case "pgdown":
		m.fm.pickCursor += limit
	case "home":
		m.fm.pickCursor = 0
	case "end":
		m.fm.pickCursor = rows - 1
	case "enter":
		// Platzhalter oder ungültige Zeile: nichts tun
		if m.fm.pickCursor < 1 || m.fm.pickCursor > len(conns) {
			return m, nil
		}
		conn := conns[m.fm.pickCursor-1]
		// Eigene Verbindung ohne Tunnel (Kopie - die Liste bleibt unverändert)
		conn.Tunnels = nil
		m.fm.panels[m.fm.pickSide] = filePanel{conn: conn}
		if m.fm.pickSide == 0 {
			// Rechte Seite: wieder auf dem Platzhalter beginnen
			m.fm.pickSide = 1
			m.fm.pickCursor = 0
			m.fm.pickOffset = 0
			return m, nil
		}
		return m.startFMConnect(0, "")
	}
	if m.fm.pickCursor >= rows {
		m.fm.pickCursor = rows - 1
	}
	if m.fm.pickCursor < 0 {
		m.fm.pickCursor = 0
	}
	m.fm.pickOffset = scrollOffset(rows, m.fm.pickCursor, m.fm.pickOffset, limit)
	return m, nil
}

// startFMConnect baut die Verbindung einer Seite im Hintergrund auf und
// liest danach gleich das Startverzeichnis (Home des Users).
//
// @param side - Seite (0 links, 1 rechts)
// @param password - Passwort oder leer für Auto-Auth
// @return tea.Model - Modell in ViewFMConnecting
// @return tea.Cmd - Hintergrundkommando
// @date   2026-10-21 19:45
func (m AppModel) startFMConnect(side int, password string) (tea.Model, tea.Cmd) {
	m.state = ViewFMConnecting
	m.fm.connectSide = side
	connect := m.fmConnect
	conn := m.fm.panels[side].conn
	session := m.fm.session
	return m, func() tea.Msg {
		result := fmConnectedMsg{session: session, side: side, withPassword: password != ""}
		fsys, err := connect(conn, password)
		if err != nil {
			result.err = err
			return result
		}
		result.fs = fsys
		result.home = "/"
		if home, err := fsys.Getwd(); err == nil && home != "" {
			result.home = home
		}
		result.entries, result.listErr = readPanelDir(fsys, result.home)
		return result
	}
}

// handleFMConnected übernimmt das Ergebnis eines Verbindungsaufbaus.
// Auth-Fehler führen zur Passwortabfrage; Netzwerk-, Host-Key- und
// SFTP-Fehler beenden den Datei-Manager mit Meldung in der Liste.
// Fehlertexte können Text vom Server enthalten und werden deshalb von
// Steuerzeichen bereinigt.
//
// @param msg - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando (rechte Seite verbinden, Cursor-Blinken)
// @date   2026-10-21 19:53
func (m AppModel) handleFMConnected(msg fmConnectedMsg) (tea.Model, tea.Cmd) {
	// Veraltetes Ergebnis (Nutzer hat inzwischen abgebrochen): aufräumen
	if msg.session != m.fm.session || m.state != ViewFMConnecting {
		if msg.fs != nil {
			msg.fs.Close()
		}
		return m, nil
	}
	conn := m.fm.panels[msg.side].conn
	if msg.err != nil {
		var sftpErr *fmSFTPError
		switch {
		case IsHostKeyChangedError(msg.err):
			m = m.closeFileManager()
			m.errorMsg = fmt.Sprintf(m.tr(txtFMErrHostKey), sanitizeFileName(parseHostKeyChangedHostname(msg.err)))
			return m, nil
		case errors.As(msg.err, &sftpErr):
			m = m.closeFileManager()
			m.errorMsg = fmt.Sprintf(m.tr(txtFMErrSFTP), conn.Name, sanitizeFileName(sftpErr.err.Error()))
			return m, nil
		case IsNetworkError(msg.err):
			m = m.closeFileManager()
			m.errorMsg = fmt.Sprintf(m.tr(txtFMErrConnect), conn.Name, sanitizeFileName(msg.err.Error()))
			return m, nil
		}
		// Anmeldung fehlgeschlagen: Passwort abfragen
		m.state = ViewFMPassword
		m.fm.passwordSide = msg.side
		m.fm.message = ""
		if msg.withPassword {
			m.fm.message = m.tr(txtFMWrongPassword)
		}
		m.fm.passwordInput = createPasswordInput()
		m.fm.passwordInput.Placeholder = m.lang.PlaceholderPW
		m.fm.passwordInput.Focus()
		return m, textinput.Blink
	}

	panel := &m.fm.panels[msg.side]
	panel.fs = msg.fs
	entries := msg.entries
	if msg.listErr != nil {
		// Startverzeichnis nicht lesbar: wenigstens ".." anbieten
		m.fm.message = m.fmDirError(msg.home, msg.listErr)
		entries = nil
		if msg.home != "/" {
			entries = []fmEntry{{name: "..", dir: true, parent: true}}
		}
	}
	panel.applyListing(msg.home, entries, "")
	if msg.side == 0 {
		return m.startFMConnect(1, "")
	}
	m.state = ViewFileManager
	m.fm.active = 0
	return m, nil
}

// fmDirError liefert die Meldung zu einem Lesefehler.
//
// @param dir - Verzeichnis
// @param err - Fehler
// @return string - Meldung
// @date   2026-10-21 19:45
func (m AppModel) fmDirError(dir string, err error) string {
	dir = sanitizeFileName(dir)
	if errors.Is(err, os.ErrPermission) {
		return fmt.Sprintf(m.tr(txtFMErrPermission), dir)
	}
	return fmt.Sprintf(m.tr(txtFMErrReadDir), dir, sanitizeFileName(err.Error()))
}

// handleFMConnectingKeys - während des Verbindungsaufbaus nur Abbruch.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-21 19:45
func (m AppModel) handleFMConnectingKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return m.closeFileManager(), nil
	}
	return m, nil
}

// handleFMPasswordKeys verarbeitet Tasten in der Passwortabfrage.
// "q" ist hier ein normales Zeichen (Teil des Passworts) - Abbruch mit Esc.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-10-21 19:45
func (m AppModel) handleFMPasswordKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeFileManager(), nil
	case "enter":
		password := m.fm.passwordInput.Value()
		if password == "" {
			return m, nil
		}
		m.fm.passwordInput.SetValue("")
		m.fm.message = ""
		return m.startFMConnect(m.fm.passwordSide, password)
	}
	var cmd tea.Cmd
	m.fm.passwordInput, cmd = m.fm.passwordInput.Update(msg)
	return m, cmd
}

// fmVisibleRows liefert die Zahl sichtbarer Einträge pro Fenster.
//
// @return int - Zeilen (mindestens 1)
// @date   2026-10-21 19:45
func (m AppModel) fmVisibleRows() int {
	_, height := m.fmScreenSize()
	rows := height - fmScreenChromeLines - fmPanelChromeLines
	if rows < 1 {
		return 1
	}
	return rows
}

// fmScreenSize liefert die Bildschirmgröße (Ersatzwerte, solange unbekannt).
//
// @return int - Breite
// @return int - Höhe
// @date   2026-10-21 19:45
func (m AppModel) fmScreenSize() (int, int) {
	width, height := m.termWidth, m.termHeight
	if width <= 0 {
		width = fmDefaultWidth
	}
	if height <= 0 {
		height = fmDefaultHeight
	}
	return width, height
}

// handleFileManagerKeys verarbeitet Tasten in der Zwei-Fenster-Ansicht.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando (Verzeichnis lesen)
// @date   2026-10-21 19:45
func (m AppModel) handleFileManagerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Meldungen gelten nur bis zum nächsten Tastendruck
	m.fm.message = ""
	panel := &m.fm.panels[m.fm.active]
	rows := m.fmVisibleRows()

	switch msg.String() {
	case "q", "f10":
		return m.closeFileManager(), nil
	case "tab":
		m.fm.active = 1 - m.fm.active
		return m, nil
	case "up", "k":
		panel.moveCursor(-1)
	case "down", "j":
		panel.moveCursor(1)
	case "pgup":
		panel.moveCursor(-rows)
	case "pgdown":
		panel.moveCursor(rows)
	case "home":
		panel.setCursor(0)
	case "end":
		panel.setCursor(len(panel.entries) - 1)
	case "enter":
		return m.fmEnter()
	case "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9":
		// Menü und Dateifunktionen folgen in späteren Builds
		m.fm.message = m.tr(txtFMNotYet)
	}
	panel.offset = scrollOffset(len(panel.entries), panel.cursor, panel.offset, rows)
	return m, nil
}

// fmEnter führt Enter auf dem aktuellen Eintrag des aktiven Fensters aus:
// Verzeichnis wechseln (im Hintergrund), bei Symlinks auf Verzeichnisse
// nur eine Meldung, bei Dateien nichts.
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Hintergrundkommando oder nil
// @date   2026-10-21 19:45
func (m AppModel) fmEnter() (tea.Model, tea.Cmd) {
	side := m.fm.active
	panel := &m.fm.panels[side]
	if panel.loading || panel.fs == nil {
		return m, nil
	}
	action, target, focus := panel.enterTarget()
	switch action {
	case fmEnterSymlinkDir:
		entry, _ := panel.current()
		m.fm.message = fmt.Sprintf(m.tr(txtFMErrSymlink), sanitizeFileName(entry.name))
		return m, nil
	case fmEnterChangeDir:
		panel.loading = true
		fsys := panel.fs
		session := m.fm.session
		return m, func() tea.Msg {
			entries, err := readPanelDir(fsys, target)
			return fmDirLoadedMsg{session: session, side: side, path: target, entries: entries, focus: focus, err: err}
		}
	}
	return m, nil
}

// handleFMDirLoaded übernimmt ein gelesenes Verzeichnis. Bei einem Fehler
// (z. B. fehlende Rechte) bleibt das Fenster im bisherigen Verzeichnis.
//
// @param msg - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-21 19:45
func (m AppModel) handleFMDirLoaded(msg fmDirLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.session != m.fm.session || m.state != ViewFileManager {
		return m, nil
	}
	panel := &m.fm.panels[msg.side]
	panel.loading = false
	if msg.err != nil {
		m.fm.message = m.fmDirError(msg.path, msg.err)
		return m, nil
	}
	panel.applyListing(msg.path, msg.entries, msg.focus)
	panel.offset = scrollOffset(len(panel.entries), panel.cursor, panel.offset, m.fmVisibleRows())
	return m, nil
}

// --- Darstellung ---

// renderFMPick rendert die Verbindungsauswahl (linke bzw. rechte Seite).
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-21 20:03
func (m AppModel) renderFMPick(s *strings.Builder) {
	title := m.tr(txtFMPickLeft)
	if m.fm.pickSide == 1 {
		title = m.tr(txtFMPickRight)
	}
	s.WriteString(titleStyle.Render("  " + title))
	s.WriteString("\n\n")

	conns := m.fmPickConnections()
	// Zeile 0 = Platzhalter "(nicht ausgewählt)", Zeile i = conns[i-1]
	rows := len(conns) + 1
	limit := visibleRowLimit(m.termHeight)
	offset := scrollOffset(rows, m.fm.pickCursor, m.fm.pickOffset, limit)
	end := offset + limit
	if end > rows {
		end = rows
	}
	if offset > 0 {
		s.WriteString(helpStyle.UnsetMarginTop().Render(fmt.Sprintf("  ↑ +%d", offset)) + "\n")
	}
	for i := offset; i < end; i++ {
		line := "  " + m.tr(txtFMNotSelected)
		if i > 0 {
			conn := conns[i-1]
			line = fmt.Sprintf("  %s  (%s@%s)", conn.Name, conn.User, conn.Host)
		}
		if i == m.fm.pickCursor {
			s.WriteString(selectedStyle.Render(line))
		} else {
			s.WriteString(normalStyle.Render(line))
		}
		s.WriteString("\n")
	}
	if end < rows {
		s.WriteString(helpStyle.UnsetMarginTop().Render(fmt.Sprintf("  ↓ +%d", rows-end)) + "\n")
	}
	s.WriteString(helpStyle.Render(m.tr(txtFMPickHelp)))
}

// renderFMConnecting rendert den Verbindungsaufbau.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-21 19:45
func (m AppModel) renderFMConnecting(s *strings.Builder) {
	conn := m.fm.panels[m.fm.connectSide].conn
	s.WriteString(titleStyle.Render("  " + fmt.Sprintf(m.tr(txtFMConnecting), conn.Name)))
	s.WriteString("\n\n")
	s.WriteString(m.lang.TryingAutoAuth + "\n")
	s.WriteString(helpStyle.Render(m.tr(txtFMConnectingHelp)))
}

// renderFMPassword rendert die Passwortabfrage.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-21 19:45
func (m AppModel) renderFMPassword(s *strings.Builder) {
	conn := m.fm.panels[m.fm.passwordSide].conn
	s.WriteString(titleStyle.Render("  " + fmt.Sprintf(m.tr(txtFMPasswordTitle), conn.Name)))
	s.WriteString("\n\n")
	s.WriteString("  " + conn.User + "@" + conn.Host + "\n\n")
	s.WriteString("  " + m.fm.passwordInput.View() + "\n")
	if m.fm.message != "" {
		s.WriteString("\n" + errorStyle.Render("  "+m.fm.message) + "\n")
	}
	s.WriteString(helpStyle.Render(m.tr(txtFMPasswordHelp)))
}

// renderFileManager rendert die Zwei-Fenster-Ansicht bildschirmfüllend:
// genau so viele Zeilen und Spalten wie das Terminal (sonst scrollt es).
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-21 19:45
func (m AppModel) renderFileManager(s *strings.Builder) {
	width, height := m.fmScreenSize()
	st := fmThemeStyles(m.fm.theme)

	lines := make([]string, 0, height)
	lines = append(lines, m.renderFMMenuBar(st, width))

	panelHeight := height - fmScreenChromeLines
	if panelHeight < 0 {
		panelHeight = 0
	}
	leftWidth := width / 2
	left := m.renderFMPanel(0, st, leftWidth, panelHeight)
	right := m.renderFMPanel(1, st, width-leftWidth, panelHeight)
	for i := 0; i < panelHeight; i++ {
		lines = append(lines, left[i]+right[i])
	}
	lines = append(lines, m.renderFMKeyBar(st, width))

	// Sehr kleine Fenster: auf die Terminalhöhe kürzen
	if len(lines) > height {
		lines = lines[:height]
	}
	s.WriteString(strings.Join(lines, "\n"))
}

// renderFMMenuBar rendert die Menüleiste (vorerst nur optisch).
//
// @param st - Stile des Farbmodus
// @param width - Bildschirmbreite
// @return string - Zeile
// @date   2026-10-21 19:45
func (m AppModel) renderFMMenuBar(st fmStyles, width int) string {
	items := strings.Split(m.tr(txtFMMenuBar), "|")
	return st.menu.Render(fitWidth("  "+strings.Join(items, "    "), width))
}

// renderFMKeyBar rendert die F-Tasten-Leiste: zehn gleich breite Felder
// mit Nummer und Beschriftung (das letzte Feld nimmt den Rest).
//
// @param st - Stile des Farbmodus
// @param width - Bildschirmbreite
// @return string - Zeile
// @date   2026-10-21 19:45
func (m AppModel) renderFMKeyBar(st fmStyles, width int) string {
	labels := strings.Split(m.tr(txtFMFKeys), "|")
	slot := width / len(labels)
	var bar strings.Builder
	for i, label := range labels {
		slotWidth := slot
		if i == len(labels)-1 {
			slotWidth = width - slot*(len(labels)-1)
		}
		num := strconv.Itoa(i + 1)
		if len(num) > slotWidth {
			num = num[:slotWidth]
		}
		bar.WriteString(st.keyNum.Render(num))
		bar.WriteString(st.keyLabel.Render(fitWidth(label, slotWidth-ansi.StringWidth(num))))
	}
	return bar.String()
}

// renderFMPanel rendert ein Dateifenster als genau height Zeilen zu je
// genau width Zeichen.
//
// @param side - Seite (0 links, 1 rechts)
// @param st - Stile des Farbmodus
// @param width - Breite des Fensters
// @param height - Höhe des Fensters
// @return []string - Zeilen
// @date   2026-10-21 19:45
func (m AppModel) renderFMPanel(side int, st fmStyles, width, height int) []string {
	panel := m.fm.panels[side]
	active := side == m.fm.active
	inner := width - 2
	if inner < 0 {
		inner = 0
	}
	cols := fmColumnsFor(inner)
	lines := make([]string, 0, height)

	// Rahmen oben mit Titel (aktives Fenster hervorgehoben)
	titleStyle := st.frame
	if active {
		titleStyle = st.activeTitle
	}
	title := ""
	if inner >= 3 {
		title = fitWidthLeft(" "+panel.title()+" ", inner)
	}
	leftFill := (inner - ansi.StringWidth(title)) / 2
	rightFill := inner - ansi.StringWidth(title) - leftFill
	lines = append(lines, st.frame.Render("┌"+strings.Repeat("─", leftFill))+
		titleStyle.Render(title)+
		st.frame.Render(strings.Repeat("─", rightFill)+"┐"))

	// Spaltenköpfe
	headers := strings.Split(m.tr(txtFMColumns), "|")
	lines = append(lines, st.frame.Render("│")+m.renderFMColumns(cols, st.header, st.header,
		fitCenter(headers[0], cols.name), fitCenter(headers[1], cols.size), fitCenter(headers[2], cols.date))+
		st.frame.Render("│"))

	// Einträge
	rows := height - fmPanelChromeLines
	if rows < 0 {
		rows = 0
	}
	offset := scrollOffset(len(panel.entries), panel.cursor, panel.offset, rows)
	for i := offset; i < offset+rows; i++ {
		var content string
		if i < len(panel.entries) {
			content = m.renderFMEntry(st, cols, panel.entries[i], active && i == panel.cursor)
		} else {
			content = m.renderFMColumns(cols, st.panel, st.panel, "", "", "")
		}
		lines = append(lines, st.frame.Render("│")+content+st.frame.Render("│"))
	}

	// Trennlinie, Infozeile (aktueller Eintrag oder Meldung), Rahmen unten
	lines = append(lines, st.frame.Render("├"+strings.Repeat("─", inner)+"┤"))
	info := st.panel.Render(fitWidth("", inner))
	if active && m.fm.message != "" {
		info = st.message.Render(fitWidth(m.fm.message, inner))
	} else if entry, ok := panel.current(); ok {
		info = st.panel.Render(fitWidth(sanitizeFileName(entry.name), inner))
	}
	lines = append(lines, st.frame.Render("│")+info+st.frame.Render("│"))
	lines = append(lines, st.frame.Render("└"+strings.Repeat("─", inner)+"┘"))

	// Auf die Fensterhöhe bringen (sehr kleine Terminals)
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, st.panel.Render(strings.Repeat(" ", width)))
	}
	return lines
}

// fmColumns beschreibt die Spaltenbreiten (0 = Spalte ausgeblendet).
type fmColumns struct {
	name, size, date int
}

// fmColumnsFor verteilt die Innenbreite auf die Spalten: Name, Größe,
// Datum; bei schmalen Fenstern fällt erst das Datum, dann die Größe weg.
//
// @param inner - Innenbreite des Fensters
// @return fmColumns - Spaltenbreiten
// @date   2026-10-21 19:45
func fmColumnsFor(inner int) fmColumns {
	switch {
	case inner >= 40:
		return fmColumns{name: inner - fmSizeColumnWidth - fmDateColumnWidth - 2, size: fmSizeColumnWidth, date: fmDateColumnWidth}
	case inner >= 16:
		return fmColumns{name: inner - fmSizeColumnWidth - 1, size: fmSizeColumnWidth}
	}
	return fmColumns{name: inner}
}

// renderFMColumns setzt eine Zeile aus Spalteninhalten zusammen; die
// Spaltentrenner tragen den Stil sep, die Inhalte den Stil text.
//
// @param cols - Spaltenbreiten
// @param text - Stil der Inhalte
// @param sep - Stil der Trenner
// @param name - Inhalt der Namensspalte
// @param size - Inhalt der Größenspalte (rechtsbündig)
// @param date - Inhalt der Datumsspalte
// @return string - Zeile ohne äußeren Rahmen
// @date   2026-10-21 19:45
func (m AppModel) renderFMColumns(cols fmColumns, text, sep lipgloss.Style, name, size, date string) string {
	var line strings.Builder
	line.WriteString(text.Render(fitWidth(name, cols.name)))
	if cols.size > 0 {
		line.WriteString(sep.Render("│"))
		line.WriteString(text.Render(fitRight(size, cols.size)))
	}
	if cols.date > 0 {
		line.WriteString(sep.Render("│"))
		line.WriteString(text.Render(fitWidth(date, cols.date)))
	}
	return line.String()
}

// renderFMEntry rendert einen Eintrag. Kennzeichen vor dem Namen wie im
// mc: "/" Verzeichnis, "~" Symlink auf Verzeichnis, "@" Symlink auf
// Datei, "!" defekter Symlink.
//
// @param st - Stile des Farbmodus
// @param cols - Spaltenbreiten
// @param entry - Eintrag
// @param selected - Cursorzeile?
// @return string - Zeile ohne äußeren Rahmen
// @date   2026-10-21 19:45
func (m AppModel) renderFMEntry(st fmStyles, cols fmColumns, entry fmEntry, selected bool) string {
	marker, style := " ", st.file
	switch {
	case entry.symlink && entry.broken:
		marker, style = "!", st.broken
	case entry.symlink && entry.dir:
		marker, style = "~", st.link
	case entry.symlink:
		marker, style = "@", st.link
	case entry.dir:
		marker, style = "/", st.dir
	}
	size := formatFileSize(entry.size)
	date := ""
	if entry.parent {
		size = m.tr(txtFMUpDir)
	} else if !entry.modTime.IsZero() {
		date = entry.modTime.Format(fmDateFormat)
	}
	sep := st.frame
	if selected {
		style, sep = st.cursor, st.cursor
	}
	return m.renderFMColumns(cols, style, sep, marker+sanitizeFileName(entry.name), size, date)
}

// --- Hilfsfunktionen für feste Breiten ---

// fitWidth kürzt einen Text auf width Zellen (mit "~" am Ende) bzw. füllt
// ihn mit Leerzeichen auf. Breite Zeichen (z. B. CJK) zählen doppelt.
//
// @param s - Text ohne Steuerzeichen
// @param width - Zielbreite
// @return string - Text mit genau width Zellen
// @date   2026-10-21 19:45
func fitWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > width {
		s = ansi.Truncate(s, width, "~")
	}
	return s + strings.Repeat(" ", width-ansi.StringWidth(s))
}

// fitRight richtet einen Text rechtsbündig auf width Zellen aus.
//
// @param s - Text
// @param width - Zielbreite
// @return string - Text mit genau width Zellen
// @date   2026-10-21 19:45
func fitRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > width {
		return fitWidth(s, width)
	}
	return strings.Repeat(" ", width-ansi.StringWidth(s)) + s
}

// fitCenter zentriert einen Text auf width Zellen.
//
// @param s - Text
// @param width - Zielbreite
// @return string - Text mit genau width Zellen
// @date   2026-10-21 19:45
func fitCenter(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) >= width {
		return fitWidth(s, width)
	}
	left := (width - ansi.StringWidth(s)) / 2
	return fitWidth(strings.Repeat(" ", left)+s, width)
}

// fitWidthLeft kürzt einen Text von LINKS auf höchstens width Zellen
// ("~" vorne) - bei Pfaden ist das Ende wichtiger als der Anfang.
//
// @param s - Text
// @param width - Höchstbreite
// @return string - Gekürzter Text (kann schmaler als width sein)
// @date   2026-10-21 19:45
func fitWidthLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && ansi.StringWidth("~"+string(runes)) > width {
		runes = runes[1:]
	}
	return "~" + string(runes)
}
