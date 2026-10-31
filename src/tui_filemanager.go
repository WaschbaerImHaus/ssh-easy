// Paket main - Datei-Manager (MC-Modus): Ansichten und Tastensteuerung
//
// Ablauf: Eintrag "Datei-Manager" ganz unten in der Verbindungsliste ->
// linke Verbindung wählen -> rechte Verbindung oder "(keine)" wählen ->
// die Verbindungen werden nacheinander aufgebaut (eigene SSH-Verbindungen
// ohne Tunnel, bei Bedarf mit Passwortabfrage) -> Zwei-Fenster-Ansicht wie
// im Midnight Commander: oben Menüleiste, unten F-Tasten-Leiste, keine
// Kommandozeile.
//
// q (bzw. Esc im Passwortfeld) bricht jederzeit ab, F10 beendet den
// Datei-Manager; beides führt zurück zur Liste mit dem Cursor auf dem
// Datei-Manager-Eintrag. Verzeichnisse werden im Hintergrund gelesen
// (tea.Cmd), damit die Oberfläche bei langsamen Servern nicht hängt.
//
// Stand Build 73: Navigation, Seitenwechsel, Markieren (Einfg, * kehrt
// alle Markierungen um), Ansehen (F3, tui_filemanager_view.go), Bearbeiten
// (F4, tui_filemanager_edit.go), Kopieren (F5, tui_filemanager_copy.go),
// Verschieben (F6), Verzeichnis anlegen (F7), Löschen (F8,
// tui_filemanager_ops.go), Menüs (F9, tui_filemanager_menu.go) und Neu
// einlesen (Strg+R) funktionieren; F1 und F2 sind nur optisch vorhanden.
//
// Build 82: Ein-Fenster-Modus (fileManager.single): nur eine Seite wird
// verbunden, ein Fenster über die ganze Breite. Kopieren und Verschieben
// (F5/F6) entfallen dort - sie brauchen eine zweite Verbindung -, alles
// andere bleibt gleich.
//
// Build 86: Nach einer erfolgreichen Anmeldung mit Passwort - oder wenn
// die Verbindung noch keinen eigenen Schlüssel hat - wird ungefragt ein
// Schlüssel vergeben (tui_filemanager_keydeploy.go).
//
// Build 87 (Nutzerwunsch 2026-10-28): Es gibt nur noch EINEN Eintrag
// "Datei-Manager". Der Platzhalter "(nicht ausgewählt)" entfällt:
//   - links:  Zeile 0 "Lokaler PC", danach die Verbindungen
//   - rechts: Zeile 0 "(keine)" = Ein-Fenster-Modus, dann "Lokaler PC"
//     (nur wenn links nicht der Lokale PC gewählt ist), dann die
//     Verbindungen ohne die linke
//
// Rechts wird immer gefragt, auch mit nur einer Verbindung (sonst gäbe es
// kein Kopieren zwischen Server und PC). Ist "Verbindung merken"
// eingeschaltet (AppConfig.RememberSelection), steht der Cursor auf der
// zuletzt gewählten Seite (FileManagerLastLeft/-Right), und der Abschluss
// der Wahl speichert sie. Abbruch mit q/Esc speichert nichts.
//
// @author Kurt Ingwer
// @date   2026-10-31 02:06
package main

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	// Ein-Fenster-Modus: rechts "(keine)" gewählt (ein Fenster, kein
	// Kopieren/Verschieben)
	single bool
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
	// Ergebnisse von Schlüsselvergaben, die während des Verbindungsaufbaus
	// bzw. der Passwortabfrage eintrafen (tui_filemanager_keydeploy.go):
	// keyNotice alle in Ankunftsfolge (erscheint beim Wechsel in die
	// Fensteransicht), keyNoticeOK/keyNoticeErr getrennt nach Erfolg und
	// Fehler (erscheinen in der Liste, wenn der Datei-Manager vorher
	// geschlossen wird)
	keyNotice    string
	keyNoticeOK  string
	keyNoticeErr string
	// Seite, deren Verbindung gerade aufgebaut wird bzw. ein Passwort braucht
	connectSide  int
	passwordSide int
	// Passwortfeld
	passwordInput textinput.Model
	// Offener Dialog (Kopieren usw.) und Zustand des Kopiervorgangs
	dialog fmDialog
	copy   fmCopyState
	// Namensfeld von F7 und Zustand des Löschens (F8)
	mkdirInput textinput.Model
	del        fmDeleteState
	// Menü (F9): offen?, gewähltes Menü (0 Links … 3 Rechts) und Eintrag
	menuOpen  bool
	menuIndex int
	menuItem  int
	// Optionen: Cursor im Farbmodus-Dialog, Eingabe der F9-Endungen und
	// die geprüfte Liste, die auf die Bestätigung des Risikohinweises wartet
	themeCursor int
	extInput    textinput.Model
	extPending  []string
	// Endungen-Liste (Optionen): Einträge, Cursor, Versatz, Fokus im
	// Hinzufügen-Feld?
	extList   []string
	extCursor int
	extOffset int
	extAdding bool
	// Betrachter (F3): offen?, Zustand, laufende Nummer (veraltete Ladeergebnisse)
	viewing bool
	view    fmViewer
	viewSeq int
	// Editor (F4): offen?, Zustand, laufende Nummer
	editing bool
	edit    fmEditor
	editSeq int
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
	// Name, auf den der Cursor danach soll; fehlt er, Position fallback
	focus    string
	fallback int
	// Namen, die nach dem Einlesen wieder markiert werden
	marks map[string]bool
	err   error
}

