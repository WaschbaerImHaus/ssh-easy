// Paket main - Konfigurationsverwaltung fuer ssh-easy
//
// Laden und Speichern der SSH-Verbindungen als JSON-Datei.
// Verwendet atomares Schreiben (Temp-Datei + Rename) um Datenverlust zu vermeiden.
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 18:15
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// ConfigCache implementiert Lazy-Loading der Konfiguration.
// Laedt die Datei nur neu wenn sie sich geaendert hat (Timestamp-basiert).
type ConfigCache struct {
	// Mutex fuer thread-sicheren Zugriff
	mu sync.RWMutex
	// Pfad zur Konfigurationsdatei
	path string
	// Gecachte Konfiguration
	config *AppConfig
	// Zeitstempel der letzten Dateimodifikation beim Laden
	lastModTime time.Time
}

// NewConfigCache erstellt einen neuen Config-Cache.
//
// @param path - Pfad zur Konfigurationsdatei
// @return *ConfigCache - Neuer Cache
// @date   2026-03-07 21:00
func NewConfigCache(path string) *ConfigCache {
	return &ConfigCache{path: path}
}

// Get gibt die aktuelle Konfiguration zurueck.
// Laedt nur neu von Disk wenn sich die Datei geaendert hat.
//
// @return *AppConfig - Aktuelle Konfiguration
// @return error - Fehler beim Laden
// @date   2026-03-07 21:00
func (c *ConfigCache) Get() (*AppConfig, error) {
	c.mu.RLock()
	// Pruefen ob Neuladen noetig ist
	needsReload := c.config == nil
	if !needsReload {
		info, err := os.Stat(c.path)
		if err == nil && info.ModTime().After(c.lastModTime) {
			needsReload = true
		}
	}
	c.mu.RUnlock()

	if !needsReload {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.config, nil
	}

	// Neuladen noetig
	c.mu.Lock()
	defer c.mu.Unlock()

	cfg, err := LoadConfig(c.path)
	if err != nil {
		return nil, err
	}

	c.config = cfg
	info, err := os.Stat(c.path)
	if err == nil {
		c.lastModTime = info.ModTime()
	}

	return c.config, nil
}

// Invalidate markiert den Cache als ungueltig, sodass beim naechsten
// Get() die Datei neu geladen wird.
//
// @date   2026-03-07 21:00
func (c *ConfigCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = nil
	c.lastModTime = time.Time{}
}

// GetConfigDir gibt den Pfad zum Konfigurationsverzeichnis zurueck.
// Unter Linux: ~/.ssh-easy/
// Unter Windows: %USERPROFILE%/.ssh-easy/
//
// @return string - Pfad zum Konfigurationsverzeichnis
// @return error - Fehler bei der Ermittlung des Home-Verzeichnisses
// @date   2026-03-07 18:15
func GetConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("Home-Verzeichnis konnte nicht ermittelt werden: %w", err)
	}
	return filepath.Join(home, ".ssh-easy"), nil
}

// GetConfigPath gibt den vollstaendigen Pfad zur Konfigurationsdatei zurueck
//
// @return string - Pfad zur connections.json
// @return error - Fehler bei der Pfadermittlung
// @date   2026-03-07 18:15
func GetConfigPath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "connections.json"), nil
}

// EnsureConfigDir stellt sicher, dass das Konfigurationsverzeichnis existiert.
// Erstellt es mit Berechtigung 0700 wenn noetig.
//
// @return error - Fehler beim Erstellen des Verzeichnisses
// @date   2026-03-07 18:15
func EnsureConfigDir() error {
	dir, err := GetConfigDir()
	if err != nil {
		return err
	}

	// Berechtigung 0700: nur Besitzer darf lesen/schreiben/ausfuehren
	perm := os.FileMode(0700)
	if runtime.GOOS == "windows" {
		perm = os.FileMode(0755)
	}

	return os.MkdirAll(dir, perm)
}

