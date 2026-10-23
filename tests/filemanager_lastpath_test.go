// Paket main - Tests: letzter Pfad pro Verbindung im Datei-Manager (Build 77)
//
// Nutzerwunsch 2026-10-23: "merke dir pro verbindung in welchem pfad man
// dort zuletzt war. ist der pfad nicht mehr vorhanden dann starte wieder im
// home." Gespeichert wird in der Konfiguration (AppConfig), nicht in der
// Verbindung selbst - so landet der Pfad auch nicht im Export.
//
// @author Kurt Ingwer
// @date   2026-10-23 08:40
package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// setLastPaths legt gemerkte Pfade in der Konfiguration des Modells ab.
func setLastPaths(t *testing.T, m AppModel, paths map[string]string) {
	t.Helper()
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.FileManagerLastPaths = paths
	if err := SaveConfig(m.configPath, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	m.configCache.Invalidate()
}

// lastPaths liest die gemerkten Pfade aus der Konfiguration.
func lastPaths(t *testing.T, m AppModel) map[string]string {
	t.Helper()
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg.FileManagerLastPaths
}

// reopenFileManager öffnet den Datei-Manager erneut mit beiden Verbindungen.
func reopenFileManager(t *testing.T, m AppModel) AppModel {
	t.Helper()
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	m = pickSide(t, m, "Rechts")
	if m.state != ViewFileManager {
		t.Fatalf("state = %v, erwartet ViewFileManager (Fehler: %q)", m.state, m.fm.message)
	}
	return m
}

// TestFileManagerRemembersLastPath - nach einem Verzeichniswechsel steht der
// Pfad pro Verbindung in der Konfiguration; beim nächsten Öffnen startet das
// Fenster dort statt im Home.
func TestFileManagerRemembersLastPath(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "projekte")
	m = pressRun(t, m, "enter")
	if got := m.fm.panels[0].path; got != "/home/kurt/projekte" {
		t.Fatalf("links in %q, erwartet /home/kurt/projekte", got)
	}
	paths := lastPaths(t, m)
	if paths["id-0"] != "/home/kurt/projekte" {
		t.Errorf("gemerkt links = %q, erwartet /home/kurt/projekte", paths["id-0"])
	}
	if paths["id-1"] != "/home/kurt" {
		t.Errorf("gemerkt rechts = %q, erwartet /home/kurt (Startverzeichnis)", paths["id-1"])
	}

	m = pressRun(t, m, "q")
	m = reopenFileManager(t, m)
	if got := m.fm.panels[0].path; got != "/home/kurt/projekte" {
		t.Errorf("nach erneutem Öffnen links in %q, erwartet /home/kurt/projekte", got)
	}
	if got := m.fm.panels[1].path; got != "/home/kurt" {
		t.Errorf("rechts in %q, erwartet /home/kurt", got)
	}
}

// TestFileManagerLastPathGoneStartsHome - gibt es den gemerkten Pfad nicht
// mehr (oder ist er nicht lesbar), startet das Fenster im Home - ohne
// Fehlermeldung - und merkt sich danach das Home.
func TestFileManagerLastPathGoneStartsHome(t *testing.T) {
	cases := map[string]string{
		"gelöscht":        "/home/kurt/weg",
		"keine Rechte":    "/home/kurt/geheim",
		"relativer Pfad":  "projekte",
		"leer":            "",
		"mit Steuerzeich": "/home/kurt/\x1b[2J",
	}
	for name, stored := range cases {
		m := newListTestModel(t, "Links", "Rechts")
		rec := newConnectRecorder()
		m.fmConnect = rec.connect
		m.termWidth, m.termHeight = 100, 30
		setLastPaths(t, m, map[string]string{"id-0": stored})
		m = reopenFileManager(t, m)
		if got := m.fm.panels[0].path; got != "/home/kurt" {
			t.Errorf("%s: links in %q, erwartet Home /home/kurt", name, got)
		}
		if m.fm.message != "" {
			t.Errorf("%s: Meldung %q, erwartet keine", name, m.fm.message)
		}
		if got := lastPaths(t, m)["id-0"]; got != "/home/kurt" {
			t.Errorf("%s: gemerkt %q, erwartet /home/kurt", name, got)
		}
	}
}

// TestFileManagerLastPathPerConnection - jede Verbindung hat ihren eigenen
// Pfad; ein Wechsel rechts ändert den gemerkten Pfad links nicht.
func TestFileManagerLastPathPerConnection(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = pressRun(t, m, "tab")
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "enter")
	paths := lastPaths(t, m)
	if paths["id-1"] != "/home" || paths["id-0"] != "/home/kurt" {
		t.Errorf("gemerkt = %v, erwartet id-0=/home/kurt, id-1=/home", paths)
	}
}

// TestFileManagerLastPathKeyForLocalPC - der Lokale PC hat eine interne ID
// mit NUL-Zeichen; in der Konfiguration steht er unter "local-pc".
func TestFileManagerLastPathKeyForLocalPC(t *testing.T) {
	if got := fmLastPathKey(fmLocalConnID); got != "local-pc" {
		t.Errorf("fmLastPathKey(Lokaler PC) = %q, erwartet local-pc", got)
	}
	if got := fmLastPathKey("abc-123"); got != "abc-123" {
		t.Errorf("fmLastPathKey(abc-123) = %q", got)
	}
}

// TestExportHasNoLastPaths - gemerkte Pfade gehören nicht in den Export.
func TestExportHasNoLastPaths(t *testing.T) {
	m, _ := openFileManagerFor(t)
	cfg, _ := LoadConfig(m.configPath)
	if len(cfg.FileManagerLastPaths) == 0 {
		t.Fatal("Vorbedingung: es sind keine Pfade gemerkt")
	}
	selected := map[string]bool{}
	for _, c := range cfg.Connections {
		selected[c.ID] = true
	}
	data, err := json.Marshal(buildExportConfig(cfg.Connections, selected))
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if strings.Contains(string(data), "/home/kurt") || strings.Contains(string(data), "last_path") {
		t.Errorf("Export enthält gemerkte Pfade: %s", data)
	}
}