// openFileManager startet den Datei-Manager mit der Auswahl der linken
// Verbindung. Der Farbmodus bleibt über Sitzungen hinweg erhalten. Der
// Cursor steht auf Zeile 0 ("Lokaler PC") bzw. - bei eingeschaltetem
// Merken - auf der zuletzt gewählten linken Seite; das Scroll-Fenster wird
// gleich so gesetzt, dass dieser Eintrag sichtbar ist.
//
// @return tea.Model - Modell in ViewFMPick
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-31 02:06
func (m AppModel) openFileManager() (tea.Model, tea.Cmd) {
	m.fm = fileManager{session: m.fm.session + 1, theme: m.fm.theme}
	m.state = ViewFMPick
	m.errorMsg = ""
	m.successMsg = ""
	m.fmPickStart()
	return m, nil
}

// closeFileManager schließt alle Verbindungen des Datei-Managers und
// kehrt zur Liste zurück (Cursor auf dem Datei-Manager-Eintrag). Die
// Sitzungsnummer wird erhöht, damit noch laufende Hintergrund-
// ergebnisse verworfen werden. Noch nicht gezeigte Meldungen der
// Schlüsselvergabe (fm.keyNotice) gehen nicht verloren, sondern erscheinen
// in der Liste: Erfolg in successMsg, Fehler in errorMsg.
//
// @return AppModel - Modell in ViewList
// @date   2026-10-31 02:06
func (m AppModel) closeFileManager() AppModel {
	for i := range m.fm.panels {
		if m.fm.panels[i].fs != nil {
			m.fm.panels[i].fs.Close()
		}
	}
	// Cursor auf den (einzigen) Datei-Manager-Eintrag; er steht ganz
	// unten, deshalb von hinten suchen
	rows := m.listRows()
	m.cursor = len(rows) - 1
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].kind == rowFileManager {
			m.cursor = i
			break
		}
	}
	m.successMsg = joinFMNotice(m.successMsg, m.fm.keyNoticeOK)
	m.errorMsg = joinFMNotice(m.errorMsg, m.fm.keyNoticeErr)
	m.fm = fileManager{session: m.fm.session + 1, theme: m.fm.theme}
	m.state = ViewList
	m.clampListCursor()
	return m
}

