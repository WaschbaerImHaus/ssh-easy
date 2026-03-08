// Paket main - SSH-Verbindungsmanager für ssh-easy
//
// Zentraler Manager für SSH-Verbindungen mit Keepalive, Reconnect und
// SSH-Agent-Unterstützung. Implementiert automatische Key-Erkennung:
// Beim Verbinden werden SSH-Agent und alle verfügbaren Keys automatisch
// ausprobiert, ohne den Benutzer nach der Methode zu fragen.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sshagent "golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Keepalive-Konfiguration
const (
	// KeepaliveInterval - Intervall zwischen Keepalive-Paketen
	KeepaliveInterval = 30 * time.Second
	// KeepaliveTimeout - Timeout für Keepalive-Antwort
	KeepaliveTimeout = 10 * time.Second
	// ReconnectMaxRetries - Maximale Anzahl Reconnect-Versuche
	ReconnectMaxRetries = 5
	// ReconnectDelay - Wartezeit zwischen Reconnect-Versuchen
	ReconnectDelay = 3 * time.Second
)

// SSHManager verwaltet alle SSH-Verbindungen zentral.
// Bietet Keepalive-Überwachung, automatischen Reconnect und Logging.
type SSHManager struct {
	// Mutex für thread-sicheren Zugriff auf Verbindungen
	mu sync.RWMutex
	// Aktive Verbindungen (Key = Connection.ID)
	connections map[string]*ManagedConnection
	// Logger-Instanz
	logger *Logger
}

// ManagedConnection erweitert ConnectionStatus um Reconnect-Informationen.
type ManagedConnection struct {
	// Eingebetteter Verbindungsstatus
	Status *ConnectionStatus
	// Originale Verbindungskonfiguration (für Reconnect)
	Config Connection
	// Passwort/Passphrase (nur im Speicher, für Reconnect)
	password string
	// Anzahl bisheriger Reconnect-Versuche
	reconnectCount int
	// Ob Reconnect aktiv versucht wird
	reconnecting bool
}

// NewSSHManager erstellt einen neuen SSH-Manager.
//
// @param logger - Logger-Instanz für Protokollierung
// @return *SSHManager - Neuer Manager
// @date   2026-03-08 00:00
func NewSSHManager(logger *Logger) *SSHManager {
	return &SSHManager{
		connections: make(map[string]*ManagedConnection),
		logger:      logger,
	}
}

// ConnectAuto verbindet automatisch: probiert SSH-Agent und alle verfügbaren
// Keys aus ~/.ssh/ ohne den Benutzer zu fragen. Gibt einen Fehler zurück
// wenn keine Methode funktioniert (dann sollte Passwort versucht werden).
//
// @param conn - Die Verbindungskonfiguration
// @return *ConnectionStatus - Status der aufgebauten Verbindung
// @return error - Fehler beim Verbindungsaufbau
// @date   2026-03-08 00:00
func (m *SSHManager) ConnectAuto(conn Connection) (*ConnectionStatus, error) {
	m.logger.Info("Auto-Verbindung zu %s@%s:%d (Agent + Keys)...", conn.User, conn.Host, conn.Port)

	// Auth-Methoden sammeln: Agent + alle unverschlüsselten Keys
	methods := m.buildAutoAuthMethods(conn)
	if len(methods) == 0 {
		return nil, fmt.Errorf("keine SSH-Schlüssel oder Agent verfügbar")
	}

	ctx, status, err := m.dialSSH(conn, methods)
	if err != nil {
		m.logger.Info("Auto-Verbindung fehlgeschlagen für %s: %v", conn.Name, err)
		return nil, err
	}

	m.logger.Info("Auto-Verbindung zu %s hergestellt", conn.Name)
	m.registerConnection(conn, status, "", ctx)
	return status, nil
}

