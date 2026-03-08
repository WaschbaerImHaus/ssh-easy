// Tests fuer die Datentypen und Validierung
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 18:15
package main

import (
	"testing"
)

// TestNewConnection prueft ob eine neue Verbindung korrekt erstellt wird
func TestNewConnection(t *testing.T) {
	conn := NewConnection("Test", "192.168.1.1", 22, "root", AuthPassword)

	if conn.Name != "Test" {
		t.Errorf("Name erwartet 'Test', bekommen '%s'", conn.Name)
	}
	if conn.Host != "192.168.1.1" {
		t.Errorf("Host erwartet '192.168.1.1', bekommen '%s'", conn.Host)
	}
	if conn.Port != 22 {
		t.Errorf("Port erwartet 22, bekommen %d", conn.Port)
	}
	if conn.User != "root" {
		t.Errorf("User erwartet 'root', bekommen '%s'", conn.User)
	}
	if conn.AuthType != AuthPassword {
		t.Errorf("AuthType erwartet 'password', bekommen '%s'", conn.AuthType)
	}
	if conn.ID == "" {
		t.Error("ID darf nicht leer sein")
	}
	if conn.CreatedAt == "" {
		t.Error("CreatedAt darf nicht leer sein")
	}
}

// TestValidateValidConnection prueft ob eine gueltige Verbindung akzeptiert wird
func TestValidateValidConnection(t *testing.T) {
	conn := NewConnection("Server", "10.0.0.1", 22, "admin", AuthPassword)
	if err := conn.Validate(); err != nil {
		t.Errorf("Gueltige Verbindung sollte keinen Fehler erzeugen: %v", err)
	}
}

// TestValidateEmptyName prueft ob leerer Name erkannt wird
func TestValidateEmptyName(t *testing.T) {
	conn := NewConnection("", "10.0.0.1", 22, "admin", AuthPassword)
	if err := conn.Validate(); err == nil {
		t.Error("Leerer Name sollte einen Fehler erzeugen")
	}
}

// TestValidateEmptyHost prueft ob leerer Host erkannt wird
func TestValidateEmptyHost(t *testing.T) {
	conn := NewConnection("Test", "", 22, "admin", AuthPassword)
	if err := conn.Validate(); err == nil {
		t.Error("Leerer Host sollte einen Fehler erzeugen")
	}
}

// TestValidateInvalidPort prueft ob ungueltiger Port erkannt wird
func TestValidateInvalidPort(t *testing.T) {
	cases := []int{0, -1, 65536, 100000}
	for _, port := range cases {
		conn := NewConnection("Test", "10.0.0.1", port, "admin", AuthPassword)
		if err := conn.Validate(); err == nil {
			t.Errorf("Port %d sollte einen Fehler erzeugen", port)
		}
	}
}

// TestValidateEmptyUser prueft ob leerer Benutzer erkannt wird
func TestValidateEmptyUser(t *testing.T) {
	conn := NewConnection("Test", "10.0.0.1", 22, "", AuthPassword)
	if err := conn.Validate(); err == nil {
		t.Error("Leerer Benutzer sollte einen Fehler erzeugen")
	}
}

// TestValidateKeyWithoutPath prueft ob fehlender Key-Pfad erkannt wird
func TestValidateKeyWithoutPath(t *testing.T) {
	conn := NewConnection("Test", "10.0.0.1", 22, "admin", AuthKey)
	// KeyPath ist leer
	if err := conn.Validate(); err == nil {
		t.Error("Key-Auth ohne Pfad sollte einen Fehler erzeugen")
	}
}

// TestValidateKeyWithPath prueft ob Key-Auth mit Pfad akzeptiert wird
func TestValidateKeyWithPath(t *testing.T) {
	conn := NewConnection("Test", "10.0.0.1", 22, "admin", AuthKey)
	conn.KeyPath = "/home/user/.ssh/id_rsa"
	if err := conn.Validate(); err != nil {
		t.Errorf("Key-Auth mit Pfad sollte keinen Fehler erzeugen: %v", err)
	}
}

// TestValidateInvalidAuthType prueft ob ungueltiger Auth-Typ erkannt wird
func TestValidateInvalidAuthType(t *testing.T) {
	conn := NewConnection("Test", "10.0.0.1", 22, "admin", "invalid")
	if err := conn.Validate(); err == nil {
		t.Error("Ungueltiger Auth-Typ sollte einen Fehler erzeugen")
	}
}

// TestValidateDuplicateTunnelPorts prueft ob doppelte Tunnel-Ports erkannt werden
func TestValidateDuplicateTunnelPorts(t *testing.T) {
	conn := NewConnection("Test", "10.0.0.1", 22, "admin", AuthPassword)
	conn.Tunnels = []TunnelConfig{
		{LocalPort: 3306, RemotePort: 3306, Enabled: true},
		{LocalPort: 3306, RemotePort: 3307, Enabled: true},
	}
	if err := conn.Validate(); err == nil {
		t.Error("Doppelte lokale Tunnel-Ports sollten einen Fehler erzeugen")
	}
}

// TestValidateInvalidTunnelPort prueft ob ungueltiger Tunnel-Port erkannt wird
func TestValidateInvalidTunnelPort(t *testing.T) {
	conn := NewConnection("Test", "10.0.0.1", 22, "admin", AuthPassword)
	conn.Tunnels = []TunnelConfig{
		{LocalPort: 0, RemotePort: 3306, Enabled: true},
	}
	if err := conn.Validate(); err == nil {
		t.Error("Tunnel-Port 0 sollte einen Fehler erzeugen")
	}
}

// TestValidateValidTunnels prueft ob gueltige Tunnel akzeptiert werden
func TestValidateValidTunnels(t *testing.T) {
	conn := NewConnection("Test", "10.0.0.1", 22, "admin", AuthPassword)
	conn.Tunnels = []TunnelConfig{
		{LocalPort: 3306, RemotePort: 3306, Enabled: true},
		{LocalPort: 8080, RemotePort: 80, Enabled: true},
		{LocalPort: 5432, RemotePort: 5432, Enabled: false},
	}
	if err := conn.Validate(); err != nil {
		t.Errorf("Gueltige Tunnel sollten keinen Fehler erzeugen: %v", err)
	}
}

// TestNewAppConfig prueft ob eine leere Konfiguration korrekt erstellt wird
func TestNewAppConfig(t *testing.T) {
	cfg := NewAppConfig()
	if cfg.Version != 1 {
		t.Errorf("Version erwartet 1, bekommen %d", cfg.Version)
	}
	if len(cfg.Connections) != 0 {
		t.Error("Neue Konfiguration sollte keine Verbindungen haben")
	}
}

// TestValidatePortBoundary prueft Grenzwerte fuer Ports
func TestValidatePortBoundary(t *testing.T) {
	// Port 1 sollte gueltig sein
	conn1 := NewConnection("Test", "10.0.0.1", 1, "admin", AuthPassword)
	if err := conn1.Validate(); err != nil {
		t.Errorf("Port 1 sollte gueltig sein: %v", err)
	}

	// Port 65535 sollte gueltig sein
	conn2 := NewConnection("Test", "10.0.0.1", 65535, "admin", AuthPassword)
	if err := conn2.Validate(); err != nil {
		t.Errorf("Port 65535 sollte gueltig sein: %v", err)
	}
}
