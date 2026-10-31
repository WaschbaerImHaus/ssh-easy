// Paket main - TUI-Core für ssh-easy
//
// Zentrale Steuerung der Terminal-UI mit Bubbletea.
// Definiert das Datenmodell, Styles und den Message-Dispatcher.
// Die einzelnen Views sind in tui_list.go, tui_form.go, tui_status.go
// und tui_keygen.go ausgelagert.
//
// @author Kurt Ingwer
// @date   2026-10-31 02:06
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ViewState definiert die aktuelle Ansicht der TUI
type ViewState int

const (
	// ViewList - Hauptansicht: Verbindungsliste
	ViewList ViewState = iota
	// ViewCreate - Formular zum Anlegen einer neuen Verbindung
	ViewCreate
	// ViewEdit - Formular zum Bearbeiten einer Verbindung
	ViewEdit
	// ViewDelete - Löschbestätigung
	ViewDelete
	// ViewConnecting - Verbindungsaufbau läuft (Auto-Auth)
	ViewConnecting
	// ViewConnect - Passwort-Eingabe (Auto-Auth fehlgeschlagen)
	ViewConnect
	// ViewHostKeyChanged - Host-Key hat sich geändert: Nutzer befragt
	ViewHostKeyChanged
	// ViewStatus - Statusanzeige einer aktiven Verbindung
	ViewStatus
	// ViewKeygen - Formular zur SSH-Key-Generierung
	ViewKeygen
	// ViewKeygenResult - Anzeige des generierten Public Keys
	ViewKeygenResult
	// ViewLanguage - Sprachauswahl (beim ersten Start und über 'l' erreichbar)
	ViewLanguage
	// ViewExport - Export der Verbindungsliste (Taste 'E')
	ViewExport
	// ViewImportFile - Import Schritt 1: Datei wählen (Taste 'I')
	ViewImportFile
	// ViewImportSelect - Import Schritt 2: Verbindungen auswählen
	ViewImportSelect
	// ViewFMPick - Datei-Manager: linke/rechte Verbindung wählen
	ViewFMPick
	// ViewFMConnecting - Datei-Manager: Verbindungsaufbau läuft
	ViewFMConnecting
	// ViewFMPassword - Datei-Manager: Passwortabfrage
	ViewFMPassword
	// ViewFileManager - Datei-Manager: Zwei-Fenster-Ansicht
	ViewFileManager
)

// Eingabefeld-Indizes für das Verbindungsformular.
// Auth-Typ und Key-Pfad entfernt – Authentifizierung erfolgt automatisch.
const (
	fieldName    = 0
	fieldHost    = 1
	fieldPort    = 2
	fieldUser    = 3
	fieldTunnels = 4
	fieldCount   = 5
)

// Eingabefeld-Indizes für das Keygen-Formular
const (
	keygenFieldPath       = 0
	keygenFieldPassphrase = 1
	keygenFieldCount      = 2
)

// --- Bubbletea Messages ---

// sshConnectedMsg wird gesendet wenn eine SSH-Verbindung erfolgreich ist.
// Danach wird automatisch ein eigener SSH-Key erzeugt und eingespielt, wenn
// wasPassword=true ist (Login per Passwort) ODER die gespeicherte Verbindung
// noch keinen eigenen Schlüssel hat (connectionNeedsOwnKey) - auch dann, wenn
// der Auto-Login über den Agent oder einen Schlüssel aus ~/.ssh klappte.
// Die Verbindungsdaten holt Update aus m.connections (id); conn ist beim
// Auto-Login leer und wird dafür nicht mehr gebraucht.
//
// @date   2026-10-27 19:36
type sshConnectedMsg struct {
	id          string
	status      *ConnectionStatus
	wasPassword bool       // War es Passwort-Auth? Dann Key immer automatisch deployen
	conn        Connection // Verbindungsdaten des Absenders (beim Auto-Login leer)
}

// autoDeployKeyFunc ist die Naht für AutoDeployKey: Tests ersetzen sie durch
// einen Fake, damit kein echter Schlüssel erzeugt und kein Server gebraucht wird.
//
// @date   2026-10-28 13:17
var autoDeployKeyFunc = AutoDeployKey