// ConnectWithPassword verbindet mit Passwort-Authentifizierung.
// Wird aufgerufen wenn die automatische Key-Authentifizierung fehlgeschlagen ist.
//
// @param conn - Die Verbindungskonfiguration
// @param password - Das eingegebene Passwort
// @return *ConnectionStatus - Status der aufgebauten Verbindung
// @return error - Fehler beim Verbindungsaufbau
// @date   2026-03-08 00:00
func (m *SSHManager) ConnectWithPassword(conn Connection, password string) (*ConnectionStatus, error) {
	m.logger.Info("Passwort-Verbindung zu %s@%s:%d ...", conn.User, conn.Host, conn.Port)

	var methods []ssh.AuthMethod

	// Passwort-Authentifizierung
	methods = append(methods, ssh.Password(password))

	// Keyboard-Interactive für Server die das statt Password verwenden
	pwCopy := password
	methods = append(methods, ssh.KeyboardInteractive(
		func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = pwCopy
			}
			return answers, nil
		},
	))

	ctx, status, err := m.dialSSH(conn, methods)
	if err != nil {
		m.logger.Error("Passwort-Verbindung zu %s fehlgeschlagen: %v", conn.Name, err)
		return nil, err
	}

	m.logger.Info("Passwort-Verbindung zu %s hergestellt", conn.Name)
	m.registerConnection(conn, status, password, ctx)
	return status, nil
}

// Connect baut eine SSH-Verbindung mit expliziter Auth-Methode auf (Legacy).
// Wird für gespeicherte Verbindungen mit bekanntem AuthType verwendet.
//
// @param conn - Die Verbindungskonfiguration
// @param password - Passwort oder Key-Passphrase
// @return *ConnectionStatus - Status der aufgebauten Verbindung
// @return error - Fehler beim Verbindungsaufbau
// @date   2026-03-08 00:00
func (m *SSHManager) Connect(conn Connection, password string) (*ConnectionStatus, error) {
	m.logger.Info("Verbinde zu %s@%s:%d ...", conn.User, conn.Host, conn.Port)

	authMethods, err := m.buildAuthMethods(conn, password)
	if err != nil {
		m.logger.Error("Auth fehlgeschlagen für %s: %v", conn.Name, err)
		return nil, fmt.Errorf("Authentifizierung fehlgeschlagen: %w", err)
	}

	ctx, status, err := m.dialSSH(conn, authMethods)
	if err != nil {
		m.logger.Error("SSH-Verbindung zu %s fehlgeschlagen: %v", conn.Name, err)
		return nil, fmt.Errorf("SSH-Verbindung zu %s fehlgeschlagen: %w", conn.Name, err)
	}

	m.logger.Info("SSH-Verbindung zu %s hergestellt", conn.Name)
	m.registerConnection(conn, status, password, ctx)
	return status, nil
}