// fmPickConnections liefert die wählbaren Verbindungen: zuerst "Lokaler
// PC", dann die Verbindungen flach (ohne Gruppen), alphabetisch wie in der
// Konfiguration. Bei der Wahl der rechten Seite fehlt die links gewählte
// (keine Verbindung mit sich selbst; war links der Lokale PC, fehlt er).
// "(keine)" gehört NICHT dazu: In der Anzeige steht links Verbindung i in
// Zeile i, rechts steht "(keine)" in Zeile 0 und Verbindung i in Zeile i+1
// (siehe fmPickRows/fmPickRowConn).
//
// @return []Connection - Verbindungen
// @date   2026-10-31 02:06
func (m AppModel) fmPickConnections() []Connection {
	all := append([]Connection{m.fmLocalConnection()}, m.connections...)
	if m.fm.pickSide == 0 {
		return all
	}
	leftID := m.fm.panels[0].conn.ID
	conns := make([]Connection, 0, len(all))
	for _, conn := range all {
		if conn.ID != leftID {
			conns = append(conns, conn)
		}
	}
	return conns
}

// fmLocalConnection liefert die Pseudo-Verbindung "Lokaler PC".
//
// @return Connection - nur ID und (übersetzter) Name sind gesetzt
// @date   2026-10-22 06:40
func (m AppModel) fmLocalConnection() Connection {
	return Connection{ID: fmLocalConnID, Name: m.tr(txtFMLocalPC)}
}

// fmPickRows liefert die Zahl der Zeilen der Auswahl: links die
// Verbindungen (inkl. Lokaler PC), rechts zusätzlich "(keine)" in Zeile 0.
//
// @param conns - Ergebnis von fmPickConnections
// @return int - Zeilen
// @date   2026-10-31 02:06
func (m AppModel) fmPickRows(conns []Connection) int {
	if m.fm.pickSide == 1 {
		return len(conns) + 1
	}
	return len(conns)
}

// fmPickRowConn liefert die Verbindung einer Zeile der Auswahl.
//
// @param conns - Ergebnis von fmPickConnections
// @param row - Zeile
// @return Connection - Verbindung (leer bei "(keine)" bzw. ungültiger Zeile)
// @return bool - true bei einer Verbindung, false bei "(keine)" bzw.
// ungültiger Zeile
// @date   2026-10-31 02:06
func (m AppModel) fmPickRowConn(conns []Connection, row int) (Connection, bool) {
	if m.fm.pickSide == 1 {
		row--
	}
	if row < 0 || row >= len(conns) {
		return Connection{}, false
	}
	return conns[row], true
}

// fmRememberKey liefert die Schreibweise einer Seite in
// FileManagerLastLeft/-Right: die Verbindungs-ID, rememberLocalPC für den
// Lokalen PC (intern eine ID mit NUL-Zeichen) bzw. rememberNone für
// "(keine)" (leere ID).
//
// @param connID - Verbindungs-ID der Seite ("" = keine)
// @return string - Wert für die Konfiguration
// @date   2026-10-31 02:06
func fmRememberKey(connID string) string {
	switch connID {
	case "":
		return rememberNone
	case fmLocalConnID:
		return rememberLocalPC
	}
	return connID
}

// fmRememberedPickRow liefert die Startzeile der aktuellen Seite: die
// gemerkte Wahl, wenn "Verbindung merken" eingeschaltet ist und der Wert
// zu einer wählbaren Zeile gehört, sonst Zeile 0. Die Werte stammen aus der
// Konfigurationsdatei und gelten als nicht vertrauenswürdig: Sie werden nur
// mit den wählbaren Einträgen verglichen und nie angezeigt. Ungültig (also
// Zeile 0) sind u. a. gelöschte Verbindungen, "none" links, rechts die links
// gewählte Seite und die interne ID des Lokalen PCs.
//
// @param conns - Ergebnis von fmPickConnections
// @return int - Zeile
// @date   2026-10-31 02:06
func (m AppModel) fmRememberedPickRow(conns []Connection) int {
	cfg, err := LoadConfig(m.configPath)
	if err != nil || !cfg.RememberSelection {
		return 0
	}
	value := cfg.FileManagerLastLeft
	if m.fm.pickSide == 1 {
		value = cfg.FileManagerLastRight
		if value == rememberNone {
			return 0
		}
	}
	if value == "" || value == fmLocalConnID || value == rememberNone {
		return 0
	}
	for i, conn := range conns {
		if fmRememberKey(conn.ID) != value {
			continue
		}
		if m.fm.pickSide == 1 {
			return i + 1
		}
		return i
	}
	return 0
}

