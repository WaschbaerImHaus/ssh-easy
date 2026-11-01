// Paket main - Tests für das Update beim Beenden über Schließkreuz/Alt+F4 (Build 87)
//
// Hintergrund: Das Update muss auch dann installiert werden, wenn das Fenster
// geschlossen wird (SIGHUP/CTRL_CLOSE_EVENT), nicht nur bei "q". Unter Windows
// startet dafür ein abgekoppelter Helfer-Prozess das Setup (UAC darf lange
// dauern, der Hauptprozess wird nach ca. 5 s hart beendet).
// Spezifikation: Scratchpad spec_exit.md (Abschnitt A, Testfälle 6 bis 9, 11).
//
// FESTGELEGTE SIGNATUREN (src/updater.go, updater_windows.go, updater_unix.go;
// die Specialists übernehmen sie):
//
//	func (u *Updater) SetInstallOnExit(on bool)
//	func (u *Updater) InstallOnExit() bool               // nil-Updater -> false
//	func (u *Updater) ApplyOnExit(logger *Logger)        // kein Rückgabewert; nil-Updater sicher
//	    Tut nichts, wenn InstallOnExit() false oder Pending() nil. Sonst Apply();
//	    protokolliert Erfolg ("... v0.<Build> ...") bzw. Fehler (Build + Fehlertext).
//	Updater.startSetup func(path, sha string) error      // Naht, Feld im Updater
//	    Apply() startet bei updateRunSetup über u.startSetup(pending.path,
//	    pending.sha256) (NewUpdater setzt den plattformeigenen Standard).
//	    Bei Fehler wird die Setup-Datei gelöscht (wie bisher), bei Erfolg NICHT.
//	Updater.applyMu sync.Mutex                           // hält das GANZE Apply:
//	    Ein zweiter, gleichzeitiger Apply/ApplyOnExit-Aufruf WARTET, bis der erste
//	    fertig ist (kehrt nicht vorher zurück, sonst würde der Notfall-Weg das
//	    Programm beenden, während das Setup noch gestartet wird) und findet dann
//	    pending == nil.
//	    ACHTUNG: Auch ApplyOnExit muss applyMu über Pending-Prüfung UND Installation
//	    halten (intern eine Variante applyLocked ohne eigenes Locken), sonst sieht
//	    der zweite Aufruf pending == nil und kehrt sofort zurück.
//
//	const installHelperFlag = "--install-update"
//	func installHelperArgs(path, sha string, pid int) []string
//	    Liefert GENAU 4 Elemente: {installHelperFlag, path, sha, strconv.Itoa(pid)}
//	    (Argumente für exec.Command(exePath, args...); jeder Pfad ein Argument).
//	func validateInstallHelperArgs(args []string, downloadDir, goos, goarch string)
//	    (path, sha string, pid int, err error)
//	    args sind die Argumente NACH dem Flag (os.Args[2:]), also {path, sha, pid}.
//	    Reine Funktion ohne Dateizugriff. Gültig nur: absoluter, unveränderter
//	    (filepath.Clean(path) == path) Pfad der Form
//	    <downloadDir>/setup-*/<updateAssetName(goos, goarch, true)>; SHA-256 =
//	    64 Hex-Zeichen (Groß-/Kleinschreibung egal); PID = positive Dezimalzahl
//	    (int); genau 3 Argumente; downloadDir nicht leer; Pfad ohne Steuerzeichen.
//	func isInstallHelperInvocation(args []string) bool
//	    args = os.Args; true nur bei exakt args[1] == installHelperFlag.
//
//	func cleanupUpdateLeftovers(exePath, downloadDir string, minAge time.Duration)
//	    NEUER dritter Parameter. setup-*-Ordner werden nur gelöscht, wenn ihre
//	    Änderungszeit (ModTime) mindestens minAge zurückliegt (ein Helfer wartet
//	    evtl. noch auf die UAC-Bestätigung). exe.old/.new und Setups im alten
//	    Ablageformat (Datei ssh-easy-setup-* direkt im Ordner) fallen weiter immer.
//	    Symlinks namens setup-* werden nie verfolgt oder gelöscht.
//	const setupLeftoverMinAge = 10 * time.Minute         // Wert, den main übergibt
//	    (Spannweite laut Test: 1 Minute bis 1 Stunde)
//
// ÄNDERUNGEN BUILD 88 (Spezifikation spec88.md, Abschnitte N2, N3, H1):
//
//	func touchSetupDir(setupPath string, now time.Time) error
//	    Plattformneutral; setzt Änderungs- und Zugriffszeit des Ordners
//	    filepath.Dir(setupPath) auf now (os.Chtimes). Fehler (z.B. Ordner fehlt)
//	    werden zurückgegeben, nicht verschluckt. updater_windows.go ruft die
//	    Funktion in startDetachedSetupInstaller vor dem Start des Helfers auf.
//	validateInstallHelperArgs: der Ordner <downloadDir>/setup-* wird EXAKT
//	    verglichen (kein strings.EqualFold, auch nicht unter goos == "windows").
//	Updater.ApplyOnExit: Setup-Weg protokolliert "Update-Helfer für v0.N gestartet",
//	    Ersetzen-Weg weiter "Update auf v0.N installiert".
//
// VON MIR FESTGELEGT: Das Protokoll des Setup-Wegs muss "gestartet" und die
// Version "v0.48" enthalten und darf "installiert" NICHT enthalten.
//
// @author Kurt Ingwer
// @date   2026-11-01 10:00
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// --- Hilfsmittel ---