// dialSSH stellt die eigentliche SSH-TCP-Verbindung her und startet Tunnel.
// Gibt den Kontext für Keepalive zurück (wird benötigt um Keepalive zu stoppen).
//
// @param conn - Verbindungskonfiguration
// @param methods - SSH-Authentifizierungsmethoden
// @return context.Context - Kontext (für Keepalive-Loop)
// @return *ConnectionStatus - Verbindungsstatus
// @return error - Fehler beim Verbinden
// @date   2026-03-08 00:00
func (m *SSHManager) dialSSH(conn Connection, methods []ssh.AuthMethod) (context.Context, *ConnectionStatus, error) {
	hostKeyCallback, err := m.getHostKeyCallback()
	if err != nil {
		return nil, nil, fmt.Errorf("HostKey-Prüfung fehlgeschlagen: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            conn.User,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", conn.Host, conn.Port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	status := &ConnectionStatus{
		Connected:    true,
		SSHClient:    client,
		Listeners:    make([]net.Listener, 0),
		TunnelErrors: make(map[int]string),
		Cancel:       cancel,
	}

	// Tunnel starten (nur aktivierte)
	for _, tunnel := range conn.Tunnels {
		if !tunnel.Enabled {
			continue
		}
		listener, err := startTunnel(ctx, client, tunnel)
		if err != nil {
			m.logger.Warn("Tunnel localhost:%d fehlgeschlagen: %v", tunnel.LocalPort, err)
			status.TunnelErrors[tunnel.LocalPort] = err.Error()
			continue
		}
		m.logger.Info("Tunnel localhost:%d -> remote:%d gestartet", tunnel.LocalPort, tunnel.RemotePort)
		status.Listeners = append(status.Listeners, listener)
	}

	return ctx, status, nil
}

// registerConnection registriert eine Verbindung im Manager und startet Keepalive.
//
// @param conn - Verbindungskonfiguration
// @param status - Verbindungsstatus
// @param password - Passwort (für Reconnect)
// @param ctx - Kontext für Keepalive-Loop
// @date   2026-03-08 00:00
func (m *SSHManager) registerConnection(conn Connection, status *ConnectionStatus, password string, ctx context.Context) {
	managed := &ManagedConnection{
		Status:   status,
		Config:   conn,
		password: password,
	}

	m.mu.Lock()
	m.connections[conn.ID] = managed
	m.mu.Unlock()

	go m.keepaliveLoop(ctx, conn.ID)
}

// buildAutoAuthMethods sammelt alle verfügbaren Auth-Methoden automatisch:
// SSH-Agent + alle unverschlüsselten Keys aus ~/.ssh/
// Den konfigurierten Key (conn.KeyPath) probiert er zuerst.
//
// @param conn - Verbindungskonfiguration (für bevorzugten Key-Pfad)
// @return []ssh.AuthMethod - Gesammelte Auth-Methoden
// @date   2026-03-08 00:00
func (m *SSHManager) buildAutoAuthMethods(conn Connection) []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	// 1. SSH-Agent versuchen (falls laufend)
	if agentAuth, err := m.getAgentAuth(); err == nil {
		methods = append(methods, agentAuth)
		m.logger.Info("SSH-Agent für Auto-Auth verfügbar")
	}

	// 2. Konfigurierten Key bevorzugt laden (falls angegeben)
	if conn.KeyPath != "" {
		if signers := m.loadKeysFromPaths([]string{conn.KeyPath}); len(signers) > 0 {
			methods = append(methods, ssh.PublicKeys(signers...))
		}
	}

	// 3. Alle anderen unverschlüsselten Keys aus ~/.ssh/ laden
	if signers := m.loadAllSSHKeys(conn.KeyPath); len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}

	return methods
}

// loadAllSSHKeys lädt alle SSH-Private-Keys aus ~/.ssh/ die keine Passphrase haben.
// excludePath wird übersprungen (bereits separat geladen).
//
// @param excludePath - Pfad der übersprungen werden soll (leer = nichts überspringen)
// @return []ssh.Signer - Geladene Signierer
// @date   2026-03-08 00:00
func (m *SSHManager) loadAllSSHKeys(excludePath string) []ssh.Signer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	sshDir := filepath.Join(home, ".ssh")

	entries, err := os.ReadDir(sshDir)
	if err != nil {
		return nil
	}

	// Diese Dateien sind keine privaten Schlüssel
	skipFiles := map[string]bool{
		"known_hosts":      true,
		"config":           true,
		"authorized_keys":  true,
		"known_hosts.old":  true,
		"environment":      true,
	}

	// Absoluten Ausschlusspfad auflösen
	absExclude := ""
	if excludePath != "" {
		absExclude = expandTilde(excludePath)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// .pub-Dateien und bekannte Nicht-Key-Dateien überspringen
		if strings.HasSuffix(name, ".pub") || skipFiles[name] {
			continue
		}
		absPath := filepath.Join(sshDir, name)
		// Bereits konfigurierten Schlüssel nicht doppelt laden
		if absPath == absExclude {
			continue
		}
		paths = append(paths, absPath)
	}

	return m.loadKeysFromPaths(paths)
}