// fmPickStart setzt Cursor und Scroll-Fenster der aktuellen Seite auf die
// Startzeile (fmRememberedPickRow), damit ein gemerkter Eintrag weit unten
// sofort sichtbar ist.
//
// @date   2026-10-31 02:06
func (m *AppModel) fmPickStart() {
	conns := m.fmPickConnections()
	m.fm.pickCursor = m.fmRememberedPickRow(conns)
	m.fm.pickOffset = scrollOffset(m.fmPickRows(conns), m.fm.pickCursor, 0, visibleRowLimit(m.termHeight))
}

// fmSavePickChoice speichert die abgeschlossene Wahl (beide Seiten), wenn
// "Verbindung merken" eingeschaltet ist. Synchron im Update und über
// updateConfig: Der Schalter wird unter der Sperre am aktuellen Stand
// geprüft, und eine gleichzeitige Schlüsselvergabe (KeyPath) wird nicht mit
// einem alten Stand überschrieben. Ist der Schalter aus oder sind die
// Werte schon gespeichert, bleibt die Datei unberührt.
//
// @date   2026-10-31 02:06
func (m AppModel) fmSavePickChoice() {
	left := fmRememberKey(m.fm.panels[0].conn.ID)
	right := fmRememberKey(m.fm.panels[1].conn.ID)
	saved := false
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		if !cfg.RememberSelection {
			return false
		}
		if cfg.FileManagerLastLeft == left && cfg.FileManagerLastRight == right {
			return false
		}
		cfg.FileManagerLastLeft = left
		cfg.FileManagerLastRight = right
		saved = true
		return true
	})
	if err == nil && saved {
		m.configCache.Invalidate()
	}
}

// handleFMPickKeys verarbeitet Tasten in der Verbindungsauswahl.
// Es gibt keinen Platzhalter mehr (Build 87): Links startet der Cursor auf
// "Lokaler PC" bzw. der gemerkten Seite, rechts auf "(keine)" bzw. der
// gemerkten Seite. Enter links wechselt immer zur rechten Seite - auch mit
// nur einer Verbindung. Enter rechts schließt die Wahl ab: speichern (bei
// eingeschaltetem Merken), dann verbinden; "(keine)" ergibt den
// Ein-Fenster-Modus. q/Esc brechen ab, ohne etwas zu speichern.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando (Verbindungsaufbau nach der rechten Wahl)
// @date   2026-10-31 02:06
func (m AppModel) handleFMPickKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	conns := m.fmPickConnections()
	rows := m.fmPickRows(conns)
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
		conn, isConn := m.fmPickRowConn(conns, m.fm.pickCursor)
		if m.fm.pickSide == 0 {
			// Ungültige Zeile: nichts tun
			if !isConn {
				return m, nil
			}
			// Eigene Verbindung ohne Tunnel (Kopie - die Liste bleibt unverändert)
			conn.Tunnels = nil
			m.fm.panels[0] = filePanel{conn: conn}
			// Rechte Seite: gemerkte Seite bzw. "(keine)"
			m.fm.pickSide = 1
			m.fmPickStart()
			return m, nil
		}
		if isConn {
			conn.Tunnels = nil
			m.fm.panels[1] = filePanel{conn: conn}
		} else {
			if m.fm.pickCursor != 0 {
				// Ungültige Zeile (nicht "(keine)"): nichts tun
				return m, nil
			}
			// "(keine)": Ein-Fenster-Modus, nur links wird verbunden
			m.fm.panels[1] = filePanel{}
			m.fm.single = true
		}
		// Wahl abgeschlossen: vor dem Verbindungsaufbau merken - ein
		// Verbindungsfehler lässt die Wahl gespeichert
		m.fmSavePickChoice()
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
// liest danach gleich das Startverzeichnis: das zuletzt geöffnete
// Verzeichnis dieser Verbindung, sonst das Home des Users.
//
// @param side - Seite (0 links, 1 rechts)
// @param password - Passwort oder leer für Auto-Auth
// @return tea.Model - Modell in ViewFMConnecting
// @return tea.Cmd - Hintergrundkommando
// @date   2026-10-23 19:37
func (m AppModel) startFMConnect(side int, password string) (tea.Model, tea.Cmd) {
	m.state = ViewFMConnecting
	m.fm.connectSide = side
	connect := m.fmConnect
	conn := m.fm.panels[side].conn
	if conn.ID == fmLocalConnID {
		// Lokaler PC: keine Anmeldung, kein Netzwerk
		local := m.fmLocalConnect
		connect = func(Connection, string) (remoteFS, error) { return local() }
	}
	remembered := m.fmRememberedPath(conn.ID)
	session := m.fm.session
	return m, func() tea.Msg {
		result := fmConnectedMsg{session: session, side: side, withPassword: password != ""}
		fsys, err := connect(conn, password)
		if err != nil {
			result.err = err
			return result
		}
		result.fs = fsys
		// Zuletzt geöffnetes Verzeichnis dieser Verbindung; gibt es das
		// nicht mehr (oder ist es nicht lesbar), geht es im Home los
		if validRememberedPath(remembered) {
			if entries, err := readPanelDir(fsys, remembered); err == nil {
				result.home = remembered
				result.entries = entries
				return result
			}
		}
		result.home = "/"
		if home, err := fsys.Getwd(); err == nil && home != "" {
			result.home = home
		}
		result.entries, result.listErr = readPanelDir(fsys, result.home)
		return result
	}
}