// autoDeployKeyCmd startet die Schlüsselvergabe des Hauptablaufs im
// Hintergrund. Unter fmDeployMu (dieselbe Sperre wie im Datei-Manager, erste
// Stufe der Sperr-Reihenfolge fmDeployMu -> autoDeployMu -> ...) liest das
// Kommando den AKTUELLEN Eintrag aus der Konfiguration. Nach einem Auto-Login
// (wasPassword=false) prüft es connectionNeedsOwnKey dort erneut: m.connections
// ist nur der Stand beim Anmelden. Zwei Anmeldungen kurz hintereinander, eine
// parallele Vergabe im Datei-Manager oder ein inzwischen gezählter Fehlversuch
// führen so nicht zu einer zweiten Vergabe. Ist der Eintrag weg, die Datei
// unlesbar oder kein Bedarf mehr, kommt ein Ergebnis mit skipped=true.
// Nach einem Passwort-Login wird immer vergeben (wie bisher, auch bei
// AutoKeyOff); AutoDeployKey prüft die ID dort selbst.
// AutoDeployKey nutzt aus conn nur ID, Name, Host und User und schreibt nur
// die Schlüsselfelder des gespeicherten Eintrags; ein älterer Stand von conn
// schadet also nicht.
//
// @param conn - Verbindung aus m.connections
// @param status - Status der Anmeldung (SSH-Client)
// @param configPath - Pfad zur Konfiguration
// @param wasPassword - true nach Anmeldung per Passwort
// @return tea.Cmd - Kommando, liefert sshKeyDeployedMsg
// @date   2026-10-30 16:20
func autoDeployKeyCmd(conn Connection, status *ConnectionStatus, configPath string, wasPassword bool) tea.Cmd {
	return func() tea.Msg {
		result := sshKeyDeployedMsg{connID: conn.ID}
		fmDeployMu.Lock()
		defer fmDeployMu.Unlock()
		if !wasPassword {
			fresh, ok := fmFreshConnection(configPath, conn.ID)
			if !ok || !connectionNeedsOwnKey(fresh) {
				result.skipped = true
				return result
			}
		}
		result.keyPath, result.err = autoDeployKeyFunc(conn, status.SSHClient, configPath)
		return result
	}
}

// connectionNeedsOwnKey entscheidet, ob eine Verbindung nach erfolgreichem
// Auto-Login (Agent oder Schlüssel aus ~/.ssh) ungefragt einen eigenen
// Schlüssel bekommen soll: nur bei "password" ohne Schlüsselpfad, solange die
// automatische Vergabe nicht ausgesetzt ist (AutoKeyOff) und noch keine
// autoKeyMaxFailures Versuche gescheitert sind. Agent (bewusste Wahl),
// vorhandener KeyPath und ein leerer AuthType aus alten Konfigurationen ergeben
// false - im Zweifel lieber keinen Schlüssel erzeugen. Die Anmeldung per
// Passwort (wasPassword) fragt diese Regel nicht und vergibt immer; sie bleibt
// so der Weg, die Vergabe wieder einzuschalten. Wird auch vom Datei-Manager
// genutzt.
//
// @param conn - gespeicherte Verbindung
// @return bool - true, wenn ein eigener Schlüssel eingespielt werden soll
// @date   2026-10-28 13:24
func connectionNeedsOwnKey(conn Connection) bool {
	return conn.KeyPath == "" && conn.AuthType == AuthPassword &&
		!conn.AutoKeyOff && conn.AutoKeyFailures < autoKeyMaxFailures
}

// sshNeedPasswordMsg wird gesendet wenn Auto-Auth (Agent+Keys) fehlgeschlagen ist.
// Die TUI wechselt dann zur Passwort-Eingabe.
type sshNeedPasswordMsg struct {
	id string
}

// sshKeyDeployedMsg wird gesendet wenn der automatische Key-Deployment abgeschlossen ist.
// skipped=true: Die frische Prüfung im Kommando hat die Vergabe übergangen
// (Verbindung weg, Konfiguration unlesbar oder kein Bedarf mehr); dann gibt
// es weder Erfolgs- noch Fehlermeldung.
//
// @date   2026-10-30 16:20
type sshKeyDeployedMsg struct {
	connID  string
	keyPath string
	err     error
	skipped bool
}

// sshHostKeyChangedMsg wird gesendet wenn sich der Host-Key geändert hat.
// Zeigt einen Dialog mit der Frage ob der alte Key entfernt werden soll.
type sshHostKeyChangedMsg struct {
	connID      string
	hostname    string
	wasPassword bool   // War gerade Passwort-Auth im Gange?
	password    string // Gespeichertes Passwort für Retry
}

