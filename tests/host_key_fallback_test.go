// Paket main - Tests: kein automatisches Vertrauen bei Fallback-DNS (Build 49)
//
// Stammt die Adresse eines Hosts vom öffentlichen Fallback-DNS, könnte sie
// gefälscht sein (unverschlüsseltes DNS). Ein noch UNBEKANNTER Host-Key wird
// dann nicht automatisch in known_hosts übernommen (kein Trust-on-first-use),
// sondern die Verbindung mit einer verständlichen Meldung abgelehnt.
// Bekannte Hosts sind weiterhin durch known_hosts geschützt.
//
// @author Kurt Ingwer
// @date   2026-10-16 11:00
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// newTestHostKey erzeugt einen zufälligen öffentlichen Host-Key.
func newTestHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// TestHostKeyCallbackRejectsNewHostViaFallback prüft die Ablehnung.
func TestHostKeyCallbackRejectsNewHostViaFallback(t *testing.T) {
	setupKeyHome(t, 0)
	m := NewSSHManager(NewLogger())
	cb, err := m.getHostKeyCallback(false)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 22}
	err = cb("server.example.org:22", remote, newTestHostKey(t))
	var untrusted *untrustedHostError
	if !errors.As(err, &untrusted) || untrusted.Hostname != "server.example.org:22" {
		t.Fatalf("erwartet untrustedHostError, bekommen %v", err)
	}
	home, _ := os.UserHomeDir()
	data, _ := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if strings.Contains(string(data), "server.example.org") {
		t.Error("unbekannter Key wurde trotz Fallback-DNS in known_hosts übernommen")
	}
}

// TestHostKeyCallbackAcceptsNewHostNormally prüft das bisherige Verhalten
// ohne Fallback (Trust-on-first-use) und dass ein danach bekannter Host
// auch über Fallback-DNS verbunden werden darf.
func TestHostKeyCallbackAcceptsNewHostNormally(t *testing.T) {
	setupKeyHome(t, 0)
	m := NewSSHManager(NewLogger())
	key := newTestHostKey(t)
	remote := &net.TCPAddr{IP: net.ParseIP("203.0.113.6"), Port: 22}

	cb, _ := m.getHostKeyCallback(true)
	if err := cb("nas.example.org:22", remote, key); err != nil {
		t.Fatalf("neuer Host ohne Fallback abgelehnt: %v", err)
	}
	// Jetzt bekannt: auch über Fallback-DNS erlaubt
	cb, _ = m.getHostKeyCallback(false)
	if err := cb("nas.example.org:22", remote, key); err != nil {
		t.Errorf("bekannter Host über Fallback abgelehnt: %v", err)
	}
	// Geänderter Key bleibt eine MITM-Warnung
	if err := cb("nas.example.org:22", remote, newTestHostKey(t)); !IsHostKeyChangedError(err) {
		t.Errorf("geänderter Key: erwartet HostKeyChangedError, bekommen %v", err)
	}
}

// TestUntrustedHostErrorIsReportedDirectly prüft dass der Fehler als
// Netzwerkfehler gilt (keine Passwortabfrage, Schlüsselsuche bricht ab) und
// in der TUI übersetzt angezeigt wird.
func TestUntrustedHostErrorIsReportedDirectly(t *testing.T) {
	err := &networkError{err: &untrustedHostError{Hostname: "x.example.org:22"}}
	if !IsNetworkError(err) {
		t.Error("untrustedHostError muss als Netzwerkfehler gelten")
	}
	m := newListTestModel(t, "Server")
	updated, _ := m.Update(sshErrorMsg{id: m.connections[0].ID, err: err})
	text := updated.(AppModel).errorMsg
	if !strings.Contains(text, "öffentlichen DNS") {
		t.Errorf("übersetzte Meldung fehlt: %q", text)
	}
}
