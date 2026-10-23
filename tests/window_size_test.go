// Paket main - Tests für das Merken der Fenstergröße (Build 48)
//
// Die letzte Fenstergröße (Spalten × Zeilen) wird gespeichert, sofern das
// Fenster nicht maximiert/Vollbild war, und beim nächsten Start wieder
// angewendet. Die eigentliche Konsolen-API ist nur unter Windows wirksam;
// getestet wird die plattformneutrale Logik.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00
package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// stubWindowState setzt den abgefragten Fensterzustand für die Testdauer.
func stubWindowState(t *testing.T, cols, rows int, normal bool) {
	t.Helper()
	old := queryWindowState
	queryWindowState = func() (int, int, bool) { return cols, rows, normal }
	t.Cleanup(func() { queryWindowState = old })
}

// TestAppConfigWindowSizeRoundtrip prüft JSON-Felder und omitempty.
func TestAppConfigWindowSizeRoundtrip(t *testing.T) {
	cfg := NewAppConfig()
	data, _ := json.Marshal(cfg)
	if strings.Contains(string(data), "window_") {
		t.Errorf("ohne Größe dürfen keine window-Felder erscheinen: %s", data)
	}
	cfg.WindowCols, cfg.WindowRows = 132, 43
	data, _ = json.Marshal(cfg)
	var back AppConfig
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.WindowCols != 132 || back.WindowRows != 43 {
		t.Errorf("Roundtrip = %dx%d", back.WindowCols, back.WindowRows)
	}
}

// TestValidWindowSize prüft die Plausibilitätsgrenzen.
func TestValidWindowSize(t *testing.T) {
	valid := [][2]int{{80, 25}, {minWindowCols, minWindowRows}, {maxWindowCols, maxWindowRows}}
	for _, v := range valid {
		if !validWindowSize(v[0], v[1]) {
			t.Errorf("validWindowSize(%d,%d) = false", v[0], v[1])
		}
	}
	invalid := [][2]int{{0, 0}, {80, 0}, {minWindowCols - 1, 25}, {80, maxWindowRows + 1}, {-5, 30}}
	for _, v := range invalid {
		if validWindowSize(v[0], v[1]) {
			t.Errorf("validWindowSize(%d,%d) = true", v[0], v[1])
		}
	}
}

// TestPersistWindowSize prüft Speichern, Nicht-Speichern bei Vollbild und
// unveränderter Größe.
func TestPersistWindowSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	cfg := NewAppConfig()
	if err := SaveConfig(path, &cfg); err != nil {
		t.Fatal(err)
	}

	if !persistWindowSize(path, 120, 40, true) {
		t.Fatal("normale Fenstergröße wurde nicht gespeichert")
	}
	loaded, _ := LoadConfig(path)
	if loaded.WindowCols != 120 || loaded.WindowRows != 40 {
		t.Errorf("gespeichert: %dx%d", loaded.WindowCols, loaded.WindowRows)
	}

	if persistWindowSize(path, 200, 60, false) {
		t.Error("maximiertes Fenster darf nicht gespeichert werden")
	}
	if persistWindowSize(path, 120, 40, true) {
		t.Error("unveränderte Größe darf keinen Schreibvorgang auslösen")
	}
	if persistWindowSize(path, 0, 0, true) {
		t.Error("ungültige Größe darf nicht gespeichert werden")
	}
	loaded, _ = LoadConfig(path)
	if loaded.WindowCols != 120 || loaded.WindowRows != 40 {
		t.Errorf("Größe wurde überschrieben: %dx%d", loaded.WindowCols, loaded.WindowRows)
	}
}

// TestPersistWindowSizeKeepsConnections prüft dass beim Speichern der
// Größe keine Verbindungen verloren gehen.
func TestPersistWindowSizeKeepsConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	cfg := NewAppConfig()
	cfg.Connections = append(cfg.Connections, NewConnection("A", "h", 22, "u", AuthPassword))
	_ = SaveConfig(path, &cfg)
	persistWindowSize(path, 100, 30, true)
	loaded, _ := LoadConfig(path)
	if len(loaded.Connections) != 1 {
		t.Errorf("Verbindungen nach Größen-Speichern: %d", len(loaded.Connections))
	}
}

// TestApplyStartupWindowSize prüft dass nur gültige Werte angewendet werden.
func TestApplyStartupWindowSize(t *testing.T) {
	old := setWindowSize
	defer func() { setWindowSize = old }()
	var got [][2]int
	setWindowSize = func(cols, rows int) bool { got = append(got, [2]int{cols, rows}); return true }

	applyStartupWindowSize(0, 0)
	applyStartupWindowSize(5, 2)
	applyStartupWindowSize(100, 30)
	if len(got) != 1 || got[0] != [2]int{100, 30} {
		t.Errorf("angewendet: %v, erwartet nur 100x30", got)
	}
}

