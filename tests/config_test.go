// Tests fuer die Konfigurationsverwaltung
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 18:15
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigNonExistent prueft ob eine nicht existierende Datei eine leere Config ergibt
func TestLoadConfigNonExistent(t *testing.T) {
	cfg, err := LoadConfig("/tmp/ssh-easy-test-nonexistent.json")
	if err != nil {
		t.Fatalf("Fehler beim Laden nicht existierender Config: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("Version erwartet 1, bekommen %d", cfg.Version)
	}
	if len(cfg.Connections) != 0 {
		t.Error("Keine Verbindungen erwartet")
	}
}

// TestSaveAndLoadConfig prueft ob Speichern und Laden funktioniert
func TestSaveAndLoadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test-config.json")

	// Konfiguration erstellen
	cfg := NewAppConfig()
	conn := NewConnection("Testserver", "192.168.1.100", 22, "admin", AuthPassword)
	conn.Tunnels = []TunnelConfig{
		{LocalPort: 3306, RemotePort: 3306, Enabled: true},
		{LocalPort: 8080, RemotePort: 80, Enabled: true},
	}
	cfg.Connections = append(cfg.Connections, conn)

	// Speichern
	if err := SaveConfig(path, &cfg); err != nil {
		t.Fatalf("Fehler beim Speichern: %v", err)
	}

	// Laden
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("Fehler beim Laden: %v", err)
	}

	// Pruefen
	if loaded.Version != 1 {
		t.Errorf("Version erwartet 1, bekommen %d", loaded.Version)
	}
	if len(loaded.Connections) != 1 {
		t.Fatalf("1 Verbindung erwartet, bekommen %d", len(loaded.Connections))
	}
	if loaded.Connections[0].Name != "Testserver" {
		t.Errorf("Name erwartet 'Testserver', bekommen '%s'", loaded.Connections[0].Name)
	}
	if len(loaded.Connections[0].Tunnels) != 2 {
		t.Errorf("2 Tunnel erwartet, bekommen %d", len(loaded.Connections[0].Tunnels))
	}
}

// TestLoadConfigEmptyFile prueft ob eine leere Datei eine leere Config ergibt
func TestLoadConfigEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty.json")
	os.WriteFile(path, []byte{}, 0600)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("Fehler bei leerer Datei: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("Version erwartet 1, bekommen %d", cfg.Version)
	}
}

// TestLoadConfigCorrupt prueft ob eine korrupte Datei einen Fehler erzeugt
func TestLoadConfigCorrupt(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "corrupt.json")
	os.WriteFile(path, []byte("{invalid json"), 0600)

	_, err := LoadConfig(path)
	if err == nil {
		t.Error("Korrupte Datei sollte einen Fehler erzeugen")
	}
}

// TestAddConnection prueft ob eine Verbindung hinzugefuegt wird
func TestAddConnection(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "add-test.json")

	conn := NewConnection("Server1", "10.0.0.1", 22, "user", AuthPassword)
	if err := AddConnection(path, conn); err != nil {
		t.Fatalf("Fehler beim Hinzufuegen: %v", err)
	}

	cfg, _ := LoadConfig(path)
	if len(cfg.Connections) != 1 {
		t.Fatalf("1 Verbindung erwartet, bekommen %d", len(cfg.Connections))
	}

	// Zweite Verbindung hinzufuegen
	conn2 := NewConnection("Server2", "10.0.0.2", 2222, "admin", AuthKey)
	conn2.KeyPath = "/home/user/.ssh/id_rsa"
	if err := AddConnection(path, conn2); err != nil {
		t.Fatalf("Fehler beim Hinzufuegen: %v", err)
	}

	cfg, _ = LoadConfig(path)
	if len(cfg.Connections) != 2 {
		t.Fatalf("2 Verbindungen erwartet, bekommen %d", len(cfg.Connections))
	}
}

// TestDeleteConnection prueft ob eine Verbindung geloescht wird
func TestDeleteConnection(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "delete-test.json")

	conn := NewConnection("Server", "10.0.0.1", 22, "user", AuthPassword)
	AddConnection(path, conn)

	if err := DeleteConnection(path, conn.ID); err != nil {
		t.Fatalf("Fehler beim Loeschen: %v", err)
	}

	cfg, _ := LoadConfig(path)
	if len(cfg.Connections) != 0 {
		t.Errorf("0 Verbindungen erwartet, bekommen %d", len(cfg.Connections))
	}
}

// TestDeleteConnectionNotFound prueft ob eine nicht existierende ID einen Fehler erzeugt
func TestDeleteConnectionNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "delete-notfound.json")

	cfg := NewAppConfig()
	SaveConfig(path, &cfg)

	err := DeleteConnection(path, "nicht-vorhanden")
	if err == nil {
		t.Error("Loeschen einer nicht existierenden Verbindung sollte einen Fehler erzeugen")
	}
}

// TestUpdateConnection prueft ob eine Verbindung aktualisiert wird
func TestUpdateConnection(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "update-test.json")

	conn := NewConnection("Alt", "10.0.0.1", 22, "user", AuthPassword)
	AddConnection(path, conn)

	// Verbindung aktualisieren
	conn.Name = "Neu"
	conn.Host = "10.0.0.2"
	if err := UpdateConnection(path, conn); err != nil {
		t.Fatalf("Fehler beim Aktualisieren: %v", err)
	}

	cfg, _ := LoadConfig(path)
	if cfg.Connections[0].Name != "Neu" {
		t.Errorf("Name erwartet 'Neu', bekommen '%s'", cfg.Connections[0].Name)
	}
	if cfg.Connections[0].Host != "10.0.0.2" {
		t.Errorf("Host erwartet '10.0.0.2', bekommen '%s'", cfg.Connections[0].Host)
	}
}

// TestUpdateConnectionNotFound prueft ob eine nicht existierende ID einen Fehler erzeugt
func TestUpdateConnectionNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "update-notfound.json")

	cfg := NewAppConfig()
	SaveConfig(path, &cfg)

	conn := NewConnection("Test", "10.0.0.1", 22, "user", AuthPassword)
	err := UpdateConnection(path, conn)
	if err == nil {
		t.Error("Aktualisieren einer nicht existierenden Verbindung sollte einen Fehler erzeugen")
	}
}

// TestSaveConfigAtomicWrite prueft ob atomares Schreiben funktioniert
func TestSaveConfigAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "atomic-test.json")

	cfg := NewAppConfig()
	conn := NewConnection("Test", "10.0.0.1", 22, "user", AuthPassword)
	cfg.Connections = append(cfg.Connections, conn)

	if err := SaveConfig(path, &cfg); err != nil {
		t.Fatalf("Fehler beim Speichern: %v", err)
	}

	// Datei sollte existieren
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Konfigurationsdatei sollte existieren")
	}

	// Keine Temp-Dateien sollten uebrig sein
	entries, _ := os.ReadDir(tmpDir)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" {
			t.Errorf("Temp-Datei sollte nicht uebrig sein: %s", entry.Name())
		}
	}
}

// TestSaveConfigPermissions prueft die Dateiberechtigungen
func TestSaveConfigPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "perm-test.json")

	cfg := NewAppConfig()
	SaveConfig(path, &cfg)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Dateiinfo konnte nicht gelesen werden: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("Berechtigung erwartet 0600, bekommen %o", perm)
	}
}