// fakeSetupStarter ersetzt startSetup: zeichnet Aufrufe auf, kann blockieren
// und einen Fehler liefern.
type fakeSetupStarter struct {
	mu      sync.Mutex
	calls   [][2]string
	err     error
	entered chan struct{} // bekommt pro Aufruf ein Element (gepuffert)
	release chan struct{} // nil = nicht blockieren
}

// start ist die Funktion, die in u.startSetup eingehängt wird.
func (f *fakeSetupStarter) start(path, sha string) error {
	f.mu.Lock()
	f.calls = append(f.calls, [2]string{path, sha})
	f.mu.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release != nil {
		<-f.release
	}
	return f.err
}

// callCount liefert die Zahl der Aufrufe.
func (f *fakeSetupStarter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// newPendingSetupUpdater liefert einen Updater (Windows-Setup-Weg), der ein
// heruntergeladenes Setup als Pending hat und startSetup durch einen Fake ersetzt.
func newPendingSetupUpdater(t *testing.T) (*Updater, *fakeSetupStarter) {
	t.Helper()
	pe := append([]byte("MZ"), []byte("setup-48")...)
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy-setup-amd64.exe", content: pe,
		extraAsset: "ssh-easy-windows-amd64.exe"})
	u, _ := newTestUpdater(t, srv, "47", "windows")
	u.canWriteDir = func(string) bool { return false }
	if _, err := u.CheckAndDownload(context.Background()); err != nil {
		t.Fatalf("CheckAndDownload: %v", err)
	}
	if u.Pending() == nil || u.Pending().kind != updateRunSetup {
		t.Fatal("Setup-Update nicht bereitgestellt")
	}
	fake := &fakeSetupStarter{entered: make(chan struct{}, 8)}
	u.startSetup = fake.start
	return u, fake
}

// fileLogger liefert einen Logger mit eigener Datei und deren Pfad.
func fileLogger(t *testing.T) (*Logger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.log")
	return &Logger{filePath: path, enabled: true}, path
}

// --- Fall 6: InstallOnExit, ApplyOnExit, startSetup, applyMu ---

// TestInstallOnExitToggle prüft Standard (aus) und Umschalten; nil-Updater
// ist sicher (der Beenden-Pfad darf nie paniken).
func TestInstallOnExitToggle(t *testing.T) {
	u := NewUpdater("48")
	if u.InstallOnExit() {
		t.Error("Standard muss aus sein")
	}
	u.SetInstallOnExit(true)
	if !u.InstallOnExit() {
		t.Error("nach SetInstallOnExit(true) nicht an")
	}
	u.SetInstallOnExit(false)
	if u.InstallOnExit() {
		t.Error("nach SetInstallOnExit(false) nicht aus")
	}
	var nilUpdater *Updater
	if nilUpdater.InstallOnExit() {
		t.Error("nil-Updater: InstallOnExit muss false sein")
	}
	nilUpdater.ApplyOnExit(&Logger{})
}

// TestInstallOnExitRaceFree (mit -race) setzt und liest den Schalter aus
// mehreren Goroutinen: Der Signal-Watcher liest, die Oberfläche schreibt.
func TestInstallOnExitRaceFree(t *testing.T) {
	u := NewUpdater("48")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				u.SetInstallOnExit((n+i)%2 == 0)
			}
		}(i)
		go func() {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				_ = u.InstallOnExit()
			}
		}()
	}
	wg.Wait()
}

// TestApplyOnExitDisabledDoesNothing prüft: Ist Auto-Update beim Beenden aus,
// wird nichts installiert, und das Update bleibt unangetastet.
func TestApplyOnExitDisabledDoesNothing(t *testing.T) {
	u, fake := newPendingSetupUpdater(t)
	pending := u.Pending()
	u.SetInstallOnExit(false)
	logger, _ := fileLogger(t)
	u.ApplyOnExit(logger)
	if fake.callCount() != 0 {
		t.Errorf("startSetup %d mal aufgerufen, erwartet 0", fake.callCount())
	}
	if u.Pending() != pending {
		t.Error("Pending wurde verändert")
	}
	if _, err := os.Stat(pending.path); err != nil {
		t.Errorf("Setup-Datei fehlt: %v", err)
	}
}

// TestApplyOnExitInstalls prüft den Erfolgsfall: startSetup genau einmal mit
// Pfad und Prüfsumme, die Setup-Datei bleibt (der Helfer braucht sie noch),
// Pending ist leer, das Protokoll nennt die Version.
func TestApplyOnExitInstalls(t *testing.T) {
	u, fake := newPendingSetupUpdater(t)
	pending := u.Pending()
	u.SetInstallOnExit(true)
	logger, logPath := fileLogger(t)
	u.ApplyOnExit(logger)
	if fake.callCount() != 1 {
		t.Fatalf("startSetup %d mal aufgerufen, erwartet 1", fake.callCount())
	}
	if got := fake.calls[0]; got[0] != pending.path || got[1] != pending.sha256 {
		t.Errorf("startSetup(%q, %q), erwartet (%q, %q)", got[0], got[1], pending.path, pending.sha256)
	}
	if _, err := os.Stat(pending.path); err != nil {
		t.Errorf("Setup-Datei nach Erfolg gelöscht: %v", err)
	}
	if u.Pending() != nil {
		t.Error("Pending nach Installation nicht leer")
	}
	if data, _ := os.ReadFile(logPath); !strings.Contains(string(data), "v0.48") {
		t.Errorf("Protokoll nennt die Version nicht: %q", data)
	}
}

