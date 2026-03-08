// Paket main - Tests fuer den SSH-Manager
//
// Testet SSHManager-Erstellung, Verbindungsverwaltung und Status-Abfragen.
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 22:00
package main

import (
	"testing"
)

// TestNewSSHManager prueft die korrekte Initialisierung des SSHManagers.
func TestNewSSHManager(t *testing.T) {
	logger := NewLogger()
	manager := NewSSHManager(logger)

	if manager == nil {
		t.Fatal("NewSSHManager sollte nicht nil zurueckgeben")
	}

	if manager.logger != logger {
		t.Error("Logger wurde nicht korrekt gesetzt")
	}
}

// TestSSHManagerIsConnectedEmpty prueft IsConnected ohne aktive Verbindungen.
func TestSSHManagerIsConnectedEmpty(t *testing.T) {
	logger := NewLogger()
	manager := NewSSHManager(logger)

	if manager.IsConnected("nicht-vorhanden") {
		t.Error("IsConnected sollte false fuer unbekannte ID zurueckgeben")
	}
}

// TestSSHManagerGetStatusEmpty prueft GetStatus ohne aktive Verbindungen.
func TestSSHManagerGetStatusEmpty(t *testing.T) {
	logger := NewLogger()
	manager := NewSSHManager(logger)

	status, exists := manager.GetStatus("nicht-vorhanden")
	if exists {
		t.Error("GetStatus sollte false fuer unbekannte ID zurueckgeben")
	}
	if status != nil {
		t.Error("Status sollte nil fuer unbekannte ID sein")
	}
}

// TestSSHManagerDisconnectEmpty prueft Disconnect ohne aktive Verbindungen (kein Panic).
func TestSSHManagerDisconnectEmpty(t *testing.T) {
	logger := NewLogger()
	manager := NewSSHManager(logger)

	// Sollte keinen Panic ausloesen
	manager.Disconnect("nicht-vorhanden")
}

// TestSSHManagerDisconnectAllEmpty prueft DisconnectAll ohne Verbindungen (kein Panic).
func TestSSHManagerDisconnectAllEmpty(t *testing.T) {
	logger := NewLogger()
	manager := NewSSHManager(logger)

	// Sollte keinen Panic ausloesen
	manager.DisconnectAll()
}

// TestSSHManagerConnectInvalidHost prueft Connect mit ungueltigem Host.
func TestSSHManagerConnectInvalidHost(t *testing.T) {
	logger := NewLogger()
	manager := NewSSHManager(logger)

	conn := Connection{
		ID:       "test-1",
		Name:     "Test",
		Host:     "192.0.2.1", // TEST-NET - nicht erreichbar
		Port:     22,
		User:     "testuser",
		AuthType: AuthPassword,
	}

	// Connect mit Timeout - sollte fehlschlagen
	_, err := manager.Connect(conn, "testpass")
	if err == nil {
		t.Error("Connect sollte bei unerreichbarem Host fehlschlagen")
		manager.Disconnect("test-1")
	}
}
