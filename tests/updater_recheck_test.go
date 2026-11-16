// Paket main - Tests für die wiederholte Update-Suche und die Protokollzeilen
// des Auto-Updates (Build 91)
//
// Hintergrund: Ein Prozess, der vor einem Release gestartet wurde, fand das
// Update nie, weil nur beim Start gesucht wurde. Jetzt plant das Modell nach
// "aktuell" oder einem Fehler einen Wecker (tea.Tick, 6 Stunden), der über
// updateResultMsg{tick: true, gen: ...} zurückkommt und eine neue Suche
// startet. Jedes Umschalten von Auto-Update entwertet ausstehende Wecker
// (Generation). Zusätzlich schreiben Suche und ApplyOnExit je eine
// Protokollzeile, und ApplyOnExit sucht beim Beenden nie im Netz.
//
// Es werden nie echte 6 Stunden gewartet: Die Naht Updater.recheckInterval
// verkürzt den Abstand auf Millisekunden; die Konstante selbst wird separat
// geprüft.
//
// @author Kurt Ingwer
// @date   2026-11-16 16:45
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// --- Hilfsmittel ---

// rcTransport zählt alle HTTP-Anfragen eines Updaters (nach Pfad). So lässt
// sich beweisen, dass etwas NICHT aus dem Netz geholt wurde.
type rcTransport struct {
	base  http.RoundTripper
	mu    sync.Mutex
	paths []string
}

// RoundTrip zeichnet den Pfad auf und reicht die Anfrage weiter.
func (c *rcTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.paths = append(c.paths, r.URL.Path)
	c.mu.Unlock()
	return c.base.RoundTrip(r)
}

// total liefert die Zahl aller Anfragen.
func (c *rcTransport) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.paths)
}

// withPrefix liefert die Zahl der Anfragen, deren Pfad mit prefix beginnt.
func (c *rcTransport) withPrefix(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, p := range c.paths {
		if strings.HasPrefix(p, prefix) {
			n++
		}
	}
	return n
}

// exact liefert die Zahl der Anfragen mit genau diesem Pfad.
func (c *rcTransport) exact(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, p := range c.paths {
		if p == path {
			n++
		}
	}
	return n
}

// rcCountRequests hängt einen Zähler vor den HTTP-Client des Updaters.
func rcCountRequests(u *Updater) *rcTransport {
	base := http.DefaultTransport
	if u.client != nil && u.client.Transport != nil {
		base = u.client.Transport
	}
	ct := &rcTransport{base: base}
	u.client = &http.Client{Transport: ct, Timeout: u.client.Timeout}
	return ct
}

// rcModelWith liefert ein Listenmodell mit dem gegebenen Updater, dem
// Schalter auto und einem Datei-Logger (Pfad im Rückgabewert). Der Abstand
// der wiederholten Suche ist 5 ms, nie 6 Stunden.
func rcModelWith(t *testing.T, u *Updater, auto bool) (AppModel, string) {
	t.Helper()
	m := newListTestModel(t, "A")
	if u != nil {
		u.recheckInterval = 5 * time.Millisecond
		u.SetInstallOnExit(auto)
	}
	m.updater = u
	m.autoUpdate = auto
	logger, logPath := fileLogger(t)
	m.sshManager.logger = logger
	return m, logPath
}

// rcModel wie rcModelWith, aber mit neuem Updater der Build-Nummer build.
func rcModel(t *testing.T, build string, auto bool) (AppModel, *Updater, string) {
	t.Helper()
	u := NewUpdater(build)
	m, logPath := rcModelWith(t, u, auto)
	return m, u, logPath
}

// rcRun führt ein Kommando mit Zeitlimit aus (ein hängender Wecker soll den
// Test nicht blockieren).
func rcRun(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("Kommando ist nil, erwartet eine Nachricht")
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("Kommando lieferte binnen 10 s keine Nachricht")
		return nil
	}
}

// rcResult führt ein Kommando aus und erwartet eine updateResultMsg.
func rcResult(t *testing.T, cmd tea.Cmd) updateResultMsg {
	t.Helper()
	msg, ok := rcRun(t, cmd).(updateResultMsg)
	if !ok {
		t.Fatalf("Nachricht ist keine updateResultMsg")
	}
	return msg
}

// rcUpdate schickt eine Nachricht ans Modell und liefert Modell und Kommando.
func rcUpdate(t *testing.T, m AppModel, msg tea.Msg) (AppModel, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	model, ok := updated.(AppModel)
	if !ok {
		t.Fatalf("Update lieferte kein AppModel")
	}
	return model, cmd
}

// rcKey drückt eine Taste und liefert zusätzlich das Kommando (press
// verwirft es).
func rcKey(t *testing.T, m AppModel, key string) (AppModel, tea.Cmd) {
	t.Helper()
	return rcUpdate(t, m, keyMsgFor(key))
}

// rcLog liest das Protokoll (leer, wenn noch nichts geschrieben wurde).
func rcLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("Protokoll lesen: %v", err)
	}
	return string(data)
}

// rcCountLines zählt die Protokollzeilen, die sub enthalten.
func rcCountLines(log, sub string) int {
	n := 0
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, sub) {
			n++
		}
	}
	return n
}

// --- 1. Konstante und Abstand ---

// TestRecheckIntervalConstants prüft, dass der Abstand der wiederholten
// Suche 6 Stunden beträgt und die Naht recheckInterval ihn nur verkürzt, wenn
// sie gesetzt ist (0 = Konstante). Ein zu kurzer Standard würde GitHub alle
// paar Sekunden anfragen, ein zu langer das Update nie finden.
func TestRecheckIntervalConstants(t *testing.T) {
	if updateRecheckInterval != 6*time.Hour {
		t.Errorf("updateRecheckInterval = %v, erwartet 6h", updateRecheckInterval)
	}
	u := NewUpdater("48")
	if got := u.recheckDelay(); got != 6*time.Hour {
		t.Errorf("recheckDelay() ohne Naht = %v, erwartet 6h", got)
	}
	u.recheckInterval = 7 * time.Millisecond
	if got := u.recheckDelay(); got != 7*time.Millisecond {
		t.Errorf("recheckDelay() mit Naht = %v, erwartet 7ms", got)
	}
	u.recheckInterval = -time.Second // ungültig: Konstante gilt
	if got := u.recheckDelay(); got != 6*time.Hour {
		t.Errorf("recheckDelay() bei negativer Naht = %v, erwartet 6h", got)
	}
}

// TestRecheckDefaultDelayIsNotImmediate führt den Wecker mit dem
// Standardabstand aus (Naht 0) und prüft, dass er auch nach 150 ms noch
// nichts geliefert hat. Ohne das könnte der Wecker versehentlich sofort
// feuern und eine Dauerschleife von Suchen erzeugen. Der Wecker-Goroutine
// bleibt bis Testende hängen (Timer 6 h), das ist harmlos.
func TestRecheckDefaultDelayIsNotImmediate(t *testing.T) {
	m, u, _ := rcModel(t, "48", true)
	u.recheckInterval = 0 // Standard: 6 Stunden
	_, cmd := rcUpdate(t, m, updateResultMsg{})
	if cmd == nil {
		t.Fatal("nach \"aktuell\" kein Wecker geplant")
	}
	done := make(chan struct{})
	go func() { _ = cmd(); close(done) }()
	select {
	case <-done:
		t.Fatal("Wecker mit Standardabstand lieferte schon nach kurzer Zeit eine Nachricht")
	case <-time.After(150 * time.Millisecond):
	}
}

