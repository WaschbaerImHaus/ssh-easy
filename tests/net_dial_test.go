// Paket main - Tests für Verbindungsaufbau und DNS-Fallback (Build 48)
//
// Prüft:
//   - DNS-Fallback: Rotation über 1.1.1.1, 1.0.0.1, 8.8.8.8, 8.8.4.4, wenn der
//     vom Netzwerk vorgegebene DNS-Server versagt
//   - Enges Timeout (5 s) beim ersten Verbindungsaufbau
//   - Abbruch der Schlüssel-Durchprobiererei beim ersten Netzwerkfehler
//     (ausgeschalteter Ziel-PC darf nicht N × Timeout kosten)
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00
package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- hostResolver ---

// fakeResolver baut einen hostResolver mit Stub-Lookups.
// systemErr != nil lässt den System-DNS scheitern; working enthält die
// Fallback-Server, die erfolgreich antworten.
func fakeResolver(systemErr error, working ...string) (*hostResolver, *[]string) {
	var mu sync.Mutex
	calls := &[]string{}
	r := newHostResolver()
	r.systemLookup = func(ctx context.Context, host string) ([]string, error) {
		mu.Lock()
		*calls = append(*calls, "system")
		mu.Unlock()
		if systemErr != nil {
			return nil, systemErr
		}
		return []string{"10.1.1.1"}, nil
	}
	r.serverLookup = func(ctx context.Context, server, host string) ([]string, error) {
		mu.Lock()
		*calls = append(*calls, server)
		mu.Unlock()
		for _, w := range working {
			if w == server {
				return []string{"10.2.2.2"}, nil
			}
		}
		return nil, errors.New("timeout")
	}
	return r, calls
}

// TestFallbackDNSServerList prüft die Server-Liste. Hinweis: Der Nutzer
// schrieb "8.4.4.8" - Googles zweiter DNS-Server ist jedoch 8.8.4.4.
func TestFallbackDNSServerList(t *testing.T) {
	want := []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4"}
	if strings.Join(fallbackDNSServers, ",") != strings.Join(want, ",") {
		t.Errorf("fallbackDNSServers = %v, erwartet %v", fallbackDNSServers, want)
	}
}

