// Paket main - Tests für das Auto-Update (Build 48)
//
// Ablauf: Beim Start (falls im Menü eingeschaltet) wird das neueste GitHub-
// Release abgefragt und die passende Binary heruntergeladen. Installiert
// wird erst beim Beenden. Getestet wird gegen einen lokalen httptest-Server.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeELF ist ein minimaler Inhalt, der wie eine Linux-Binary aussieht.
var fakeELF = append([]byte{0x7f, 'E', 'L', 'F'}, []byte("neue-version-48")...)

// fakeRelease beschreibt, was der Testserver ausliefert.
type fakeRelease struct {
	tag        string
	assetName  string
	content    []byte
	digest     string // leer = korrekte SHA-256 berechnen; "-" = Feld weglassen
	size       int64  // 0 = korrekte Größe
	apiStatus  int    // 0 = 200
	assetPath  string // Pfad des Assets auf dem Server
	extraAsset string // zusätzliches Asset (z.B. Setup)
}

// startReleaseServer startet einen Server mit /latest (API) und dem Asset.
func startReleaseServer(t *testing.T, rel fakeRelease) *httptest.Server {
	t.Helper()
	if rel.assetPath == "" {
		rel.assetPath = "/download/" + rel.assetName
	}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		if rel.apiStatus != 0 {
			w.WriteHeader(rel.apiStatus)
			return
		}
		sum := sha256.Sum256(rel.content)
		digest := "sha256:" + hex.EncodeToString(sum[:])
		if rel.digest == "-" {
			digest = ""
		} else if rel.digest != "" {
			digest = rel.digest
		}
		size := int64(len(rel.content))
		if rel.size != 0 {
			size = rel.size
		}
		assets := []map[string]any{{
			"name":                 rel.assetName,
			"size":                 size,
			"digest":               digest,
			"browser_download_url": srv.URL + rel.assetPath,
		}}
		if rel.extraAsset != "" {
			assets = append(assets, map[string]any{
				"name":                 rel.extraAsset,
				"size":                 len(rel.content),
				"digest":               "sha256:" + hex.EncodeToString(sum[:]),
				"browser_download_url": srv.URL + "/download/" + rel.extraAsset,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": rel.tag, "assets": assets})
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(rel.content)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newTestUpdater erstellt einen Updater mit temporärer "installierter" Binary.
func newTestUpdater(t *testing.T, srv *httptest.Server, build, goos string) (*Updater, string) {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "ssh-easy")
	if goos == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte("\x7fELFalte-version"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := NewUpdater(build)
	u.client = srv.Client()
	u.apiURL = srv.URL + "/latest"
	u.allowedURLPrefix = srv.URL + "/download/"
	u.exePath = exe
	u.goos = goos
	u.goarch = "amd64"
	u.downloadDir = filepath.Join(dir, "update")
	return u, exe
}

// --- Hilfsfunktionen ---

// TestParseReleaseBuild prüft das Versionsschema v0.<Build>.
func TestParseReleaseBuild(t *testing.T) {
	cases := map[string]int{"v0.48": 48, "v0.1": 1, "v0.100": 100}
	for tag, want := range cases {
		if got, ok := parseReleaseBuild(tag); !ok || got != want {
			t.Errorf("parseReleaseBuild(%q) = %d/%v, erwartet %d", tag, got, ok, want)
		}
	}
	for _, tag := range []string{"", "v0.", "v1.48", "v0.48.1", "0.48", "v0.x", "v0.-3"} {
		if _, ok := parseReleaseBuild(tag); ok {
			t.Errorf("parseReleaseBuild(%q) sollte ungültig sein", tag)
		}
	}
}

// TestIsNewerRelease prüft den Versionsvergleich.
func TestIsNewerRelease(t *testing.T) {
	cases := []struct {
		current, tag string
		want         bool
	}{
		{"47", "v0.48", true},
		{"48", "v0.48", false},
		{"49", "v0.48", false},
		{"dev", "v0.48", false},
		{"", "v0.48", false},
		{"47", "kaputt", false},
		{" 47\n", "v0.48", true},
	}
	for _, c := range cases {
		if got := isNewerRelease(c.current, c.tag); got != c.want {
			t.Errorf("isNewerRelease(%q, %q) = %v, erwartet %v", c.current, c.tag, got, c.want)
		}
	}
}

// TestUpdateAssetName prüft die Zuordnung Plattform -> Release-Asset.
func TestUpdateAssetName(t *testing.T) {
	cases := []struct {
		goos, goarch string
		setup        bool
		want         string
		ok           bool
	}{
		{"linux", "amd64", false, "ssh-easy", true},
		{"linux", "arm64", false, "ssh-easy-linux-arm64", true},
		{"windows", "amd64", false, "ssh-easy-windows-amd64.exe", true},
		{"windows", "arm64", false, "ssh-easy-windows-arm64.exe", true},
		{"windows", "amd64", true, "ssh-easy-setup-amd64.exe", true},
		{"windows", "arm64", true, "ssh-easy-setup-arm64.exe", true},
		{"linux", "amd64", true, "", false},
		{"darwin", "arm64", false, "", false},
		{"linux", "386", false, "", false},
	}
	for _, c := range cases {
		got, ok := updateAssetName(c.goos, c.goarch, c.setup)
		if got != c.want || ok != c.ok {
			t.Errorf("updateAssetName(%s,%s,%v) = %q/%v, erwartet %q/%v", c.goos, c.goarch, c.setup, got, ok, c.want, c.ok)
		}
	}
}

// TestLooksLikeExecutable prüft die Formatprüfung des Downloads.
func TestLooksLikeExecutable(t *testing.T) {
	if !looksLikeExecutable([]byte("\x7fELF...."), "linux") {
		t.Error("ELF nicht erkannt")
	}
	if !looksLikeExecutable([]byte("MZ\x90\x00"), "windows") {
		t.Error("PE nicht erkannt")
	}
	if looksLikeExecutable([]byte("MZ\x90\x00"), "linux") || looksLikeExecutable([]byte("<html>"), "windows") || looksLikeExecutable(nil, "linux") {
		t.Error("falsches Format akzeptiert")
	}
}

// --- CheckAndDownload ---

// TestCheckAndDownloadNewerRelease prüft den Normalfall.
func TestCheckAndDownloadNewerRelease(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, exe := newTestUpdater(t, srv, "47", "linux")

	pending, err := u.CheckAndDownload(context.Background())
	if err != nil {
		t.Fatalf("CheckAndDownload: %v", err)
	}
	if pending == nil || pending.build != 48 || pending.kind != updateReplaceBinary {
		t.Fatalf("pending = %+v", pending)
	}
	data, err := os.ReadFile(pending.path)
	if err != nil || string(data) != string(fakeELF) {
		t.Errorf("heruntergeladene Datei = %q, %v", data, err)
	}
	if filepath.Dir(pending.path) != filepath.Dir(exe) {
		t.Errorf("Download muss neben der Binary liegen (atomarer Rename): %s", pending.path)
	}
	if u.Pending() == nil {
		t.Error("Pending() liefert nach erfolgreichem Download nil")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(pending.path); info.Mode().Perm()&0o100 == 0 {
			t.Errorf("Download ist nicht ausführbar: %v", info.Mode())
		}
	}
}

// TestCheckAndDownloadUpToDate prüft dass ohne neuere Version nichts passiert.
func TestCheckAndDownloadUpToDate(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, exe := newTestUpdater(t, srv, "48", "linux")
	pending, err := u.CheckAndDownload(context.Background())
	if err != nil || pending != nil {
		t.Errorf("aktuelle Version: pending=%+v err=%v", pending, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
		t.Errorf("es wurden Dateien angelegt: %v", entries)
	}
}

// expectDownloadError prüft einen Fehlerfall und dass keine Reste bleiben.
func expectDownloadError(t *testing.T, rel fakeRelease, wantInErr string) {
	t.Helper()
	srv := startReleaseServer(t, rel)
	u, exe := newTestUpdater(t, srv, "47", "linux")
	pending, err := u.CheckAndDownload(context.Background())
	if err == nil || pending != nil {
		t.Fatalf("Fehler erwartet, bekommen pending=%+v err=%v", pending, err)
	}
	if wantInErr != "" && !strings.Contains(err.Error(), wantInErr) {
		t.Errorf("Fehlertext %q enthält nicht %q", err.Error(), wantInErr)
	}
	if u.Pending() != nil {
		t.Error("nach Fehler darf kein Update anstehen")
	}
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 1 {
		t.Errorf("Download-Reste nicht aufgeräumt: %v", entries)
	}
}

// TestCheckAndDownloadDigestMismatch prüft die SHA-256-Prüfung.
func TestCheckAndDownloadDigestMismatch(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF,
		digest: "sha256:" + strings.Repeat("0", 64)}, "Prüfsumme")
}

// TestCheckAndDownloadMissingDigest prüft dass ohne Prüfsumme nicht
// installiert wird.
func TestCheckAndDownloadMissingDigest(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, digest: "-"}, "Prüfsumme")
}

// TestCheckAndDownloadSizeMismatch prüft die Größenprüfung.
func TestCheckAndDownloadSizeMismatch(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, size: 9999}, "Größe")
}