// --- 1. Wecker nach "aktuell" und nach Fehler ---

// TestRecheckTickAfterCurrent prüft: Meldet die Suche "aktuell", liefert
// handleUpdateResult einen Wecker. Dessen Nachricht kommt erst nach dem
// Abstand (hier 30 ms), hat tick=true und die aktuelle Generation (>0).
// Ohne Wecker fände ein lange laufender Prozess kein später erscheinendes
// Release.
func TestRecheckTickAfterCurrent(t *testing.T) {
	m, u, _ := rcModel(t, "48", true)
	u.recheckInterval = 30 * time.Millisecond
	m.updateChecking = true
	start := time.Now()
	m, cmd := rcUpdate(t, m, updateResultMsg{})
	if m.updateChecking {
		t.Error("updateChecking nach Ergebnis noch true")
	}
	msg := rcResult(t, cmd)
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond {
		t.Errorf("Wecker kam nach %v, erwartet mindestens ~30 ms Abstand", elapsed)
	}
	if !msg.tick {
		t.Error("Nachricht ist kein Wecker (tick=false)")
	}
	if msg.pending != nil || msg.err != nil {
		t.Errorf("Wecker trägt Daten: pending=%v err=%v", msg.pending, msg.err)
	}
	if msg.gen == 0 || msg.gen != u.recheckGen.Load() {
		t.Errorf("gen = %d, recheckGen = %d, erwartet gleich und > 0", msg.gen, u.recheckGen.Load())
	}
}

// TestRecheckTickAfterError prüft dasselbe nach einem Fehler der Suche (z. B.
// kein Netz): Der Fehler bleibt sichtbar, trotzdem wird später erneut
// gesucht. Sonst wäre ein kurzer Netzausfall beim Start endgültig.
func TestRecheckTickAfterError(t *testing.T) {
	m, u, _ := rcModel(t, "48", true)
	m.updateChecking = true
	m, cmd := rcUpdate(t, m, updateResultMsg{err: errors.New("kein Netz-4711")})
	if m.updateChecking {
		t.Error("updateChecking nach Fehler noch true")
	}
	if text := stripANSI(renderListText(m)); !strings.Contains(text, "kein Netz-4711") {
		t.Errorf("Fehlertext fehlt in der Anzeige:\n%s", text)
	}
	msg := rcResult(t, cmd)
	if !msg.tick || msg.gen == 0 || msg.gen != u.recheckGen.Load() {
		t.Errorf("Wecker = %+v, recheckGen = %d, erwartet tick und aktuelle Generation", msg, u.recheckGen.Load())
	}
}

// TestRecheckGenGrowsWithEveryScheduling prüft: Jedes Planen erhöht die
// Generation um genau 1; die jeweils vorherige gilt danach nicht mehr. Nur so
// kann es nie zwei gültige Wecker gleichzeitig geben.
func TestRecheckGenGrowsWithEveryScheduling(t *testing.T) {
	m, u, _ := rcModel(t, "48", true)
	prev := u.recheckGen.Load()
	var gens []uint64
	for i := 0; i < 4; i++ {
		var cmd tea.Cmd
		if i%2 == 0 {
			m, cmd = rcUpdate(t, m, updateResultMsg{})
		} else {
			m, cmd = rcUpdate(t, m, updateResultMsg{err: errors.New("x")})
		}
		msg := rcResult(t, cmd)
		if msg.gen != prev+1 {
			t.Fatalf("Durchlauf %d: gen = %d, erwartet %d", i, msg.gen, prev+1)
		}
		if u.isCurrentRecheck(prev) && i > 0 {
			t.Errorf("Durchlauf %d: alte Generation %d gilt noch", i, prev)
		}
		if !u.isCurrentRecheck(msg.gen) {
			t.Errorf("Durchlauf %d: neue Generation %d gilt nicht", i, msg.gen)
		}
		gens = append(gens, msg.gen)
		prev = msg.gen
	}
	for i := 1; i < len(gens); i++ {
		if gens[i] <= gens[i-1] {
			t.Errorf("Generationen nicht streng steigend: %v", gens)
		}
	}
}

// --- 2. Kein Wecker ---

// TestRecheckNoTickCases prüft alle Fälle, in denen NICHT neu gesucht werden
// darf. Jeder Fall hat eine Kontrolle im selben Aufbau (ohne den
// Hinderungsgrund liefert dasselbe Ergebnis einen Wecker); sonst wäre
// "kein Wecker" trivial wahr.
func TestRecheckNoTickCases(t *testing.T) {
	// Kontrolle: der Grundaufbau plant einen Wecker
	t.Run("Kontrolle", func(t *testing.T) {
		m, _, _ := rcModel(t, "48", true)
		if _, cmd := rcUpdate(t, m, updateResultMsg{}); cmd == nil {
			t.Fatal("Kontrolle: auch im Grundaufbau kein Wecker, Tests wären sinnlos")
		}
	})

	t.Run("Update gefunden (msg.pending)", func(t *testing.T) {
		m, u, _ := rcModel(t, "48", true)
		before := u.recheckGen.Load()
		_, cmd := rcUpdate(t, m, updateResultMsg{pending: &pendingUpdate{build: 49}})
		if cmd != nil {
			t.Error("trotz gefundenem Update ein Wecker geplant")
		}
		if u.recheckGen.Load() != before {
			t.Error("Generation wurde ohne Planung verändert")
		}
	})

	t.Run("Auto-Update aus", func(t *testing.T) {
		m, u, _ := rcModel(t, "48", false)
		before := u.recheckGen.Load()
		if _, cmd := rcUpdate(t, m, updateResultMsg{}); cmd != nil {
			t.Error("bei \"aktuell\" ein Wecker, obwohl Auto-Update aus ist")
		}
		if _, cmd := rcUpdate(t, m, updateResultMsg{err: errors.New("x")}); cmd != nil {
			t.Error("bei Fehler ein Wecker, obwohl Auto-Update aus ist")
		}
		if u.recheckGen.Load() != before {
			t.Error("Generation wurde ohne Planung verändert")
		}
	})

	t.Run("Updater hat schon ein Pending", func(t *testing.T) {
		u, _ := newPendingSetupUpdater(t)
		m, _ := rcModelWith(t, u, true)
		if _, cmd := rcUpdate(t, m, updateResultMsg{}); cmd != nil {
			t.Error("bei bereitliegendem Update (Ergebnis \"aktuell\") ein Wecker")
		}
		if _, cmd := rcUpdate(t, m, updateResultMsg{err: errors.New("x")}); cmd != nil {
			t.Error("bei bereitliegendem Update (Fehler) ein Wecker")
		}
	})

	t.Run("kein Updater", func(t *testing.T) {
		m, _ := rcModelWith(t, nil, true)
		// darf nicht paniken (buildLabel und Planung sind nil-sicher)
		if _, cmd := rcUpdate(t, m, updateResultMsg{}); cmd != nil {
			t.Error("ohne Updater ein Wecker (\"aktuell\")")
		}
		if _, cmd := rcUpdate(t, m, updateResultMsg{err: errors.New("x")}); cmd != nil {
			t.Error("ohne Updater ein Wecker (Fehler)")
		}
		if _, cmd := rcUpdate(t, m, updateResultMsg{tick: true, gen: 1}); cmd != nil {
			t.Error("ohne Updater startet ein Wecker eine Suche")
		}
	})

	// Entwicklerversionen und alles Nicht-Numerische: nie aktualisieren
	for _, build := range []string{"dev", "", " ", "48-dev", "v48", "4x8"} {
		build := build
		t.Run(fmt.Sprintf("Build %q", build), func(t *testing.T) {
			m, u, _ := rcModel(t, build, true)
			before := u.recheckGen.Load()
			if _, cmd := rcUpdate(t, m, updateResultMsg{}); cmd != nil {
				t.Error("bei Entwicklerversion ein Wecker (\"aktuell\")")
			}
			if _, cmd := rcUpdate(t, m, updateResultMsg{err: errors.New("x")}); cmd != nil {
				t.Error("bei Entwicklerversion ein Wecker (Fehler)")
			}
			if u.recheckGen.Load() != before {
				t.Error("Generation wurde ohne Planung verändert")
			}
		})
	}

	// Gegenprobe zur Entwicklerversion: Leerraum um eine echte Nummer stört nicht
	t.Run("Build mit Leerraum ist numerisch", func(t *testing.T) {
		m, _, _ := rcModel(t, " 48\n", true)
		if _, cmd := rcUpdate(t, m, updateResultMsg{}); cmd == nil {
			t.Error("Build \" 48\\n\" wird als Entwicklerversion behandelt")
		}
	})
}