// TestApplyOnExitSetupLogsStarted (H1): Auf dem Setup-Weg (Windows) ist beim
// Rückkehren von startSetup nur der Helfer GESTARTET, das Update aber noch
// nicht installiert (UAC steht noch aus). Das Protokoll sagt deshalb
// "gestartet" mit der Version und nicht "installiert".
func TestApplyOnExitSetupLogsStarted(t *testing.T) {
	u, fake := newPendingSetupUpdater(t)
	u.SetInstallOnExit(true)
	logger, logPath := fileLogger(t)
	u.ApplyOnExit(logger)
	if fake.callCount() != 1 {
		t.Fatalf("startSetup %d mal aufgerufen, erwartet 1", fake.callCount())
	}
	data, _ := os.ReadFile(logPath)
	log := string(data)
	if !strings.Contains(log, "gestartet") || !strings.Contains(log, "v0.48") {
		t.Errorf("Protokoll = %q, erwartet \"gestartet\" und v0.48", log)
	}
	if strings.Contains(log, "installiert") {
		t.Errorf("Protokoll behauptet \"installiert\", obwohl nur der Helfer gestartet wurde: %q", log)
	}
}

// TestApplyOnExitReplaceLogsInstalled (H1, Gegenprobe): Auf dem Ersetzen-Weg
// (Binary getauscht) bleibt es bei "installiert", nicht "gestartet".
func TestApplyOnExitReplaceLogsInstalled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Ersetzen-Weg wird hier mit Unix-Rename geprüft")
	}
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, exe := newTestUpdater(t, srv, "47", "linux")
	if _, err := u.CheckAndDownload(context.Background()); err != nil {
		t.Fatal(err)
	}
	u.SetInstallOnExit(true)
	logger, logPath := fileLogger(t)
	u.ApplyOnExit(logger)
	if data, _ := os.ReadFile(exe); string(data) != string(fakeELF) {
		t.Fatalf("Binary nicht ersetzt: %q", data)
	}
	data, _ := os.ReadFile(logPath)
	log := string(data)
	if !strings.Contains(log, "installiert") || !strings.Contains(log, "v0.48") {
		t.Errorf("Protokoll = %q, erwartet \"installiert\" und v0.48", log)
	}
	if strings.Contains(log, "gestartet") {
		t.Errorf("Ersetzen-Weg protokolliert \"gestartet\": %q", log)
	}
}

// TestApplyOnExitWithoutPending prüft: Schalter an, aber nichts geladen ->
// nichts passiert (kein Aufruf, kein Absturz).
func TestApplyOnExitWithoutPending(t *testing.T) {
	u, fake := newPendingSetupUpdater(t)
	u.pending = nil
	u.SetInstallOnExit(true)
	u.ApplyOnExit(&Logger{})
	if fake.callCount() != 0 {
		t.Errorf("startSetup %d mal aufgerufen, erwartet 0", fake.callCount())
	}
}

// TestApplyOnExitStartFailure prüft den Fehlerfall (z.B. UAC abgelehnt):
// Der Fehler steht im Protokoll, die Setup-Datei wird entfernt (wie bisher bei
// Fehlern), Pending ist leer, kein Absturz.
func TestApplyOnExitStartFailure(t *testing.T) {
	u, fake := newPendingSetupUpdater(t)
	pending := u.Pending()
	fake.err = errors.New("UAC abgelehnt-4711")
	u.SetInstallOnExit(true)
	logger, logPath := fileLogger(t)
	u.ApplyOnExit(logger)
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "UAC abgelehnt-4711") || !strings.Contains(string(data), "v0.48") {
		t.Errorf("Protokoll = %q, erwartet Version und Fehlertext", data)
	}
	if _, err := os.Stat(pending.path); !os.IsNotExist(err) {
		t.Error("Setup-Datei nach Fehler nicht entfernt")
	}
	if u.Pending() != nil {
		t.Error("Pending nach Fehler nicht leer")
	}
}

// runTwoConcurrentCalls (mit -race) ruft zweimal gleichzeitig auf, während
// startSetup noch läuft. Erwartet: startSetup nur einmal, und der ZWEITE Aufruf
// kehrt erst zurück, wenn der erste fertig ist. Sonst würde der Notfall-Weg
// (Signal-Watcher) das Programm beenden, während der Hauptweg das Setup noch
// startet.
func runTwoConcurrentCalls(t *testing.T, call func(u *Updater)) {
	t.Helper()
	u, fake := newPendingSetupUpdater(t)
	u.SetInstallOnExit(true)
	fake.release = make(chan struct{})

	firstDone := make(chan struct{})
	secondDone := make(chan struct{})
	go func() { call(u); close(firstDone) }()
	select {
	case <-fake.entered:
	case <-time.After(3 * time.Second):
		close(fake.release)
		t.Fatal("startSetup wurde nicht aufgerufen")
	}
	go func() { call(u); close(secondDone) }()
	if waitChan(secondDone, 300*time.Millisecond) {
		close(fake.release)
		t.Fatal("zweiter Aufruf kehrte zurück, während der erste noch installiert (applyMu fehlt)")
	}
	close(fake.release)
	if !waitChan(firstDone, 3*time.Second) || !waitChan(secondDone, 3*time.Second) {
		t.Fatal("Aufrufe kehrten nach Freigabe nicht zurück")
	}
	if fake.callCount() != 1 {
		t.Errorf("startSetup %d mal aufgerufen, erwartet genau 1", fake.callCount())
	}
}

// TestApplyConcurrentStartsSetupOnce prüft Apply() bei zwei gleichzeitigen Aufrufen.
func TestApplyConcurrentStartsSetupOnce(t *testing.T) {
	runTwoConcurrentCalls(t, func(u *Updater) { _ = u.Apply() })
}

// TestApplyOnExitConcurrentWaits prüft dasselbe über ApplyOnExit (Normalweg
// und Notfallweg landen gleichzeitig dort).
func TestApplyOnExitConcurrentWaits(t *testing.T) {
	runTwoConcurrentCalls(t, func(u *Updater) { u.ApplyOnExit(&Logger{}) })
}