// loadKeysFromPaths lädt SSH-Private-Keys aus den angegebenen Pfaden.
// Verschlüsselte Keys werden still übersprungen.
//
// @param paths - Pfade zu den Key-Dateien
// @return []ssh.Signer - Erfolgreich geladene Signierer
// @date   2026-03-08 00:00
func (m *SSHManager) loadKeysFromPaths(paths []string) []ssh.Signer {
	var signers []ssh.Signer
	for _, path := range paths {
		path = expandTilde(path)

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			// Verschlüsselter Key (passphrase-protected) - still überspringen
			m.logger.Info("SSH-Key %s ist verschlüsselt oder ungültig, überspringe", filepath.Base(path))
			continue
		}
		m.logger.Info("SSH-Key geladen: %s", path)
		signers = append(signers, signer)
	}
	return signers
}

// expandTilde löst eine Tilde am Anfang eines Pfades zum Home-Verzeichnis auf.
//
// @param path - Pfad mit möglicher Tilde
// @return string - Absoluter Pfad
// @date   2026-03-08 00:00
func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// IsSSHAuthError prüft ob ein Fehler ein SSH-Authentifizierungsfehler ist.
// Wird verwendet um zu entscheiden ob ein Passwort abgefragt werden soll.
//
// @param err - Zu prüfender Fehler
// @return bool - Ob es ein Auth-Fehler ist
// @date   2026-03-08 00:00
func IsSSHAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unable to authenticate") ||
		strings.Contains(msg, "no supported methods remain") ||
		strings.Contains(msg, "Permission denied") ||
		strings.Contains(msg, "permission denied")
}

// IsNetworkError prüft ob ein Fehler ein Netzwerkfehler ist (kein Auth-Fehler).
//
// @param err - Zu prüfender Fehler
// @return bool - Ob es ein Netzwerkfehler ist
// @date   2026-03-08 00:00
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "network unreachable") ||
		strings.Contains(msg, "connection timed out") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dial tcp") && strings.Contains(msg, "timeout")
}

// Disconnect trennt eine SSH-Verbindung und entfernt sie aus dem Manager.
//
// @param id - ID der zu trennenden Verbindung
// @date   2026-03-08 00:00
func (m *SSHManager) Disconnect(id string) {
	m.mu.Lock()
	managed, ok := m.connections[id]
	if ok {
		delete(m.connections, id)
	}
	m.mu.Unlock()

	if !ok || managed.Status == nil {
		return
	}

	m.logger.Info("Trenne Verbindung %s", managed.Config.Name)
	DisconnectSSH(managed.Status)
}

// DisconnectAll trennt alle aktiven Verbindungen.
//
// @date   2026-03-08 00:00
func (m *SSHManager) DisconnectAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.connections))
	for id := range m.connections {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.Disconnect(id)
	}
}

// GetStatus gibt den Verbindungsstatus für eine ID zurück.
//
// @param id - Verbindungs-ID
// @return *ConnectionStatus - Status oder nil
// @return bool - Ob die Verbindung existiert
// @date   2026-03-08 00:00
func (m *SSHManager) GetStatus(id string) (*ConnectionStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	managed, ok := m.connections[id]
	if !ok {
		return nil, false
	}
	return managed.Status, true
}

// IsConnected prüft ob eine Verbindung aktiv ist.
//
// @param id - Verbindungs-ID
// @return bool - Ob verbunden
// @date   2026-03-08 00:00
func (m *SSHManager) IsConnected(id string) bool {
	status, ok := m.GetStatus(id)
	return ok && status != nil && status.Connected
}

// buildAuthMethods erstellt SSH-Authentifizierungsmethoden für eine bestimmte
// konfigurierte Auth-Methode (wird für gespeicherte Konfigurationen verwendet).
//
// @param conn - Verbindungskonfiguration
// @param password - Passwort oder Passphrase
// @return []ssh.AuthMethod - Auth-Methoden
// @return error - Fehler
// @date   2026-03-08 00:00
func (m *SSHManager) buildAuthMethods(conn Connection, password string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	switch conn.AuthType {
	case AuthPassword:
		// Passwort-Authentifizierung
		methods = append(methods, ssh.Password(password))

	case AuthKey:
		// SSH-Schlüssel lesen
		keyData, err := os.ReadFile(expandTilde(conn.KeyPath))
		if err != nil {
			return nil, fmt.Errorf("SSH-Schlüssel %s konnte nicht gelesen werden: %w", conn.KeyPath, err)
		}

		var signer ssh.Signer
		if password != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(password))
		} else {
			signer, err = ssh.ParsePrivateKey(keyData)
		}
		if err != nil {
			return nil, fmt.Errorf("SSH-Schlüssel konnte nicht geparst werden: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))

	case AuthAgent:
		// SSH-Agent-Authentifizierung
		agentAuth, err := m.getAgentAuth()
		if err != nil {
			return nil, fmt.Errorf("SSH-Agent nicht verfügbar: %w", err)
		}
		methods = append(methods, agentAuth)
	}

	return methods, nil
}

