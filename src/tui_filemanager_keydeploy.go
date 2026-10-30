// Paket main - Datei-Manager: Schlüssel nach der Anmeldung vergeben
//
// Nutzerwunsch 2026-10-27: Eine Verbindung, die noch keinen eigenen
// Schlüssel hat, bekommt nach JEDER erfolgreichen Anmeldung ungefragt einen
// - nach einem Passwort genauso wie nach Auto-Auth (Agent oder ein anderer
// Schlüssel aus ~/.ssh). Sonst bliebe sie auf "Passwort" stehen. Gilt im
// Datei-Manager mit einer und mit zwei Verbindungen.
//
// Ablauf:
//  1. handleFMConnected: Anmeldung erfolgreich, nicht der Lokale PC, und
//     Passwort benutzt ODER connectionNeedsOwnKey -> fmDeployKeyCmd.
//  2. fmDeployKeyCmd läuft im Hintergrund (der Datei-Manager bleibt sofort
//     bedienbar). Unter fmDeployMu liest es den AKTUELLEN Eintrag aus der
//     Konfiguration, prüft die Regel erneut und ruft fmDeployKey auf.
//  3. handleFMKeyDeployed zeigt dieselben Texte wie der Hauptablauf
//     (KeyDeployedMsg bzw. KeyDeployFailed) und lädt die Konfiguration neu
//     - nach Erfolg UND nach Fehler, denn AutoDeployKey zählt auch
//     Fehlversuche in der Konfiguration mit. Nur bei Erfolg wird der
//     Auth-Cache der Verbindung geleert. Ein Fehler beendet nichts: Die
//     Verbindung steht. Schließt der Datei-Manager vor der Anzeige,
//     erscheint die Meldung in der Verbindungsliste.
//
// Warum der frische Eintrag: AutoDeployKey schreibt zwar nur seine eigenen
// Felder (updateKeyFields in ssh.go), entscheidet aber nach dem übergebenen
// Stand. Die Kopie im Fenster stammt aus der Zeit der Auswahl (ohne Tunnel,
// ohne spätere Änderungen). Die Regel (fmNeedsKeyDeploy) wird deshalb unter
// fmDeployMu am aktuellen Eintrag erneut geprüft: Hat die Verbindung
// inzwischen einen Schlüssel - etwa durch die Vergabe im anderen Fenster -
// oder wurde sie gelöscht, unterbleibt die Vergabe.
//
// Das Passwort wird hier NICHT gebraucht und nicht weitergereicht: Die
// Vergabe nutzt die schon angemeldete SSH-Verbindung.
//
// @author Kurt Ingwer
// @date   2026-10-30 16:13
package main

import (
	"errors"
	"fmt"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"
)

// errFMKeyDeploySkipped: Das Dateisystem hat keine SSH-Verbindung (z. B.
// Testfälschungen) - es gibt nichts zu vergeben, und es kommt keine Meldung.
//
// @date   2026-10-28 13:16
var errFMKeyDeploySkipped = errors.New("keine SSH-Verbindung für die Schlüsselvergabe")

// fmSSHConnHolder ist ein Dateisystem, das seine SSH-Verbindung herausgibt
// (sftpFS). Nur über diese Verbindung kann ein Schlüssel vergeben werden.
//
// @date   2026-10-28 13:16
type fmSSHConnHolder interface {
	sshConnection() *ssh.Client
}

// fmDeployKeyFunc vergibt nach einer erfolgreichen Anmeldung einen Schlüssel.
//
// @param conn - aktueller Eintrag aus der Konfiguration
// @param fsys - verbundenes Dateisystem des Fensters
// @param configPath - Pfad zur Konfiguration
// @return string - Schlüsselpfad (~/.ssh/...)
// @return error - Fehler; errFMKeyDeploySkipped = still übergehen
// @date   2026-10-28 13:16
type fmDeployKeyFunc func(conn Connection, fsys remoteFS, configPath string) (string, error)

// fmDeployKey ist die Naht für Tests: Standard ist fmDeployKeyOverSSH
// (AutoDeployKey über die SSH-Verbindung des Fensters). Tests ersetzen die
// Funktion und brauchen so keinen echten sshd. Nicht in parallelen Tests
// austauschen.
//
// @date   2026-10-28 13:16
var fmDeployKey fmDeployKeyFunc = fmDeployKeyOverSSH