// TestSetUpdaterAdoptsAutoUpdate prüft: SetUpdater übernimmt den Schalter aus
// der Konfiguration (nicht aus dem Modell, das der Signal-Watcher nicht lesen
// darf) - an, aus, und ein vorher gesetzter Wert wird überschrieben.
func TestSetUpdaterAdoptsAutoUpdate(t *testing.T) {
	m := newListTestModel(t, "A")
	if err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		cfg.AutoUpdate = true
		return true
	}); err != nil {
		t.Fatal(err)
	}
	on := NewAppModel(m.configPath, "48", NewSSHManager(NewLogger()))
	u := NewUpdater("48")
	on.SetUpdater(u)
	if !u.InstallOnExit() {
		t.Error("Config auto_update=true: InstallOnExit müsste an sein")
	}

	if err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		cfg.AutoUpdate = false
		return true
	}); err != nil {
		t.Fatal(err)
	}
	off := NewAppModel(m.configPath, "48", NewSSHManager(NewLogger()))
	u2 := NewUpdater("48")
	u2.SetInstallOnExit(true) // alter Wert muss überschrieben werden
	off.SetUpdater(u2)
	if u2.InstallOnExit() {
		t.Error("Config auto_update=false: InstallOnExit müsste aus sein")
	}
	off.SetUpdater(nil) // darf nicht paniken
}

// TestToggleAutoUpdateSetsInstallOnExit prüft, dass die Taste u den Schalter
// im Updater mitführt (sonst installierte das Schließkreuz nach dem Umschalten
// falsch).
func TestToggleAutoUpdateSetsInstallOnExit(t *testing.T) {
	m := newListTestModel(t, "A")
	u := NewUpdater("48")
	m.updater = u
	updated, _ := m.Update(keyMsgFor("u")) // Kommando (Netzsuche) wird nicht ausgeführt
	m = updated.(AppModel)
	if !m.autoUpdate || !u.InstallOnExit() {
		t.Errorf("nach u: autoUpdate=%v, InstallOnExit=%v, erwartet beide an", m.autoUpdate, u.InstallOnExit())
	}
	updated, _ = m.Update(keyMsgFor("u"))
	m = updated.(AppModel)
	if m.autoUpdate || u.InstallOnExit() {
		t.Errorf("nach 2. u: autoUpdate=%v, InstallOnExit=%v, erwartet beide aus", m.autoUpdate, u.InstallOnExit())
	}
}

// replaceFlowOutcome simuliert "Fenster geschlossen im Menü": SIGHUP kommt an,
// quit hängt (tea.Exec), der Notfall-Ablauf des Watchers ruft ApplyOnExit.
// Liefert den Inhalt der Binary danach und den exit-Code.
func replaceFlowOutcome(t *testing.T, installOnExit bool) (binary []byte, exitCode int) {
	t.Helper()
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, exe := newTestUpdater(t, srv, "47", "linux")
	if _, err := u.CheckAndDownload(context.Background()); err != nil {
		t.Fatal(err)
	}
	u.SetInstallOnExit(installOnExit)
	release := make(chan struct{})
	defer close(release)
	h := startWatcher(100*time.Millisecond, func(h *watchHarness) { <-release },
		func(h *watchHarness) *exitTasks {
			return &exitTasks{apply: func() { u.ApplyOnExit(&Logger{}) }}
		})
	h.sigs <- syscall.SIGHUP
	select {
	case exitCode = <-h.exitCodes:
	case <-time.After(5 * time.Second):
		t.Fatal("kein exit nach SIGHUP")
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	return data, exitCode
}

// TestReplacePathAfterSimulatedHangup prüft den Ersetzen-Weg (Linux/portabel):
// Nach einem simulierten SIGHUP (Fenster schließen) ist die Binary ersetzt,
// auch wenn die Oberfläche nicht mehr reagiert. Mit ausgeschaltetem Schalter
// bleibt sie unverändert.
func TestReplacePathAfterSimulatedHangup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Ersetzen-Weg wird hier mit Unix-Rename geprüft")
	}
	data, code := replaceFlowOutcome(t, true)
	if string(data) != string(fakeELF) {
		t.Errorf("Binary nach SIGHUP = %q, erwartet das Update", data)
	}
	if code != 0 {
		t.Errorf("exit-Code = %d, erwartet 0", code)
	}
	data, _ = replaceFlowOutcome(t, false)
	if string(data) != "\x7fELFalte-version" {
		t.Errorf("Binary bei ausgeschaltetem Schalter = %q, erwartet unverändert", data)
	}
}

// --- Fälle 7 bis 9: Helfer-Aufruf ---

// helperFixture liefert downloadDir, einen gültigen Pfad und Dateinamen.
func helperFixture(t *testing.T) (downloadDir, path string) {
	t.Helper()
	downloadDir = filepath.Join(t.TempDir(), "Müller & Söhne 100% (neu)", "update")
	path = filepath.Join(downloadDir, "setup-1234567", "ssh-easy-setup-amd64.exe")
	return downloadDir, path
}

// TestInstallHelperFlagValue hält den Namen des Schalters fest: Er steht in
// Prozessaufrufen und in icon_windows.go; eine Änderung bräuchte Absicht.
func TestInstallHelperFlagValue(t *testing.T) {
	if installHelperFlag != "--install-update" {
		t.Errorf("installHelperFlag = %q", installHelperFlag)
	}
}

