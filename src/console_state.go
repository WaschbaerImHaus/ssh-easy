// Paket main - Fenster- und Schriftgröße der Konsole: sofort speichern und
// zurücksetzen (Taste r in der Verbindungsliste)
//
// Bis Build 83 wurden Fenster- und Schriftgröße teils erst beim Beenden
// gespeichert. Wurde das Fenster während einer SSH-Sitzung per X
// geschlossen, beendet Windows den Prozess nach ca. 5 s - dann gingen die
// Änderungen verloren. Jetzt wird bei jeder Änderung gespeichert:
//   - in der Oberfläche über die periodische Abfrage (handleWindowPoll)
//   - während einer SSH-Sitzung über consoleStateSaver im Größen-Watcher
//
// Für "zurücksetzen" wird beim Programmstart festgehalten, wie groß Schrift
// und Fenster waren, BEVOR die gespeicherten Werte angewendet wurden - das
// ist der Systemstandard der Konsole (captureConsoleDefaults).
//
// Seit Build 87 löscht r außerdem immer die gemerkte Auswahl (zuletzt
// gewählte Verbindung, linke und rechte Seite des Datei-Managers) - auch
// dann, wenn sich Schrift und Fenster nicht steuern lassen.
//
// @author Kurt Ingwer
// @date   2026-10-31 02:05
package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"
)

// consoleSaveInterval - Abstand, in dem eine laufende SSH-Sitzung Fenster-
// und Schriftgröße prüft und bei Änderung speichert.
const consoleSaveInterval = 2 * time.Second

// queryFontSize liefert die aktuelle Schriftgröße (0 = nicht steuerbar).
// Funktionszeiger für Tests.
var queryFontSize = getConsoleFontSize

// applyFontSize setzt die Schriftgröße. Funktionszeiger für Tests.
var applyFontSize = setConsoleFontSize

// consoleDefaults hält Schrift- und Fenstergröße beim Programmstart fest.
// Ein Wert 0 bedeutet: unbekannt bzw. nicht steuerbar.
type consoleDefaults struct {
	// Schriftgröße in Pixeln
	fontSize int
	// Fensterbreite in Zeichen
	cols int
	// Fensterhöhe in Zeilen
	rows int
}

// captureConsoleDefaults liest den aktuellen Konsolenzustand als
// Systemstandard. Muss vor applyStartupFontSize/applyStartupWindowSize
// aufgerufen werden. Ein maximiertes oder Vollbild-Fenster liefert keine
// Fenstergröße (sonst würde "zurücksetzen" auf Bildschirmgröße stellen).
//
// @return consoleDefaults - Startzustand
// @date   2026-10-27 17:10
func captureConsoleDefaults() consoleDefaults {
	var d consoleDefaults
	if size := queryFontSize(); size > 0 {
		d.fontSize = size
	}
	if cols, rows, normal := queryWindowState(); normal && validWindowSize(cols, rows) {
		d.cols, d.rows = cols, rows
	}
	return d
}

// supported meldet, ob sich wenigstens Schrift oder Fenster zurücksetzen lässt.
//
// @return bool - true wenn ein Standardwert bekannt ist
// @date   2026-10-27 17:10
func (d consoleDefaults) supported() bool {
	return d.fontSize > 0 || validWindowSize(d.cols, d.rows)
}

// SetConsoleDefaults übergibt den beim Start festgehaltenen Konsolenzustand.
//
// @param d - Startzustand aus captureConsoleDefaults
// @date   2026-10-27 17:10
func (m *AppModel) SetConsoleDefaults(d consoleDefaults) {
	m.consoleDefaults = d
}

// persistConsoleState speichert die aktuelle Schrift- und Fenstergröße in
// einem Durchgang, soweit sie sich vom gespeicherten Stand unterscheiden.
// Entspricht ein Wert dem Systemstandard (Stand beim Start), wird 0
// gespeichert - "nichts anwenden". Sonst würde die Abfrage nach "r" sofort
// wieder feste Zahlen speichern, die spätere Änderungen am Konsolen-Standard
// (Einstellungen, Monitor, DPI) überstimmen.
//
// @param configPath - Pfad zur connections.json
// @param defaults - Systemstandard aus captureConsoleDefaults
// @return bool - true wenn etwas gespeichert wurde
// @date   2026-10-27 18:10
func persistConsoleState(configPath string, defaults consoleDefaults) bool {
	font := queryFontSize()
	cols, rows, normal := queryWindowState()
	saved := false
	err := updateConfig(configPath, func(cfg *AppConfig) bool {
		if font > 0 {
			want := font
			if font == defaults.fontSize {
				want = 0
			}
			if cfg.FontSize != want {
				cfg.FontSize = want
				saved = true
			}
		}
		if normal && validWindowSize(cols, rows) {
			wantCols, wantRows := cols, rows
			if cols == defaults.cols && rows == defaults.rows {
				wantCols, wantRows = 0, 0
			}
			if cfg.WindowCols != wantCols || cfg.WindowRows != wantRows {
				cfg.WindowCols, cfg.WindowRows = wantCols, wantRows
				saved = true
			}
		}
		return saved
	})
	return saved && err == nil
}