// fmDeployMu lässt immer nur eine Vergabe des Datei-Managers gleichzeitig
// laufen. Grund: Melden sich beide Fenster per Passwort an - etwa an einer
// Verbindung und ihrer Kopie (Taste c) mit gleichem Host und Benutzer -,
// wählt AutoDeployKey den Dateinamen nach dem Stand der Konfiguration.
// Gleichzeitig gestartet sähen beide denselben Stand, wählten denselben
// Namen und schrieben in dieselbe Schlüsseldatei. Nacheinander sieht die
// zweite Vergabe den Schlüssel der ersten und nimmt einen eigenen Namen.
//
// @date   2026-10-28 13:16
var fmDeployMu sync.Mutex

// fmKeyDeployedMsg meldet das Ergebnis einer Schlüsselvergabe.
//
// @date   2026-10-28 13:16
type fmKeyDeployedMsg struct {
	session int
	side    int
	connID  string
	keyPath string
	err     error
}

// fmDeployKeyOverSSH vergibt den Schlüssel über die SSH-Verbindung des
// Fensters mit AutoDeployKey (wie der Hauptablauf).
//
// @param conn - Verbindung
// @param fsys - verbundenes Dateisystem
// @param configPath - Pfad zur Konfiguration
// @return string - Schlüsselpfad (~/.ssh/...)
// @return error - Fehler; errFMKeyDeploySkipped ohne SSH-Verbindung
// @date   2026-10-27 19:31
func fmDeployKeyOverSSH(conn Connection, fsys remoteFS, configPath string) (string, error) {
	holder, ok := fsys.(fmSSHConnHolder)
	if !ok {
		return "", errFMKeyDeploySkipped
	}
	client := holder.sshConnection()
	if client == nil {
		return "", errFMKeyDeploySkipped
	}
	return AutoDeployKey(conn, client, configPath)
}

// fmNeedsKeyDeploy entscheidet, ob nach einer erfolgreichen Anmeldung ein
// Schlüssel vergeben wird: nie für den Lokalen PC; sonst nach einer
// Anmeldung mit Passwort (wie im Hauptablauf) oder wenn die Verbindung noch
// keinen eigenen Schlüssel hat (connectionNeedsOwnKey), auch nach Auto-Auth.
//
// @param conn - aktuelle Verbindungsdaten (aus der Konfiguration)
// @param withPassword - Anmeldung mit Passwort?
// @return bool - true = Schlüssel vergeben
// @date   2026-10-27 19:31
func fmNeedsKeyDeploy(conn Connection, withPassword bool) bool {
	if conn.ID == fmLocalConnID {
		return false
	}
	return withPassword || connectionNeedsOwnKey(conn)
}

// fmCurrentConnection liefert die Verbindungsdaten zu einer ID aus
// m.connections (Stand der geladenen Konfiguration). Fehlt sie dort, gilt
// die Kopie aus dem Fenster.
//
// @param side - Seite (0 links, 1 rechts)
// @return Connection - Verbindungsdaten
// @date   2026-10-27 19:31
func (m AppModel) fmCurrentConnection(side int) Connection {
	panelConn := m.fm.panels[side].conn
	for _, conn := range m.connections {
		if conn.ID == panelConn.ID {
			return conn
		}
	}
	return panelConn
}

// fmFreshConnection liest den aktuellen Eintrag einer Verbindung aus der
// Konfigurationsdatei (mit Tunneln und allen Feldern).
//
// @param configPath - Pfad zur Konfiguration
// @param id - Verbindungs-ID
// @return Connection - Eintrag
// @return bool - false bei unlesbarer Datei oder gelöschter Verbindung
// @date   2026-10-27 19:31
func fmFreshConnection(configPath, id string) (Connection, bool) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return Connection{}, false
	}
	for _, conn := range cfg.Connections {
		if conn.ID == id {
			return conn, true
		}
	}
	return Connection{}, false
}

// fmDeployKeyCmd startet die Vergabe im Hintergrund. Unter fmDeployMu wird
// der aktuelle Eintrag gelesen und die Regel erneut geprüft - so vergibt
// eine zweite Anmeldung derselben Verbindung (z. B. beide Fenster) keinen
// zweiten Schlüssel, wenn die erste schon einen eingetragen hat (außer nach
// einem Passwort, wie im Hauptablauf). Ist der Eintrag nicht lesbar oder
// gelöscht, unterbleibt die Vergabe still (errFMKeyDeploySkipped).
//
// @param side - Seite (0 links, 1 rechts)
// @param fsys - verbundenes Dateisystem
// @param withPassword - Anmeldung mit Passwort?
// @return tea.Cmd - Hintergrundkommando, liefert fmKeyDeployedMsg
// @date   2026-10-27 19:31
func (m AppModel) fmDeployKeyCmd(side int, fsys remoteFS, withPassword bool) tea.Cmd {
	connID := m.fm.panels[side].conn.ID
	configPath := m.configPath
	session := m.fm.session
	deploy := fmDeployKey
	return func() tea.Msg {
		result := fmKeyDeployedMsg{session: session, side: side, connID: connID}
		fmDeployMu.Lock()
		defer fmDeployMu.Unlock()
		conn, ok := fmFreshConnection(configPath, connID)
		if !ok || !fmNeedsKeyDeploy(conn, withPassword) {
			result.err = errFMKeyDeploySkipped
			return result
		}
		result.keyPath, result.err = deploy(conn, fsys, configPath)
		return result
	}
}

