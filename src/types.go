// Paket main - Datentypen für ssh-easy
//
// Definiert alle Strukturen für SSH-Verbindungen, Tunnel-Konfiguration
// und Anwendungskonfiguration.
//
// @author Reisen macht Spaß... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 18:15
package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// AuthMethod definiert die Art der SSH-Authentifizierung
type AuthMethod string

const (
	// AuthPassword - Passwort-basierte Authentifizierung
	AuthPassword AuthMethod = "password"
	// AuthKey - SSH-Schlüssel-basierte Authentifizierung
	AuthKey AuthMethod = "key"
	// AuthAgent - SSH-Agent-basierte Authentifizierung
	AuthAgent AuthMethod = "agent"
)

// TunnelConfig repräsentiert einen einzelnen Port-Forward-Tunnel.
// Lokaler Port wird immer auf 127.0.0.1 gebunden.
type TunnelConfig struct {
	// Lokaler Port auf dem eigenen Rechner
	LocalPort int `json:"local_port"`
	// Zielport auf dem Remote-Server
	RemotePort int `json:"remote_port"`
	// Ob dieser Tunnel aktiv genutzt werden soll
	Enabled bool `json:"enabled"`
}

// Connection repräsentiert eine gespeicherte SSH-Verbindung
// mit allen Konfigurationsdetails.
type Connection struct {
	// Eindeutige ID der Verbindung (UUID-Format)
	ID string `json:"id"`
	// Anzeigename für die Verbindungsliste
	Name string `json:"name"`
	// Hostname oder IP-Adresse des SSH-Servers
	Host string `json:"host"`
	// SSH-Port (Standard: 22)
	Port int `json:"port"`
	// SSH-Benutzername
	User string `json:"user"`
	// Authentifizierungsmethode ("password" oder "key")
	AuthType AuthMethod `json:"auth_type"`
	// Pfad zum SSH-Schlüssel (nur bei AuthKey)
	KeyPath string `json:"key_path,omitempty"`
	// Liste der Port-Forward-Tunnel
	Tunnels []TunnelConfig `json:"tunnels"`
	// Erstellungszeitpunkt
	CreatedAt string `json:"created_at"`
	// Zeitpunkt der letzten Änderung
	UpdatedAt string `json:"updated_at"`
}

// AppConfig ist die gesamte Konfigurationsdatei mit allen Verbindungen
type AppConfig struct {
	// Schema-Version für zukünftige Migrationen
	Version int `json:"version"`
	// Gewählte UI-Sprache (ISO 639-1 Code, z.B. "de", "en")
	Language Language `json:"language,omitempty"`
	// Liste aller gespeicherten Verbindungen
	Connections []Connection `json:"connections"`
}

// ConnectionStatus speichert den Laufzeitstatus einer aktiven SSH-Verbindung
type ConnectionStatus struct {
	// Ob die SSH-Verbindung aktiv ist
	Connected bool
	// Der aktive SSH-Client
	SSHClient *ssh.Client
	// Aktive Tunnel-Listener (einer pro Tunnel)
	Listeners []net.Listener
	// Fehlermeldungen pro Tunnel-Port
	TunnelErrors map[int]string
	// Funktion zum Beenden der Verbindung
	Cancel context.CancelFunc
}

// Validate prüft ob eine Verbindung gültige Werte hat.
// Gibt einen Fehler zurück wenn Pflichtfelder fehlen oder ungültig sind.
//
// @return error - Fehlerbeschreibung oder nil
// @date   2026-03-07 18:15
func (c *Connection) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("Name darf nicht leer sein")
	}
	if c.Host == "" {
		return fmt.Errorf("Host darf nicht leer sein")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("Port muss zwischen 1 und 65535 liegen")
	}
	if c.User == "" {
		return fmt.Errorf("Benutzer darf nicht leer sein")
	}
	if c.AuthType != AuthPassword && c.AuthType != AuthKey && c.AuthType != AuthAgent {
		return fmt.Errorf("Authentifizierungsmethode muss 'password', 'key' oder 'agent' sein")
	}
	if c.AuthType == AuthKey && c.KeyPath == "" {
		return fmt.Errorf("SSH-Schlüssel-Pfad darf bei Key-Authentifizierung nicht leer sein")
	}

	// Tunnel-Ports prüfen
	seenPorts := make(map[int]bool)
	for _, t := range c.Tunnels {
		if t.LocalPort < 1 || t.LocalPort > 65535 {
			return fmt.Errorf("Lokaler Tunnel-Port %d ist ungültig", t.LocalPort)
		}
		if t.RemotePort < 1 || t.RemotePort > 65535 {
			return fmt.Errorf("Remote Tunnel-Port %d ist ungültig", t.RemotePort)
		}
		if seenPorts[t.LocalPort] {
			return fmt.Errorf("Lokaler Port %d ist doppelt vergeben", t.LocalPort)
		}
		seenPorts[t.LocalPort] = true
	}

	return nil
}

// NewConnection erstellt eine neue Verbindung mit Standardwerten.
// Die ID wird als Zeitstempel-basierte eindeutige Kennung generiert.
//
// @param name - Anzeigename der Verbindung
// @param host - Hostname oder IP
// @param port - SSH-Port
// @param user - Benutzername
// @param authType - Authentifizierungsmethode
// @return Connection - Neue Verbindung mit Standardwerten
// @date   2026-03-07 18:15
func NewConnection(name, host string, port int, user string, authType AuthMethod) Connection {
	now := time.Now().Format(time.RFC3339)
	return Connection{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Name:      name,
		Host:      host,
		Port:      port,
		User:      user,
		AuthType:  authType,
		Tunnels:   []TunnelConfig{},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewAppConfig erstellt eine leere Konfiguration mit Version 1
//
// @return AppConfig - Leere Konfiguration
// @date   2026-03-07 18:15
func NewAppConfig() AppConfig {
	return AppConfig{
		Version:     1,
		Connections: []Connection{},
	}
}
