// Paket main - Tests: Konfiguration nur über updateConfig schreiben (Build 87)
//
// Hintergrund: Während des Beendens (Signal-Watcher, Hauptablauf) und der SSH-
// Sitzung schreiben mehrere Goroutinen die connections.json. Wer die Datei mit
// LoadConfig + SaveConfig ohne configUpdateMu ändert, kann eine gleichzeitige
// Änderung überschreiben (Lost Update). Betroffen waren vier Stellen:
// toggleAutoUpdate (updater.go), changeFontSize (console_font.go),
// persistLanguage (tui_language.go), persistFMSettings (tui_filemanager_menu.go).
//
// Prüfverfahren (deterministisch): Der Test hält configUpdateMu selbst und
// startet den Schreiber. Schreibt der Schreiber über updateConfig, MUSS er
// warten (kein Ergebnis). Währenddessen ändert der Test ein anderes Feld
// (WindowCols) direkt. Nach dem Freigeben müssen BEIDE Änderungen in der Datei
// stehen. Ohne Sperre läuft der Schreiber sofort durch und/oder überschreibt
// die Änderung des Tests.
//
// Die Schrift-Änderung (changeFontSize) ist unter Linux nicht erreichbar
// (getConsoleFontSize liefert 0), deshalb prüft ein Quelltext-Test zusätzlich,
// dass keine der vier Dateien mehr LoadConfig/SaveConfig direkt aufruft.
//
// @author Kurt Ingwer
// @date   2026-10-31 02:20
package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// assertWritesUnderConfigLock führt write aus, während der Test configUpdateMu
// hält, und prüft, dass write wartet und keine gleichzeitige Änderung verliert.
func assertWritesUnderConfigLock(t *testing.T, configPath string, write func(), check func(cfg *AppConfig)) {
	t.Helper()
	const marker = 77

	configUpdateMu.Lock()
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			configUpdateMu.Unlock()
		}
	}
	defer unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		write()
	}()
	if waitChan(done, 250*time.Millisecond) {
		t.Error("Schreiber lief durch, obwohl configUpdateMu gehalten wird - er schreibt ohne updateConfig")
	}

	// Gleichzeitige Änderung eines anderen Feldes (unter derselben Sperre)
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.WindowCols = marker
	if err := SaveConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	unlock()

	if !waitChan(done, 5*time.Second) {
		t.Fatal("Schreiber kehrte nach Freigabe der Sperre nicht zurück")
	}
	final, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if final.WindowCols != marker {
		t.Errorf("gleichzeitige Änderung ging verloren: WindowCols = %d, erwartet %d", final.WindowCols, marker)
	}
	check(final)
}

// TestToggleAutoUpdateWritesUnderLock prüft toggleAutoUpdate (Taste u).
func TestToggleAutoUpdateWritesUnderLock(t *testing.T) {
	m := newListTestModel(t, "A")
	assertWritesUnderConfigLock(t, m.configPath, func() {
		m.Update(keyMsgFor("u"))
	}, func(cfg *AppConfig) {
		if !cfg.AutoUpdate {
			t.Error("auto_update wurde nicht gespeichert")
		}
		if len(cfg.Connections) != 1 {
			t.Errorf("Verbindungen = %d, erwartet 1", len(cfg.Connections))
		}
	})
}

// TestPersistLanguageWritesUnderLock prüft persistLanguage (Sprachauswahl).
func TestPersistLanguageWritesUnderLock(t *testing.T) {
	m := newListTestModel(t, "A")
	assertWritesUnderConfigLock(t, m.configPath, func() {
		m.persistLanguage(LangEnglish)
	}, func(cfg *AppConfig) {
		if cfg.Language != LangEnglish {
			t.Errorf("Sprache = %q, erwartet %q", cfg.Language, LangEnglish)
		}
	})
}

// TestPersistFMSettingsWritesUnderLock prüft persistFMSettings (Farbmodus und
// Endungen des Datei-Managers).
func TestPersistFMSettingsWritesUnderLock(t *testing.T) {
	m := newListTestModel(t, "A")
	m.fm.theme = fmThemeIndex("mono")
	m.fmOpenExtensions = []string{".txt", ".log"}
	assertWritesUnderConfigLock(t, m.configPath, func() {
		if err := m.persistFMSettings(); err != nil {
			t.Errorf("persistFMSettings: %v", err)
		}
	}, func(cfg *AppConfig) {
		if cfg.FileManagerTheme != "mono" {
			t.Errorf("Farbmodus = %q, erwartet mono", cfg.FileManagerTheme)
		}
		if strings.Join(cfg.FileManagerOpenExtensions, " ") != ".txt .log" {
			t.Errorf("Endungen = %v", cfg.FileManagerOpenExtensions)
		}
	})
}

// TestPersistFontSizeWritesUnderLock prüft persistFontSize, den gesperrten
// Weg, über den changeFontSize künftig speichern soll.
func TestPersistFontSizeWritesUnderLock(t *testing.T) {
	m := newListTestModel(t, "A")
	assertWritesUnderConfigLock(t, m.configPath, func() {
		persistFontSize(m.configPath, 18)
	}, func(cfg *AppConfig) {
		if cfg.FontSize != 18 {
			t.Errorf("Schriftgröße = %d, erwartet 18", cfg.FontSize)
		}
	})
}

// TestNoUnlockedConfigWritesInSource prüft den Quelltext der vier Dateien:
// Außerhalb von Kommentaren darf weder LoadConfig noch SaveConfig vorkommen
// (alles läuft über updateConfig). Deckt auch changeFontSize ab, das unter
// Linux nicht auslösbar ist.
func TestNoUnlockedConfigWritesInSource(t *testing.T) {
	for _, file := range []string{"updater.go", "console_font.go", "tui_language.go", "tui_filemanager_menu.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "//") {
				continue
			}
			if idx := strings.Index(code, "//"); idx >= 0 {
				code = code[:idx]
			}
			if strings.Contains(code, "LoadConfig(") || strings.Contains(code, "SaveConfig(") {
				t.Errorf("%s:%d schreibt/liest die Konfiguration ohne updateConfig: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
}