// --- 3. Keine doppelten Wecker ---

// TestRecheckToggleBumpsGeneration prüft: Jedes Umschalten mit u erhöht die
// Generation um genau 1 (auch Ausschalten), damit ältere Wecker verfallen.
func TestRecheckToggleBumpsGeneration(t *testing.T) {
	m, u, _ := rcModel(t, "48", true)
	for i := 0; i < 3; i++ {
		before := u.recheckGen.Load()
		m, _ = rcKey(t, m, "u")
		if got := u.recheckGen.Load(); got != before+1 {
			t.Errorf("Umschalten %d: Generation %d -> %d, erwartet +1", i+1, before, got)
		}
	}
	if m.autoUpdate {
		t.Error("nach 3 mal u (Start: an) muss Auto-Update aus sein")
	}
}

// TestRecheckStaleTickIgnoredAfterToggle prüft den Kern der Doppelwecker-
// Sperre: Ein Wecker, der vor dem Umlegen des Schalters geplant wurde, startet
// nach dem Wiedereinschalten KEINE Suche, auch wenn alles andere passt
// (Auto-Update an, keine Suche läuft, kein Pending). Nur der jüngste Wecker
// darf suchen. Ohne Generationsprüfung gäbe es nach u, u zwei Wecker-Ketten.
func TestRecheckStaleTickIgnoredAfterToggle(t *testing.T) {
	m, u, _ := rcModel(t, "48", true)

	// 1. Wecker planen und Nachricht merken (noch nicht zugestellt)
	m, cmd := rcUpdate(t, m, updateResultMsg{})
	oldTick := rcResult(t, cmd)

	// 2. Aus und wieder an: das Einschalten startet eine Suche
	m, _ = rcKey(t, m, "u")
	m, searchCmd := rcKey(t, m, "u")
	if !m.autoUpdate || !m.updateChecking || searchCmd == nil {
		t.Fatalf("nach u, u: autoUpdate=%v checking=%v cmd=%v, erwartet Suche", m.autoUpdate, m.updateChecking, searchCmd != nil)
	}

	// 3. Suche endet "aktuell": der jüngste Wecker wird geplant
	m, cmd = rcUpdate(t, m, updateResultMsg{})
	newTick := rcResult(t, cmd)
	if newTick.gen == oldTick.gen {
		t.Fatalf("beide Wecker haben dieselbe Generation %d", newTick.gen)
	}
	if m.updateChecking {
		t.Fatal("Aufbau: updateChecking sollte false sein")
	}

	// 4. Alter Wecker trifft ein: nichts passiert
	m, cmd = rcUpdate(t, m, oldTick)
	if cmd != nil || m.updateChecking {
		t.Errorf("alter Wecker startete eine Suche (cmd=%v, updateChecking=%v)", cmd != nil, m.updateChecking)
	}

	// 5. Der jüngste Wecker darf suchen (Kontrolle, dass es nicht an
	// etwas anderem liegt)
	m, cmd = rcUpdate(t, m, newTick)
	if cmd == nil || !m.updateChecking {
		t.Errorf("jüngster Wecker startete keine Suche (cmd=%v, updateChecking=%v)", cmd != nil, m.updateChecking)
	}
	if !u.isCurrentRecheck(newTick.gen) {
		t.Error("jüngster Wecker gilt nicht mehr")
	}
}

// TestRecheckStaleTickAfterThreeToggles prüft u, u, u (aus, an, aus): Der vor
// dem Umlegen geplante Wecker ist veraltet und der Schalter am Ende aus; es
// darf nichts starten. Zusätzlich: Auch ein NEU gemeldeter Wecker mit der
// jetzt aktuellen Generation startet nichts, solange der Schalter aus ist.
func TestRecheckStaleTickAfterThreeToggles(t *testing.T) {
	m, u, _ := rcModel(t, "48", true)
	m, cmd := rcUpdate(t, m, updateResultMsg{})
	oldTick := rcResult(t, cmd)

	m = press(t, m, "u", "u", "u")
	if m.autoUpdate {
		t.Fatal("nach 3 mal u muss Auto-Update aus sein")
	}
	// Hinweis: updateChecking ist hier noch true, weil das mittlere Einschalten
	// eine Suche startete, die nie endete (Zustand des Aufbaus, nicht des Weckers)
	_, cmd = rcUpdate(t, m, oldTick)
	if cmd != nil {
		t.Error("veralteter Wecker bei ausgeschaltetem Schalter startete eine Suche")
	}
	m.updateChecking = false // Suche gilt als beendet: nur der Schalter hält den Wecker auf
	_, cmd = rcUpdate(t, m, oldTick)
	if cmd != nil {
		t.Error("veralteter Wecker bei ausgeschaltetem Schalter und freier Lage startete eine Suche")
	}
	valid := updateResultMsg{tick: true, gen: u.recheckGen.Load()}
	_, cmd = rcUpdate(t, m, valid)
	if cmd != nil {
		t.Error("gültiger Wecker bei ausgeschaltetem Schalter startete eine Suche")
	}
}