// LoadConfig laedt die Konfiguration aus der JSON-Datei.
// Wenn die Datei nicht existiert, wird eine leere Konfiguration zurueckgegeben.
//
// @param path - Pfad zur Konfigurationsdatei
// @return *AppConfig - Geladene Konfiguration
// @return error - Fehler beim Lesen oder Parsen
// @date   2026-03-07 18:15
func LoadConfig(path string) (*AppConfig, error) {
	// Datei lesen
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Datei existiert nicht - leere Konfiguration zurueckgeben
			cfg := NewAppConfig()
			return &cfg, nil
		}
		return nil, fmt.Errorf("Konfiguration konnte nicht gelesen werden: %w", err)
	}

	// Leere Datei behandeln
	if len(data) == 0 {
		cfg := NewAppConfig()
		return &cfg, nil
	}

	// JSON parsen
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("Konfiguration konnte nicht geparst werden: %w", err)
	}

	// Version pruefen
	if cfg.Version == 0 {
		cfg.Version = 1
	}

	return &cfg, nil
}

// SaveConfig speichert die Konfiguration als JSON-Datei.
// Verwendet atomares Schreiben: zuerst in temporaere Datei schreiben,
// dann per Rename ueberschreiben. Verhindert Datenverlust bei Absturz.
//
// @param path - Pfad zur Konfigurationsdatei
// @param cfg - Zu speichernde Konfiguration
// @return error - Fehler beim Schreiben
// @date   2026-03-07 18:15
func SaveConfig(path string, cfg *AppConfig) error {
	// JSON mit Einrueckung formatieren (lesbar fuer Menschen)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("Konfiguration konnte nicht serialisiert werden: %w", err)
	}

	// Verzeichnis sicherstellen
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("Verzeichnis konnte nicht erstellt werden: %w", err)
	}

	// Atomares Schreiben: Temp-Datei im gleichen Verzeichnis erstellen
	tmpFile, err := os.CreateTemp(dir, "ssh-easy-*.tmp")
	if err != nil {
		return fmt.Errorf("Temporaere Datei konnte nicht erstellt werden: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Aufraumen bei Fehler
	defer func() {
		// Wenn die Temp-Datei noch existiert (Fehler aufgetreten), loeschen
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			os.Remove(tmpPath)
		}
	}()

	// Daten in Temp-Datei schreiben
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("Daten konnten nicht geschrieben werden: %w", err)
	}

	// Datei schliessen und sicherstellen, dass Daten auf Disk sind
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("Daten konnten nicht synchronisiert werden: %w", err)
	}
	tmpFile.Close()

	// Berechtigung setzen (nur Besitzer darf lesen/schreiben)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0600); err != nil {
			return fmt.Errorf("Berechtigung konnte nicht gesetzt werden: %w", err)
		}
	}

	// Atomarer Rename: Temp-Datei -> Zieldatei
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("Konfiguration konnte nicht gespeichert werden: %w", err)
	}

	return nil
}

// AddConnection fuegt eine neue Verbindung zur Konfiguration hinzu und speichert.
//
// @param path - Pfad zur Konfigurationsdatei
// @param conn Connection - Die neue Verbindung
// @return error - Fehler beim Speichern
// @date   2026-03-07 18:15
func AddConnection(path string, conn Connection) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}
	cfg.Connections = append(cfg.Connections, conn)
	return SaveConfig(path, cfg)
}

// DeleteConnection entfernt eine Verbindung anhand ihrer ID und speichert.
//
// @param path - Pfad zur Konfigurationsdatei
// @param id string - ID der zu loeschenden Verbindung
// @return error - Fehler beim Speichern
// @date   2026-03-07 18:15
func DeleteConnection(path string, id string) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}

	// Verbindung suchen und entfernen
	found := false
	newConns := make([]Connection, 0, len(cfg.Connections))
	for _, c := range cfg.Connections {
		if c.ID == id {
			found = true
			continue
		}
		newConns = append(newConns, c)
	}

	if !found {
		return fmt.Errorf("Verbindung mit ID %s nicht gefunden", id)
	}

	cfg.Connections = newConns
	return SaveConfig(path, cfg)
}

// UpdateConnection aktualisiert eine bestehende Verbindung und speichert.
//
// @param path - Pfad zur Konfigurationsdatei
// @param conn Connection - Die aktualisierte Verbindung
// @return error - Fehler beim Speichern
// @date   2026-03-07 18:15
func UpdateConnection(path string, conn Connection) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}

	// Verbindung suchen und ersetzen
	found := false
	for i, c := range cfg.Connections {
		if c.ID == conn.ID {
			cfg.Connections[i] = conn
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("Verbindung mit ID %s nicht gefunden", conn.ID)
	}

	return SaveConfig(path, cfg)
}