// TestWindowResizeDebouncedSave prüft das verzögerte Speichern: Nur das
// Tick-Ereignis der letzten Größenänderung speichert.
func TestWindowResizeDebouncedSave(t *testing.T) {
	m := newListTestModel(t, "A")
	stubWindowState(t, 101, 33, true)

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = updated.(AppModel)
	if cmd == nil {
		t.Fatal("Größenänderung muss ein verzögertes Speichern planen")
	}
	firstSeq := m.windowSaveSeq
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 101, Height: 33})
	m = updated.(AppModel)

	// Veraltetes Tick-Ereignis: nichts speichern
	updated, _ = m.Update(windowSaveMsg{seq: firstSeq})
	m = updated.(AppModel)
	if cfg, _ := LoadConfig(m.configPath); cfg.WindowCols != 0 {
		t.Errorf("veraltetes Ereignis hat gespeichert: %d", cfg.WindowCols)
	}

	// Aktuelles Tick-Ereignis: speichern
	updated, _ = m.Update(windowSaveMsg{seq: m.windowSaveSeq})
	m = updated.(AppModel)
	cfg, _ := LoadConfig(m.configPath)
	if cfg.WindowCols != 101 || cfg.WindowRows != 33 {
		t.Errorf("gespeichert: %dx%d, erwartet 101x33", cfg.WindowCols, cfg.WindowRows)
	}
	if len(cfg.Connections) != 1 {
		t.Error("Verbindungen gingen beim Speichern verloren")
	}
}

// TestWindowResizeMaximizedNotSaved prüft dass maximierte Fenster nicht
// gespeichert werden.
func TestWindowResizeMaximizedNotSaved(t *testing.T) {
	m := newListTestModel(t, "A")
	stubWindowState(t, 240, 70, false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 240, Height: 70})
	m = updated.(AppModel)
	updated, _ = m.Update(windowSaveMsg{seq: m.windowSaveSeq})
	if cfg, _ := LoadConfig(updated.(AppModel).configPath); cfg.WindowCols != 0 {
		t.Errorf("maximiertes Fenster gespeichert: %d", cfg.WindowCols)
	}
}

// TestWindowPollSavesSize prüft die periodische Abfrage: Bubbletea meldet
// unter Windows nur Pufferänderungen, eine reine Höhenänderung des Fensters
// käme sonst nie an. Jeder Poll plant den nächsten.
func TestWindowPollSavesSize(t *testing.T) {
	m := newListTestModel(t, "A")
	stubWindowState(t, 111, 44, true)
	updated, cmd := m.Update(windowPollMsg{})
	if cmd == nil {
		t.Error("Poll muss den nächsten Poll planen")
	}
	cfg, _ := LoadConfig(updated.(AppModel).configPath)
	if cfg.WindowCols != 111 || cfg.WindowRows != 44 {
		t.Errorf("Poll hat nicht gespeichert: %dx%d", cfg.WindowCols, cfg.WindowRows)
	}
}

// TestInitStartsWindowPoll prüft dass Init den Poll startet.
func TestInitStartsWindowPoll(t *testing.T) {
	m := newListTestModel(t, "A")
	if m.Init() == nil {
		t.Error("Init liefert kein Kommando")
	}
	if windowPollInterval < time.Second || windowPollInterval > 10*time.Second {
		t.Errorf("windowPollInterval = %v", windowPollInterval)
	}
}

// TestWindowSaveDelay prüft eine sinnvolle Verzögerung.
func TestWindowSaveDelay(t *testing.T) {
	if windowSaveDelay < 200*time.Millisecond || windowSaveDelay > 5*time.Second {
		t.Errorf("windowSaveDelay = %v", windowSaveDelay)
	}
}