// handleFMConnected übernimmt das Ergebnis eines Verbindungsaufbaus und
// merkt sich das Startverzeichnis.
// Auth-Fehler führen zur Passwortabfrage; Netzwerk-, Host-Key- und
// SFTP-Fehler beenden den Datei-Manager mit Meldung in der Liste.
// Fehlertexte können Text vom Server enthalten und werden deshalb von
// Steuerzeichen bereinigt. Nach einer erfolgreichen Anmeldung startet bei
// Bedarf die Schlüsselvergabe im Hintergrund (fmNeedsKeyDeploy).
//
// @param msg - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando (rechte Seite verbinden, Cursor-Blinken,
// Schlüsselvergabe)
// @date   2026-10-28 13:16
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
		// Nach closeFileManager steht in m.errorMsg höchstens eine
		// gescheiterte Schlüsselvergabe; der Verbindungsfehler kommt davor
		// (joinFMNotice), damit keine der beiden Meldungen verloren geht.
		var sftpErr *fmSFTPError
		switch {
		case conn.ID == fmLocalConnID:
			// Lokaler PC: es gibt kein Passwort, das helfen könnte
			m = m.closeFileManager()
			m.errorMsg = joinFMNotice(fmt.Sprintf(m.tr(txtFMErrConnect), conn.Name, sanitizeFileName(msg.err.Error())), m.errorMsg)
			return m, nil
		case IsHostKeyChangedError(msg.err):
			m = m.closeFileManager()
			m.errorMsg = joinFMNotice(fmt.Sprintf(m.tr(txtFMErrHostKey), sanitizeFileName(parseHostKeyChangedHostname(msg.err))), m.errorMsg)
			return m, nil
		case errors.As(msg.err, &sftpErr):
			m = m.closeFileManager()
			m.errorMsg = joinFMNotice(fmt.Sprintf(m.tr(txtFMErrSFTP), conn.Name, sanitizeFileName(sftpErr.err.Error())), m.errorMsg)
			return m, nil
		case IsNetworkError(msg.err):
			m = m.closeFileManager()
			m.errorMsg = joinFMNotice(fmt.Sprintf(m.tr(txtFMErrConnect), conn.Name, sanitizeFileName(msg.err.Error())), m.errorMsg)
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
	if msg.listErr == nil {
		m.rememberFMPath(msg.side)
	}
	// Noch kein eigener Schlüssel bzw. Passwort benutzt: ungefragt einen
	// vergeben, im Hintergrund (tui_filemanager_keydeploy.go)
	var deployCmd tea.Cmd
	if fmNeedsKeyDeploy(m.fmCurrentConnection(msg.side), msg.withPassword) {
		deployCmd = m.fmDeployKeyCmd(msg.side, msg.fs, msg.withPassword)
	}
	if msg.side == 0 && !m.fm.single {
		next, connectCmd := m.startFMConnect(1, "")
		return next, tea.Batch(deployCmd, connectCmd)
	}
	m.state = ViewFileManager
	m.fm.active = 0
	// Unterwegs eingetroffene Meldung der Schlüsselvergabe zeigen
	m.fm.message = joinFMNotice(m.fm.message, m.fm.keyNotice)
	m.fm.keyNotice, m.fm.keyNoticeOK, m.fm.keyNoticeErr = "", "", ""
	return m, deployCmd
}