// handleFMKeyDeployed übernimmt das Ergebnis einer Schlüsselvergabe.
//
// Immer (auch bei veralteter Sitzung bzw. nach dem Schließen) wird die
// Konfiguration neu geladen:
//   - nach Erfolg wegen des neuen KeyPath und AuthKey,
//   - nach einem Fehler wegen des Fehlerzählers, den AutoDeployKey in der
//     Konfiguration führt. Ohne Neuladen bliebe m.connections beim alten
//     Stand, und ein späteres Bearbeiten (Taste e) schriebe ihn zurück.
//
// Nur bei Erfolg wird zusätzlich der Auth-Cache dieser Verbindung geleert,
// damit die nächste Anmeldung den neuen Schlüssel probiert statt des
// gemerkten Wegs (wie im Hauptablauf). Ein übergangenes Ergebnis
// (errFMKeyDeploySkipped) meldet nichts und ändert nichts - auch kein
// Neuladen.
//
// Wo die Meldung erscheint (Texte wie im Hauptablauf, Fehlertext bereinigt):
//   - laufende Sitzung, Fensteransicht: an fm.message angehängt
//     (joinFMNotice), eine vorhandene Meldung bleibt vorn stehen;
//   - laufende Sitzung, Verbindungsaufbau/Passwortabfrage: wartet in
//     fm.keyNotice (siehe handleFMConnected und closeFileManager);
//   - veraltete Sitzung (Datei-Manager geschlossen oder neu geöffnet): in
//     der Liste, Erfolg in successMsg, Fehler in errorMsg.
//
// @param msg - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-30 16:13
func (m AppModel) handleFMKeyDeployed(msg fmKeyDeployedMsg) (tea.Model, tea.Cmd) {
	if errors.Is(msg.err, errFMKeyDeploySkipped) {
		return m, nil
	}
	failed := msg.err != nil
	// Konfiguration neu laden - bei Erfolg (neuer Schlüssel) wie bei Fehler
	// (Fehlerzähler der Verbindung)
	m.configCache.Invalidate()
	m.reloadConfig()
	var notice string
	if failed {
		notice = m.lang.KeyDeployFailed + sanitizeFileName(msg.err.Error())
	} else {
		notice = fmt.Sprintf(m.lang.KeyDeployedMsg, sanitizeFileName(msg.keyPath))
		// Die nächste Anmeldung soll den neuen Schlüssel probieren
		if m.sshManager != nil {
			m.sshManager.ClearAuthCache(msg.connID)
		}
	}
	switch {
	case msg.session != m.fm.session:
		// Datei-Manager inzwischen geschlossen bzw. neu geöffnet: in der Liste
		if failed {
			m.errorMsg = joinFMNotice(m.errorMsg, notice)
		} else {
			m.successMsg = joinFMNotice(m.successMsg, notice)
		}
	case m.state == ViewFileManager:
		m.fm.message = joinFMNotice(m.fm.message, notice)
	default:
		m.fm.addKeyNotice(notice, failed)
	}
	return m, nil
}

// addKeyNotice merkt eine Meldung der Schlüsselvergabe, bis die
// Fensteransicht erscheint (keyNotice) oder der Datei-Manager vorher
// schließt (keyNoticeOK bzw. keyNoticeErr).
//
// @param notice - Meldungstext
// @param failed - true = Fehlermeldung
// @date   2026-10-28 13:16
func (f *fileManager) addKeyNotice(notice string, failed bool) {
	f.keyNotice = joinFMNotice(f.keyNotice, notice)
	if failed {
		f.keyNoticeErr = joinFMNotice(f.keyNoticeErr, notice)
	} else {
		f.keyNoticeOK = joinFMNotice(f.keyNoticeOK, notice)
	}
}

// joinFMNotice hängt eine Meldung an (zwei Leerzeichen als Trenner).
//
// @param first - bisherige Meldung (darf leer sein)
// @param second - neue Meldung (darf leer sein)
// @return string - beide Meldungen
// @date   2026-10-27 19:31
func joinFMNotice(first, second string) string {
	switch {
	case first == "":
		return second
	case second == "":
		return first
	}
	return first + "  " + second
}
