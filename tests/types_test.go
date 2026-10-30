// Tests fuer die Datentypen und Validierung
//
// @author Kurt Ingwer
// @date   2026-10-30 16:22
package main

import (
	"encoding/json"
	"strings"
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

// TestConnectionAutoKeyJSONFields prueft die Felder des Merkens und
// Aussetzens der automatischen Schluesselvergabe (Build 86): Namen im JSON
// "auto_key_off" und "auto_key_failures", beide mit omitempty. Nullwerte
// fehlen in der Datei (alte Konfigurationen bleiben unveraendert), gesetzte
// Werte kommen beim Laden zurueck, und eine alte Datei ohne die Felder ergibt
// false und 0.
//
// @date 2026-10-28 14:00
func TestConnectionAutoKeyJSONFields(t *testing.T) {
	plain := NewConnection("A", "h", 22, "u", AuthPassword)
	data, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "auto_key") {
		t.Errorf("Nullwerte stehen in der Datei: %s", data)
	}

	set := plain
	set.AutoKeyOff = true
	set.AutoKeyFailures = 2
	data, err = json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"auto_key_off":true`) || !strings.Contains(string(data), `"auto_key_failures":2`) {
		t.Errorf("JSON ohne die erwarteten Felder: %s", data)
	}
	var back Connection
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !back.AutoKeyOff || back.AutoKeyFailures != 2 {
		t.Errorf("Rueckweg: AutoKeyOff=%v AutoKeyFailures=%d", back.AutoKeyOff, back.AutoKeyFailures)
	}

	var old Connection
	if err := json.Unmarshal([]byte(`{"id":"1","name":"alt","host":"h","port":22,"user":"u","auth_type":"password"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.AutoKeyOff || old.AutoKeyFailures != 0 {
		t.Errorf("alte Datei: AutoKeyOff=%v AutoKeyFailures=%d, erwartet false und 0", old.AutoKeyOff, old.AutoKeyFailures)
	}
}
