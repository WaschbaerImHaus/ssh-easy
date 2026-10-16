// Paket main - Verbindungsaufbau mit kurzem Timeout und DNS-Fallback
//
// Zwei Probleme werden hier gelöst:
//
//  1. Ausgeschalteter Ziel-PC: Früher wurde jeder SSH-Schlüssel einzeln mit
//     10 s Timeout probiert - bei N Schlüsseln wartete man N × 10 s. Jetzt gilt
//     beim ersten Verbindungsaufbau ein Timeout von 5 s, und ein Netzwerk-
//     fehler beendet die Schlüsselsuche sofort (siehe ssh_manager.go).
//
//  2. Defekter DNS-Server des Netzwerks: Scheitert die Namensauflösung über
//     den System-DNS, wird nacheinander über öffentliche DNS-Server aufgelöst
//     (1.1.1.1, 1.0.0.1, 8.8.8.8, 8.8.4.4). Der zuletzt erfolgreiche Server
//     wird beim nächsten Mal zuerst gefragt (Rotation).
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	// InitialConnectTimeout - Frist für den TCP-Aufbau beim ersten Verbinden.
	// Kurz gehalten, damit ein ausgeschalteter Ziel-PC schnell gemeldet wird.
	InitialConnectTimeout = 5 * time.Second
	// ReconnectDialTimeout - Frist beim automatischen Reconnect (im Hintergrund)
	ReconnectDialTimeout = 10 * time.Second
	// sshHandshakeTimeout - Frist für SSH-Handshake inkl. Anmeldung. Schützt vor
	// Servern, die die TCP-Verbindung annehmen, aber nie antworten.
	sshHandshakeTimeout = 20 * time.Second
	// systemDNSTimeout - Frist für die Auflösung über den System-DNS
	systemDNSTimeout = 2 * time.Second
	// fallbackDNSTimeout - Frist pro öffentlichem DNS-Server (antworten
	// normalerweise in < 100 ms; 4 Server zusammen höchstens 4 s)
	fallbackDNSTimeout = time.Second
)

// fallbackDNSServers sind die öffentlichen DNS-Server für den Fallback
// (Cloudflare und Google). Hinweis: Googles zweiter Server ist 8.8.4.4.
var fallbackDNSServers = []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4"}

// networkError kennzeichnet Fehler beim Erreichen des Servers (DNS, TCP,
// Handshake-Timeout) - im Gegensatz zu Anmeldefehlern. Bei Netzwerkfehlern
// ist das Ausprobieren weiterer Schlüssel sinnlos.
type networkError struct {
	err error
}

// Error liefert die ursprüngliche Fehlermeldung.
//
// @return string - Fehlermeldung
// @date   2026-10-16 10:00
func (e *networkError) Error() string { return e.err.Error() }

// Unwrap ermöglicht errors.Is/errors.As auf den ursprünglichen Fehler.
//
// @return error - Ursprünglicher Fehler
// @date   2026-10-16 10:00
func (e *networkError) Unwrap() error { return e.err }

// untrustedHostError meldet: Die Adresse stammt vom öffentlichen Fallback-
// DNS (unverschlüsselt, fälschbar) und der Host-Key ist noch unbekannt. Dann
// wird NICHT automatisch vertraut (kein Trust-on-first-use). Wird in einen
// networkError verpackt: keine Passwortabfrage, Schlüsselsuche bricht ab.
type untrustedHostError struct {
	// Hostname wie an den Host-Key-Callback übergeben ("host:port")
	Hostname string
}

// Error liefert die (deutsche) Fehlermeldung für das Log. Die TUI zeigt
// stattdessen den übersetzten Text txtErrUntrustedHost.
//
// @return string - Fehlermeldung
// @date   2026-10-16 11:00
func (e *untrustedHostError) Error() string {
	return fmt.Sprintf("unbekannter Host-Key für %s und Adresse nur über öffentlichen DNS ermittelt - nicht automatisch vertraut", e.Hostname)
}

// internalNameSuffixes sind Namensendungen, die nur im lokalen Netz
// existieren. Solche Namen gehen nie an öffentliche DNS-Server.
var internalNameSuffixes = []string{
	".local", ".lan", ".internal", ".intranet", ".home.arpa", ".home",
	".localdomain", ".corp", ".private", ".fritz.box",
}

