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
	"crypto/ed25519"
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
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"ssh-easy/releasesig"
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

	// Signierte Prüfsummenliste (seit Build 51)
	unsigned     bool               // true = Release ohne Liste/Signatur
	noSignature  bool               // true = Liste ohne Signaturdatei
	manifestTag  string             // leer = rel.tag (sonst: Liste eines anderen Releases)
	manifestSum  string             // leer = korrekte SHA-256 des Assets
	omitInList   bool               // true = Asset fehlt in der Liste
	signWith     ed25519.PrivateKey // nil = testSigningKey
	manifestPath string             // Pfad der Liste auf dem Server (leer = /download/...)
}

// startReleaseServer startet einen Server mit /latest (API) und dem Asset.
func startReleaseServer(t *testing.T, rel fakeRelease) *httptest.Server {
	t.Helper()
	if rel.assetPath == "" {
		rel.assetPath = "/download/" + rel.assetName
	}
	manifest, signature := signedManifestFor(rel)
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
		if !rel.unsigned {
			manifestURL := srv.URL + "/download/" + releasesig.ManifestName
			if rel.manifestPath != "" {
				manifestURL = srv.URL + rel.manifestPath
			}
			assets = append(assets, map[string]any{
				"name": releasesig.ManifestName, "size": len(manifest), "browser_download_url": manifestURL,
			})
			if !rel.noSignature {
				assets = append(assets, map[string]any{
					"name": releasesig.SignatureName, "size": len(signature),
					"browser_download_url": srv.URL + "/download/" + releasesig.SignatureName,
				})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": rel.tag, "assets": assets})
	})
	serveFile := func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case releasesig.ManifestName:
			_, _ = w.Write(manifest)
		case releasesig.SignatureName:
			_, _ = w.Write(signature)
		default:
			_, _ = w.Write(rel.content)
		}
	}
	mux.HandleFunc("/download/", serveFile)
	mux.HandleFunc("/fremd/", serveFile)
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// signedManifestFor erzeugt Prüfsummenliste und Signatur wie build.sh.
func signedManifestFor(rel fakeRelease) (manifest, signature []byte) {
	sum := hexSum(rel.content)
	if rel.manifestSum != "" {
		sum = rel.manifestSum
	}
	sums := map[string]string{"ssh-easy-unbeteiligt": hexSum([]byte("x"))}
	if !rel.omitInList {
		sums[rel.assetName] = sum
	}
	if rel.extraAsset != "" {
		sums[rel.extraAsset] = hexSum(rel.content)
	}
	tag := rel.tag
	if rel.manifestTag != "" {
		tag = rel.manifestTag
	}
	key := rel.signWith
	if key == nil {
		key = testSigningKey
	}
	manifest = releasesig.BuildManifest(tag, sums)
	return manifest, releasesig.Sign(key, manifest)
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
	u.signingKey = testSigningPub
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

// TestCheckAndDownloadWithoutAPIDigest prüft dass die Prüfsumme der API
// nur noch zum Gegenprüfen dient: Maßgeblich ist die signierte Liste, ein
// fehlendes API-Feld verhindert das Update deshalb nicht.
func TestCheckAndDownloadWithoutAPIDigest(t *testing.T) {
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, digest: "-"})
	u, _ := newTestUpdater(t, srv, "47", "linux")
	pending, err := u.CheckAndDownload(context.Background())
	if err != nil || pending == nil {
		t.Fatalf("ohne API-Prüfsumme: pending=%+v err=%v", pending, err)
	}
	if pending.sha256 != hexSum(fakeELF) {
		t.Errorf("pending.sha256 = %q, erwartet die signierte Prüfsumme", pending.sha256)
	}
}

// --- Signierte Prüfsummenliste (Build 51) ---

// TestCheckAndDownloadUnsignedRelease prüft dass ein Release ohne
// signierte Liste nicht installiert wird - auch wenn die API-Prüfsumme
// stimmt (die könnte ein Angreifer mit Zugriff auf das Konto mitliefern).
func TestCheckAndDownloadUnsignedRelease(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, unsigned: true}, "signiert")
}

// TestCheckAndDownloadMissingSignature prüft Liste ohne Signaturdatei.
func TestCheckAndDownloadMissingSignature(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, noSignature: true}, "signiert")
}

// TestCheckAndDownloadForeignSignature prüft eine mit fremdem Schlüssel
// signierte Liste (Angreifer kennt den privaten Schlüssel nicht).
func TestCheckAndDownloadForeignSignature(t *testing.T) {
	evil := ed25519.NewKeyFromSeed([]byte("angreifer-schluessel-0123456789!"))
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, signWith: evil}, "Signatur")
}