// TestResolveIPLiteralSkipsDNS prüft dass IP-Adressen nicht aufgelöst werden.
func TestResolveIPLiteralSkipsDNS(t *testing.T) {
	r, calls := fakeResolver(errors.New("kaputt"))
	for _, ip := range []string{"192.168.1.10", "::1", "[fe80::1]"} {
		got, err := r.resolve(context.Background(), ip)
		if err != nil || len(got) != 1 {
			t.Errorf("resolve(%q) = %v, %v", ip, got, err)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("bei IP-Adressen wurde DNS gefragt: %v", *calls)
	}
}

// TestResolveSystemDNSFirst prüft dass ohne Fehler kein Fallback genutzt wird.
func TestResolveSystemDNSFirst(t *testing.T) {
	r, calls := fakeResolver(nil)
	got, err := r.resolve(context.Background(), "server.local")
	if err != nil || len(got) != 1 || got[0] != "10.1.1.1" {
		t.Fatalf("resolve = %v, %v", got, err)
	}
	if strings.Join(*calls, ",") != "system" {
		t.Errorf("Aufrufe = %v, erwartet nur system", *calls)
	}
}

// TestResolveRotatesFallbackServers prüft Reihenfolge und Rotation: Der
// zuletzt erfolgreiche Server wird beim nächsten Mal zuerst gefragt.
func TestResolveRotatesFallbackServers(t *testing.T) {
	r, calls := fakeResolver(errors.New("system-dns tot"), "8.8.8.8")

	got, err := r.resolve(context.Background(), "example.org")
	if err != nil || got[0] != "10.2.2.2" {
		t.Fatalf("resolve = %v, %v", got, err)
	}
	if strings.Join(*calls, ",") != "system,1.1.1.1,1.0.0.1,8.8.8.8" {
		t.Errorf("erste Runde: %v", *calls)
	}

	*calls = nil
	if _, err := r.resolve(context.Background(), "example.org"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, ",") != "system,8.8.8.8" {
		t.Errorf("zweite Runde soll mit 8.8.8.8 beginnen: %v", *calls)
	}
}

// TestResolveRotationWrapsAround prüft das Weiterrotieren über das Listenende.
func TestResolveRotationWrapsAround(t *testing.T) {
	r, calls := fakeResolver(errors.New("tot"), "1.0.0.1")
	r.next = 3 // zuletzt erfolgreich: 8.8.4.4
	if _, err := r.resolve(context.Background(), "x.org"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, ",") != "system,8.8.4.4,1.1.1.1,1.0.0.1" {
		t.Errorf("Rotation über das Ende: %v", *calls)
	}
	if r.next != 1 {
		t.Errorf("next = %d, erwartet 1 (1.0.0.1)", r.next)
	}
}

// TestResolveAllFail prüft dass bei Totalausfall der System-Fehler gemeldet
// wird und als Netzwerkfehler erkennbar ist.
func TestResolveAllFail(t *testing.T) {
	r, calls := fakeResolver(errors.New("lookup x.org: no such host"))
	_, err := r.resolve(context.Background(), "x.org")
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	if !strings.Contains(err.Error(), "no such host") {
		t.Errorf("Fehlertext verliert Ursache: %v", err)
	}
	if !IsNetworkError(err) {
		t.Errorf("DNS-Totalausfall muss als Netzwerkfehler gelten: %v", err)
	}
	if len(*calls) != 5 {
		t.Errorf("erwartet system + 4 Fallbacks, bekommen %v", *calls)
	}
}

// TestResolveNotFoundSkipsFallback prüft dass bei NXDOMAIN (der DNS des
// Netzwerks funktioniert, kennt den Namen aber nicht) KEINE öffentlichen
// Server gefragt werden: Datenschutz und kein Umweg bei Split-Horizon-DNS.
func TestResolveNotFoundSkipsFallback(t *testing.T) {
	r, calls := fakeResolver(&net.DNSError{Err: "no such host", Name: "intern.lan", IsNotFound: true}, "1.1.1.1")
	_, err := r.resolve(context.Background(), "intern.lan")
	if err == nil || !IsNetworkError(err) {
		t.Fatalf("erwartet Netzwerkfehler, bekommen %v", err)
	}
	if strings.Join(*calls, ",") != "system" {
		t.Errorf("bei NXDOMAIN wurden Fallback-Server gefragt: %v", *calls)
	}
}

// TestResolveTimeoutUsesFallback prüft dass ein DNS-Timeout (Server
// antwortet nicht) den Fallback auslöst.
func TestResolveTimeoutUsesFallback(t *testing.T) {
	r, _ := fakeResolver(&net.DNSError{Err: "i/o timeout", Name: "x.org", IsTimeout: true}, "1.1.1.1")
	got, err := r.resolve(context.Background(), "x.org")
	if err != nil || got[0] != "10.2.2.2" {
		t.Errorf("Timeout muss Fallback nutzen: %v, %v", got, err)
	}
}

// TestDNSTimeoutsBounded prüft dass die DNS-Fristen zusammen klein bleiben
// (ausgeschaltetes Netz soll nicht minutenlang hängen).
func TestDNSTimeoutsBounded(t *testing.T) {
	total := systemDNSTimeout + time.Duration(len(fallbackDNSServers))*fallbackDNSTimeout
	if total > 6*time.Second {
		t.Errorf("DNS-Fristen zusammen %v, erwartet höchstens 6s", total)
	}
}

// TestResolveEmptyHost prüft die Fehlermeldung bei leerem Host.
func TestResolveEmptyHost(t *testing.T) {
	r, _ := fakeResolver(nil)
	if _, err := r.resolve(context.Background(), ""); err == nil {
		t.Error("leerer Host muss fehlschlagen")
	}
}

// --- dialResolved ---

// TestDialResolvedPrefersIPv4AndFallsBack prüft Reihenfolge (IPv4 zuerst)
// und das Weiterprobieren der nächsten Adresse.
func TestDialResolvedPrefersIPv4AndFallsBack(t *testing.T) {
	r := newHostResolver()
	r.systemLookup = func(ctx context.Context, host string) ([]string, error) {
		return []string{"2001:db8::1", "10.0.0.1", "10.0.0.2"}, nil
	}
	var tried []string
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		tried = append(tried, addr)
		if addr == "10.0.0.2:22" {
			c1, c2 := net.Pipe()
			c2.Close()
			return c1, nil
		}
		return nil, errors.New("connection refused")
	}
	conn, err := dialResolved(context.Background(), r, dial, "host.example", 22, time.Second)
	if err != nil {
		t.Fatalf("dialResolved: %v", err)
	}
	conn.Close()
	if strings.Join(tried, ",") != "10.0.0.1:22,10.0.0.2:22" {
		t.Errorf("Reihenfolge = %v, erwartet IPv4 zuerst", tried)
	}
}