// fmLastPathKey liefert den Schlüssel einer Verbindung für die gemerkten
// Pfade. Der Lokale PC hat intern eine ID mit NUL-Zeichen; in der
// Konfiguration steht er lesbar als rememberLocalPC ("local-pc", dieselbe
// Schreibweise wie bei der gemerkten Wahl).
//
// @param connID - Verbindungs-ID
// @return string - Schlüssel in AppConfig.FileManagerLastPaths
// @date   2026-10-31 02:06
func fmLastPathKey(connID string) string {
	if connID == fmLocalConnID {
		return rememberLocalPC
	}
	return connID
}

// validRememberedPath prüft einen gemerkten Pfad aus der Konfiguration:
// absolut, bereinigt und ohne Steuerzeichen. Alles andere wird ignoriert
// (dann startet das Fenster im Home).
//
// @param p - gemerkter Pfad
// @return bool - true wenn er versucht werden darf
// @date   2026-10-23 19:37
func validRememberedPath(p string) bool {
	if !strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return false
	}
	return strings.IndexFunc(p, unicode.IsControl) < 0
}

// fmRememberedPath liefert das zuletzt geöffnete Verzeichnis einer
// Verbindung ("" wenn keins gemerkt ist).
//
// @param connID - Verbindungs-ID
// @return string - gemerkter Pfad
// @date   2026-10-23 19:37
func (m AppModel) fmRememberedPath(connID string) string {
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		return ""
	}
	return cfg.FileManagerLastPaths[fmLastPathKey(connID)]
}

// rememberFMPath merkt sich das aktuelle Verzeichnis eines Fensters für
// dessen Verbindung (nur wenn es sich geändert hat). Einträge gelöschter
// Verbindungen werden dabei entfernt.
//
// @param side - Seite (0 links, 1 rechts)
// @date   2026-10-30 16:13
func (m AppModel) rememberFMPath(side int) {
	panel := m.fm.panels[side]
	if panel.path == "" || panel.conn.ID == "" {
		return
	}
	// updateConfig (unter configUpdateMu) statt LoadConfig + SaveConfig:
	// Die Schlüsselvergabe schreibt im Hintergrund gleichzeitig (KeyPath,
	// AuthKey bzw. Fehlerzähler über updateKeyFields, ebenfalls unter
	// configUpdateMu). Ohne Sperre konnte dieses Speichern deren eben
	// geschriebene Felder mit dem alten Stand überschreiben - dann läge der
	// Schlüssel auf dem Server, die Konfiguration kennte ihn nicht.
	key := fmLastPathKey(panel.conn.ID)
	saved := false
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		if cfg.FileManagerLastPaths[key] == panel.path {
			return false
		}
		known := map[string]bool{fmLastPathKey(fmLocalConnID): true}
		for _, conn := range cfg.Connections {
			known[conn.ID] = true
		}
		paths := map[string]string{}
		for k, v := range cfg.FileManagerLastPaths {
			if known[k] {
				paths[k] = v
			}
		}
		paths[key] = panel.path
		cfg.FileManagerLastPaths = paths
		saved = true
		return true
	})
	if err == nil && saved {
		m.configCache.Invalidate()
	}
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

