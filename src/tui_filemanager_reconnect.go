// Paket main - Datei-Manager (MC-Modus): Neu verbinden nach Verbindungsabriss
//
// Reißt die Verbindung eines Dateifensters ab (watchConnection erkennt das,
// fsConnectionLost meldet es), musste der Datei-Manager bis Build 89 ganz
// geschlossen und neu geöffnet werden. Seit Build 90 verbindet ein Fenster
// beim nächsten Einlesen neu - Enter in ein Verzeichnis, Strg+R bzw. Menü
// "Neu einlesen" und das Neuladen nach Kopieren, Löschen oder Anlegen:
//   - Ist die Verbindung schon vorher als verloren bekannt, wird zuerst neu
//     verbunden und dann der Pfad des Fensters gelesen (der Pfad bleibt).
//   - Scheitert das Lesen und ist die Verbindung danach verloren, wird
//     einmal neu verbunden und erneut gelesen.
//   - Höchstens EIN Verbindungsversuch pro Aktion; nichts wiederholt sich
//     von selbst. Laufende oder abgebrochene Kopiervorgänge werden nie
//     fortgesetzt - neu eingelesen wird erst nach ihrem Ende.
//   - Anmeldung wie beim Öffnen über m.fmConnect (SSHManager.ConnectStandalone:
//     known_hosts ist Pflicht, Auto-Auth über Agent und Schlüssel). Klappt
//     die Anmeldung nicht, fragt ein Dialog nach dem Passwort; es wird nur
//     für diesen einen Versuch benutzt und nirgends gespeichert.
//   - Ein für die alte Verbindung abgeschalteter find-Schnellweg bleibt auch
//     für die neue aus (inheritFastScanOff, filemanager_fastscan.go).
//   - Host-Key geändert oder Netzwerkfehler: nur eine Meldung; das nächste
//     Strg+R versucht es erneut.
//
// @author Kurt Ingwer
// @date   2026-11-16 11:37
package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// fmReadPanelCmd liest ein Verzeichnis für ein Fenster im Hintergrund und
// verbindet dabei bei Bedarf neu (siehe Dateikopf). Mit Passwort wird in
// jedem Fall neu verbunden (Antwort auf den Passwort-Dialog).
//
// @param side - Seite (0 links, 1 rechts)
// @param dir - zu lesendes Verzeichnis
// @param focus - Name, auf den der Cursor danach soll
// @param fallback - Cursorposition, falls focus fehlt
// @param marks - Namen, die danach wieder markiert werden
// @param password - Passwort für das Neu-Verbinden ("" = Auto-Auth)
// @return tea.Cmd - Kommando, das fmDirLoadedMsg liefert
// @date   2026-11-16 11:37
func (m AppModel) fmReadPanelCmd(side int, dir, focus string, fallback int, marks map[string]bool, password string) tea.Cmd {
	panel := m.fm.panels[side]
	fsys, conn, connect, session := panel.fs, panel.conn, m.fmConnect, m.fm.session
	return func() tea.Msg {
		msg := fmDirLoadedMsg{session: session, side: side, path: dir, focus: focus, fallback: fallback, marks: marks, withPassword: password != ""}
		// Genau ein Verbindungsversuch; der Lokale PC verliert keine Verbindung
		reconnect := func() bool {
			if connect == nil || conn.ID == fmLocalConnID {
				return false
			}
			newFS, err := connect(conn, password)
			if err != nil {
				msg.reconnectErr = err
				return false
			}
			// Abgeschalteten find-Schnellweg mitnehmen (sonst hinge das
			// nächste F5/F6 wieder bis zur Leerlaufgrenze)
			inheritFastScanOff(fsys, newFS)
			msg.newFS, fsys = newFS, newFS
			return true
		}
		tried := false
		if password != "" || fsConnectionLost(fsys) {
			tried = true
			if !reconnect() {
				return msg
			}
		}
		msg.entries, msg.err = readPanelDir(fsys, dir)
		// fsLostAfter statt fsConnectionLost: das Flag setzt die Überwachung
		// erst kurz nach dem Fehler (sonst verband erst das zweite Strg+R neu)
		if msg.err != nil && !tried && fsLostAfter(fsys, msg.err) {
			// Beim Lesen abgerissen: einmal neu verbinden und erneut lesen
			if reconnect() {
				msg.entries, msg.err = readPanelDir(fsys, dir)
			}
		}
		return msg
	}
}

// fmAdoptReconnect übernimmt nach erfolgreichem Neu-Verbinden die neue
// Verbindung ins Fenster und schließt die alte (tote).
//
// @param msg - Ergebnis mit newFS
// @date   2026-11-13 09:08
func (m *AppModel) fmAdoptReconnect(msg fmDirLoadedMsg) {
	panel := &m.fm.panels[msg.side]
	if panel.fs != nil && panel.fs != msg.newFS {
		panel.fs.Close()
	}
	panel.fs = msg.newFS
	m.fm.message = fmt.Sprintf(m.tr(txtFMReconnected), sanitizeFileName(panel.conn.Name))
}