// TestCheckAndDownloadReplayedManifest prüft dass eine echte, aber für ein
// anderes Release signierte Liste nicht gilt (z.B. alte Version mit Lücke
// als "neue" ausgeben).
func TestCheckAndDownloadReplayedManifest(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, manifestTag: "v0.40"}, "v0.48")
}

// TestCheckAndDownloadAssetNotInManifest prüft eine Liste ohne die Datei.
func TestCheckAndDownloadAssetNotInManifest(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF, omitInList: true}, "ssh-easy")
}

// TestCheckAndDownloadContentNotMatchingSignedSum prüft den eigentlichen
// Angriff: Binary und API-Prüfsumme ausgetauscht, signierte Liste echt.
func TestCheckAndDownloadContentNotMatchingSignedSum(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF,
		manifestSum: hexSum([]byte("die echte, signierte Version"))}, "Prüfsumme")
}

// TestCheckAndDownloadManifestForeignURL prüft dass auch die Liste nur von
// der erlaubten Adresse geladen wird.
func TestCheckAndDownloadManifestForeignURL(t *testing.T) {
	expectDownloadError(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy", content: fakeELF,
		manifestPath: "/fremd/" + releasesig.ManifestName}, "Adresse")
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
	if filepath.Dir(filepath.Dir(pending.path)) != u.downloadDir || !strings.HasSuffix(pending.path, "ssh-easy-setup-amd64.exe") {
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

	cleanupUpdateLeftovers(exe, downloadDir, setupLeftoverMinAge)
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
	// Tastenhilfe steht im Hauptmenü, das erst m einblendet (Build 84)
	m = press(t, m, "m")
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

// TestSetupDownloadGetsFreshDirectory prüft dass jedes Setup in einem frisch
// angelegten, leeren Unterordner landet (Schutz vor DLL-Hijacking: das Setup
// läuft mit Adminrechten und dürfte keine untergeschobenen DLLs finden).
func TestSetupDownloadGetsFreshDirectory(t *testing.T) {
	pe := append([]byte("MZ"), []byte("setup-49")...)
	srv := startReleaseServer(t, fakeRelease{tag: "v0.48", assetName: "ssh-easy-setup-amd64.exe", content: pe})
	u, _ := newTestUpdater(t, srv, "47", "windows")
	u.canWriteDir = func(string) bool { return false }
	_ = os.MkdirAll(u.downloadDir, 0o700)
	_ = os.WriteFile(filepath.Join(u.downloadDir, "version.dll"), []byte("boese"), 0o600)

	pending, err := u.CheckAndDownload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(pending.path)
	if dir == u.downloadDir || filepath.Dir(dir) != u.downloadDir {
		t.Fatalf("Setup liegt nicht in eigenem Unterordner: %s", pending.path)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("Setup-Ordner enthält weitere Dateien: %v", entries)
	}
}

// TestCheckSetupDirectoryClean prüft die Kontrolle vor dem Start.
func TestCheckSetupDirectoryClean(t *testing.T) {
	dir := t.TempDir()
	setup := filepath.Join(dir, "ssh-easy-setup-amd64.exe")
	_ = os.WriteFile(setup, []byte("MZ"), 0o600)
	if err := checkSetupDirectoryClean(setup); err != nil {
		t.Errorf("sauberer Ordner abgelehnt: %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, "version.dll"), []byte("boese"), 0o600)
	if err := checkSetupDirectoryClean(setup); err == nil {
		t.Error("untergeschobene DLL im Setup-Ordner nicht erkannt")
	}
}

// TestCleanupRemovesSetupDirectories prüft das Aufräumen der Unterordner.
func TestCleanupRemovesSetupDirectories(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ssh-easy.exe")
	_ = os.WriteFile(exe, []byte("x"), 0o755)
	downloadDir := filepath.Join(dir, "update")
	sub := filepath.Join(downloadDir, "setup-123")
	_ = os.MkdirAll(sub, 0o700)
	_ = os.WriteFile(filepath.Join(sub, "ssh-easy-setup-amd64.exe"), []byte("MZ"), 0o600)
	// Seit Build 87 räumt der Start nur noch ältere Ordner ab (ein Helfer
	// könnte noch auf die UAC-Bestätigung warten) - der Ordner ist 2 h alt
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(sub, old, old)
	cleanupUpdateLeftovers(exe, downloadDir, setupLeftoverMinAge)
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Error("Setup-Unterordner wurde nicht aufgeräumt")
	}
}