// TestValidateInstallHelperArgsValid prüft gültige Aufrufe, auch mit
// Leerzeichen, &, % und Umlauten im Pfad (ein Windows-Benutzerordner kann so
// heißen; der Pfad bleibt ein einziges Argument), Groß-/Kleinschreibung der SHA
// und beiden Architekturen.
func TestValidateInstallHelperArgsValid(t *testing.T) {
	downloadDir, path := helperFixture(t)
	sha := strings.Repeat("ab", 32)
	gotPath, gotSHA, gotPID, err := validateInstallHelperArgs([]string{path, sha, "4242"}, downloadDir, "windows", "amd64")
	if err != nil {
		t.Fatalf("gültiger Aufruf abgelehnt: %v", err)
	}
	if gotPath != path || gotSHA != sha || gotPID != 4242 {
		t.Errorf("Ergebnis = (%q, %q, %d)", gotPath, gotSHA, gotPID)
	}
	if _, _, _, err := validateInstallHelperArgs([]string{path, strings.ToUpper(sha), "1"}, downloadDir, "windows", "amd64"); err != nil {
		t.Errorf("SHA in Großbuchstaben abgelehnt: %v", err)
	}
	armPath := filepath.Join(downloadDir, "setup-9", "ssh-easy-setup-arm64.exe")
	if _, _, _, err := validateInstallHelperArgs([]string{armPath, sha, "7"}, downloadDir, "windows", "arm64"); err != nil {
		t.Errorf("arm64 abgelehnt: %v", err)
	}
}

// TestValidateInstallHelperArgsRejects prüft die Ablehnungen. Der Helfer läuft
// bis zur UAC-Bestätigung mit Adminrechten im Spiel: Er darf nur das eigene,
// frisch geladene Setup anfassen - nie einen beliebigen Pfad.
func TestValidateInstallHelperArgsRejects(t *testing.T) {
	downloadDir, path := helperFixture(t)
	sha := strings.Repeat("0f", 32)
	sep := string(filepath.Separator)
	asset := "ssh-easy-setup-amd64.exe"
	other := t.TempDir()

	pathCases := map[string]string{
		"relativer Pfad":             filepath.Join("update", "setup-1", asset),
		"nur Dateiname":              asset,
		"leerer Pfad":                "",
		"Pfad mit ..":                downloadDir + sep + "setup-1" + sep + ".." + sep + "setup-1" + sep + asset,
		"Ausbruch mit ..":            downloadDir + sep + "setup-1" + sep + ".." + sep + ".." + sep + "x" + sep + "setup-2" + sep + asset,
		"doppelter Trenner":          downloadDir + sep + sep + "setup-1" + sep + asset,
		"außerhalb des Ordners":      filepath.Join(other, "setup-1", asset),
		"Nachbarordner":              filepath.Join(downloadDir+"-evil", "setup-1", asset),
		"Ordner ohne setup-":         filepath.Join(downloadDir, "foo", asset),
		"direkt im Update-Ordner":    filepath.Join(downloadDir, asset),
		"zu tief verschachtelt":      filepath.Join(downloadDir, "setup-1", "sub", asset),
		"falscher Dateiname":         filepath.Join(downloadDir, "setup-1", "evil.exe"),
		"falsche Architektur":        filepath.Join(downloadDir, "setup-1", "ssh-easy-setup-arm64.exe"),
		"Binary statt Setup":         filepath.Join(downloadDir, "setup-1", "ssh-easy-windows-amd64.exe"),
		"Steuerzeichen im Ordner":    filepath.Join(downloadDir, "setup-1\x00x", asset),
		"Zeilenumbruch im Ordner":    filepath.Join(downloadDir, "setup-1\n", asset),
		"Update-Ordner selbst":       downloadDir,
		"Dateiname mit Endung davor": filepath.Join(downloadDir, "setup-1", "x"+asset),
	}
	for name, p := range pathCases {
		if _, _, _, err := validateInstallHelperArgs([]string{p, sha, "10"}, downloadDir, "windows", "amd64"); err == nil {
			t.Errorf("%s: %q nicht abgelehnt", name, p)
		}
	}

	shaCases := map[string]string{
		"leer":           "",
		"63 Zeichen":     strings.Repeat("a", 63),
		"65 Zeichen":     strings.Repeat("a", 65),
		"kein Hex":       strings.Repeat("g", 64),
		"ein Nicht-Hex":  strings.Repeat("a", 63) + "z",
		"Leerzeichen":    strings.Repeat("a", 63) + " ",
		"Zeilenumbruch":  strings.Repeat("a", 63) + "\n",
		"Präfix sha256:": "sha256:" + strings.Repeat("a", 57),
		"Umlaut statt a": strings.Repeat("a", 62) + "ä",
	}
	for name, s := range shaCases {
		if _, _, _, err := validateInstallHelperArgs([]string{path, s, "10"}, downloadDir, "windows", "amd64"); err == nil {
			t.Errorf("SHA %s: %q nicht abgelehnt", name, s)
		}
	}

	pidCases := []string{"0", "-1", "-4242", "abc", "", " 5", "5 ", "1 2", "1.5", "0x10", "99999999999999999999999"}
	for _, p := range pidCases {
		if _, _, _, err := validateInstallHelperArgs([]string{path, sha, p}, downloadDir, "windows", "amd64"); err == nil {
			t.Errorf("PID %q nicht abgelehnt", p)
		}
	}

	for n := 0; n <= 5; n++ {
		if n == 3 {
			continue
		}
		args := make([]string, n)
		for i := range args {
			args[i] = []string{path, sha, "10", "x", "y"}[i%5]
		}
		if _, _, _, err := validateInstallHelperArgs(args, downloadDir, "windows", "amd64"); err == nil {
			t.Errorf("%d Argumente nicht abgelehnt", n)
		}
	}
	if _, _, _, err := validateInstallHelperArgs(nil, downloadDir, "windows", "amd64"); err == nil {
		t.Error("nil-Argumente nicht abgelehnt")
	}

	if _, _, _, err := validateInstallHelperArgs([]string{path, sha, "10"}, "", "windows", "amd64"); err == nil {
		t.Error("leerer downloadDir nicht abgelehnt")
	}
	if _, _, _, err := validateInstallHelperArgs([]string{path, sha, "10"}, downloadDir, "linux", "amd64"); err == nil {
		t.Error("Plattform ohne Setup (linux) nicht abgelehnt")
	}
	if _, _, _, err := validateInstallHelperArgs([]string{path, sha, "10"}, downloadDir, "windows", "386"); err == nil {
		t.Error("unbekannte Architektur nicht abgelehnt")
	}
}