// TestHorizontalScrollFix - Bug seit Build 52: unten erscheint spontan ein
// waagrechter Bildlaufbalken und verdeckt die letzte Zeile. Ursache: der
// Konsolenpuffer ist breiter als das sichtbare Fenster (z. B. ist das
// Fenster nach Schriftänderung oder Monitorwechsel in Pixeln knapp eine
// Spalte zu schmal). Korrektur: Fenster an Spalte 0 und Pufferbreite =
// Fensterbreite.
func TestHorizontalScrollFix(t *testing.T) {
	cases := []struct {
		name                       string
		bufWidth, winLeft, winRight int
		wantWidth                  int
		wantFix                    bool
	}{
		{"passt genau", 120, 0, 119, 120, false},
		{"eine Spalte zu breit", 120, 0, 118, 119, true},
		{"waagrecht gescrollt", 120, 1, 119, 119, true},
		{"viel zu breit", 300, 0, 79, 80, true},
		{"kaputte Werte", 0, 0, -1, 0, false},
	}
	for _, c := range cases {
		width, fix := horizontalScrollFix(c.bufWidth, c.winLeft, c.winRight)
		if fix != c.wantFix || (fix && width != c.wantWidth) {
			t.Errorf("%s: horizontalScrollFix(%d, %d, %d) = %d, %v; erwartet %d, %v",
				c.name, c.bufWidth, c.winLeft, c.winRight, width, fix, c.wantWidth, c.wantFix)
		}
	}
}

// TestWindowPollRemovesHorizontalScroll - die regelmäßige Fensterabfrage
// entfernt einen waagrechten Bildlauf, egal wodurch er entstanden ist.
func TestWindowPollRemovesHorizontalScroll(t *testing.T) {
	m := newListTestModel(t, "A")
	stubWindowState(t, 0, 0, false)
	old := fixHorizontalScroll
	calls := 0
	fixHorizontalScroll = func() bool { calls++; return true }
	t.Cleanup(func() { fixHorizontalScroll = old })
	m.Update(windowPollMsg{})
	if calls != 1 {
		t.Errorf("fixHorizontalScroll %d-mal aufgerufen, erwartet 1", calls)
	}
}

// TestHScrollGuardPhantomBar - Nutzerhinweis: der Balken lässt sich gar
// nicht scrollen. Puffer und Fenster sind dann in Zeichen gleich breit,
// conhost zeigt den Balken trotzdem (Pixel-Überstand des Rahmens). Dann
// hilft kein Verkleinern des Puffers, sondern ein kurzes Neusetzen der
// Fensterbreite ("Anstupsen") - aber höchstens alle 2 s und nur 3-mal in
// Folge, damit es nicht flackert, falls es nichts nützt.
func TestHScrollGuardPhantomBar(t *testing.T) {
	var g hscrollGuard
	t0 := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)

	// Kein Balken, passende Breite: nichts tun
	if _, a := g.decide(t0, 120, 0, 119, false); a != hscrollNone {
		t.Errorf("ohne Balken: Aktion %v, erwartet keine", a)
	}
	// Echter Überstand (Puffer breiter): immer Puffer verkleinern, ohne Bremse
	for i := 0; i < 5; i++ {
		if w, a := g.decide(t0, 121, 0, 119, true); a != hscrollShrinkBuffer || w != 120 {
			t.Fatalf("Puffer breiter: Aktion %v, Breite %d; erwartet Verkleinern auf 120", a, w)
		}
	}
	// Scheinbalken: anstupsen
	if w, a := g.decide(t0, 120, 0, 119, true); a != hscrollNudge || w != 120 {
		t.Fatalf("Scheinbalken: Aktion %v, Breite %d; erwartet Anstupsen bei 120", a, w)
	}
	// Gleich danach (100 ms): noch nicht wieder
	if _, a := g.decide(t0.Add(100*time.Millisecond), 120, 0, 119, true); a != hscrollNone {
		t.Errorf("nach 100 ms erneut angestupst (%v) - Bremse fehlt", a)
	}
	// Nach 2 s zweiter, nach 4 s dritter Versuch, danach Schluss
	for i, d := range []time.Duration{2 * time.Second, 4 * time.Second} {
		if _, a := g.decide(t0.Add(d), 120, 0, 119, true); a != hscrollNudge {
			t.Errorf("Versuch %d nach %v: Aktion %v, erwartet Anstupsen", i+2, d, a)
		}
	}
	if _, a := g.decide(t0.Add(10*time.Second), 120, 0, 119, true); a != hscrollNone {
		t.Errorf("4. Versuch (%v) - nach 3 erfolglosen Versuchen muss Schluss sein", a)
	}
	// Balken weg: Zähler zurück, ein neuer Scheinbalken wird wieder behandelt
	g.decide(t0.Add(11*time.Second), 120, 0, 119, false)
	if _, a := g.decide(t0.Add(12*time.Second), 120, 0, 119, true); a != hscrollNudge {
		t.Errorf("neuer Scheinbalken nach Erholung: Aktion %v, erwartet Anstupsen", a)
	}
}