// TestDialResolvedWrapsNetworkError prüft den typisierten Netzwerkfehler.
func TestDialResolvedWrapsNetworkError(t *testing.T) {
	r := newHostResolver()
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, errors.New("exotischer fehler")
	}
	_, err := dialResolved(context.Background(), r, dial, "10.9.9.9", 22, time.Second)
	if err == nil || !IsNetworkError(err) {
		t.Errorf("Fehler muss als Netzwerkfehler erkennbar sein: %v", err)
	}
}

// TestDialResolvedUsesTimeout prüft dass die Verbindungsfrist an den Dialer
// weitergegeben wird.
func TestDialResolvedUsesTimeout(t *testing.T) {
	r := newHostResolver()
	var remaining time.Duration
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("Dialer bekam keine Frist")
		}
		remaining = time.Until(deadline)
		return nil, os.ErrDeadlineExceeded
	}
	_, _ = dialResolved(context.Background(), r, dial, "10.9.9.9", 22, InitialConnectTimeout)
	if remaining > InitialConnectTimeout || remaining < InitialConnectTimeout-time.Second {
		t.Errorf("Frist = %v, erwartet ca. %v", remaining, InitialConnectTimeout)
	}
}

// TestInitialConnectTimeoutIsFiveSeconds prüft die Vorgabe des Nutzers.
func TestInitialConnectTimeoutIsFiveSeconds(t *testing.T) {
	if InitialConnectTimeout != 5*time.Second {
		t.Errorf("InitialConnectTimeout = %v, erwartet 5s", InitialConnectTimeout)
	}
}

// --- ConnectAuto bricht beim ersten Netzwerkfehler ab ---

// setupKeyHome legt ein temporäres Home mit n unverschlüsselten SSH-Keys an
// und deaktiviert den SSH-Agent.
func setupKeyHome(t *testing.T, n int) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	for i := 0; i < n; i++ {
		path := filepath.Join(home, ".ssh", "id_test_"+string(rune('a'+i)))
		if _, err := GenerateSSHKey(path, ""); err != nil {
			t.Fatalf("GenerateSSHKey: %v", err)
		}
	}
}

// TestConnectAutoStopsAfterFirstNetworkError prüft dass bei einem nicht
// erreichbaren Host nur EIN Verbindungsversuch unternommen wird - nicht
// einer pro Schlüssel (das kostete früher N × 10 s).
func TestConnectAutoStopsAfterFirstNetworkError(t *testing.T) {
	setupKeyHome(t, 3)
	m := NewSSHManager(NewLogger())
	var mu sync.Mutex
	calls := 0
	var budget time.Duration
	m.dialTCP = func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		calls++
		if d, ok := ctx.Deadline(); ok {
			budget = time.Until(d)
		}
		mu.Unlock()
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	}
	conn := Connection{ID: "off", Name: "Aus", Host: "192.0.2.50", Port: 22, User: "root", AuthType: AuthPassword}

	start := time.Now()
	_, err := m.ConnectAuto(conn)
	if err == nil {
		t.Fatal("ConnectAuto muss fehlschlagen")
	}
	if !IsNetworkError(err) {
		t.Errorf("Fehler muss Netzwerkfehler sein (sonst Passwortabfrage statt Meldung): %v", err)
	}
	if calls != 1 {
		t.Errorf("Verbindungsversuche = %d, erwartet 1", calls)
	}
	if budget > InitialConnectTimeout || budget < InitialConnectTimeout-time.Second {
		t.Errorf("Frist beim ersten Verbindungsaufbau = %v, erwartet ca. 5s", budget)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("ConnectAuto dauerte %v trotz sofortigem Fehler", time.Since(start))
	}
}