// TestCheckAndDownloadNotExecutable prüft die Formatprüfung.
func TestCheckAndDownloadNotExecutable(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: []byte("<html>Fehler</html>")}, "ausführbar")
}

// TestCheckAndDownloadMissingAsset prüft ein Release ohne passende Datei.
func TestCheckAndDownloadMissingAsset(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy-linux-arm64", content: fakeELF}, "ssh-easy")
}

// TestCheckAndDownloadAPIError prüft HTTP-Fehler der API.
func TestCheckAndDownloadAPIError(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, apiStatus: 500}, "500")
}

// TestCheckAndDownloadForeignURL prüft dass nur von der erlaubten Adresse
// geladen wird (Schutz vor manipulierten API-Antworten).
func TestCheckAndDownloadForeignURL(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, assetPath: "/fremd/ssh-easy"}, "Adresse")
}

// TestCheckAndDownloadDevBuild prüft dass Entwicklerversionen nicht updaten.
func TestCheckAndDownloadDevBuild(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, _ := newTestUpdater(t, srv, "dev", "linux")
	if pending, err := u.CheckAndDownload(context.Background()); pending != nil || err != nil {
		t.Errorf("dev-Build: pending=%+v err=%v", pending, err)
	}
}

// TestCheckAndDownloadLinuxNoWritePermission prüft die Meldung, wenn das
// Programmverzeichnis nicht beschreibbar ist (z.B. /usr/local/bin).
func TestCheckAndDownloadLinuxNoWritePermission(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, _ := newTestUpdater(t, srv, "47", "linux")
	u.canWriteDir = func(string) bool { return false }
	_, err := u.CheckAndDownload(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Schreibrechte") {
		t.Errorf("erwartet Schreibrechte-Fehler, bekommen %v", err)
	}
}