// handleFileManagerKeys verarbeitet Tasten in der Zwei-Fenster-Ansicht
// (bzw. im einzigen Fenster: dort wirken Tab, F5 und F6 nicht).
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando (Verzeichnis lesen)
// @date   2026-10-26 21:04
func (m AppModel) handleFileManagerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Betrachter bzw. offener Dialog bekommt alle Tasten (F10 schließt
	// dann nur den Betrachter bzw. ruht im Dialog)
	if m.fm.editing {
		return m.handleFMEditorKeys(msg)
	}
	if m.fm.viewing {
		return m.handleFMViewerKeys(msg)
	}
	if m.fm.menuOpen {
		return m.handleFMMenuKeys(msg)
	}
	if m.fm.dialog != fmDialogNone {
		return m.handleFMDialogKeys(msg)
	}
	// Meldungen gelten nur bis zum nächsten Tastendruck
	m.fm.message = ""
	panel := &m.fm.panels[m.fm.active]
	rows := m.fmVisibleRows()

	// Einfg: unter Windows als einzelne NUL-Rune (VK_INSERT ist in Bubbletea
	// nicht gemappt); die Alt-Taste (Alt+Tab) sieht genauso aus und darf
	// nicht markieren, Shift+Einfg wäre Einfügen
	if isInsertKey(msg) {
		panel.toggleMark()
		panel.offset = scrollOffset(len(panel.entries), panel.cursor, panel.offset, rows)
		return m, nil
	}

	switch msg.String() {
	case "q", "f10":
		return m.closeFileManager(), nil
	case "tab":
		if !m.fm.single {
			m.fm.active = 1 - m.fm.active
		}
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
	case "*":
		// Markierung aller Einträge umkehren (wie im mc)
		panel.invertMarks()
	case "f3":
		return m.fmOpenViewer()
	case "f4":
		return m.fmOpenEditor()
	case "f5", "f6":
		if m.fm.single {
			// Kopieren/Verschieben braucht ein zweites Fenster als Ziel
			m.fm.message = m.tr(txtFMSingleNoCopy)
			return m, nil
		}
		return m.fmOpenCopyConfirm(msg.String() == "f6")
	case "f7":
		return m.fmOpenMkdir()
	case "f8":
		return m.fmOpenDeleteConfirm()
	case "f9":
		return m.fmOpenMenu()
	case "ctrl+r":
		return m, m.fmRescan(m.fm.active)
	case "f1", "f2":
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

// handleFMDirLoaded übernimmt ein gelesenes Verzeichnis und merkt es sich
// für die Verbindung. Bei einem Fehler (z. B. fehlende Rechte) bleibt das
// Fenster im bisherigen Verzeichnis.
//
// @param msg - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-23 19:37
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
	m.rememberFMPath(msg.side)
	if msg.focus != "" {
		if entry, ok := panel.current(); !ok || entry.name != msg.focus {
			// Eintrag gibt es nicht mehr (gelöscht/verschoben): Position halten
			panel.setCursor(msg.fallback)
		}
	}
	for i := range panel.entries {
		if msg.marks[panel.entries[i].name] && !panel.entries[i].parent {
			panel.entries[i].marked = true
		}
	}
	panel.offset = scrollOffset(len(panel.entries), panel.cursor, panel.offset, m.fmVisibleRows())
	return m, nil
}

// --- Darstellung ---

// renderFMPick rendert die Verbindungsauswahl (linke bzw. rechte Seite).
// Rechts steht "(keine)" in Zeile 0. Angezeigt werden nur Namen, Benutzer
// und Host aus der Liste - nie ein gemerkter Wert aus der Konfiguration.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-31 02:06
func (m AppModel) renderFMPick(s *strings.Builder) {
	title := m.tr(txtFMPickLeft)
	if m.fm.pickSide == 1 {
		title = m.tr(txtFMPickRight)
	}
	s.WriteString(titleStyle.Render("  " + title))
	s.WriteString("\n\n")

	conns := m.fmPickConnections()
	rows := m.fmPickRows(conns)
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
		line := "  " + m.tr(txtFMNone)
		if conn, ok := m.fmPickRowConn(conns, i); ok {
			line = fmt.Sprintf("  %s  (%s@%s)", conn.Name, conn.User, conn.Host)
			if conn.ID == fmLocalConnID {
				line = "  " + conn.Name
			}
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

// renderFileManager rendert die Zwei-Fenster-Ansicht (bzw. das einzige
// Fenster über die ganze Breite) bildschirmfüllend: genau so viele Zeilen
// und Spalten wie das Terminal (sonst scrollt es).
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-26 21:04
func (m AppModel) renderFileManager(s *strings.Builder) {
	if m.fm.editing {
		m.renderFMEditor(s)
		return
	}
	if m.fm.viewing {
		m.renderFMViewer(s)
		return
	}
	width, height := m.fmScreenSize()
	st := fmThemeStyles(m.fm.theme)

	lines := make([]string, 0, height)
	lines = append(lines, m.renderFMMenuBarLine(st, width))

	panelHeight := height - fmScreenChromeLines
	if panelHeight < 0 {
		panelHeight = 0
	}
	if m.fm.single {
		lines = append(lines, m.renderFMPanel(0, st, width, panelHeight)...)
	} else {
		leftWidth := width / 2
		left := m.renderFMPanel(0, st, leftWidth, panelHeight)
		right := m.renderFMPanel(1, st, width-leftWidth, panelHeight)
		for i := 0; i < panelHeight; i++ {
			lines = append(lines, left[i]+right[i])
		}
	}
	lines = append(lines, m.renderFMKeyBar(st, width))

	// Offener Dialog mittig über den Fenstern
	if m.fm.dialog != fmDialogNone {
		box, boxWidth := m.renderFMDialogBox(st, width, height)
		overlayDialog(lines, box, boxWidth, width)
	}
	// Aufgeklapptes Menü unter seinem Titel
	if m.fm.menuOpen && len(lines) > 1 {
		box, boxWidth, left := m.renderFMMenuBox(st, width, height)
		overlayAt(lines, box, boxWidth, 1, left, width)
	}

	// Sehr kleine Fenster: auf die Terminalhöhe kürzen
	if len(lines) > height {
		lines = lines[:height]
	}
	s.WriteString(strings.Join(lines, "\n"))
}

// renderFMKeyBar rendert die F-Tasten-Leiste: zehn gleich breite Felder
// mit Nummer und Beschriftung (das letzte Feld nimmt den Rest). Mit nur
// einer Verbindung bleiben F5 und F6 ohne Beschriftung (ohne Funktion).
//
// @param st - Stile des Farbmodus
// @param width - Bildschirmbreite
// @return string - Zeile
// @date   2026-10-26 21:04
func (m AppModel) renderFMKeyBar(st fmStyles, width int) string {
	labels := strings.Split(m.tr(txtFMFKeys), "|")
	if m.fm.single && len(labels) > 5 {
		labels[4], labels[5] = "", ""
	}
	return renderKeyBar(st, width, labels)
}

// renderKeyBar rendert eine F-Tasten-Leiste wie im mc (Datei-Manager,
// Betrachter, Editor): gleich breite Felder mit Nummer und Beschriftung,
// das letzte Feld nimmt den Rest. Leere Beschriftung = Taste ohne Funktion.
//
// @param st - Stile des Farbmodus
// @param width - Bildschirmbreite
// @param labels - Beschriftungen F1, F2, …
// @return string - Zeile
// @date   2026-10-21 21:35
func renderKeyBar(st fmStyles, width int, labels []string) string {
	if len(labels) == 0 {
		return st.keyLabel.Render(fitWidth("", width))
	}
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
// @date   2026-10-21 20:10
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
	if count, size := panel.markedSummary(); active && m.fm.message != "" {
		info = st.message.Render(fitWidth(m.fm.message, inner))
	} else if count > 0 {
		// Markierte Einträge: Anzahl und Größe (wie im mc)
		info = st.mark.Render(fitWidth(fmt.Sprintf(m.tr(txtFMMarkedInfo), count, formatSizeWithUnit(size)), inner))
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
// @date   2026-10-21 21:03
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
		date = formatFMTime(entry.modTime)
	}
	sep := st.frame
	if entry.marked {
		// Spaltentrenner auf dem Markierungshintergrund (durchgehendes Band)
		style, sep = st.mark, st.markFrame
	}
	if selected {
		style, sep = st.cursor, st.cursor
		if entry.marked {
			style = st.cursorMark
		}
	}
	return m.renderFMColumns(cols, style, sep, marker+sanitizeFileName(entry.name), size, date)
}

// formatFMTime formatiert eine Änderungszeit in lokaler Zeit.
//
// @param t - Zeitpunkt
// @return string - z. B. "2026-10-21 19:45"
// @date   2026-10-21 21:03
func formatFMTime(t time.Time) string {
	return t.Local().Format(fmDateFormat)
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