// getAgentAuth stellt eine Verbindung zum SSH-Agent her und gibt die
// Authentifizierungsmethode zurück.
//
// @return ssh.AuthMethod - Agent-basierte Authentifizierung
// @return error - Fehler bei Agent-Verbindung
// @date   2026-03-08 00:00
func (m *SSHManager) getAgentAuth() (ssh.AuthMethod, error) {
	// SSH_AUTH_SOCK Umgebungsvariable lesen
	socketPath := os.Getenv("SSH_AUTH_SOCK")
	if socketPath == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK nicht gesetzt - SSH-Agent läuft nicht")
	}

	// Verbindung zum Agent-Socket aufbauen
	agentConn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("Verbindung zum SSH-Agent fehlgeschlagen: %w", err)
	}

	// Agent-Client erstellen
	agentClient := sshagent.NewClient(agentConn)

	return ssh.PublicKeysCallback(agentClient.Signers), nil
}

// getHostKeyCallback erstellt einen HostKey-Callback.
// known_hosts MUSS existieren. Neue Hosts werden automatisch hinzugefügt,
// aber geänderte Keys werden abgelehnt (MITM-Schutz).
//
// @return ssh.HostKeyCallback - Callback-Funktion
// @return error - Fehler wenn known_hosts nicht verfügbar
// @date   2026-03-08 00:00
func (m *SSHManager) getHostKeyCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("Home-Verzeichnis nicht ermittelbar: %w", err)
	}

	sshDir := home + "/.ssh"
	knownHostsPath := sshDir + "/known_hosts"

	// SSH-Verzeichnis erstellen falls nötig
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return nil, fmt.Errorf("SSH-Verzeichnis konnte nicht erstellt werden: %w", err)
	}

	// known_hosts erstellen falls nötig
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		if err := os.WriteFile(knownHostsPath, []byte{}, 0600); err != nil {
			return nil, fmt.Errorf("known_hosts konnte nicht erstellt werden: %w", err)
		}
		m.logger.Info("known_hosts erstellt: %s", knownHostsPath)
	}

	// known_hosts-Callback erstellen
	hostKeyCallback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("known_hosts konnte nicht geladen werden: %w", err)
	}

	// Wrapper: unbekannte Hosts automatisch hinzufügen,
	// geänderte Keys ablehnen (MITM-Schutz)
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := hostKeyCallback(hostname, remote, key)
		if err != nil {
			knownHostsErr, ok := err.(*knownhosts.KeyError)
			if ok && len(knownHostsErr.Want) == 0 {
				// Neuer Host - Key speichern
				m.logger.Info("Neuer Host-Key gespeichert für %s", hostname)
				return appendKnownHost(knownHostsPath, hostname, key)
			}
			// Key geändert - MITM-Warnung!
			m.logger.Error("HOST-KEY GEAENDERT fuer %s - möglicher MITM-Angriff!", hostname)
			return fmt.Errorf("HOST-KEY GEAENDERT fuer %s! Möglicher MITM-Angriff. "+
				"Alten Key in ~/.ssh/known_hosts löschen wenn beabsichtigt", hostname)
		}
		return nil
	}, nil
}

