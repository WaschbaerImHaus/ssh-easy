// Paket main - Tests fuer den ConfigCache
//
// Testet Lazy-Loading und Cache-Invalidierung.
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 22:00
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewConfigCache prueft die korrekte Initialisierung.
func TestNewConfigCache(t *testing.T) {
	cache := NewConfigCache("/tmp/test-cache-config.json")
	if cache == nil {
		t.Fatal("NewConfigCache sollte nicht nil zurueckgeben")
	}
}

// TestConfigCacheGetNonExistent prueft Get mit nicht existierender Datei.
func TestConfigCacheGetNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nicht-vorhanden.json")

	cache := NewConfigCache(path)
	cfg, err := cache.Get()
	if err != nil {
		t.Fatalf("Get sollte bei nicht existierender Datei keinen Fehler werfen: %v", err)
	}
	if cfg == nil {
		t.Fatal("Get sollte eine leere Config zurueckgeben")
	}
}

// TestConfigCacheGetValid prueft Get mit gueltige Config-Datei.
func TestConfigCacheGetValid(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.json")

	// Konfiguration erstellen
	cfg := NewAppConfig()
	conn := NewConnection("Test", "192.168.1.1", 22, "root", AuthPassword)
	cfg.Connections = append(cfg.Connections, conn)
	if err := SaveConfig(path, &cfg); err != nil {
		t.Fatalf("SaveConfig fehlgeschlagen: %v", err)
	}

	cache := NewConfigCache(path)
	loaded, err := cache.Get()
	if err != nil {
		t.Fatalf("Get fehlgeschlagen: %v", err)
	}
	if len(loaded.Connections) != 1 {
		t.Errorf("Erwartet 1 Verbindung, erhalten: %d", len(loaded.Connections))
	}
	if loaded.Connections[0].Name != "Test" {
		t.Errorf("Erwartet Name 'Test', erhalten: %s", loaded.Connections[0].Name)
	}
}

// TestConfigCacheInvalidate prueft die Cache-Invalidierung.
func TestConfigCacheInvalidate(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.json")

	cfg := NewAppConfig()
	if err := SaveConfig(path, &cfg); err != nil {
		t.Fatalf("SaveConfig fehlgeschlagen: %v", err)
	}

	cache := NewConfigCache(path)

	// Ersten Aufruf durchfuehren
	_, err := cache.Get()
	if err != nil {
		t.Fatalf("Erster Get fehlgeschlagen: %v", err)
	}

	// Invalidieren
	cache.Invalidate()

	// Datei aendern
	cfg.Connections = append(cfg.Connections, NewConnection("Neu", "10.0.0.1", 22, "user", AuthPassword))
	if err := SaveConfig(path, &cfg); err != nil {
		t.Fatalf("SaveConfig fehlgeschlagen: %v", err)
	}

	// Erneut laden - sollte aktualisierte Daten haben
	loaded, err := cache.Get()
	if err != nil {
		t.Fatalf("Zweiter Get fehlgeschlagen: %v", err)
	}
	if len(loaded.Connections) != 1 {
		t.Errorf("Erwartet 1 Verbindung nach Invalidate+Reload, erhalten: %d", len(loaded.Connections))
	}
}

// TestConfigCacheConcurrentAccess prueft Thread-Sicherheit.
func TestConfigCacheConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.json")

	cfg := NewAppConfig()
	if err := SaveConfig(path, &cfg); err != nil {
		t.Fatalf("SaveConfig fehlgeschlagen: %v", err)
	}

	cache := NewConfigCache(path)

	// Paralleler Zugriff
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = cache.Get()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestConfigCacheCorruptFile prueft Verhalten bei beschaedigter Datei.
func TestConfigCacheCorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "corrupt.json")

	if err := os.WriteFile(path, []byte("{invalid json}"), 0600); err != nil {
		t.Fatalf("Schreiben fehlgeschlagen: %v", err)
	}

	cache := NewConfigCache(path)
	_, err := cache.Get()
	if err == nil {
		t.Error("Get sollte bei beschaedigter Datei einen Fehler werfen")
	}
}
