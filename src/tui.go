// Paket main - TUI-Core für ssh-easy
//
// Zentrale Steuerung der Terminal-UI mit Bubbletea.
// Definiert das Datenmodell, Styles und den Message-Dispatcher.
// Die einzelnen Views sind in tui_list.go, tui_form.go, tui_status.go
// und tui_keygen.go ausgelagert.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00
package main

import (
	"fmt"
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
// wasPassword=true bedeutet: Key wurde per Passwort authentifiziert ->
// automatisch einen SSH-Key generieren und deployen.
type sshConnectedMsg struct {
	id          string
	status      *ConnectionStatus
	wasPassword bool       // War es Passwort-Auth? Dann Key automatisch deployen
	conn        Connection // Verbindungsdaten für Key-Deployment
}

// sshNeedPasswordMsg wird gesendet wenn Auto-Auth (Agent+Keys) fehlgeschlagen ist.
// Die TUI wechselt dann zur Passwort-Eingabe.
type sshNeedPasswordMsg struct {
	id string
}

// sshKeyDeployedMsg wird gesendet wenn der automatische Key-Deployment abgeschlossen ist.
type sshKeyDeployedMsg struct {
	connID  string
	keyPath string
	err     error
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
	id             string
	err            error
	returnToConnect bool // Fehler im Passwort-Modus -> in ViewConnect bleiben
}

// keygenResultMsg wird gesendet wenn ein SSH-Key generiert wurde
type keygenResultMsg struct {
	pubKey string
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
}

// NewAppModel erstellt ein neues TUI-Modell mit geladener Konfiguration.
//
// @param configPath - Pfad zur Konfigurationsdatei
// @param buildNumber - Aktuelle Build-Nummer
// @param sshManager - SSH-Verbindungsmanager
// @return AppModel - Initialisiertes Modell
// @date   2026-03-07 21:00
func NewAppModel(configPath string, buildNumber string, sshManager *SSHManager) AppModel {
	cache := NewConfigCache(configPath)

	m := AppModel{
		sshManager:  sshManager,
		configCache: cache,
		configPath:  configPath,
		buildNumber: buildNumber,
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
		m.connections = cfg.Connections
		m.language = cfg.Language
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

// createFormInputs erstellt die Eingabefelder für das Verbindungsformular.
// Auth-Typ und Key-Pfad entfernt – Authentifizierung erfolgt automatisch.
//
// @return []textinput.Model - Liste der Eingabefelder
// @date   2026-03-08 00:00
func createFormInputs() []textinput.Model {
	inputs := make([]textinput.Model, fieldCount)

	inputs[fieldName] = textinput.New()
	inputs[fieldName].CharLimit = 50

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
//
// @return tea.Cmd - Initiales Kommando
// @date   2026-03-07 21:00
func (m AppModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update verarbeitet eingehende Nachrichten und aktualisiert das Modell.
//
// @param msg - Eingehende Nachricht
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-07 21:00
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		// Terminalgroeße merken für SSH-PTY-Sessions
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		return m, nil

	case terminalDoneMsg:
		// Interaktive Terminal-Session beendet, zurück zur Statusansicht
		if msg.err != nil {
			m.errorMsg = m.lang.ErrTerminal + msg.err.Error()
		} else {
			m.successMsg = m.lang.TerminalDone
		}
		m.state = ViewStatus
		return m, nil

	case sshConnectedMsg:
		m.successMsg = m.lang.ConnectedMsg
		m.errorMsg = ""
		m.state = ViewList
		// Bei Passwort-Auth: automatisch SSH-Key generieren und deployen
		if msg.wasPassword {
			connCopy := msg.conn
			status := msg.status
			configPath := m.configPath
			return m, func() tea.Msg {
				keyPath, err := AutoDeployKey(connCopy, status.SSHClient, configPath)
				return sshKeyDeployedMsg{connID: msg.id, keyPath: keyPath, err: err}
			}
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
		// Key-Deployment abgeschlossen
		if msg.err != nil {
			m.errorMsg = m.lang.KeyDeployFailed + msg.err.Error()
		} else {
			m.successMsg = fmt.Sprintf(m.lang.KeyDeployedMsg, msg.keyPath)
			// Konfiguration neu laden (Key-Pfad wurde gespeichert)
			m.configCache.Invalidate()
			m.reloadConfig()
		}
		return m, nil

	case sshErrorMsg:
		m.errorMsg = m.lang.ConnErrPrefix + msg.err.Error()
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

	return m.updateInputs(msg)
}

// handleKeyPress verarbeitet Tastendrücke und dispatcht an die View-Handler.
//
// @param msg - Tastendruck-Nachricht
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-07 21:00
func (m AppModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Globale Taste: Ctrl+C beendet immer
	if msg.String() == "ctrl+c" {
		m.sshManager.DisconnectAll()
		return m, tea.Quit
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
	}

	return s.String()
}

// reloadConfig lädt die Konfiguration über den Cache neu.
//
// @date   2026-03-07 21:00
func (m *AppModel) reloadConfig() {
	cfg, err := m.configCache.Get()
	if err != nil {
		m.errorMsg = "Fehler beim Laden: " + err.Error()
		return
	}
	m.connections = cfg.Connections
}