// sshErrorMsg wird gesendet bei SSH-Verbindungsfehlern
type sshErrorMsg struct {
	id              string
	err             error
	returnToConnect bool // Fehler im Passwort-Modus -> in ViewConnect bleiben
}

// keygenResultMsg wird gesendet wenn ein SSH-Key generiert wurde
type keygenResultMsg struct {
	pubKey string
	err    error
}

// sshKeyRemovedMsg wird gesendet wenn RemoveDeployedKey abgeschlossen ist.
type sshKeyRemovedMsg struct {
	connID string
	err    error
}

// terminalDoneMsg wird gesendet wenn die interaktive SSH-Terminal-Session endet.
// err ist nil bei normalem Ende (Nutzer schreibt "exit"), sonst Fehlerbeschreibung.
type terminalDoneMsg struct {
	err error
}

// --- Styles ---
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("57"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	connectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Bold(true)

	disconnectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Width(26)

	infoBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("57")).
			Padding(1, 2)
)

// AppModel ist das zentrale Datenmodell der TUI-Anwendung
type AppModel struct {
	// Aktuelle Ansicht
	state ViewState
	// Alle Verbindungen aus der Konfiguration
	connections []Connection
	// SSH-Manager für Verbindungsverwaltung
	sshManager *SSHManager
	// Config-Cache für Lazy-Loading
	configCache *ConfigCache
	// Cursor-Position in der Verbindungsliste
	cursor int
	// Eingabefelder fuer Formulare
	inputs []textinput.Model
	// Index des aktuell fokussierten Eingabefelds
	focusedInput int
	// Tunnel der Verbindung, mit der das Formular vorbelegt wurde (e und c).
	// Das Formular zeigt nur lokale Ports; Remote-Port und Enabled-Flag
	// kommen beim Speichern aus dieser Kopie (formTunnels). nil bei "n".
	formTunnelTemplate []TunnelConfig
	// Aktuelle Fehlermeldung
	errorMsg string
	// Aktuelle Erfolgsmeldung
	successMsg string
	// Pfad zur Konfigurationsdatei
	configPath string
	// Build-Nummer
	buildNumber string
	// ID der aktiven Verbindung (für Bearbeiten/Löschen/Status)
	activeID string
	// Passwort-Eingabefeld
	passwordInput textinput.Model
	// Eingabefelder fuer Key-Generierung
	keygenInputs []textinput.Model
	// Index des fokussierten Keygen-Felds
	keygenFocused int
	// Generierter Public Key (fuer Ergebnisanzeige)
	generatedPubKey string
	// Hostname bei dem sich der Host-Key geändert hat (für Dialog)
	hostKeyHostname string
	// Passwort das beim Host-Key-Dialog-Retry verwendet werden soll
	hostKeyPassword string
	// Ob beim Host-Key-Dialog Passwort-Auth verwendet werden soll
	hostKeyWasPassword bool
	// Aktuelle Terminalbreite in Zeichen (aus WindowSizeMsg)
	termWidth int
	// Aktuelle Terminalhöhe in Zeilen (aus WindowSizeMsg)
	termHeight int
	// Aktuell gewählte Sprache (ISO 639-1 Code)
	language Language
	// Alle UI-Texte der gewählten Sprache
	lang Translations
	// Cursor-Position in der Sprachauswahl-Liste
	langCursor int
	// Erste sichtbare Zeile der (scrollbaren) Verbindungsliste
	listOffset int
	// Aufgeklappte Gruppen der Verbindungsliste (Schlüssel = Kleinbuchstaben)
	expandedGroups map[string]bool
	// Auto-Update eingeschaltet? (Config auto_update, Standard: aus)
	autoUpdate bool
	// Updater für GitHub-Releases (nil = Auto-Update nicht verfügbar)
	updater *Updater
	// Läuft gerade eine Update-Suche?
	updateChecking bool
	// Ergebnis der letzten Update-Suche (Anzeige unter dem Schalter)
	updateStatus string
	// Laufende Nummer der letzten Fenstergrößen-Änderung (Entprellen)
	windowSaveSeq int
	// Hauptmenü (Tastenhilfe unten) eingeblendet? Taste m, Standard: aus
	showMenu bool
	// Schalter "Verbindung merken" (Taste v, Standard aus, Build 87): Nur wenn
	// an, wird die per Enter gewählte Verbindung gespeichert und beim Start
	// vorausgewählt. Spiegelt AppConfig.RememberSelection.
	rememberSelection bool
	// Schrift- und Fenstergröße beim Programmstart (Ziel von Taste r)
	consoleDefaults consoleDefaults
	// Statusansicht: offene Rückfrage "Schlüssel entfernen?" (Taste r, j/y)
	confirmKeyRemove bool
	// Export: Auswahlliste der Verbindungen
	exportList connChecklist
	// Export: Dateinamenfeld und ob es den Fokus hat
	exportPathInput textinput.Model
	exportFocusPath bool
	// Export/Import: Bezugsordner für relative Dateinamen (Home-Ordner)
	exportHomeDir string
	// Import: Dateinamenfeld (Schritt 1)
	importPathInput textinput.Model
	// Import: Auswahlliste der Verbindungen aus der Datei (Schritt 2)
	importList connChecklist
	// Import: Namen vorhandener Verbindungen (Kleinbuchstaben)
	importExisting map[string]bool
	// Import: Zahl übersprungener (ungültiger/doppelter) Einträge der Datei
	importSkipped int
	// Import: geladene Datei (Anzeige)
	importFile string
	// Datei-Manager (MC-Modus): Zustand
	fm fileManager
	// Datei-Manager: Verbindungsaufbau (in Tests austauschbar)
	fmConnect fmConnectFunc
	// Datei-Manager: "Lokaler PC" öffnen (in Tests austauschbar)
	fmLocalConnect func() (remoteFS, error)
	// Datei-Manager: erlaubte Endungen für F9 (nil = sichere Standardliste)
	fmOpenExtensions []string
}