// TestRecheckValidTickDoesNothingWhenSwitchedOff prüft: Ein Wecker mit
// aktueller Generation startet bei Auto-Update "aus" nichts. Die
// Gegenprobe im selben Aufbau (Schalter an) startet die Suche; sonst wäre die
// Aussage trivial.
func TestRecheckValidTickDoesNothingWhenSwitchedOff(t *testing.T) {
	m, u, _ := rcModel(t, "48", true)
	tick := updateResultMsg{tick: true, gen: u.recheckGen.Load()}

	// Gegenprobe: an -> Suche startet
	on, cmd := rcUpdate(t, m, tick)
	if cmd == nil || !on.updateChecking {
		t.Fatal("Gegenprobe: gültiger Wecker startet bei Auto-Update an keine Suche")
	}

	// aus (direkt gesetzt, Generation bleibt unverändert, also gültig)
	m.autoUpdate = false
	off, cmd := rcUpdate(t, m, tick)
	if cmd != nil || off.updateChecking {
		t.Errorf("gültiger Wecker bei Auto-Update aus startete Suche (cmd=%v, checking=%v)", cmd != nil, off.updateChecking)
	}

	// aus über die Taste: der laufende Wecker ist durch das Umschalten
	// ohnehin veraltet
	m.autoUpdate = true
	m, _ = rcKey(t, m, "u")
	if m.autoUpdate {
		t.Fatal("u schaltet nicht aus")
	}
	if _, cmd := rcUpdate(t, m, tick); cmd != nil {
		t.Error("Wecker aus der Zeit vor dem Ausschalten startete eine Suche")
	}
}

// TestRecheckTickNeedsIdleAndNoPending prüft die übrigen Bedingungen des
// Weckers (gültige Generation, Auto-Update an): läuft schon eine Suche, startet
// er keine zweite; liegt ein Pending bereit, startet er keine. Mit
// freier Lage startet er (Kontrolle).
func TestRecheckTickNeedsIdleAndNoPending(t *testing.T) {
	m, u, _ := rcModel(t, "48", true)
	tick := updateResultMsg{tick: true, gen: u.recheckGen.Load()}

	idle, cmd := rcUpdate(t, m, tick)
	if cmd == nil || !idle.updateChecking {
		t.Fatal("Kontrolle: freier Zustand startet keine Suche")
	}

	// Suche läuft schon
	m.updateChecking = true
	same, cmd := rcUpdate(t, m, tick)
	if cmd != nil {
		t.Error("Wecker startete eine zweite, parallele Suche")
	}
	if !same.updateChecking {
		t.Error("laufende Suche wurde fälschlich als beendet markiert")
	}

	// Pending liegt bereit
	pu, _ := newPendingSetupUpdater(t)
	pm, _ := rcModelWith(t, pu, true)
	if _, cmd := rcUpdate(t, pm, updateResultMsg{tick: true, gen: pu.recheckGen.Load()}); cmd != nil {
		t.Error("Wecker startete eine Suche trotz bereitliegendem Update")
	}
}

// TestRecheckToggleDuringSearchStartsNoSecondSearch prüft: Wird der Schalter
// während einer laufenden Suche aus- und wieder eingeschaltet, startet das
// Einschalten keine zweite Suche. Das Ergebnis der laufenden Suche plant dann
// genau einen Wecker.
func TestRecheckToggleDuringSearchStartsNoSecondSearch(t *testing.T) {
	m, _, _ := rcModel(t, "48", true)
	m.updateChecking = true // Suche läuft

	m, cmd := rcKey(t, m, "u") // aus
	if cmd != nil {
		t.Error("Ausschalten startete eine Suche")
	}
	m, cmd = rcKey(t, m, "u") // an
	if cmd != nil {
		t.Error("Einschalten startete eine zweite Suche, obwohl eine läuft")
	}
	if !m.autoUpdate || !m.updateChecking {
		t.Errorf("autoUpdate=%v updateChecking=%v, erwartet beide true", m.autoUpdate, m.updateChecking)
	}
	m, cmd = rcUpdate(t, m, updateResultMsg{})
	if cmd == nil {
		t.Fatal("Ergebnis der laufenden Suche plante keinen Wecker")
	}
	if tick := rcResult(t, cmd); !tick.tick {
		t.Error("geplante Nachricht ist kein Wecker")
	}
	if m.updateChecking {
		t.Error("updateChecking nach dem Ergebnis noch true")
	}
}

// TestRecheckResultWhileSwitchedOffPlansNothing prüft: Wird während einer
// laufenden Suche ausgeschaltet, plant deren spätes Ergebnis keinen Wecker
// (auch nicht bei Fehler). Sonst liefe die Suche gegen den Willen des
// Nutzers weiter.
func TestRecheckResultWhileSwitchedOffPlansNothing(t *testing.T) {
	m, _, _ := rcModel(t, "48", true)
	m.updateChecking = true
	m, _ = rcKey(t, m, "u")
	if m.autoUpdate {
		t.Fatal("u schaltet nicht aus")
	}
	if _, cmd := rcUpdate(t, m, updateResultMsg{}); cmd != nil {
		t.Error("spätes Ergebnis (aktuell) plante nach dem Ausschalten einen Wecker")
	}
	if _, cmd := rcUpdate(t, m, updateResultMsg{err: errors.New("x")}); cmd != nil {
		t.Error("spätes Ergebnis (Fehler) plante nach dem Ausschalten einen Wecker")
	}
}

// --- 4. Echte Suche gegen den Testserver ---

// TestRecheckEndToEndFindsLaterRelease prüft den ganzen Weg: Wecker -> Suche
// (updateCheckCmd gegen einen httptest-Server mit v0.48, Build 47) -> Ergebnis
// mit Pending -> KEIN weiterer Wecker. Danach starten auch gültige Wecker
// nichts mehr, und die Statuszeile nennt Version und Beenden (Punkt 5).
func TestRecheckEndToEndFindsLaterRelease(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, _ := newTestUpdater(t, srv, "47", "linux")
	ct := rcCountRequests(u)
	m, logPath := rcModelWith(t, u, true)

	tick := updateResultMsg{tick: true, gen: u.recheckGen.Load()}
	m, cmd := rcUpdate(t, m, tick)
	if cmd == nil || !m.updateChecking {
		t.Fatalf("Wecker startete keine Suche (cmd=%v, checking=%v)", cmd != nil, m.updateChecking)
	}
	if ct.total() != 0 {
		t.Fatalf("schon vor Ausführen des Kommandos %d Netzanfragen", ct.total())
	}

	result := rcResult(t, cmd)
	if result.tick || result.pending == nil || result.err != nil {
		t.Fatalf("Suchergebnis = %+v, erwartet pending ohne Fehler", result)
	}
	if result.pending.build != 48 {
		t.Errorf("pending.build = %d, erwartet 48", result.pending.build)
	}
	if ct.withPrefix("/latest") != 1 {
		t.Errorf("Anfragen an /latest: %d, erwartet 1", ct.withPrefix("/latest"))
	}

	genBefore := u.recheckGen.Load()
	m, cmd = rcUpdate(t, m, result)
	if cmd != nil {
		t.Error("nach gefundenem Update wurde ein weiterer Wecker geplant")
	}
	if m.updateChecking {
		t.Error("updateChecking nach Ergebnis noch true")
	}
	if u.recheckGen.Load() != genBefore {
		t.Error("Generation nach gefundenem Update verändert (es wurde geplant)")
	}
	if u.Pending() == nil || u.Pending().build != 48 {
		t.Errorf("Updater.Pending() = %v, erwartet Build 48", u.Pending())
	}
	// "v0.48" steht auch im Kopf (eigene Version), daher der Satzteil
	text := stripANSI(renderListText(m))
	if !strings.Contains(text, "heruntergeladen") || !strings.Contains(text, "Beenden") {
		t.Errorf("Statuszeile ohne \"heruntergeladen\" und \"Beenden\":\n%s", text)
	}
	if log := rcLog(t, logPath); !strings.Contains(log, "Update v0.48 heruntergeladen und geprüft, wird beim Beenden installiert") {
		t.Errorf("Protokoll ohne Download-Zeile: %q", log)
	}

	// Ein (gültiger) Wecker danach startet nichts mehr
	requests := ct.total()
	m, cmd = rcUpdate(t, m, updateResultMsg{tick: true, gen: u.recheckGen.Load()})
	if cmd != nil || m.updateChecking {
		t.Error("Wecker startete nach gefundenem Update noch eine Suche")
	}
	if ct.total() != requests {
		t.Errorf("weitere Netzanfragen nach gefundenem Update: %d", ct.total()-requests)
	}
}

