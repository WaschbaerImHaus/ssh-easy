// Paket main - TUI-Core fuer ssh-easy
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
	// ViewDelete - Loeschbestaetigung
	ViewDelete
	// ViewConnecting - Verbindungsaufbau laeuft (Auto-Auth)
	ViewConnecting
	// ViewConnect - Passwort-Eingabe (Auto-Auth fehlgeschlagen)
	ViewConnect
	// ViewHostKeyChanged - Host-Key hat sich geaendert: Nutzer befragt
	ViewHostKeyChanged
	// ViewStatus - Statusanzeige einer aktiven Verbindung
	ViewStatus
	// ViewKeygen - Formular zur SSH-Key-Generierung
	ViewKeygen
	// ViewKeygenResult - Anzeige des generierten Public Keys
	ViewKeygenResult
)

// Eingabefeld-Indizes fuer das Verbindungsformular
const (
	fieldName     = 0
	fieldHost     = 1
	fieldPort     = 2
	fieldUser     = 3
	fieldAuthType = 4
	fieldKeyPath  = 5
	fieldTunnels  = 6
	fieldCount    = 7
)

// Eingabefeld-Indizes fuer das Keygen-Formular
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
	conn        Connection // Verbindungsdaten fuer Key-Deployment
}

// sshNeedPasswordMsg wird gesendet wenn Auto-Auth (Agent+Keys) fehlschlug.
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

// sshHostKeyChangedMsg wird gesendet wenn sich der Host-Key geaendert hat.
// Zeigt einen Dialog mit der Frage ob der alte Key entfernt werden soll.
type sshHostKeyChangedMsg struct {
	connID      string
	hostname    string
	wasPassword bool   // War gerade Passwort-Auth im Gange?
	password    string // Gespeichertes Passwort fuer Retry
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
	// SSH-Manager fuer Verbindungsverwaltung
	sshManager *SSHManager
	// Config-Cache fuer Lazy-Loading
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
	// ID der aktiven Verbindung (fuer Bearbeiten/Loeschen/Status)
	activeID string
	// Passwort-Eingabefeld
	passwordInput textinput.Model
	// Eingabefelder fuer Key-Generierung
	keygenInputs []textinput.Model
	// Index des fokussierten Keygen-Felds
	keygenFocused int
	// Generierter Public Key (fuer Ergebnisanzeige)
	generatedPubKey string
	// Hostname bei dem sich der Host-Key geaendert hat (fuer Dialog)
	hostKeyHostname string
	// Passwort das beim Host-Key-Dialog-Retry verwendet werden soll
	hostKeyPassword string
	// Ob beim Host-Key-Dialog Passwort-Auth verwendet werden soll
	hostKeyWasPassword bool
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
		state:       ViewList,
		sshManager:  sshManager,
		configCache: cache,
		configPath:  configPath,
		buildNumber: buildNumber,
	}

	// Konfiguration ueber Cache laden
	cfg, err := cache.Get()
	if err != nil {
		m.errorMsg = "Fehler beim Laden: " + err.Error()
		m.connections = []Connection{}
	} else {
		m.connections = cfg.Connections
	}

	m.inputs = createFormInputs()
	m.passwordInput = createPasswordInput()

	return m
}

// createFormInputs erstellt die Eingabefelder fuer das Verbindungsformular.
//
// @return []textinput.Model - Liste der Eingabefelder
// @date   2026-03-07 21:00
func createFormInputs() []textinput.Model {
	inputs := make([]textinput.Model, fieldCount)

	inputs[fieldName] = textinput.New()
	inputs[fieldName].Placeholder = "Mein Server"
	inputs[fieldName].CharLimit = 50

	inputs[fieldHost] = textinput.New()
	inputs[fieldHost].Placeholder = "192.168.1.100"
	inputs[fieldHost].CharLimit = 255

	inputs[fieldPort] = textinput.New()
	inputs[fieldPort].Placeholder = "22"
	inputs[fieldPort].CharLimit = 5

	inputs[fieldUser] = textinput.New()
	inputs[fieldUser].Placeholder = "root"
	inputs[fieldUser].CharLimit = 50

	inputs[fieldAuthType] = textinput.New()
	inputs[fieldAuthType].Placeholder = "password, key oder agent"
	inputs[fieldAuthType].CharLimit = 10

	inputs[fieldKeyPath] = textinput.New()
	inputs[fieldKeyPath].Placeholder = "~/.ssh/id_ed25519"
	inputs[fieldKeyPath].CharLimit = 255

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
	pw.Placeholder = "Passwort/Passphrase eingeben"
	pw.EchoMode = textinput.EchoPassword
	pw.EchoCharacter = '*'
	pw.CharLimit = 255
	return pw
}

// createKeygenInputs erstellt die Eingabefelder fuer die Key-Generierung.
//
// @return []textinput.Model - Liste der Keygen-Eingabefelder
// @date   2026-03-07 21:00
func createKeygenInputs() []textinput.Model {
	inputs := make([]textinput.Model, keygenFieldCount)

	inputs[keygenFieldPath] = textinput.New()
	inputs[keygenFieldPath].Placeholder = "~/.ssh/id_ed25519_myserver"
	inputs[keygenFieldPath].CharLimit = 255

	inputs[keygenFieldPassphrase] = textinput.New()
	inputs[keygenFieldPassphrase].Placeholder = "leer = ohne Passphrase"
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

	case sshConnectedMsg:
		m.successMsg = "Verbindung hergestellt!"
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
		m.passwordInput.Focus()
		m.errorMsg = ""
		m.successMsg = ""
		return m, textinput.Blink

	case sshHostKeyChangedMsg:
		// Host-Key hat sich geaendert - Dialog anzeigen
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
			m.errorMsg = "Key-Deployment fehlgeschlagen: " + msg.err.Error()
		} else {
			m.successMsg = "SSH-Key deployed! Naechste Verbindung ohne Passwort: " + msg.keyPath
			// Konfiguration neu laden (Key-Pfad wurde gespeichert)
			m.configCache.Invalidate()
			m.reloadConfig()
		}
		return m, nil

	case sshErrorMsg:
		m.errorMsg = "Verbindungsfehler: " + msg.err.Error()
		// Bei Passwort-Fehler: in ViewConnect bleiben statt zur Liste zurueck
		if msg.returnToConnect {
			m.state = ViewConnect
			m.passwordInput = createPasswordInput()
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

// handleKeyPress verarbeitet Tastendruecke und dispatcht an die View-Handler.
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
	case ViewList:
		return m.handleListKeys(msg)
	case ViewCreate, ViewEdit:
		return m.handleFormKeys(msg)
	case ViewDelete:
		return m.handleDeleteKeys(msg)
	case ViewConnecting:
		// Waehrend Auto-Connect laeuft: keine Tastenverarbeitung (nur Ctrl+C oben)
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
	case ViewList:
		m.renderList(&s)
	case ViewCreate:
		m.renderForm(&s, "Neue Verbindung erstellen")
	case ViewEdit:
		m.renderForm(&s, "Verbindung bearbeiten")
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

// reloadConfig laedt die Konfiguration ueber den Cache neu.
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