// NewAppModel erstellt ein neues TUI-Modell mit geladener Konfiguration.
//
// @param configPath - Pfad zur Konfigurationsdatei
// @param buildNumber - Aktuelle Build-Nummer
// @param sshManager - SSH-Verbindungsmanager
// @return AppModel - Initialisiertes Modell
// @date   2026-10-31 02:06
func NewAppModel(configPath string, buildNumber string, sshManager *SSHManager) AppModel {
	cache := NewConfigCache(configPath)

	m := AppModel{
		sshManager:     sshManager,
		configCache:    cache,
		configPath:     configPath,
		buildNumber:    buildNumber,
		expandedGroups: make(map[string]bool),
		fmConnect:      newSFTPConnector(sshManager),
		fmLocalConnect: newLocalFS,
	}
	if home, err := os.UserHomeDir(); err == nil {
		m.exportHomeDir = home
	}

	// Konfiguration über Cache laden
	cfg, err := cache.Get()
	if err != nil {
		// Fallback auf Englisch bei Ladefehler
		m.lang = GetTranslations(LangEnglish)
		m.language = LangEnglish
		m.errorMsg = m.lang.ErrLoading + err.Error()
		m.connections = []Connection{}
	} else {
		m.connections = sortConnectionsByName(cfg.Connections)
		m.language = cfg.Language
		m.autoUpdate = cfg.AutoUpdate
		m.rememberSelection = cfg.RememberSelection
		m.applyFMSettings(cfg)
		// Gemerkte Verbindung vorauswählen (nur bei eingeschaltetem Schalter).
		// Eine unbekannte oder gelöschte ID ändert nichts: Cursor bleibt auf 0,
		// keine Meldung. Die ID wird nur verglichen, nie angezeigt.
		if m.rememberSelection && cfg.LastConnectionID != "" {
			m.selectConnectionByID(cfg.LastConnectionID)
		}
	}

	// Sprache initialisieren
	if m.language == "" {
		// Erster Start: Sprachauswahl zeigen, Cursor auf Englisch vorbelegen
		m.lang = GetTranslations(LangEnglish)
		m.state = ViewLanguage
		for i, opt := range AvailableLanguages {
			if opt.Code == LangEnglish {
				m.langCursor = i
				break
			}
		}
	} else {
		m.lang = GetTranslations(m.language)
		m.state = ViewList
		// Cursor auf aktuell gewählte Sprache vorbelegen
		for i, opt := range AvailableLanguages {
			if opt.Code == m.language {
				m.langCursor = i
				break
			}
		}
	}

	// Eingabefelder mit sprachspezifischen Platzhaltern erstellen
	m.inputs = createFormInputs()
	m.inputs[fieldName].Placeholder = m.lang.PlaceholderName
	m.passwordInput = createPasswordInput()
	m.passwordInput.Placeholder = m.lang.PlaceholderPW

	return m
}