// TestRecheckEndToEndSameBuildKeepsPolling prüft: Ist das Release nicht neuer
// (Build 47 gegen v0.47), liefert die echte Suche "aktuell" und plant wieder
// einen Wecker. Dreimal hintereinander (Schleife ohne Download), und während
// eine Suche läuft, startet ein zweiter Wecker keine zweite.
func TestRecheckEndToEndSameBuildKeepsPolling(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.47", assetName: "ssh-easy", content: fakeELF})
	u, _ := newTestUpdater(t, srv, "47", "linux")
	ct := rcCountRequests(u)
	m, logPath := rcModelWith(t, u, true)

	tick := updateResultMsg{tick: true, gen: u.recheckGen.Load()}
	for round := 1; round <= 3; round++ {
		var cmd tea.Cmd
		m, cmd = rcUpdate(t, m, tick)
		if cmd == nil || !m.updateChecking {
			t.Fatalf("Runde %d: Wecker startete keine Suche", round)
		}
		// Während die Suche läuft: derselbe Wecker nochmal startet nichts
		if _, second := rcUpdate(t, m, tick); second != nil {
			t.Fatalf("Runde %d: zweiter Wecker startete eine parallele Suche", round)
		}
		result := rcResult(t, cmd)
		if result.tick || result.pending != nil || result.err != nil {
			t.Fatalf("Runde %d: Ergebnis = %+v, erwartet \"aktuell\"", round, result)
		}
		m, cmd = rcUpdate(t, m, result)
		if cmd == nil {
			t.Fatalf("Runde %d: nach \"aktuell\" kein Wecker", round)
		}
		next := rcResult(t, cmd)
		if !next.tick || next.gen != u.recheckGen.Load() || next.gen == tick.gen {
			t.Fatalf("Runde %d: Wecker = %+v, erwartet neue aktuelle Generation (alt %d)", round, next, tick.gen)
		}
		tick = next
	}
	if got := ct.withPrefix("/latest"); got != 3 {
		t.Errorf("Anfragen an /latest = %d, erwartet 3", got)
	}
	if got := ct.withPrefix("/download/"); got != 0 {
		t.Errorf("%d Downloads, obwohl nie ein neueres Release da war", got)
	}
	if got := rcCountLines(rcLog(t, logPath), "Update-Suche: aktuell (v0.47)"); got != 3 {
		t.Errorf("Protokollzeilen \"aktuell\" = %d, erwartet 3", got)
	}
}

// TestRecheckEndToEndErrorKeepsPolling prüft: Antwortet der Server mit 500,
// ist das Ergebnis ein Fehler (Warnung im Protokoll, Fehlertext in der
// Anzeige) und es wird trotzdem ein neuer Wecker geplant.
func TestRecheckEndToEndErrorKeepsPolling(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, apiStatus: 500})
	u, _ := newTestUpdater(t, srv, "47", "linux")
	m, logPath := rcModelWith(t, u, true)

	m, cmd := rcUpdate(t, m, updateResultMsg{tick: true, gen: u.recheckGen.Load()})
	result := rcResult(t, cmd)
	if result.err == nil || result.pending != nil {
		t.Fatalf("Ergebnis = %+v, erwartet Fehler", result)
	}
	m, cmd = rcUpdate(t, m, result)
	if next := rcResult(t, cmd); !next.tick || next.gen != u.recheckGen.Load() {
		t.Errorf("nach Fehler kein gültiger Wecker: %+v", next)
	}
	if !strings.Contains(rcLog(t, logPath), "[WARN] Auto-Update fehlgeschlagen: ") {
		t.Errorf("Protokoll ohne Warnung: %q", rcLog(t, logPath))
	}
}

// --- 5. Statusanzeige beim Umschalten mit bereitliegendem Update ---

// TestRecheckToggleWithPendingShowsStatusWithoutSearch prüft: Liegt schon ein
// Update bereit, zeigt Ausschalten und Wiedereinschalten wieder "Update v0.N
// ... Beenden ..." an - ohne neue Suche (Cmd nil, updateChecking false, 0
// Netzanfragen). Zwischen aus und an ist der Hinweis weg (Statuszeile leer).
func TestRecheckToggleWithPendingShowsStatusWithoutSearch(t *testing.T) {
	u, _ := newPendingSetupUpdater(t) // v0.48 liegt bereit
	ct := rcCountRequests(u)
	m, _ := rcModelWith(t, u, true)
	m.updateStatus = fmt.Sprintf(m.tr(txtUpdateReady), 48)

	m, cmd := rcKey(t, m, "u") // aus
	if m.autoUpdate || cmd != nil {
		t.Fatalf("Ausschalten: autoUpdate=%v cmd=%v", m.autoUpdate, cmd != nil)
	}
	if text := stripANSI(renderListText(m)); strings.Contains(text, "heruntergeladen") {
		t.Errorf("ausgeschaltet steht noch der Update-Hinweis da:\n%s", text)
	}

	genBefore := u.recheckGen.Load()
	m, cmd = rcKey(t, m, "u") // an
	if !m.autoUpdate {
		t.Fatal("u schaltet nicht ein")
	}
	if cmd != nil {
		t.Error("Einschalten mit bereitliegendem Update startete eine Suche")
	}
	if m.updateChecking {
		t.Error("updateChecking nach Einschalten mit Pending true")
	}
	if u.recheckGen.Load() != genBefore+1 {
		t.Errorf("Generation %d -> %d, erwartet +1 beim Umschalten", genBefore, u.recheckGen.Load())
	}
	text := stripANSI(renderListText(m))
	if !strings.Contains(text, "heruntergeladen") || !strings.Contains(text, "Beenden") {
		t.Errorf("Hinweis nach Wiedereinschalten fehlt:\n%s", text)
	}
	if ct.total() != 0 {
		t.Errorf("%d Netzanfragen beim Umschalten mit Pending, erwartet 0", ct.total())
	}
	// Auch danach: kein Wecker mehr, der etwas startet
	if _, cmd := rcUpdate(t, m, updateResultMsg{tick: true, gen: u.recheckGen.Load()}); cmd != nil {
		t.Error("Wecker startete nach Wiedereinschalten mit Pending eine Suche")
	}
}