// TestValidateInstallHelperArgsDirExactMatch (N3): Der Ordner
// <downloadDir>/setup-* wird EXAKT verglichen. Frühere Fassung: strings.EqualFold
// unter Windows - das gleicht aber auch Unicode-Verwandte an: das lange s
// (U+017F) gilt als "s", das Kelvin-Zeichen (U+212A) als "k". Ein Angreifer
// könnte so einen anderen Ordner einschmuggeln. Auch bloße ASCII-Großschreibung
// wird abgelehnt: Der Pfad stammt aus unserem eigenen Aufruf und stimmt
// buchstabengleich. Die Zeichen entstehen per string(rune(...)), damit keine
// Escapes in Tool-Parametern zu echten Zeichen werden.
func TestValidateInstallHelperArgsDirExactMatch(t *testing.T) {
	root := t.TempDir()
	sha := strings.Repeat("ab", 32)
	asset := "ssh-easy-setup-amd64.exe"
	longS := string(rune(0x017F))  // sieht aus wie s, EqualFold("s", ...) ist wahr
	kelvin := string(rune(0x212A)) // sieht aus wie K, EqualFold("k", ...) ist wahr
	build := func(users, kai, update string) string {
		return filepath.Join(root, users, kai, update)
	}
	pathIn := func(dir string) string { return filepath.Join(dir, "setup-1", asset) }
	downloadDir := build("Users", "Kai", "update")

	// Gegenprobe: der exakte Pfad wird angenommen
	if _, _, _, err := validateInstallHelperArgs([]string{pathIn(downloadDir), sha, "5"}, downloadDir, "windows", "amd64"); err != nil {
		t.Fatalf("exakter Pfad abgelehnt: %v", err)
	}

	cases := map[string]string{
		"langes s statt s":                  build("U"+longS+"ers", "Kai", "update"),
		"Kelvin-Zeichen statt K":            build("Users", kelvin+"ai", "update"),
		"ASCII-Großschreibung":              build("Users", "Kai", "UPDATE"),
		"ASCII-Großschreibung Elternordner": build("USERS", "Kai", "update"),
		"ASCII-Kleinschreibung":             build("Users", "kai", "update"),
	}
	for name, variant := range cases {
		if variant == downloadDir {
			t.Fatalf("%s: Testfall ist identisch mit dem Original", name)
		}
		if _, _, _, err := validateInstallHelperArgs([]string{pathIn(variant), sha, "5"}, downloadDir, "windows", "amd64"); err == nil {
			t.Errorf("%s: %q wurde trotz anderer Schreibweise angenommen (downloadDir %q)", name, pathIn(variant), downloadDir)
		}
	}

	// Dasselbe, wenn das lange s im LETZTEN Ordnernamen steckt (Update-Ordner)
	staging := filepath.Join(root, "Users", "Kai", "staging")
	fake := filepath.Join(root, "Users", "Kai", longS+"taging")
	if _, _, _, err := validateInstallHelperArgs([]string{pathIn(fake), sha, "5"}, staging, "windows", "amd64"); err == nil {
		t.Errorf("langes s im letzten Ordner: %q angenommen (downloadDir %q)", pathIn(fake), staging)
	}
}

// TestInstallHelperArgs prüft die Argumentliste für den Prozessstart: genau
// vier Elemente, der Pfad bleibt EIN Argument (keine Shell, kein Aufspalten
// an Leerzeichen), und validateInstallHelperArgs nimmt sie wieder an.
func TestInstallHelperArgs(t *testing.T) {
	downloadDir, path := helperFixture(t)
	sha := strings.Repeat("c3", 32)
	args := installHelperArgs(path, sha, 31337)
	if len(args) != 4 {
		t.Fatalf("%d Elemente, erwartet 4: %q", len(args), args)
	}
	if args[0] != installHelperFlag || args[1] != path || args[2] != sha || args[3] != strconv.Itoa(31337) {
		t.Errorf("Argumente = %q", args)
	}
	gotPath, gotSHA, gotPID, err := validateInstallHelperArgs(args[1:], downloadDir, "windows", "amd64")
	if err != nil || gotPath != path || gotSHA != sha || gotPID != 31337 {
		t.Errorf("Rundlauf: (%q, %q, %d, %v)", gotPath, gotSHA, gotPID, err)
	}
	// Anführungszeichen und Metazeichen im Pfad bleiben im selben Argument
	weird := filepath.Join(downloadDir, "setup-1", "a \"b\" & c.exe")
	if got := installHelperArgs(weird, sha, 1); len(got) != 4 || got[1] != weird {
		t.Errorf("Sonderzeichen-Pfad zerlegt: %q", got)
	}
}