// connectionNameLimit ist die höchste Länge eines Verbindungsnamens in Runen.
// Das Namensfeld des Formulars und nextCopyName (Taste c) nutzen denselben Wert.
//
// @date   2026-10-28 13:17
const connectionNameLimit = 50

// createFormInputs erstellt die Eingabefelder für das Verbindungsformular.
// Auth-Typ und Key-Pfad entfernt – Authentifizierung erfolgt automatisch.
// Das Namensfeld ist auf connectionNameLimit Zeichen begrenzt.
//
// @return []textinput.Model - Liste der Eingabefelder
// @date   2026-10-27 18:05
func createFormInputs() []textinput.Model {
	inputs := make([]textinput.Model, fieldCount)

	inputs[fieldName] = textinput.New()
	inputs[fieldName].CharLimit = connectionNameLimit

	inputs[fieldHost] = textinput.New()
	inputs[fieldHost].Placeholder = "192.168.1.100"
	inputs[fieldHost].CharLimit = 255

	inputs[fieldPort] = textinput.New()
	inputs[fieldPort].Placeholder = "22"
	inputs[fieldPort].CharLimit = 5
	inputs[fieldPort].SetValue("22") // Default-Wert direkt setzen, Placeholder wäre im fokussierten Feld unsichtbar

	inputs[fieldUser] = textinput.New()
	inputs[fieldUser].Placeholder = "root"
	inputs[fieldUser].CharLimit = 50

	inputs[fieldTunnels] = textinput.New()
	inputs[fieldTunnels].Placeholder = "3306,8080,5432"
	inputs[fieldTunnels].CharLimit = 255

	return inputs
}

// createPasswordInput erstellt das Passwort-Eingabefeld.
//
// @return textinput.Model - Passwort-Eingabefeld
// @date   2026-03-07 21:00
func createPasswordInput() textinput.Model {
	pw := textinput.New()
	pw.EchoMode = textinput.EchoPassword
	pw.EchoCharacter = '*'
	pw.CharLimit = 255
	return pw
}

// createKeygenInputs erstellt die Eingabefelder für die Key-Generierung.
//
// @return []textinput.Model - Liste der Keygen-Eingabefelder
// @date   2026-03-07 21:00
func createKeygenInputs() []textinput.Model {
	inputs := make([]textinput.Model, keygenFieldCount)

	inputs[keygenFieldPath] = textinput.New()
	inputs[keygenFieldPath].Placeholder = "~/.ssh/id_ed25519_myserver"
	inputs[keygenFieldPath].CharLimit = 255

	inputs[keygenFieldPassphrase] = textinput.New()
	inputs[keygenFieldPassphrase].EchoMode = textinput.EchoPassword
	inputs[keygenFieldPassphrase].EchoCharacter = '*'
	inputs[keygenFieldPassphrase].CharLimit = 255

	return inputs
}

// Init ist die Bubbletea-Initialisierungsfunktion.
// Ist Auto-Update eingeschaltet, startet hier die Update-Suche im Hintergrund.
//
// @return tea.Cmd - Initiales Kommando
// @date   2026-10-16 10:00
func (m AppModel) Init() tea.Cmd {
	if m.updateChecking {
		return tea.Batch(textinput.Blink, windowPollCmd(), m.updateCheckCmd())
	}
	return tea.Batch(textinput.Blink, windowPollCmd())
}