// TestRecheckToggleOnWithoutPendingSearchesOnce prüft die Gegenprobe zum
// vorigen Test: Ohne bereitliegendes Update startet das Einschalten genau eine
// Suche mit Status "Suche ..." (Cmd != nil, updateChecking true).
func TestRecheckToggleOnWithoutPendingSearchesOnce(t *testing.T) {
	m, _, _ := rcModel(t, "48", false)
	m, cmd := rcKey(t, m, "u")
	if cmd == nil || !m.updateChecking || !m.autoUpdate {
		t.Errorf("Einschalten ohne Pending: cmd=%v checking=%v auto=%v", cmd != nil, m.updateChecking, m.autoUpdate)
	}
}

// --- 6. Protokollzeilen der Suche ---

// TestRecheckLogCurrent prüft die Info-Zeile "Update-Suche: aktuell (v0.N)"
// mit der eigenen Build-Nummer des Updaters.
func TestRecheckLogCurrent(t *testing.T) {
	m, _, logPath := rcModel(t, "52", true)
	rcUpdate(t, m, updateResultMsg{})
	log := rcLog(t, logPath)
	if !strings.Contains(log, "[INFO] Update-Suche: aktuell (v0.52)") {
		t.Errorf("Protokoll = %q, erwartet Info-Zeile \"Update-Suche: aktuell (v0.52)\"", log)
	}
}

// TestRecheckLogDownloadedAutoOn prüft die Info-Zeile beim Download mit
// eingeschaltetem Auto-Update.
func TestRecheckLogDownloadedAutoOn(t *testing.T) {
	m, _, logPath := rcModel(t, "48", true)
	rcUpdate(t, m, updateResultMsg{pending: &pendingUpdate{build: 49}})
	log := rcLog(t, logPath)
	want := "[INFO] Update v0.49 heruntergeladen und geprüft, wird beim Beenden installiert"
	if !strings.Contains(log, want) {
		t.Errorf("Protokoll = %q, erwartet %q", log, want)
	}
	if strings.Contains(log, "keine Installation") {
		t.Errorf("Protokoll behauptet \"keine Installation\" bei Auto-Update an: %q", log)
	}
}

// TestRecheckLogDownloadedAutoOff prüft die Zeile bei ausgeschaltetem
// Auto-Update (Suche lief noch, Nutzer schaltete zwischendurch aus): Sie sagt
// ehrlich "keine Installation" statt "wird beim Beenden installiert".
func TestRecheckLogDownloadedAutoOff(t *testing.T) {
	m, _, logPath := rcModel(t, "48", false)
	rcUpdate(t, m, updateResultMsg{pending: &pendingUpdate{build: 49}})
	log := rcLog(t, logPath)
	want := "[INFO] Update v0.49 heruntergeladen und geprüft, Auto-Update ist aus (keine Installation)"
	if !strings.Contains(log, want) {
		t.Errorf("Protokoll = %q, erwartet %q", log, want)
	}
	if strings.Contains(log, "wird beim Beenden installiert") {
		t.Errorf("Protokoll verspricht Installation bei Auto-Update aus: %q", log)
	}
}

// TestRecheckLogError prüft die Warnung bei Fehlern (Level WARN, nicht ERROR).
func TestRecheckLogError(t *testing.T) {
	m, _, logPath := rcModel(t, "48", true)
	rcUpdate(t, m, updateResultMsg{err: errors.New("kein Netz-4711")})
	log := rcLog(t, logPath)
	if !strings.Contains(log, "[WARN] Auto-Update fehlgeschlagen: kein Netz-4711") {
		t.Errorf("Protokoll = %q, erwartet WARN-Zeile mit Fehlertext", log)
	}
	if strings.Contains(log, "[ERROR]") {
		t.Errorf("Fehler der Suche als ERROR protokolliert: %q", log)
	}
}

// TestRecheckLogWithoutLogger prüft: Ohne Protokoll (sshManager nil, Logger
// nil) darf die Verarbeitung der Ergebnisse nicht paniken.
func TestRecheckLogWithoutLogger(t *testing.T) {
	m, _, _ := rcModel(t, "48", true)
	m.sshManager = nil
	rcUpdate(t, m, updateResultMsg{})
	rcUpdate(t, m, updateResultMsg{err: errors.New("x")})
	rcUpdate(t, m, updateResultMsg{pending: &pendingUpdate{build: 49}})
	m.sshManager = NewSSHManager(nil)
	rcUpdate(t, m, updateResultMsg{})
}

// TestRecheckLogTickWritesNothing prüft: Der Wecker selbst (Suchstart) schreibt
// keine Zeile - sonst füllte sich das Protokoll mit Rauschen. Erst das
// Ergebnis wird protokolliert.
func TestRecheckLogTickWritesNothing(t *testing.T) {
	m, u, logPath := rcModel(t, "48", true)
	if _, cmd := rcUpdate(t, m, updateResultMsg{tick: true, gen: u.recheckGen.Load()}); cmd == nil {
		t.Fatal("Aufbau: Wecker startete keine Suche")
	}
	if log := rcLog(t, logPath); log != "" {
		t.Errorf("Wecker schrieb ins Protokoll: %q", log)
	}
}

// --- 6. ApplyOnExit ---

// TestApplyOnExitLogsAutoUpdateOff prüft die Hinweiszeile bei ausgeschaltetem
// Auto-Update; ein bereitliegendes Update bleibt unberührt, nichts wird
// gestartet.
func TestApplyOnExitLogsAutoUpdateOff(t *testing.T) {
	u, fake := newPendingSetupUpdater(t)
	pending := u.Pending()
	u.SetInstallOnExit(false)
	logger, logPath := fileLogger(t)
	u.ApplyOnExit(logger)
	log := rcLog(t, logPath)
	if !strings.Contains(log, "[INFO] Auto-Update aus, nichts installiert") {
		t.Errorf("Protokoll = %q, erwartet \"Auto-Update aus, nichts installiert\"", log)
	}
	if fake.callCount() != 0 || u.Pending() != pending {
		t.Errorf("Installation trotz Schalter aus (Aufrufe %d)", fake.callCount())
	}
}

