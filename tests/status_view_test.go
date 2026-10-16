// Paket main - Tests für Status- und Passwortansicht (Build 48)
//
// Prüft:
//   - Nicht verbundene Tunnel-Ports werden rot angezeigt, ohne die konkrete
//     Fehlermeldung dahinter
//   - Bei der Passwortabfrage stehen Host und Benutzername in eigenen Zeilen
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00
package main

import (
	"strings"
	"testing"
)

// TestTunnelFailed prüft die Fehlerermittlung pro Tunnel-Port.
func TestTunnelFailed(t *testing.T) {
	status := &ConnectionStatus{Connected: true, TunnelErrors: map[int]string{9090: "bind: address already in use"}}
	used := map[int]string{7070: "Andere"}
	if !tunnelFailed(status, 9090, used) {
		t.Error("Port mit Startfehler nicht als fehlgeschlagen erkannt")
	}
	if !tunnelFailed(status, 7070, used) {
		t.Error("von anderer Verbindung belegter Port nicht erkannt")
	}
	if tunnelFailed(status, 8080, used) {
		t.Error("funktionierender Port als fehlgeschlagen erkannt")
	}
	if tunnelFailed(nil, 9090, used) {
		t.Error("ohne Status darf kein Fehler gemeldet werden")
	}
}

// TestStatusViewFailedTunnelRedWithoutMessage prüft die Statusansicht.
func TestStatusViewFailedTunnelRedWithoutMessage(t *testing.T) {
	withColorProfile(t)
	m := newListTestModel(t, "Server")
	m.connections[0].Tunnels = []TunnelConfig{
		{LocalPort: 8080, RemotePort: 8080, Enabled: true},
		{LocalPort: 9090, RemotePort: 9090, Enabled: true},
	}
	markConnected(m, m.connections[0], map[int]string{9090: "listen tcp 127.0.0.1:9090: bind: address already in use"})
	m.activeID = m.connections[0].ID
	m.state = ViewStatus

	text := m.View()
	plain := stripANSI(text)
	if strings.Contains(plain, "address already in use") || strings.Contains(plain, "bind") {
		t.Errorf("Fehlermeldung darf nicht erscheinen:\n%s", plain)
	}
	failed := selectedLine(t, text, "localhost:9090")
	if !strings.Contains(failed, "38;5;196") {
		t.Errorf("fehlgeschlagener Tunnel ist nicht rot: %q", failed)
	}
	if strings.Contains(stripANSI(failed), m.lang.TunnelActive) {
		t.Errorf("fehlgeschlagener Tunnel darf nicht als aktiv gelten: %q", stripANSI(failed))
	}
	ok := selectedLine(t, text, "localhost:8080")
	if strings.Contains(ok, "38;5;196") || !strings.Contains(stripANSI(ok), m.lang.TunnelActive) {
		t.Errorf("funktionierender Tunnel falsch dargestellt: %q", stripANSI(ok))
	}
}

// TestStatusViewPortConflictWithoutMessage prüft den Konfliktfall (Port von
// anderer Verbindung belegt): ebenfalls rot und ohne Text.
func TestStatusViewPortConflictWithoutMessage(t *testing.T) {
	m := newListTestModel(t, "Andere", "Server")
	other := connByName(t, m, "Andere")
	server := connByName(t, m, "Server")
	server.Tunnels = []TunnelConfig{{LocalPort: 7070, RemotePort: 7070, Enabled: true}}
	for i := range m.connections {
		if m.connections[i].ID == server.ID {
			m.connections[i] = server
		}
	}
	markConnected(m, server, map[int]string{7070: "bind failed"})
	markConnected(m, other, nil)
	m.activeID = server.ID
	m.state = ViewStatus

	plain := stripANSI(m.View())
	if strings.Contains(plain, "bind failed") || strings.Contains(plain, "Andere") {
		t.Errorf("Konflikttext darf nicht erscheinen:\n%s", plain)
	}
	if !strings.Contains(plain, "localhost:7070 -> remote:7070") {
		t.Errorf("Tunnelzeile fehlt:\n%s", plain)
	}
}

// TestPasswordPromptShowsHostAndUser prüft dass bei der Passwortabfrage
// Host und Benutzer in eigenen Zeilen stehen (Benutzer nach dem Host).
func TestPasswordPromptShowsHostAndUser(t *testing.T) {
	m := newListTestModel(t, "Server")
	m.connections[0].Host = "nas.lan"
	m.connections[0].Port = 2222
	m.connections[0].User = "admin"
	m.activeID = m.connections[0].ID
	m.state = ViewConnect

	plain := stripANSI(m.View())
	lines := strings.Split(plain, "\n")
	hostLine, userLine := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "nas.lan:2222") {
			hostLine = i
		}
		if strings.Contains(line, "admin") {
			userLine = i
		}
	}
	if hostLine < 0 || userLine < 0 {
		t.Fatalf("Host oder Benutzer fehlt:\n%s", plain)
	}
	if userLine != hostLine+1 {
		t.Errorf("Benutzer (Zeile %d) soll direkt nach dem Host (Zeile %d) stehen:\n%s", userLine, hostLine, plain)
	}
	if !strings.Contains(lines[userLine], "Benutzer") {
		t.Errorf("Benutzerzeile ohne Beschriftung: %q", lines[userLine])
	}
}

// TestStatusViewNoTunnels prüft: Ohne (aktive) Tunnel erscheint statt der
// leeren Überschrift "Tunnel" der Hinweis "Keine Tunnel".
func TestStatusViewNoTunnels(t *testing.T) {
	m := newListTestModel(t, "Server")
	markConnected(m, m.connections[0], nil)
	m.activeID = m.connections[0].ID
	m.state = ViewStatus

	plain := stripANSI(m.View())
	if !strings.Contains(plain, "Keine Tunnel") {
		t.Errorf("Hinweis 'Keine Tunnel' fehlt:\n%s", plain)
	}
	if strings.Contains(plain, m.lang.LabelTunnel) {
		t.Errorf("Überschrift %q darf ohne Tunnel nicht erscheinen:\n%s", m.lang.LabelTunnel, plain)
	}

	// Nur deaktivierte Tunnel zählen ebenfalls als "keine"
	m.connections[0].Tunnels = []TunnelConfig{{LocalPort: 80, RemotePort: 80, Enabled: false}}
	if plain = stripANSI(m.View()); !strings.Contains(plain, "Keine Tunnel") {
		t.Errorf("nur deaktivierte Tunnel: Hinweis fehlt:\n%s", plain)
	}

	// Mit Tunnel: Überschrift da, Hinweis weg
	m.connections[0].Tunnels = []TunnelConfig{{LocalPort: 80, RemotePort: 80, Enabled: true}}
	plain = stripANSI(m.View())
	if !strings.Contains(plain, m.lang.LabelTunnel) || strings.Contains(plain, "Keine Tunnel") {
		t.Errorf("mit Tunnel falsch dargestellt:\n%s", plain)
	}
}