// isInternalHostName erkennt Namen, die nur im lokalen Netz auflösbar sind:
// Namen ohne Punkt (z.B. "nas", Windows-Rechnernamen) und bekannte interne
// Endungen. Öffentliche DNS-Server kennen sie nicht - eine Anfrage dort
// würde nur interne Namen preisgeben.
//
// @param host - Hostname
// @return bool - true bei internem Namen
// @date   2026-10-16 11:00
func isInternalHostName(host string) bool {
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if !strings.Contains(name, ".") {
		return true
	}
	if name == "fritz.box" {
		return true
	}
	for _, suffix := range internalNameSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// tcpDialFunc baut eine TCP-Verbindung auf (austauschbar für Tests).
type tcpDialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// defaultTCPDial nutzt den Standard-Dialer von Go.
var defaultTCPDial tcpDialFunc = (&net.Dialer{}).DialContext

// hostResolver löst Hostnamen auf - erst über den System-DNS, bei Fehlern
// rotierend über die öffentlichen Fallback-Server.
type hostResolver struct {
	// Mutex für next
	mu sync.Mutex
	// Index des Fallback-Servers, mit dem begonnen wird (zuletzt erfolgreich)
	next int
	// Fallback-Server
	servers []string
	// Auflösung über den System-DNS (austauschbar für Tests).
	// Windows: inkl. LLMNR/NetBIOS-Namen aus dem lokalen Netz.
	systemLookup func(ctx context.Context, host string) ([]string, error)
	// Auflösung über einen bestimmten DNS-Server (austauschbar für Tests)
	serverLookup func(ctx context.Context, server, host string) ([]string, error)
}

// newHostResolver erstellt einen Resolver mit den Standard-Fallback-Servern.
//
// @return *hostResolver - Neuer Resolver
// @date   2026-10-16 10:00
func newHostResolver() *hostResolver {
	return &hostResolver{
		servers:      fallbackDNSServers,
		systemLookup: net.DefaultResolver.LookupHost,
		serverLookup: lookupViaServer,
	}
}

// lookupViaServer löst einen Namen gezielt über einen DNS-Server auf.
// Nutzt den Go-eigenen Resolver, dessen Netzwerkzugriff auf server:53
// umgebogen wird.
//
// @param ctx - Kontext mit Frist
// @param server - IP des DNS-Servers
// @param host - Aufzulösender Name
// @return []string - IP-Adressen
// @return error - Fehler der Auflösung
// @date   2026-10-16 10:00
func lookupViaServer(ctx context.Context, server, host string) ([]string, error) {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort(server, "53"))
		},
	}
	return resolver.LookupHost(ctx, host)
}

// resolve liefert die IP-Adressen eines Hosts. IP-Adressen werden direkt
// zurückgegeben. Funktioniert der System-DNS nicht (Timeout, Serverfehler),
// werden die Fallback-Server der Reihe nach gefragt - beginnend beim zuletzt
// erfolgreichen. Meldet der System-DNS "Name unbekannt", gibt es keinen
// Fallback.
//
// Interne Namen (siehe isInternalHostName) gehen nie an öffentliche Server.
//
// @param ctx - Übergeordneter Kontext
// @param host - Hostname oder IP-Adresse
// @return []string - IP-Adressen (mindestens eine)
// @return bool - true wenn die Adresse vom öffentlichen Fallback-DNS stammt
// @return error - Netzwerkfehler, wenn kein Server den Namen auflösen konnte
// @date   2026-10-16 11:00
func (r *hostResolver) resolve(ctx context.Context, host string) ([]string, bool, error) {
	host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(host), "["), "]")
	if host == "" {
		return nil, false, &networkError{err: errors.New("kein Host angegeben")}
	}
	if net.ParseIP(host) != nil {
		return []string{host}, false, nil
	}

	// 1. System-DNS (vom Netzwerk vorgegeben)
	sysCtx, cancel := context.WithTimeout(ctx, systemDNSTimeout)
	addrs, sysErr := r.systemLookup(sysCtx, host)
	cancel()
	if sysErr == nil && len(addrs) > 0 {
		return addrs, false, nil
	}
	if sysErr == nil {
		sysErr = fmt.Errorf("lookup %s: keine Adresse", host)
	}
	// NXDOMAIN: Der DNS des Netzwerks funktioniert, kennt den Namen aber
	// nicht. Dann NICHT öffentliche Server fragen - der Name ist meist intern
	// (Datenschutz) und bei Split-Horizon-DNS käme eine falsche Adresse heraus.
	var dnsErr *net.DNSError
	if errors.As(sysErr, &dnsErr) && dnsErr.IsNotFound {
		return nil, false, &networkError{err: sysErr}
	}
	// Interne Namen kennen öffentliche Server nicht - nicht preisgeben
	if isInternalHostName(host) {
		return nil, false, &networkError{err: sysErr}
	}

	// 2. Fallback-Server rotierend durchprobieren
	r.mu.Lock()
	start := r.next
	r.mu.Unlock()
	for i := 0; i < len(r.servers); i++ {
		idx := (start + i) % len(r.servers)
		srvCtx, cancel := context.WithTimeout(ctx, fallbackDNSTimeout)
		addrs, err := r.serverLookup(srvCtx, r.servers[idx], host)
		cancel()
		if err == nil && len(addrs) > 0 {
			r.mu.Lock()
			r.next = idx
			r.mu.Unlock()
			return addrs, true, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, false, &networkError{err: sysErr}
}

// orderAddresses sortiert IPv4-Adressen vor IPv6-Adressen. In vielen
// Heim- und Büronetzen ist IPv6 nur halb eingerichtet; mit IPv4 zuerst
// kostet eine kaputte IPv6-Route keine Wartezeit.
//
// @param addrs - Aufgelöste Adressen
// @return []string - Sortierte Kopie
// @date   2026-10-16 10:00
func orderAddresses(addrs []string) []string {
	ordered := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
			ordered = append(ordered, a)
		}
	}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip == nil || ip.To4() == nil {
			ordered = append(ordered, a)
		}
	}
	return ordered
}