// keepaliveLoop sendet regelmäßig Keepalive-Pakete und erkennt
// Verbindungsabbrüche. Bei Abbruch wird automatisch ein Reconnect versucht.
//
// @param ctx - Kontext zum Beenden
// @param connID - ID der zu überwachenden Verbindung
// @date   2026-03-08 00:00
func (m *SSHManager) keepaliveLoop(ctx context.Context, connID string) {
	ticker := time.NewTicker(KeepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			managed, ok := m.connections[connID]
			m.mu.RUnlock()

			if !ok || managed.Status == nil || !managed.Status.Connected {
				return
			}

			// Keepalive-Paket senden (SSH Global Request)
			_, _, err := managed.Status.SSHClient.SendRequest("keepalive@ssh-easy", true, nil)
			if err != nil {
				m.logger.Warn("Keepalive fehlgeschlagen für %s: %v", managed.Config.Name, err)
				managed.Status.Connected = false

				// Reconnect versuchen
				go m.reconnect(connID)
				return
			}
		}
	}
}

// reconnect versucht eine abgebrochene Verbindung wiederherzustellen.
// Maximal ReconnectMaxRetries Versuche mit ReconnectDelay Pause.
// Verwendet Auto-Auth (Agent + alle Schlüssel) dann gespeichertes Passwort.
//
// @param connID - ID der wiederherzustellenden Verbindung
// @date   2026-03-08 00:00
func (m *SSHManager) reconnect(connID string) {
	m.mu.Lock()
	managed, ok := m.connections[connID]
	if !ok {
		m.mu.Unlock()
		return
	}

	// Prüfen ob bereits Reconnect läuft
	if managed.reconnecting {
		m.mu.Unlock()
		return
	}
	managed.reconnecting = true
	managed.reconnectCount = 0
	m.mu.Unlock()

	m.logger.Info("Starte Reconnect für %s...", managed.Config.Name)

	// Alte Verbindung aufräumen
	DisconnectSSH(managed.Status)

	for i := 0; i < ReconnectMaxRetries; i++ {
		m.mu.RLock()
		_, stillExists := m.connections[connID]
		m.mu.RUnlock()

		if !stillExists {
			// Verbindung wurde manuell gelöscht
			return
		}

		m.logger.Info("Reconnect-Versuch %d/%d fuer %s",
			i+1, ReconnectMaxRetries, managed.Config.Name)

		time.Sleep(ReconnectDelay)

		// Auto-Auth versuchen (Agent + alle Schlüssel)
		autoMethods := m.buildAutoAuthMethods(managed.Config)
		var status *ConnectionStatus
		var ctx context.Context
		var err error

		if len(autoMethods) > 0 {
			ctx, status, err = m.dialSSH(managed.Config, autoMethods)
		}

		// Falls Auto fehlschlägt und Passwort bekannt: Passwort versuchen
		if (err != nil || len(autoMethods) == 0) && managed.password != "" {
			m.logger.Info("Auto-Reconnect fehlgeschlagen, versuche Passwort...")
			pwMethods := []ssh.AuthMethod{ssh.Password(managed.password)}
			ctx, status, err = m.dialSSH(managed.Config, pwMethods)
		}

		if err != nil {
			m.logger.Warn("Reconnect %d fehlgeschlagen: %v", i+1, err)
			continue
		}

		// Reconnect erfolgreich
		m.mu.Lock()
		if existingManaged, ok := m.connections[connID]; ok {
			existingManaged.Status = status
			existingManaged.reconnecting = false
			existingManaged.reconnectCount = i + 1
		}
		m.mu.Unlock()

		m.logger.Info("Reconnect erfolgreich für %s nach %d Versuchen", managed.Config.Name, i+1)

		// Keepalive-Loop fuer neue Verbindung starten
		go m.keepaliveLoop(ctx, connID)
		return
	}

	m.logger.Error("Reconnect fehlgeschlagen für %s nach %d Versuchen",
		managed.Config.Name, ReconnectMaxRetries)

	m.mu.Lock()
	if existingManaged, ok := m.connections[connID]; ok {
		existingManaged.reconnecting = false
	}
	m.mu.Unlock()
}