// consoleStateSaveFunc liefert die Speicherfunktion für die SSH-Sitzung.
// Sie läuft in der Goroutine des Größen-Watchers, während Bubbletea
// angehalten ist (tea.Exec). Andere Hintergrundbefehle (Schlüssel nach der
// Anmeldung) können gleichzeitig schreiben - daher updateConfig mit Sperre.
//
// @return func() - Speichert Schrift- und Fenstergröße
// @date   2026-10-27 17:10
func (m AppModel) consoleStateSaveFunc() func() {
	configPath, defaults := m.configPath, m.consoleDefaults
	return func() {
		persistConsoleState(configPath, defaults)
	}
}

// newTerminalCmd baut das interaktive SSH-Terminal mit der aktuellen Größe
// und der Speicherfunktion für Fenster- und Schriftgröße.
//
// @param client - verbundener SSH-Client
// @return *sshTerminalCmd - bereit für tea.Exec
// @date   2026-10-27 18:10
func (m AppModel) newTerminalCmd(client *ssh.Client) *sshTerminalCmd {
	cmd := newSSHTerminalCmd(client, m.termWidth, m.termHeight)
	cmd.saveConsoleState = m.consoleStateSaveFunc()
	return cmd
}

// consoleStateSaver ruft eine Speicherfunktion höchstens alle
// consoleSaveInterval auf (erster Aufruf sofort).
type consoleStateSaver struct {
	// Speicherfunktion (nil = nichts tun)
	save func()
	// Zeitpunkt des letzten Aufrufs
	last time.Time
}

// newConsoleStateSaver erstellt einen Speicher-Taktgeber.
//
// @param save - Speicherfunktion (nil erlaubt)
// @return *consoleStateSaver - Taktgeber
// @date   2026-10-27 17:10
func newConsoleStateSaver(save func()) *consoleStateSaver {
	return &consoleStateSaver{save: save}
}

// tick speichert, wenn seit dem letzten Speichern consoleSaveInterval
// vergangen ist.
//
// @param now - aktuelle Zeit
// @date   2026-10-27 17:10
func (s *consoleStateSaver) tick(now time.Time) {
	if s.save == nil {
		return
	}
	if !s.last.IsZero() && now.Sub(s.last) < consoleSaveInterval {
		return
	}
	s.last = now
	s.save()
}

// clearRememberedSelection löscht die drei gemerkten Auswahlwerte (Build 87):
// zuletzt gewählte Verbindung und die beiden Seiten des Datei-Managers.
// Der Schalter RememberSelection und die gemerkten Verzeichnisse
// (FileManagerLastPaths, Build 77) bleiben unberührt.
//
// @param cfg - zu ändernde Konfiguration
// @return bool - true wenn sich etwas geändert hat
// @date   2026-10-31 02:05
func clearRememberedSelection(cfg *AppConfig) bool {
	changed := cfg.LastConnectionID != "" || cfg.FileManagerLastLeft != "" || cfg.FileManagerLastRight != ""
	cfg.LastConnectionID, cfg.FileManagerLastLeft, cfg.FileManagerLastRight = "", "", ""
	return changed
}

// resetConsoleSize setzt zurück (Taste r in der Verbindungsliste):
//   - IMMER die gemerkte Auswahl (clearRememberedSelection) - auch ohne
//     steuerbare Konsole und bei ausgeschaltetem Schalter
//   - bei steuerbarer Konsole zusätzlich Schrift- und Fenstergröße auf den
//     Startzustand; die gespeicherten Werte werden 0 (Systemstandard)
//
// Ohne steuerbare Konsole (Linux, Windows Terminal) bleiben Schrift- und
// Fensterwerte in der Konfiguration stehen. Alles wird in EINEM
// updateConfig-Aufruf gespeichert. Cursor und Verbindungen im laufenden
// Programm bleiben unverändert - erst der nächste Start beginnt oben.
// Reihenfolge wie beim Start: erst Schrift, dann Fenster - die mögliche
// Fenstergröße hängt von der Schrift ab.
//
// @return tea.Model - Modell mit Meldung
// @return tea.Cmd - Immer nil
// @date   2026-10-31 02:05
func (m AppModel) resetConsoleSize() (tea.Model, tea.Cmd) {
	d := m.consoleDefaults
	controllable := d.supported()
	if controllable {
		if d.fontSize > 0 && queryFontSize() > 0 {
			applyFontSize(d.fontSize)
			fixHorizontalScroll()
		}
		if validWindowSize(d.cols, d.rows) {
			setWindowSize(d.cols, d.rows)
		}
	}

	// 0 = Systemstandard: beim nächsten Start wird nichts angewendet. Die
	// Abfrage alle 2 s speichert danach ebenfalls 0 (persistConsoleState).
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		changed := clearRememberedSelection(cfg)
		if controllable {
			cfg.FontSize, cfg.WindowCols, cfg.WindowRows = 0, 0, 0
			changed = true
		}
		return changed
	})
	if err != nil {
		m.errorMsg = err.Error()
		m.successMsg = ""
		return m, nil
	}
	m.configCache.Invalidate()
	m.errorMsg = ""
	if controllable {
		m.successMsg = m.tr(txtConsoleResetAll)
	} else {
		m.successMsg = m.tr(txtSelectionResetOnly)
	}
	return m, nil
}