// Update verarbeitet eingehende Nachrichten und aktualisiert das Modell.
//
// @param msg - Eingehende Nachricht
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-10-28 13:17
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		// Terminalgroeße merken für SSH-PTY-Sessions
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		m.clampListCursor()
		// Fenstergröße verzögert speichern (beim Ziehen kommen viele Ereignisse)
		return m.scheduleWindowSave()

	case windowSaveMsg:
		return m.handleWindowSave(msg)

	case windowPollMsg:
		return m.handleWindowPoll()

	case updateResultMsg:
		return m.handleUpdateResult(msg)

	case fmConnectedMsg:
		return m.handleFMConnected(msg)

	case fmDirLoadedMsg:
		return m.handleFMDirLoaded(msg)

	case fmCopyEventMsg:
		return m.handleFMCopyEvent(msg)

	case fmViewLoadedMsg:
		return m.handleFMViewLoaded(msg)

	case fmOpenExternalMsg:
		return m.handleFMOpenExternal(msg)

	case fmEditLoadedMsg:
		return m.handleFMEditLoaded(msg)

	case fmEditSavedMsg:
		return m.handleFMEditSaved(msg)

	case fmDeleteEventMsg:
		return m.handleFMDeleteEvent(msg)

	case fmMkdirDoneMsg:
		return m.handleFMMkdirDone(msg)

	case fmKeyDeployedMsg:
		return m.handleFMKeyDeployed(msg)

	case terminalDoneMsg:
		// Alt+F4 in der SSH-Session: gesamtes Programm sauber beenden (wie PuTTY)
		if msg.err != nil && errors.Is(msg.err, errAltF4Quit) {
			m.sshManager.DisconnectAll()
			return m, tea.Quit
		}
		// Interaktive Terminal-Session beendet, zurück zur Statusansicht.
		// Während der Sitzung wurde ggf. Fenster-/Schriftgröße gespeichert.
		m.configCache.Invalidate()
		if msg.err != nil {
			m.errorMsg = m.lang.ErrTerminal + msg.err.Error()
		} else {
			m.successMsg = m.lang.TerminalDone
		}
		m.state = ViewStatus
		return m, nil

	case sshKeyRemovedMsg:
		// Auth-Cache leeren, Verbindung trennen, zurück zur Liste
		m.sshManager.ClearAuthCache(msg.connID)
		m.sshManager.Disconnect(msg.connID)
		m.configCache.Invalidate()
		m.reloadConfig()
		m.state = ViewList
		if msg.err != nil {
			m.errorMsg = m.lang.ErrPrefix + sanitizeFileName(msg.err.Error())
		} else {
			m.successMsg = m.lang.KeyRemovedMsg
		}
		return m, nil

	case sshConnectedMsg:
		m.successMsg = m.lang.ConnectedMsg
		m.errorMsg = ""
		// Direkt zur Statusansicht – Nutzer kann sofort 't' für Terminal drücken
		m.activeID = msg.id
		m.state = ViewStatus
		// Eigenen SSH-Key generieren und deployen: nach Passwort-Login immer,
		// nach Auto-Login (Agent/~/.ssh) nur, wenn die Verbindung noch auf
		// "password" ohne Schlüssel steht. Die Daten kommen aus der Liste,
		// weil msg.conn beim Auto-Login leer ist.
		var conn Connection
		found := false
		for _, c := range m.connections {
			if c.ID == msg.id {
				conn, found = c, true
				break
			}
		}
		// Unbekannte ID (z. B. inzwischen gelöscht) oder kein Status: kein Einsatz
		if !found || msg.status == nil {
			return m, nil
		}
		if msg.wasPassword || connectionNeedsOwnKey(conn) {
			return m, autoDeployKeyCmd(conn, msg.status, m.configPath, msg.wasPassword)
		}
		return m, nil

	case sshNeedPasswordMsg:
		// Auto-Auth fehlgeschlagen - Passwort abfragen
		m.state = ViewConnect
		m.passwordInput = createPasswordInput()
		m.passwordInput.Placeholder = m.lang.PlaceholderPW
		m.passwordInput.Focus()
		m.errorMsg = ""
		m.successMsg = ""
		return m, textinput.Blink

	case sshHostKeyChangedMsg:
		// Host-Key hat sich geändert - Dialog anzeigen
		m.state = ViewHostKeyChanged
		m.hostKeyHostname = msg.hostname
		m.hostKeyPassword = msg.password
		m.hostKeyWasPassword = msg.wasPassword
		m.activeID = msg.connID
		m.errorMsg = ""
		m.successMsg = ""
		return m, nil

	case sshKeyDeployedMsg:
		// Key-Deployment abgeschlossen. In JEDEM Fall die Liste neu laden:
		//   - Erfolg: neuer KeyPath und AuthKey,
		//   - Fehler: AutoDeployKey hat AutoKeyFailures hochgezählt. Ohne
		//     Neuladen bliebe m.connections bei 0; jeder Auto-Login vergäbe
		//     erneut, und "e" schriebe die 0 zurück (Review H1),
		//   - übergangen: jemand anderes hat den Eintrag geändert.
		// Neu laden VOR dem Setzen der Meldung, damit ein Ladefehler die
		// Meldung zur Schlüsselvergabe nicht überschreibt.
		m.configCache.Invalidate()
		m.reloadConfig()
		if msg.skipped {
			// Frische Prüfung hat die Vergabe übergangen: keine Meldung
			return m, nil
		}
		if msg.err != nil {
			// Auth-Cache bleibt stehen: Es gibt keinen neuen Schlüssel
			m.errorMsg = m.lang.KeyDeployFailed + sanitizeFileName(msg.err.Error())
		} else {
			m.successMsg = fmt.Sprintf(m.lang.KeyDeployedMsg, msg.keyPath)
			// Gemerkte Anmeldemethode NUR dieser Verbindung vergessen: Sonst
			// meldet sie sich weiter per Agent bzw. fremdem Schlüssel an statt
			// mit dem neuen eigenen. Bei einem Fehler bleibt der Cache stehen.
			if m.sshManager != nil {
				m.sshManager.ClearAuthCache(msg.connID)
			}
		}
		return m, nil

	case sshErrorMsg:
		// Fehlertext kann Text vom Server enthalten (Handshake) -> bereinigen
		m.errorMsg = m.lang.ConnErrPrefix + sanitizeFileName(msg.err.Error())
		// Unbekannter Host über öffentlichen Fallback-DNS: verständlich erklären
		var untrusted *untrustedHostError
		if errors.As(msg.err, &untrusted) {
			m.errorMsg = fmt.Sprintf(m.tr(txtErrUntrustedHost), untrusted.Hostname)
		}
		// Bei Passwort-Fehler: in ViewConnect bleiben statt zur Liste zurück
		if msg.returnToConnect {
			m.state = ViewConnect
			m.passwordInput = createPasswordInput()
			m.passwordInput.Placeholder = m.lang.PlaceholderPW
			m.passwordInput.Focus()
			return m, textinput.Blink
		}
		m.state = ViewList
		return m, nil

	case keygenResultMsg:
		if msg.err != nil {
			m.errorMsg = msg.err.Error()
			return m, nil
		}
		m.generatedPubKey = msg.pubKey
		m.state = ViewKeygenResult
		m.errorMsg = ""
		return m, nil
	}

	// Shift+Einfg aus Unix-Terminals (ESC[2;2~ als unbekannte CSI-Sequenz)
	if isShiftInsertSequence(msg) {
		return m.pasteIntoFocusedInput()
	}

	return m.updateInputs(msg)
}