// TestIsInstallHelperInvocation prüft die Erkennung: nur wenn GENAU das zweite
// Element (os.Args[1]) der Schalter ist. Ein Schalter an anderer Stelle oder in
// anderer Schreibweise darf den normalen Start nicht in den Helfer-Modus kippen.
func TestIsInstallHelperInvocation(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{"ssh-easy"}, false},
		{[]string{"ssh-easy", "--install-update"}, true},
		{[]string{"ssh-easy", "--install-update", "p", "sha", "1"}, true},
		{[]string{"--install-update"}, false},
		{[]string{"ssh-easy", "x", "--install-update"}, false},
		{[]string{"ssh-easy", "--INSTALL-UPDATE"}, false},
		{[]string{"ssh-easy", "-install-update"}, false},
		{[]string{"ssh-easy", "--install-update=1"}, false},
		{[]string{"ssh-easy", " --install-update"}, false},
		{[]string{"ssh-easy", "--install-updates"}, false},
		{[]string{"ssh-easy", ""}, false},
	}
	for _, c := range cases {
		if got := isInstallHelperInvocation(c.args); got != c.want {
			t.Errorf("isInstallHelperInvocation(%q) = %v, erwartet %v", c.args, got, c.want)
		}
	}
}

// --- Fall 11: cleanupUpdateLeftovers mit Altersgrenze ---

// setAge setzt die Änderungszeit eines Eintrags relativ zu jetzt.
func setAge(t *testing.T, path string, age time.Duration) {
	t.Helper()
	ts := time.Now().Add(-age)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

// makeSetupDir legt update/<name>/ssh-easy-setup-amd64.exe an und setzt das
// Alter des Ordners (nach dem Anlegen der Datei, die den Ordner verändert).
func makeSetupDir(t *testing.T, downloadDir, name string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(downloadDir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ssh-easy-setup-amd64.exe"), []byte("MZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	setAge(t, dir, age)
	return dir
}

// TestSetupLeftoverMinAgeRange hält die Altersgrenze in einem sinnvollen
// Rahmen: lang genug für einen wartenden Helfer, kurz genug zum Aufräumen.
func TestSetupLeftoverMinAgeRange(t *testing.T) {
	if setupLeftoverMinAge < time.Minute || setupLeftoverMinAge > time.Hour {
		t.Errorf("setupLeftoverMinAge = %v, erwartet zwischen 1 Minute und 1 Stunde", setupLeftoverMinAge)
	}
}

// TestCleanupKeepsYoungSetupDirs prüft den Randfall "Neustart, während der
// Helfer noch auf die UAC-Bestätigung wartet": junge setup-*-Ordner bleiben,
// alte werden gelöscht. Auch knapp unter bzw. über der Grenze und eine
// Änderungszeit in der Zukunft (Uhrensprung) - die bleibt stehen.
func TestCleanupKeepsYoungSetupDirs(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "ssh-easy.exe")
	_ = os.WriteFile(exe, []byte("aktuell"), 0o755)
	downloadDir := filepath.Join(root, "update")
	const minAge = 10 * time.Minute

	young := makeSetupDir(t, downloadDir, "setup-young", 0)
	underLimit := makeSetupDir(t, downloadDir, "setup-under", minAge-3*time.Minute)
	future := makeSetupDir(t, downloadDir, "setup-future", -time.Hour)
	overLimit := makeSetupDir(t, downloadDir, "setup-over", minAge+3*time.Minute)
	old := makeSetupDir(t, downloadDir, "setup-old", 3*time.Hour)

	cleanupUpdateLeftovers(exe, downloadDir, minAge)

	for name, dir := range map[string]string{"jung": young, "knapp darunter": underLimit, "Zukunft": future} {
		if _, err := os.Stat(filepath.Join(dir, "ssh-easy-setup-amd64.exe")); err != nil {
			t.Errorf("Ordner (%s) wurde gelöscht: %v", name, err)
		}
	}
	for name, dir := range map[string]string{"knapp darüber": overLimit, "alt": old} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("Ordner (%s) wurde nicht gelöscht", name)
		}
	}
	if _, err := os.Stat(exe); err != nil {
		t.Error("aktuelle Binary wurde gelöscht!")
	}
}

// TestCleanupAlwaysRemovesOldBinariesAndLegacySetups prüft, dass die
// Altersgrenze nur für setup-*-Ordner gilt: exe.old, exe.new und Setups im
// alten Ablageformat fallen auch ganz frisch.
func TestCleanupAlwaysRemovesOldBinariesAndLegacySetups(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "ssh-easy.exe")
	_ = os.WriteFile(exe, []byte("aktuell"), 0o755)
	_ = os.WriteFile(exe+".old", []byte("alt"), 0o755)
	_ = os.WriteFile(exe+".new", []byte("halb"), 0o755)
	downloadDir := filepath.Join(root, "update")
	_ = os.MkdirAll(downloadDir, 0o700)
	legacy := filepath.Join(downloadDir, "ssh-easy-setup-amd64.exe")
	_ = os.WriteFile(legacy, []byte("MZ"), 0o600)
	unrelated := filepath.Join(downloadDir, "notizen.txt")
	_ = os.WriteFile(unrelated, []byte("meins"), 0o600)

	cleanupUpdateLeftovers(exe, downloadDir, 10*time.Minute)

	for _, p := range []string{exe + ".old", exe + ".new", legacy} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s wurde nicht aufgeräumt", p)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Error("fremde Datei im Update-Ordner wurde gelöscht")
	}
	if _, err := os.Stat(exe); err != nil {
		t.Error("aktuelle Binary wurde gelöscht!")
	}
}