// TestCheckAndDownloadWindowsSetupFallback prüft dass unter Windows ohne
// Schreibrechte (Programme-Ordner) das Setup geladen wird.
func TestCheckAndDownloadWindowsSetupFallback(t *testing.T) {
	pe := append([]byte("MZ"), []byte("setup-48")...)
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy-setup-amd64.exe", content: pe,
		extraAsset: "ssh-easy-windows-amd64.exe"})
	u, _ := newTestUpdater(t, srv, "47", "windows")
	u.canWriteDir = func(string) bool { return false }

	pending, err := u.CheckAndDownload(context.Background())
	if err != nil {
		t.Fatalf("CheckAndDownload: %v", err)
	}
	if pending.kind != updateRunSetup {
		t.Errorf("kind = %v, erwartet updateRunSetup", pending.kind)
	}
	if filepath.Dir(pending.path) != u.downloadDir || !strings.HasSuffix(pending.path, "ssh-easy-setup-amd64.exe") {
		t.Errorf("Setup-Pfad = %s", pending.path)
	}
}

// --- Installation beim Beenden ---

// TestReplaceExecutableDirect prüft den Unix-Weg (Rename über die Binary).
func TestReplaceExecutableDirect(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ssh-easy")
	_ = os.WriteFile(exe, []byte("alt"), 0o755)
	_ = os.WriteFile(exe+".new", []byte("neu"), 0o755)

	if err := replaceExecutable(exe, exe+".new", false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "neu" {
		t.Errorf("Binary = %q", data)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Error(".new existiert noch")
	}
}

// TestReplaceExecutableKeepOld prüft den Windows-Weg: laufende EXE wird zu
// .old umbenannt (überschreiben ist unter Windows nicht möglich).
func TestReplaceExecutableKeepOld(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ssh-easy.exe")
	_ = os.WriteFile(exe, []byte("alt"), 0o755)
	_ = os.WriteFile(exe+".old", []byte("uralt"), 0o755)
	_ = os.WriteFile(exe+".new", []byte("neu"), 0o755)

	if err := replaceExecutable(exe, exe+".new", true); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "neu" {
		t.Errorf("Binary = %q", data)
	}
	if data, _ := os.ReadFile(exe + ".old"); string(data) != "alt" {
		t.Errorf(".old = %q, erwartet die bisherige Version", data)
	}
}

// TestReplaceExecutableMissingNew prüft dass die alte Binary bei fehlendem
// Download unangetastet bleibt.
func TestReplaceExecutableMissingNew(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ssh-easy.exe")
	_ = os.WriteFile(exe, []byte("alt"), 0o755)
	if err := replaceExecutable(exe, exe+".new", true); err == nil {
		t.Error("Fehler erwartet")
	}
	if data, _ := os.ReadFile(exe); string(data) != "alt" {
		t.Errorf("alte Binary verändert: %q", data)
	}
}

// TestApplyInstallsPendingUpdate prüft Apply() nach erfolgreichem Download.
func TestApplyInstallsPendingUpdate(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, exe := newTestUpdater(t, srv, "47", "linux")
	if _, err := u.CheckAndDownload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if data, _ := os.ReadFile(exe); string(data) != string(fakeELF) {
		t.Errorf("Binary nach Apply = %q", data)
	}
	if u.Pending() != nil {
		t.Error("nach Apply darf kein Update mehr anstehen")
	}
}

// TestApplyWithoutPending prüft Apply() ohne Download.
func TestApplyWithoutPending(t *testing.T) {
	u := NewUpdater("47")
	if err := u.Apply(); err != nil {
		t.Errorf("Apply ohne Update: %v", err)
	}
	var nilUpdater *Updater
	if err := nilUpdater.Apply(); err != nil || nilUpdater.Pending() != nil {
		t.Error("nil-Updater muss sicher sein")
	}
}

// TestCleanupUpdateLeftovers prüft das Aufräumen alter Dateien beim Start.
func TestCleanupUpdateLeftovers(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ssh-easy.exe")
	_ = os.WriteFile(exe, []byte("aktuell"), 0o755)
	_ = os.WriteFile(exe+".old", []byte("alt"), 0o755)
	_ = os.WriteFile(exe+".new", []byte("halb"), 0o755)
	downloadDir := filepath.Join(dir, "update")
	_ = os.MkdirAll(downloadDir, 0o700)
	_ = os.WriteFile(filepath.Join(downloadDir, "ssh-easy-setup-amd64.exe"), []byte("x"), 0o600)

	cleanupUpdateLeftovers(exe, downloadDir)
	for _, p := range []string{exe + ".old", exe + ".new", filepath.Join(downloadDir, "ssh-easy-setup-amd64.exe")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s wurde nicht aufgeräumt", p)
		}
	}
	if _, err := os.Stat(exe); err != nil {
		t.Error("aktuelle Binary wurde gelöscht!")
	}
}