// handleKeyPress verarbeitet Tastendrücke und dispatcht an die View-Handler.
//
// @param msg - Tastendruck-Nachricht
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-10-31 07:02
func (m AppModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Globale Tasten: Ctrl+C und Alt+F4 beenden immer.
	// Alt+F4 kommt nur in conhost hier an: Dort ist ENABLE_PROCESSED_INPUT
	// aus, die Konsole gibt Alt+F4 als Taste weiter, statt das Fenster zu
	// schließen. In Windows Terminal schließt Alt+F4 dagegen das Fenster und
	// löst CTRL_CLOSE_EVENT aus (in Go SIGTERM, wie das Schließkreuz) - das
	// behandelt main.go (watchCloseSignals, finishRun).
	if msg.String() == "ctrl+c" || msg.String() == "alt+f4" {
		m.sshManager.DisconnectAll()
		return m, tea.Quit
	}

	// Globale Tasten: Schriftgröße ändern (nur Windows wirksam).
	// Primär Strg+Pfeil-hoch/-runter: Bubbletea v1.x verwirft im Windows-
	// coninput-Pfad den Ctrl-Modifier bei Zeichentasten (Strg+Plus kommt als
	// Rune 0x00 an, "ctrl++" kann nie entstehen) - VK_UP/VK_DOWN mit Ctrl
	// sind dagegen explizit als KeyCtrlUp/KeyCtrlDown gemappt.
	// Die Plus/Minus-Aliase bleiben für Terminals die sie liefern können
	// (z.B. Bubbletea-Unix-Pfad mit entsprechendem Terminal-Emulator).
	switch msg.String() {
	case "ctrl+up", "ctrl++", "ctrl+=", "ctrl+plus":
		return m.changeFontSize(+FontSizeStep)
	case "ctrl+down", "ctrl+-", "ctrl+_", "ctrl+minus":
		return m.changeFontSize(-FontSizeStep)
	}

	// Dispatch an View-spezifische Handler
	switch m.state {
	case ViewLanguage:
		return m.handleLanguageKeys(msg)
	case ViewList:
		return m.handleListKeys(msg)
	case ViewCreate, ViewEdit:
		return m.handleFormKeys(msg)
	case ViewDelete:
		return m.handleDeleteKeys(msg)
	case ViewConnecting:
		// Während Auto-Connect läuft: keine Tastenverarbeitung (nur Ctrl+C oben)
		return m, nil
	case ViewHostKeyChanged:
		return m.handleHostKeyChangedKeys(msg)
	case ViewConnect:
		return m.handleConnectKeys(msg)
	case ViewStatus:
		return m.handleStatusKeys(msg)
	case ViewKeygen:
		return m.handleKeygenKeys(msg)
	case ViewKeygenResult:
		return m.handleKeygenResultKeys(msg)
	case ViewExport:
		return m.handleExportKeys(msg)
	case ViewImportFile:
		return m.handleImportFileKeys(msg)
	case ViewImportSelect:
		return m.handleImportSelectKeys(msg)
	case ViewFMPick:
		return m.handleFMPickKeys(msg)
	case ViewFMConnecting:
		return m.handleFMConnectingKeys(msg)
	case ViewFMPassword:
		return m.handleFMPasswordKeys(msg)
	case ViewFileManager:
		return m.handleFileManagerKeys(msg)
	}

	return m, nil
}