// TestCleanupIgnoresSetupSymlink prüft den Angriff "Symlink namens setup-evil
// zeigt auf einen fremden Ordner": Weder wird er verfolgt noch das Ziel
// gelöscht, egal wie alt der Link ist.
func TestCleanupIgnoresSetupSymlink(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "ssh-easy.exe")
	_ = os.WriteFile(exe, []byte("x"), 0o755)
	downloadDir := filepath.Join(root, "update")
	_ = os.MkdirAll(downloadDir, 0o700)
	victim := filepath.Join(root, "wichtig")
	_ = os.MkdirAll(victim, 0o700)
	keep := filepath.Join(victim, "daten.txt")
	_ = os.WriteFile(keep, []byte("unverändert"), 0o600)
	if err := os.Symlink(victim, filepath.Join(downloadDir, "setup-evil")); err != nil {
		t.Skipf("Symlink nicht möglich: %v", err)
	}
	setAge(t, victim, 5*time.Hour)

	cleanupUpdateLeftovers(exe, downloadDir, time.Minute)

	if data, err := os.ReadFile(keep); err != nil || string(data) != "unverändert" {
		t.Errorf("Ziel des Symlinks wurde angetastet: %q, %v", data, err)
	}
}

// TestCleanupMissingDirs prüft Randfälle: leere Pfade und fehlender
// Update-Ordner dürfen nicht paniken.
func TestCleanupMissingDirs(t *testing.T) {
	cleanupUpdateLeftovers("", "", 10*time.Minute)
	cleanupUpdateLeftovers(filepath.Join(t.TempDir(), "gibt-es-nicht.exe"), filepath.Join(t.TempDir(), "fehlt"), 10*time.Minute)
}

// --- N2: Setup-Ordner beim Start des Helfers "anfassen" ---

// TestTouchSetupDirSetsModTime (N2): touchSetupDir setzt die Änderungszeit des
// Ordners, der das Setup enthält, auf den übergebenen Zeitpunkt. Damit misst
// setupLeftoverMinAge "seit Start des Helfers" statt "seit dem Download".
func TestTouchSetupDirSetsModTime(t *testing.T) {
	downloadDir := filepath.Join(t.TempDir(), "update")
	dir := makeSetupDir(t, downloadDir, "setup-1", 5*time.Hour)
	setup := filepath.Join(dir, "ssh-easy-setup-amd64.exe")

	now := time.Now().Add(-90 * time.Second).Truncate(time.Second)
	if err := touchSetupDir(setup, now); err != nil {
		t.Fatalf("touchSetupDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if diff := info.ModTime().Sub(now); diff < -time.Second || diff > time.Second {
		t.Errorf("ModTime = %v, erwartet %v", info.ModTime(), now)
	}
	// Die Datei selbst und der Update-Ordner bleiben unberührt
	if fi, err := os.Stat(setup); err != nil || time.Since(fi.ModTime()) > time.Minute {
		t.Errorf("Setup-Datei verändert oder fehlt: %v, %v", fi, err)
	}
}

// TestTouchSetupDirKeepsFromCleanup (N2): Ein alter setup-*-Ordner (Download vor
// Stunden, Helfer wartet erst jetzt auf UAC) wird nach dem "Anfassen" nicht
// aufgeräumt - ein Neustart des Programms würde sonst das Setup unter dem
// wartenden Helfer löschen. Der unberührte Nachbar (Kontrolle) fällt weg.
func TestTouchSetupDirKeepsFromCleanup(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "ssh-easy.exe")
	_ = os.WriteFile(exe, []byte("aktuell"), 0o755)
	downloadDir := filepath.Join(root, "update")
	touched := makeSetupDir(t, downloadDir, "setup-touched", 5*time.Hour)
	untouched := makeSetupDir(t, downloadDir, "setup-untouched", 5*time.Hour)

	if err := touchSetupDir(filepath.Join(touched, "ssh-easy-setup-amd64.exe"), time.Now()); err != nil {
		t.Fatalf("touchSetupDir: %v", err)
	}
	cleanupUpdateLeftovers(exe, downloadDir, setupLeftoverMinAge)

	if _, err := os.Stat(filepath.Join(touched, "ssh-easy-setup-amd64.exe")); err != nil {
		t.Errorf("angefasster Ordner wurde aufgeräumt: %v", err)
	}
	if _, err := os.Stat(untouched); !os.IsNotExist(err) {
		t.Error("Kontrolle: unberührter alter Ordner wurde nicht aufgeräumt")
	}
}

// TestTouchSetupDirErrors (N2): Fehlt der Ordner, kommt ein Fehler zurück
// (der Aufrufer protokolliert ihn nur) - kein Absturz, und es wird nichts
// angelegt. Auch ein leerer Pfad darf nicht paniken.
func TestTouchSetupDirErrors(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "update", "setup-gibt-es-nicht", "ssh-easy-setup-amd64.exe")
	if err := touchSetupDir(missing, time.Now()); err == nil {
		t.Error("fehlender Ordner: kein Fehler")
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Error("touchSetupDir hat den fehlenden Ordner angelegt")
	}
	_ = touchSetupDir("", time.Now())
}

// TestWindowsSetupStartTouchesSetupDir (N2): Quelltext-Prüfung, weil
// startDetachedSetupInstaller nur unter Windows kompiliert: Innerhalb der
// Funktion wird touchSetupDir VOR dem Start des Helfers (cmd.Start()) gerufen.
func TestWindowsSetupStartTouchesSetupDir(t *testing.T) {
	data, err := os.ReadFile("updater_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func startDetachedSetupInstaller(")
	if start < 0 {
		t.Fatal("startDetachedSetupInstaller fehlt in updater_windows.go")
	}
	body := text[start:]
	touch := strings.Index(body, "touchSetupDir(")
	launch := strings.Index(body, "cmd.Start()")
	if touch < 0 {
		t.Fatal("startDetachedSetupInstaller ruft touchSetupDir nicht auf")
	}
	if launch < 0 || touch > launch {
		t.Error("touchSetupDir muss VOR cmd.Start() (Start des Helfers) aufgerufen werden")
	}
}