// --- Menü-Integration ---

// TestAutoUpdateDefaultOff prüft den Standard (aus) und die Anzeige ganz unten.
func TestAutoUpdateDefaultOff(t *testing.T) {
	m := newListTestModel(t, "A")
	if m.autoUpdate {
		t.Error("Auto-Update muss standardmäßig aus sein")
	}
	text := strings.TrimRight(stripANSI(renderListText(m)), "\n ")
	lines := strings.Split(text, "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "Auto-Update: aus") || !strings.Contains(last, "u") {
		t.Errorf("letzte Menüzeile = %q, erwartet Auto-Update-Schalter", last)
	}
}

// TestToggleAutoUpdatePersists prüft das Umschalten mit 'u'.
func TestToggleAutoUpdatePersists(t *testing.T) {
	m := newListTestModel(t, "A")
	m.updater = NewUpdater("48")

	updated, cmd := m.Update(keyMsgFor("u"))
	m = updated.(AppModel)
	if !m.autoUpdate {
		t.Fatal("u schaltet Auto-Update nicht ein")
	}
	if cmd == nil {
		t.Error("Einschalten soll sofort nach Updates suchen")
	}
	if cfg, _ := LoadConfig(m.configPath); !cfg.AutoUpdate || len(cfg.Connections) != 1 {
		t.Errorf("Config: auto_update=%v, Verbindungen=%d", cfg.AutoUpdate, len(cfg.Connections))
	}
	if !strings.Contains(stripANSI(renderListText(m)), "Auto-Update: an") {
		t.Error("Anzeige zeigt nicht 'an'")
	}

	m = press(t, m, "u")
	if m.autoUpdate {
		t.Error("zweites u schaltet nicht aus")
	}
	if cfg, _ := LoadConfig(m.configPath); cfg.AutoUpdate {
		t.Error("Config nicht zurückgesetzt")
	}
}