// updateInputs aktualisiert Eingabefelder basierend auf der aktuellen Ansicht.
//
// @param msg - Eingehende Nachricht
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-07 21:00
func (m AppModel) updateInputs(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch m.state {
	case ViewCreate, ViewEdit:
		m.inputs[m.focusedInput], cmd = m.inputs[m.focusedInput].Update(msg)
	case ViewConnect:
		m.passwordInput, cmd = m.passwordInput.Update(msg)
	case ViewKeygen:
		m.keygenInputs[m.keygenFocused], cmd = m.keygenInputs[m.keygenFocused].Update(msg)
	case ViewExport:
		m.exportPathInput, cmd = m.exportPathInput.Update(msg)
	case ViewImportFile:
		m.importPathInput, cmd = m.importPathInput.Update(msg)
	case ViewFMPassword:
		m.fm.passwordInput, cmd = m.fm.passwordInput.Update(msg)
	case ViewFileManager:
		// Eingabezeile des Betrachters bzw. Editors (Cursor-Blinken)
		if m.fm.editing && m.fm.edit.prompt != edPromptNone {
			m.fm.edit.input, cmd = m.fm.edit.input.Update(msg)
		} else if m.fm.viewing && m.fm.view.prompt != fmPromptNone {
			m.fm.view.input, cmd = m.fm.view.input.Update(msg)
		} else if m.fm.dialog == fmDialogMkdir {
			m.fm.mkdirInput, cmd = m.fm.mkdirInput.Update(msg)
		} else if m.fm.dialog == fmDialogExtensions && m.fm.extAdding {
			m.fm.extInput, cmd = m.fm.extInput.Update(msg)
		}
	}

	return m, cmd
}

// View rendert die aktuelle Ansicht als String.
//
// @return string - Gerenderte Ansicht
// @date   2026-03-07 21:00
func (m AppModel) View() string {
	var s strings.Builder

	switch m.state {
	case ViewLanguage:
		m.renderLanguage(&s)
	case ViewList:
		m.renderList(&s)
	case ViewCreate:
		m.renderForm(&s, m.lang.FormTitleNew)
	case ViewEdit:
		m.renderForm(&s, m.lang.FormTitleEdit)
	case ViewDelete:
		m.renderDeleteConfirm(&s)
	case ViewConnecting:
		m.renderConnecting(&s)
	case ViewHostKeyChanged:
		m.renderHostKeyChanged(&s)
	case ViewConnect:
		m.renderConnect(&s)
	case ViewStatus:
		m.renderStatus(&s)
	case ViewKeygen:
		m.renderKeygen(&s)
	case ViewKeygenResult:
		m.renderKeygenResult(&s)
	case ViewExport:
		m.renderExport(&s)
	case ViewImportFile:
		m.renderImportFile(&s)
	case ViewImportSelect:
		m.renderImportSelect(&s)
	case ViewFMPick:
		m.renderFMPick(&s)
	case ViewFMConnecting:
		m.renderFMConnecting(&s)
	case ViewFMPassword:
		m.renderFMPassword(&s)
	case ViewFileManager:
		m.renderFileManager(&s)
	}

	return s.String()
}

// reloadConfig lädt die Konfiguration über den Cache neu.
// Verbindungen werden alphabetisch nach Name sortiert.
//
// @date   2026-03-23 11:00
func (m *AppModel) reloadConfig() {
	cfg, err := m.configCache.Get()
	if err != nil {
		m.errorMsg = "Fehler beim Laden: " + err.Error()
		return
	}
	m.connections = sortConnectionsByName(cfg.Connections)
	m.clampListCursor()
}