// TestApplyOnExitLogsNoUpdateStaged prüft die Hinweiszeile "Kein Update
// vorgemerkt" bei eingeschaltetem Auto-Update ohne geladenes Update.
func TestApplyOnExitLogsNoUpdateStaged(t *testing.T) {
	u := NewUpdater("48")
	u.SetInstallOnExit(true)
	logger, logPath := fileLogger(t)
	u.ApplyOnExit(logger)
	log := rcLog(t, logPath)
	if !strings.Contains(log, "[INFO] Kein Update vorgemerkt") {
		t.Errorf("Protokoll = %q, erwartet \"Kein Update vorgemerkt\"", log)
	}
	if strings.Contains(log, "Auto-Update aus") {
		t.Errorf("Protokoll nennt \"Auto-Update aus\" bei eingeschaltetem Schalter: %q", log)
	}
}

// TestApplyOnExitNoteOnlyOnce prüft: Normalweg und Notfallweg rufen
// ApplyOnExit nacheinander auf; die Hinweiszeile erscheint nur einmal - bei
// "aus", bei "Kein Update vorgemerkt" und auch wenn zwischen den Aufrufen
// der Schalter wechselt (dann gilt der Hinweis des ersten Aufrufs).
func TestApplyOnExitNoteOnlyOnce(t *testing.T) {
	t.Run("aus", func(t *testing.T) {
		u := NewUpdater("48")
		logger, logPath := fileLogger(t)
		u.ApplyOnExit(logger)
		u.ApplyOnExit(logger)
		if n := rcCountLines(rcLog(t, logPath), "Auto-Update aus, nichts installiert"); n != 1 {
			t.Errorf("Hinweis %d mal geschrieben, erwartet 1", n)
		}
	})
	t.Run("kein Update", func(t *testing.T) {
		u := NewUpdater("48")
		u.SetInstallOnExit(true)
		logger, logPath := fileLogger(t)
		u.ApplyOnExit(logger)
		u.ApplyOnExit(logger)
		u.ApplyOnExit(logger)
		if n := rcCountLines(rcLog(t, logPath), "Kein Update vorgemerkt"); n != 1 {
			t.Errorf("Hinweis %d mal geschrieben, erwartet 1", n)
		}
	})
	t.Run("Schalter wechselt dazwischen", func(t *testing.T) {
		u := NewUpdater("48")
		logger, logPath := fileLogger(t)
		u.ApplyOnExit(logger) // aus
		u.SetInstallOnExit(true)
		u.ApplyOnExit(logger) // an, nichts vorgemerkt
		log := rcLog(t, logPath)
		if n := rcCountLines(log, "nichts installiert") + rcCountLines(log, "Kein Update vorgemerkt"); n != 1 {
			t.Errorf("%d Hinweiszeilen insgesamt, erwartet 1:\n%s", n, log)
		}
	})
	t.Run("gleichzeitig (race)", func(t *testing.T) {
		u := NewUpdater("48")
		logger, logPath := fileLogger(t)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); u.ApplyOnExit(logger) }()
		}
		wg.Wait()
		if n := rcCountLines(rcLog(t, logPath), "Auto-Update aus, nichts installiert"); n != 1 {
			t.Errorf("Hinweis bei 8 gleichzeitigen Aufrufen %d mal geschrieben, erwartet 1", n)
		}
	})
	t.Run("nil-Logger", func(t *testing.T) {
		u := NewUpdater("48")
		u.ApplyOnExit(nil) // darf nicht paniken
		u.SetInstallOnExit(true)
		u.ApplyOnExit(nil)
		var nilUpdater *Updater
		nilUpdater.ApplyOnExit(nil)
	})
}

// TestApplyOnExitNoNoteAfterInstall prüft: Nach einer Installation schreibt
// der zweite Aufruf (Notfallweg) NICHT "Kein Update vorgemerkt" - das wäre
// irreführend, weil das Update eben installiert wurde. Die Installationszeile
// bleibt genau einmal.
func TestApplyOnExitNoNoteAfterInstall(t *testing.T) {
	u, fake := newPendingSetupUpdater(t)
	u.SetInstallOnExit(true)
	logger, logPath := fileLogger(t)
	u.ApplyOnExit(logger)
	u.ApplyOnExit(logger)
	log := rcLog(t, logPath)
	if n := rcCountLines(log, "Update-Helfer für v0.48 gestartet"); n != 1 {
		t.Errorf("Installationszeile %d mal, erwartet 1:\n%s", n, log)
	}
	if strings.Contains(log, "Kein Update vorgemerkt") {
		t.Errorf("zweiter Aufruf schrieb \"Kein Update vorgemerkt\" nach der Installation:\n%s", log)
	}
	if fake.callCount() != 1 {
		t.Errorf("startSetup %d mal aufgerufen, erwartet 1", fake.callCount())
	}
}

// TestApplyOnExitNoNoteAfterFailedInstall prüft: Auch nach einer
// fehlgeschlagenen Installation kommt im zweiten Aufruf keine Hinweiszeile;
// der Fehler selbst bleibt als ERROR im Protokoll.
func TestApplyOnExitNoNoteAfterFailedInstall(t *testing.T) {
	u, fake := newPendingSetupUpdater(t)
	fake.err = errors.New("UAC abgelehnt-4711")
	u.SetInstallOnExit(true)
	logger, logPath := fileLogger(t)
	u.ApplyOnExit(logger)
	u.ApplyOnExit(logger)
	log := rcLog(t, logPath)
	if !strings.Contains(log, "[ERROR] Update auf v0.48 fehlgeschlagen") || !strings.Contains(log, "UAC abgelehnt-4711") {
		t.Errorf("Fehler nicht protokolliert:\n%s", log)
	}
	if strings.Contains(log, "Kein Update vorgemerkt") || strings.Contains(log, "nichts installiert") {
		t.Errorf("Hinweiszeile nach fehlgeschlagener Installation:\n%s", log)
	}
}

// TestApplyOnExitInstallStillLoggedAfterNote prüft: Eine früher geschriebene
// Hinweiszeile unterdrückt NIE die Meldung einer echten Installation. Erst
// "Auto-Update aus" (Hinweis), dann Schalter an und Aufruf mit Pending: Die
// Installation wird protokolliert.
func TestApplyOnExitInstallStillLoggedAfterNote(t *testing.T) {
	u, fake := newPendingSetupUpdater(t)
	u.SetInstallOnExit(false)
	logger, logPath := fileLogger(t)
	u.ApplyOnExit(logger)
	u.SetInstallOnExit(true)
	u.ApplyOnExit(logger)
	log := rcLog(t, logPath)
	if !strings.Contains(log, "Update-Helfer für v0.48 gestartet") {
		t.Errorf("Installation nicht protokolliert, weil vorher ein Hinweis stand:\n%s", log)
	}
	if fake.callCount() != 1 {
		t.Errorf("startSetup %d mal aufgerufen, erwartet 1", fake.callCount())
	}
}