// dialResolved löst den Host auf und baut eine TCP-Verbindung auf. Die
// Frist gilt für alle Adressen zusammen; jede Adresse bekommt einen
// gleichmäßigen Anteil der Restzeit (wie Gos eigener Dialer).
//
// @param ctx - Übergeordneter Kontext
// @param resolver - Resolver mit DNS-Fallback
// @param dial - TCP-Dialer
// @param host - Hostname oder IP
// @param port - Zielport
// @param timeout - Gesamtfrist für den TCP-Aufbau
// @return net.Conn - Aufgebaute Verbindung
// @return bool - true wenn die Adresse vom öffentlichen Fallback-DNS stammt
// @return error - *networkError bei Fehlschlag
// @date   2026-10-16 11:00
func dialResolved(ctx context.Context, resolver *hostResolver, dial tcpDialFunc, host string, port int, timeout time.Duration) (net.Conn, bool, error) {
	addrs, viaFallback, err := resolver.resolve(ctx, host)
	if err != nil {
		return nil, false, err
	}
	addrs = orderAddresses(addrs)

	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deadline, _ := dialCtx.Deadline()

	var lastErr error
	for i, addr := range addrs {
		// Restzeit gleichmäßig auf die verbleibenden Adressen verteilen
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		share := remaining / time.Duration(len(addrs)-i)
		attemptCtx, attemptCancel := context.WithTimeout(dialCtx, share)
		conn, err := dial(attemptCtx, "tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
		attemptCancel()
		if err == nil {
			return conn, viaFallback, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("dial tcp %s: i/o timeout", net.JoinHostPort(host, strconv.Itoa(port)))
	}
	return nil, false, &networkError{err: lastErr}
}

// dialSSHClient baut eine SSH-Verbindung auf: Namensauflösung mit Fallback,
// TCP mit Frist und SSH-Handshake mit eigener Frist.
// Dem Host-Key-Callback wird "host:port" übergeben (wie bei ssh.Dial), damit
// known_hosts-Einträge weiterhin über den Hostnamen gefunden werden.
//
// @param resolver - Resolver mit DNS-Fallback
// @param dial - TCP-Dialer
// @param host - Hostname oder IP
// @param port - SSH-Port
// @param timeout - Frist für den TCP-Aufbau
// @param newConfig - Liefert die SSH-Konfiguration; viaFallback = Adresse
//
//	stammt vom öffentlichen DNS (dann keinem unbekannten Host-Key vertrauen)
//
// @return *ssh.Client - Verbundener Client
// @return error - Fehler (Netzwerkfehler als *networkError)
// @date   2026-10-16 11:00
func dialSSHClient(resolver *hostResolver, dial tcpDialFunc, host string, port int, timeout time.Duration, newConfig func(viaFallback bool) (*ssh.ClientConfig, error)) (*ssh.Client, error) {
	conn, viaFallback, err := dialResolved(context.Background(), resolver, dial, host, port, timeout)
	if err != nil {
		return nil, err
	}
	config, err := newConfig(viaFallback)
	if err != nil {
		conn.Close()
		return nil, err
	}

	// Handshake-Frist: hängt der Server, bricht der Lesezugriff ab
	_ = conn.SetDeadline(time.Now().Add(sshHandshakeTimeout))
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		var untrusted *untrustedHostError
		if errors.Is(err, os.ErrDeadlineExceeded) || errors.As(err, &untrusted) {
			return nil, &networkError{err: err}
		}
		return nil, err
	}
	// Frist wieder aufheben - die Sitzung darf beliebig lange laufen
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(clientConn, chans, reqs), nil
}