// TestConnectWithPasswordUsesInitialTimeout prüft die 5-s-Frist auch für
// die Passwort-Verbindung.
func TestConnectWithPasswordUsesInitialTimeout(t *testing.T) {
	setupKeyHome(t, 0)
	m := NewSSHManager(NewLogger())
	var budget time.Duration
	m.dialTCP = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if d, ok := ctx.Deadline(); ok {
			budget = time.Until(d)
		}
		return nil, errors.New("connection refused")
	}
	conn := Connection{ID: "pw", Name: "PW", Host: "192.0.2.51", Port: 22, User: "root", AuthType: AuthPassword}
	if _, err := m.ConnectWithPassword(conn, "geheim"); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if budget > InitialConnectTimeout || budget < InitialConnectTimeout-time.Second {
		t.Errorf("Frist = %v, erwartet ca. 5s", budget)
	}
}

// TestIsNetworkErrorTyped prüft die Erkennung des typisierten Fehlers auch
// durch fmt.Errorf-Verpackung hindurch.
func TestIsNetworkErrorTyped(t *testing.T) {
	base := &networkError{err: errors.New("irgendwas")}
	wrapped := errors.Join(errors.New("kontext"), base)
	if !IsNetworkError(base) || !IsNetworkError(wrapped) {
		t.Error("typisierter Netzwerkfehler wird nicht erkannt")
	}
	if IsNetworkError(errors.New("ssh: unable to authenticate")) {
		t.Error("Auth-Fehler darf kein Netzwerkfehler sein")
	}
}

// TestConnectAutoWithoutKeysProbesReachability prüft den Fall ohne
// SSH-Schlüssel und ohne Agent: Statt sofort nach dem Passwort zu fragen,
// wird die Erreichbarkeit geprüft - ein ausgeschalteter Ziel-PC wird so
// direkt gemeldet (vorher kam der Fehler erst nach der Passworteingabe).
func TestConnectAutoWithoutKeysProbesReachability(t *testing.T) {
	setupKeyHome(t, 0)
	m := NewSSHManager(NewLogger())
	calls := 0
	m.dialTCP = func(ctx context.Context, network, addr string) (net.Conn, error) {
		calls++
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	}
	conn := Connection{ID: "nokey", Name: "OhneKey", Host: "192.0.2.60", Port: 22, User: "root", AuthType: AuthPassword}
	_, err := m.ConnectAuto(conn)
	if err == nil || !IsNetworkError(err) {
		t.Fatalf("erwartet Netzwerkfehler (direkte Meldung statt Passwortabfrage), bekommen %v", err)
	}
	if calls != 1 {
		t.Errorf("Erreichbarkeitsprüfungen = %d, erwartet 1", calls)
	}
}

// TestConnectAutoWithoutKeysReachableAsksPassword prüft dass ein
// erreichbarer Host ohne Schlüssel weiterhin zur Passwortabfrage führt
// (kein Netzwerkfehler).
func TestConnectAutoWithoutKeysReachableAsksPassword(t *testing.T) {
	setupKeyHome(t, 0)
	m := NewSSHManager(NewLogger())
	m.dialTCP = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c1, c2 := net.Pipe()
		go c2.Close()
		return c1, nil
	}
	conn := Connection{ID: "nokey2", Name: "OhneKey2", Host: "192.0.2.61", Port: 22, User: "root", AuthType: AuthPassword}
	_, err := m.ConnectAuto(conn)
	if err == nil || IsNetworkError(err) {
		t.Errorf("erreichbarer Host ohne Schlüssel: erwartet Nicht-Netzwerkfehler (-> Passwortabfrage), bekommen %v", err)
	}
}