// TestApplyOnExitNeverUsesNetwork prüft: ApplyOnExit sucht und lädt NIE, auch
// nicht bei eingeschaltetem Auto-Update ohne Pending, obwohl auf dem Server
// ein neueres Release liegt (v0.48 gegen Build 47). Kontrolle: Derselbe
// Updater findet es über CheckAndDownload sehr wohl (Zähler > 0). Der
// Notfallweg hat nur Sekunden Zeit, ein Download würde ihn sprengen.
func TestApplyOnExitNeverUsesNetwork(t *testing.T) {
	for _, on := range []bool{true, false} {
		on := on
		t.Run(fmt.Sprintf("Auto-Update %v", on), func(t *testing.T) {
			srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
			u, exe := newTestUpdater(t, srv, "47", "linux")
			ct := rcCountRequests(u)
			u.SetInstallOnExit(on)
			logger, logPath := fileLogger(t)

			u.ApplyOnExit(logger)
			u.ApplyOnExit(logger)
			if ct.total() != 0 {
				t.Errorf("ApplyOnExit stellte %d Netzanfragen, erwartet 0", ct.total())
			}
			if u.Pending() != nil {
				t.Error("ApplyOnExit hat ein Update geladen")
			}
			if data, _ := os.ReadFile(exe); string(data) == string(fakeELF) {
				t.Error("Binary wurde ersetzt, obwohl nichts geladen war")
			}
			if _, err := os.Stat(exe + ".new"); err == nil {
				t.Error("Download-Datei .new entstand beim Beenden")
			}
			if log := rcLog(t, logPath); log == "" {
				t.Error("kein Hinweis im Protokoll")
			}

			// Kontrolle: der Server hätte ein Update geliefert, der Zähler zählt
			if _, err := u.CheckAndDownload(context.Background()); err != nil {
				t.Fatalf("Kontrolle: CheckAndDownload: %v", err)
			}
			if ct.total() == 0 || u.Pending() == nil {
				t.Errorf("Kontrolle: Zähler %d, Pending %v - der Aufbau beweist nichts", ct.total(), u.Pending())
			}
		})
	}
}

// --- 7. CheckAndDownload mit vorhandenem Pending ---

// TestCheckAndDownloadReturnsExistingPending prüft: Liegt schon ein
// geprüftes Update bereit, liefert ein zweiter Aufruf dasselbe Objekt und der
// Server sieht keinen zweiten Download (auch keine zweite API-Abfrage).
// Sonst überschriebe ein zweiter Download die Datei, während ApplyOnExit sie
// womöglich gerade installiert.
func TestCheckAndDownloadReturnsExistingPending(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, _ := newTestUpdater(t, srv, "47", "linux")
	ct := rcCountRequests(u)

	first, err := u.CheckAndDownload(context.Background())
	if err != nil || first == nil {
		t.Fatalf("erster Aufruf: %v, %v", first, err)
	}
	if ct.exact("/download/ssh-easy") != 1 {
		t.Fatalf("Aufbau: %d Downloads der Binary beim ersten Aufruf, erwartet 1", ct.exact("/download/ssh-easy"))
	}
	requests := ct.total()
	// Datei markieren: Ein zweiter Download würde den Inhalt überschreiben.
	// Die Markierung ist bewusst NICHT der Inhalt des Servers (fakeELF); sonst
	// fiele ein erneuter Download nicht auf. CheckAndDownload prüft bei
	// vorhandenem Pending die Datei nicht erneut (die SHA-256 prüft erst die
	// Installation), die Markierung stört den Test also nicht.
	marker := []byte("MARKIERUNG-kein-zweiter-Download")
	if err := os.WriteFile(first.path, marker, 0o755); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		again, err := u.CheckAndDownload(context.Background())
		if err != nil {
			t.Fatalf("Aufruf %d: %v", i+2, err)
		}
		if again != first {
			t.Errorf("Aufruf %d lieferte ein anderes Objekt (%p statt %p)", i+2, again, first)
		}
	}
	if ct.total() != requests {
		t.Errorf("zweiter Aufruf stellte %d weitere Netzanfragen (API oder Download)", ct.total()-requests)
	}
	// Die Datei trägt noch die Markierung: kein erneuter Download hat sie überschrieben
	if data, err := os.ReadFile(first.path); err != nil || string(data) != string(marker) {
		t.Errorf("Datei nach den Folgeaufrufen: %q (Fehler %v), erwartet die Markierung %q - es wurde erneut geladen",
			data, err, marker)
	}
}

// TestCheckAndDownloadExistingPendingIgnoresCancelledContext prüft: Mit
// bereitliegendem Update kommt die Antwort auch dann sofort, wenn der Kontext
// schon abgebrochen ist - ein Beweis, dass gar nicht erst das Netz benutzt wird.
func TestCheckAndDownloadExistingPendingIgnoresCancelledContext(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, _ := newTestUpdater(t, srv, "47", "linux")
	first, err := u.CheckAndDownload(context.Background())
	if err != nil || first == nil {
		t.Fatalf("erster Aufruf: %v, %v", first, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	again, err := u.CheckAndDownload(ctx)
	if err != nil || again != first {
		t.Errorf("mit abgebrochenem Kontext: %v, %v; erwartet dasselbe Pending ohne Fehler", again, err)
	}
}

// TestCheckAndDownloadSetupLoadedOnlyOnce prüft den Windows-Setup-Weg: Ein
// zweiter Aufruf legt keinen weiteren setup-*-Ordner an (der würde sonst
// liegen bleiben).
func TestCheckAndDownloadSetupLoadedOnlyOnce(t *testing.T) {
	u, _ := newPendingSetupUpdater(t)
	first := u.Pending()
	ct := rcCountRequests(u)
	again, err := u.CheckAndDownload(context.Background())
	if err != nil || again != first {
		t.Fatalf("zweiter Aufruf: %v, %v; erwartet dasselbe Pending", again, err)
	}
	if ct.total() != 0 {
		t.Errorf("%d Netzanfragen beim zweiten Aufruf", ct.total())
	}
	entries, err := os.ReadDir(u.downloadDir)
	if err != nil {
		t.Fatal(err)
	}
	dirs := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "setup-") {
			dirs++
		}
	}
	if dirs != 1 {
		t.Errorf("%d setup-*-Ordner in %s, erwartet 1", dirs, filepath.Base(u.downloadDir))
	}
}

// TestCheckAndDownloadLoadsAgainAfterApply prüft die Gegenprobe: Nach der
// Installation ist das Pending leer, und ein weiterer Aufruf lädt wieder
// (neues Objekt, neue Netzanfragen). Die Sperre hängt also nur am
// bereitliegenden Update, nicht an einem "schon mal geladen"-Merker.
func TestCheckAndDownloadLoadsAgainAfterApply(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, _ := newTestUpdater(t, srv, "47", "linux")
	ct := rcCountRequests(u)
	first, err := u.CheckAndDownload(context.Background())
	if err != nil || first == nil {
		t.Fatalf("erster Aufruf: %v, %v", first, err)
	}
	if err := u.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if u.Pending() != nil {
		t.Fatal("Pending nach Apply nicht leer")
	}
	requests := ct.total()
	second, err := u.CheckAndDownload(context.Background())
	if err != nil || second == nil {
		t.Fatalf("Aufruf nach Apply: %v, %v", second, err)
	}
	if second == first {
		t.Error("nach Apply dasselbe Objekt geliefert")
	}
	if ct.total() <= requests {
		t.Error("nach Apply keine neuen Netzanfragen")
	}
}