// TestInitChecksOnlyWhenEnabled prüft die Suche beim Start.
func TestInitChecksOnlyWhenEnabled(t *testing.T) {
	m := newListTestModel(t, "A")
	m.SetUpdater(NewUpdater("48"))
	if m.updateChecking {
		t.Error("ausgeschaltet darf beim Start nicht gesucht werden")
	}

	cfg, _ := LoadConfig(m.configPath)
	cfg.AutoUpdate = true
	_ = SaveConfig(m.configPath, cfg)
	m = NewAppModel(m.configPath, "48", NewSSHManager(NewLogger()))
	if !m.autoUpdate {
		t.Fatal("auto_update aus der Config nicht übernommen")
	}
	m.SetUpdater(NewUpdater("48"))
	if !m.updateChecking {
		t.Error("eingeschaltet muss beim Start gesucht werden")
	}
}

// TestUpdateResultMessages prüft die Statusanzeige nach der Suche.
func TestUpdateResultMessages(t *testing.T) {
	m := newListTestModel(t, "A")
	m.autoUpdate = true
	m.updateChecking = true

	updated, _ := m.Update(updateResultMsg{pending: &pendingUpdate{build: 49}})
	text := stripANSI(renderListText(updated.(AppModel)))
	if !strings.Contains(text, "v0.49") || !strings.Contains(text, "Beenden") {
		t.Errorf("Hinweis auf bereitstehendes Update fehlt:\n%s", text)
	}

	updated, _ = m.Update(updateResultMsg{err: fmt.Errorf("kein Netz")})
	text = stripANSI(renderListText(updated.(AppModel)))
	if !strings.Contains(text, "kein Netz") {
		t.Errorf("Fehlerhinweis fehlt:\n%s", text)
	}
	var _ tea.Msg = updateResultMsg{}
}

// TestApplyRejectsTamperedDownload prüft dass eine nach dem Download
// veränderte Datei nicht installiert wird (Schutz vor Austausch zwischen
// Download beim Start und Installation beim Beenden - beim Windows-Setup
// mit Adminrechten wäre das sonst eine Rechteausweitung).
func TestApplyRejectsTamperedDownload(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, exe := newTestUpdater(t, srv, "47", "linux")
	pending, err := u.CheckAndDownload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending.path, []byte("\x7fELFboese"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(); err == nil || !strings.Contains(err.Error(), "Prüfsumme") {
		t.Errorf("manipulierte Datei: Apply-Fehler = %v, erwartet Prüfsummen-Fehler", err)
	}
	if data, _ := os.ReadFile(exe); string(data) == "\x7fELFboese" {
		t.Error("manipulierte Datei wurde installiert")
	}
	if _, err := os.Stat(pending.path); !os.IsNotExist(err) {
		t.Error("manipulierte Datei wurde nicht entfernt")
	}
}

// TestCheckAndDownloadRejectsSymlinkedDownloadDir prüft dass der Ablage-
// Ordner für das (mit Adminrechten gestartete) Setup kein Symlink sein darf -
// sonst könnte Schadsoftware den Download an einen Ort ihrer Wahl umlenken.
func TestCheckAndDownloadRejectsSymlinkedDownloadDir(t *testing.T) {
	pe := append([]byte("MZ"), []byte("setup-48")...)
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy-setup-amd64.exe", content: pe})
	u, _ := newTestUpdater(t, srv, "47", "windows")
	u.canWriteDir = func(string) bool { return false }
	target := t.TempDir()
	if err := os.Symlink(target, u.downloadDir); err != nil {
		t.Skipf("Symlink nicht möglich: %v", err)
	}
	if _, err := u.CheckAndDownload(context.Background()); err == nil {
		t.Error("Symlink als Update-Ordner wurde akzeptiert")
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Errorf("Download landete im Symlink-Ziel: %v", entries)
	}
}

// TestDownloadReplacesPlantedSymlink prüft dass ein vorab angelegter
// Symlink am Zielpfad nicht verfolgt wird (Datei wird neu angelegt).
func TestDownloadReplacesPlantedSymlink(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF})
	u, exe := newTestUpdater(t, srv, "47", "linux")
	victim := filepath.Join(t.TempDir(), "opfer")
	_ = os.WriteFile(victim, []byte("unverändert"), 0o600)
	if err := os.Symlink(victim, exe+".new"); err != nil {
		t.Skipf("Symlink nicht möglich: %v", err)
	}
	if _, err := u.CheckAndDownload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "unverändert" {
		t.Errorf("Symlink-Ziel wurde überschrieben: %q", data)
	}
}