// fmReconnectFailed meldet ein gescheitertes Neu-Verbinden. Scheiterte nur
// die Anmeldung, öffnet sich der Passwort-Dialog (wenn gerade kein anderer
// Dialog, Betrachter, Editor oder Menü offen ist - sonst nur die Meldung).
// Die alte Verbindung bleibt im Fenster; das nächste Einlesen versucht es
// erneut.
//
// @param msg - Ergebnis mit reconnectErr
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Cursor-Blinken des Passwortfelds oder nil
// @date   2026-11-13 09:08
func (m AppModel) fmReconnectFailed(msg fmDirLoadedMsg) (tea.Model, tea.Cmd) {
	err := msg.reconnectErr
	conn := m.fm.panels[msg.side].conn
	name := sanitizeFileName(conn.Name)
	failed := fmt.Sprintf(m.tr(txtFMReconnectFailed), name, sanitizeFileName(err.Error()))
	var sftpErr *fmSFTPError
	switch {
	case IsHostKeyChangedError(err):
		m.fm.message = fmt.Sprintf(m.tr(txtFMErrHostKey), sanitizeFileName(parseHostKeyChangedHostname(err)))
		return m, nil
	case errors.As(err, &sftpErr):
		m.fm.message = fmt.Sprintf(m.tr(txtFMErrSFTP), name, sanitizeFileName(sftpErr.err.Error()))
		return m, nil
	case IsNetworkError(err):
		m.fm.message = failed
		return m, nil
	}
	if m.fm.dialog != fmDialogNone || m.fm.editing || m.fm.viewing || m.fm.menuOpen {
		// Etwas anderes ist offen: nicht dazwischenfunken
		m.fm.message = failed
		return m, nil
	}
	m.fm.dialog = fmDialogReconnect
	m.fm.reconnectSide = msg.side
	m.fm.message = ""
	if msg.withPassword {
		m.fm.message = m.tr(txtFMWrongPassword)
	}
	m.fm.passwordInput = createPasswordInput()
	m.fm.passwordInput.Placeholder = m.lang.PlaceholderPW
	m.fm.passwordInput.Focus()
	return m, textinput.Blink
}

// handleFMReconnectKeys verarbeitet Tasten im Passwort-Dialog des
// Neu-Verbindens. "q" ist hier ein normales Zeichen (Teil des Passworts);
// Esc schließt den Dialog ohne Verbindungsversuch. Enter verbindet mit dem
// Passwort und leert das Feld sofort (das Passwort steht danach nur noch
// im laufenden Kommando).
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Neu-Verbinden oder Eingabe-Kommando
// @date   2026-11-13 09:08
func (m AppModel) handleFMReconnectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.fm.dialog = fmDialogNone
		m.fm.message = ""
		m.fm.passwordInput.SetValue("")
		return m, nil
	case tea.KeyEnter:
		password := m.fm.passwordInput.Value()
		side := m.fm.reconnectSide
		panel := &m.fm.panels[side]
		if password == "" || panel.loading {
			return m, nil
		}
		m.fm.passwordInput.SetValue("")
		m.fm.dialog = fmDialogNone
		m.fm.message = ""
		panel.loading = true
		focus, cursor := m.fmPanelFocus(side)
		return m, m.fmReadPanelCmd(side, panel.path, focus, cursor, m.fmRemainingMarks(side, nil), password)
	}
	m.fm.message = ""
	var cmd tea.Cmd
	m.fm.passwordInput, cmd = m.fm.passwordInput.Update(msg)
	return m, cmd
}

// fmReconnectDialogLines liefert Titel, Text und Tastenhilfe des
// Passwort-Dialogs.
//
// @param inner - Innenbreite des Dialogs
// @return string - Titel
// @return []string - Inhalt
// @return string - Tastenhilfe
// @date   2026-11-13 09:08
func (m AppModel) fmReconnectDialogLines(inner int) (string, []string, string) {
	conn := m.fm.panels[m.fm.reconnectSide].conn
	account := sanitizeFileName(conn.User + "@" + conn.Host)
	text := fmt.Sprintf(m.tr(txtFMReconnectPrompt), sanitizeFileName(conn.Name), account)
	input := m.fm.passwordInput
	input.Width = max(inner-1, 1)
	lines := append(wrapText(strings.TrimSpace(text), inner), input.View())
	return m.tr(txtFMReconnectTitle), lines, m.tr(txtFMReconnectHelp)
}
