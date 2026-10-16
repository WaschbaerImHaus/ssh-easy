// Paket main - Auto-Update über GitHub-Releases
//
// Ablauf (nur wenn im Menü eingeschaltet, Standard: aus):
//  1. Beim Start: neuestes Release über die GitHub-API abfragen
//     (https://api.github.com/repos/WaschbaerImHaus/ssh-easy/releases/latest)
//  2. Ist das Release neuer als der eigene Build (Schema v0.<Build>), wird
//     die passende Datei im Hintergrund heruntergeladen und geprüft:
//     SHA-256 (aus der API), Größe, Dateiformat (ELF bzw. PE)
//  3. Beim Beenden wird installiert:
//     - Programmverzeichnis beschreibbar: Binary ersetzen
//     (Linux: atomarer Rename; Windows: laufende EXE -> .old umbenennen)
//     - Windows ohne Schreibrecht (Programme-Ordner): Setup still (/S) mit
//     Administratorrechten starten
//
// Sicherheit: Es wird nur von der Release-Adresse dieses Repositorys geladen
// (allowedURLPrefix), die Prüfsumme ist Pflicht, Antworten sind größen-
// begrenzt.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// updateRepo - GitHub-Repository der Releases
	updateRepo = "WaschbaerImHaus/ssh-easy"
	// updateAPIURL - API-Adresse des neuesten Releases
	updateAPIURL = "https://api.github.com/repos/" + updateRepo + "/releases/latest"
	// updateDownloadPrefix - einzige erlaubte Download-Adresse
	updateDownloadPrefix = "https://github.com/" + updateRepo + "/releases/download/"
	// maxReleaseInfoBytes - Obergrenze für die API-Antwort
	maxReleaseInfoBytes = 1 << 20
	// maxUpdateBytes - Obergrenze für eine heruntergeladene Datei
	maxUpdateBytes = 200 << 20
	// updateCheckTimeout - Gesamtfrist für Suche und Download
	updateCheckTimeout = 5 * time.Minute
)

// updateKind beschreibt, wie ein Update beim Beenden installiert wird.
type updateKind int

const (
	// updateReplaceBinary - heruntergeladene Binary ersetzt die laufende
	updateReplaceBinary updateKind = iota
	// updateRunSetup - Windows-Setup wird still mit Adminrechten gestartet
	updateRunSetup
)

// pendingUpdate ist ein heruntergeladenes, geprüftes Update.
type pendingUpdate struct {
	// Build-Nummer des Updates
	build int
	// Pfad der heruntergeladenen Datei
	path string
	// Installationsart
	kind updateKind
	// Erwartete SHA-256 (hex) - wird vor der Installation erneut geprüft
	sha256 string
}

// releaseAsset ist eine Datei eines GitHub-Releases.
type releaseAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
}

// releaseInfo ist die (gekürzte) Antwort der Releases-API.
type releaseInfo struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

// updateResultMsg meldet der TUI das Ergebnis der Update-Suche.
type updateResultMsg struct {
	// Heruntergeladenes Update (nil = keines bzw. Fehler)
	pending *pendingUpdate
	// Fehler der Suche oder des Downloads
	err error
}

// Updater sucht, lädt und installiert Updates.
type Updater struct {
	// HTTP-Client (mit DNS-Fallback)
	client *http.Client
	// API-Adresse des neuesten Releases
	apiURL string
	// Erlaubter Präfix für Download-Adressen
	allowedURLPrefix string
	// Eigene Build-Nummer
	currentBuild string
	// Zielplattform
	goos, goarch string
	// Pfad der laufenden Binary
	exePath string
	// Ablage für das Windows-Setup (~/.ssh-easy/update)
	downloadDir string
	// Prüft ob ein Verzeichnis beschreibbar ist (austauschbar für Tests)
	canWriteDir func(dir string) bool
	// Mutex für pending
	mu sync.Mutex
	// Heruntergeladenes Update, das beim Beenden installiert wird
	pending *pendingUpdate
}

// NewUpdater erstellt einen Updater für die laufende Binary.
//
// @param currentBuild - Eigene Build-Nummer (aus build.txt)
// @return *Updater - Neuer Updater
// @date   2026-10-16 10:00
func NewUpdater(currentBuild string) *Updater {
	exe, err := os.Executable()
	if err == nil {
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			exe = resolved
		}
	}
	downloadDir := ""
	if dir, derr := GetConfigDir(); derr == nil {
		downloadDir = filepath.Join(dir, "update")
	}

	// HTTP mit derselben Namensauflösung (inkl. DNS-Fallback) wie SSH
	resolver := newHostResolver()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, err
		}
		// TLS prüft das Zertifikat - eine Fallback-Adresse ist hier unkritisch
		conn, _, err := dialResolved(ctx, resolver, defaultTCPDial, host, port, 15*time.Second)
		return conn, err
	}

	return &Updater{
		client:           &http.Client{Transport: transport, Timeout: updateCheckTimeout},
		apiURL:           updateAPIURL,
		allowedURLPrefix: updateDownloadPrefix,
		currentBuild:     currentBuild,
		goos:             runtime.GOOS,
		goarch:           runtime.GOARCH,
		exePath:          exe,
		downloadDir:      downloadDir,
		canWriteDir:      dirWritable,
	}
}

// parseReleaseBuild liest die Build-Nummer aus einem Tag "v0.<Build>".
//
// @param tag - Release-Tag
// @return int - Build-Nummer
// @return bool - false bei anderem Format
// @date   2026-10-16 10:00
func parseReleaseBuild(tag string) (int, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(tag), "v0.")
	if !ok || rest == "" {
		return 0, false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	build, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return build, true
}

// isNewerRelease prüft ob ein Release-Tag neuer als der eigene Build ist.
// Entwicklerversionen ("dev") werden nie aktualisiert.
//
// @param currentBuild - Eigene Build-Nummer
// @param tag - Release-Tag
// @return bool - true wenn das Release neuer ist
// @date   2026-10-16 10:00
func isNewerRelease(currentBuild, tag string) bool {
	current, err := strconv.Atoi(strings.TrimSpace(currentBuild))
	if err != nil {
		return false
	}
	release, ok := parseReleaseBuild(tag)
	return ok && release > current
}

// updateAssetName liefert den Dateinamen des Release-Assets für eine
// Plattform (so wie build.sh die Dateien benennt).
//
// @param goos - Betriebssystem
// @param goarch - Architektur
// @param setup - true = Windows-Setup statt Binary
// @return string - Asset-Name
// @return bool - false wenn es für die Plattform kein Asset gibt
// @date   2026-10-16 10:00
func updateAssetName(goos, goarch string, setup bool) (string, bool) {
	if goarch != "amd64" && goarch != "arm64" {
		return "", false
	}
	switch goos {
	case "linux":
		if setup {
			return "", false
		}
		if goarch == "amd64" {
			return "ssh-easy", true
		}
		return "ssh-easy-linux-arm64", true
	case "windows":
		if setup {
			return "ssh-easy-setup-" + goarch + ".exe", true
		}
		return "ssh-easy-windows-" + goarch + ".exe", true
	}
	return "", false
}

// looksLikeExecutable prüft die ersten Bytes auf das Binärformat der
// Plattform (Linux: ELF, Windows: PE/"MZ"). Schützt vor installierten
// HTML-Fehlerseiten o.ä.
//
// @param head - Anfang der Datei
// @param goos - Zielplattform
// @return bool - true wenn das Format passt
// @date   2026-10-16 10:00
func looksLikeExecutable(head []byte, goos string) bool {
	if goos == "windows" {
		return bytes.HasPrefix(head, []byte("MZ"))
	}
	return bytes.HasPrefix(head, []byte{0x7f, 'E', 'L', 'F'})
}

// dirWritable prüft ob in einem Verzeichnis Dateien angelegt werden können.
//
// @param dir - Verzeichnis
// @return bool - true wenn beschreibbar
// @date   2026-10-16 10:00
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".ssh-easy-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// Pending liefert das heruntergeladene Update (nil = keines).
//
// @return *pendingUpdate - Update oder nil
// @date   2026-10-16 10:00
func (u *Updater) Pending() *pendingUpdate {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.pending
}

// CheckAndDownload sucht nach einem neueren Release und lädt es herunter.
//
// @param ctx - Kontext (Abbruch beim Beenden)
// @return *pendingUpdate - Heruntergeladenes Update, nil wenn aktuell
// @return error - Fehler bei Suche, Download oder Prüfung
// @date   2026-10-16 10:00
func (u *Updater) CheckAndDownload(ctx context.Context) (*pendingUpdate, error) {
	if _, err := strconv.Atoi(strings.TrimSpace(u.currentBuild)); err != nil {
		// Entwicklerversion: nie aktualisieren
		return nil, nil
	}
	rel, err := u.fetchLatestRelease(ctx)
	if err != nil {
		return nil, err
	}
	if !isNewerRelease(u.currentBuild, rel.TagName) {
		return nil, nil
	}
	build, _ := parseReleaseBuild(rel.TagName)

	// Installationsart: Binary direkt ersetzen oder (Windows) Setup nutzen
	kind := updateReplaceBinary
	exeDir := filepath.Dir(u.exePath)
	if !u.canWriteDir(exeDir) {
		if u.goos != "windows" {
			return nil, fmt.Errorf("keine Schreibrechte für %s", exeDir)
		}
		kind = updateRunSetup
	}

	assetName, ok := updateAssetName(u.goos, u.goarch, kind == updateRunSetup)
	if !ok {
		return nil, fmt.Errorf("kein Update für %s/%s verfügbar", u.goos, u.goarch)
	}
	var asset *releaseAsset
	for i := range rel.Assets {
		if rel.Assets[i].Name == assetName {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		return nil, fmt.Errorf("Release %s enthält keine Datei %s", rel.TagName, assetName)
	}

	target := u.exePath + ".new"
	if kind == updateRunSetup {
		if err := os.MkdirAll(u.downloadDir, 0o700); err != nil {
			return nil, fmt.Errorf("Update-Verzeichnis: %w", err)
		}
		// Das Setup wird mit Adminrechten gestartet: Der Ordner darf kein
		// Symlink/Reparse-Point sein, sonst ließe sich der Download umlenken
		if info, err := os.Lstat(u.downloadDir); err != nil || !info.IsDir() ||
			info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return nil, fmt.Errorf("Update-Verzeichnis %s ist kein normaler Ordner", u.downloadDir)
		}
		// Eigener, frisch angelegter (leerer) Unterordner pro Setup: Ein mit
		// Adminrechten gestartetes Setup darf neben sich keine untergeschobenen
		// DLLs finden (DLL-Hijacking). Vor dem Start wird erneut geprüft.
		setupDir, err := os.MkdirTemp(u.downloadDir, "setup-")
		if err != nil {
			return nil, fmt.Errorf("Update-Verzeichnis: %w", err)
		}
		// Unix: MkdirTemp legt bereits 0700 an. Windows: DACL nur für den
		// Besitzer (0700 wirkt dort nicht)
		if runtime.GOOS == "windows" {
			_ = restrictFilePermissions(setupDir)
		}
		target = filepath.Join(setupDir, assetName)
	}
	if err := u.downloadAsset(ctx, *asset, target); err != nil {
		if kind == updateRunSetup {
			_ = os.Remove(filepath.Dir(target))
		}
		return nil, err
	}

	wantDigest, _ := strings.CutPrefix(asset.Digest, "sha256:")
	pending := &pendingUpdate{build: build, path: target, kind: kind, sha256: strings.ToLower(wantDigest)}
	u.mu.Lock()
	u.pending = pending
	u.mu.Unlock()
	return pending, nil
}

// fetchLatestRelease fragt das neueste Release über die GitHub-API ab.
//
// @param ctx - Kontext
// @return *releaseInfo - Release-Daten
// @return error - HTTP- oder Parse-Fehler
// @date   2026-10-16 10:00
func (u *Updater) fetchLatestRelease(ctx context.Context) (*releaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ssh-easy/"+strings.TrimSpace(u.currentBuild))
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub-API antwortet mit HTTP %d", resp.StatusCode)
	}
	var rel releaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseInfoBytes)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("Antwort der GitHub-API unlesbar: %w", err)
	}
	return &rel, nil
}

// downloadAsset lädt eine Release-Datei nach target und prüft Adresse,
// Größe, SHA-256 und Dateiformat. Bei jedem Fehler wird die Datei gelöscht.
//
// @param ctx - Kontext
// @param asset - Release-Datei laut API
// @param target - Zielpfad
// @return error - Fehler bei Download oder Prüfung
// @date   2026-10-16 10:00
func (u *Updater) downloadAsset(ctx context.Context, asset releaseAsset, target string) (err error) {
	if !strings.HasPrefix(asset.URL, u.allowedURLPrefix) {
		return fmt.Errorf("unerwartete Download-Adresse %q", asset.URL)
	}
	wantDigest, ok := strings.CutPrefix(asset.Digest, "sha256:")
	if !ok || len(wantDigest) != sha256.Size*2 {
		return fmt.Errorf("Release-Datei %s hat keine SHA-256-Prüfsumme", asset.Name)
	}
	if asset.Size <= 0 || asset.Size > maxUpdateBytes {
		return fmt.Errorf("unplausible Größe %d für %s", asset.Size, asset.Name)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ssh-easy/"+strings.TrimSpace(u.currentBuild))
	resp, err := u.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Download antwortet mit HTTP %d", resp.StatusCode)
	}

	// Vorhandene Datei bzw. untergeschobenen Symlink entfernen und exklusiv
	// neu anlegen - O_EXCL folgt keinem Symlink am Zielpfad
	_ = os.Remove(target)
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("Update-Datei: %w", err)
	}
	// Aufräumen bei jedem Fehler: keine halben Downloads liegen lassen
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(target)
		}
	}()

	hash := sha256.New()
	head := &headCapture{limit: 4}
	written, err := io.Copy(io.MultiWriter(f, hash, head), io.LimitReader(resp.Body, asset.Size+1))
	if err != nil {
		return fmt.Errorf("Download abgebrochen: %w", err)
	}
	if written != asset.Size {
		return fmt.Errorf("Größe stimmt nicht: %d statt %d Bytes", written, asset.Size)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, wantDigest) {
		return fmt.Errorf("Prüfsumme stimmt nicht (SHA-256)")
	}
	if !looksLikeExecutable(head.data, u.goos) {
		return fmt.Errorf("Download ist keine ausführbare Datei für %s", u.goos)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("Update-Datei: %w", err)
	}
	return nil
}

// headCapture merkt sich die ersten limit Bytes eines Datenstroms.
type headCapture struct {
	limit int
	data  []byte
}

// Write speichert Bytes, bis limit erreicht ist.
//
// @param p - Daten
// @return int - immer len(p)
// @return error - immer nil
// @date   2026-10-16 10:00
func (h *headCapture) Write(p []byte) (int, error) {
	if missing := h.limit - len(h.data); missing > 0 {
		if missing > len(p) {
			missing = len(p)
		}
		h.data = append(h.data, p[:missing]...)
	}
	return len(p), nil
}

// Apply installiert ein heruntergeladenes Update. Wird beim Beenden
// aufgerufen; ohne Update passiert nichts.
//
// @return error - Fehler bei der Installation
// @date   2026-10-16 10:00
func (u *Updater) Apply() error {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	pending := u.pending
	u.pending = nil
	u.mu.Unlock()
	if pending == nil {
		return nil
	}
	// Prüfsumme unmittelbar vor der Installation erneut prüfen: Zwischen
	// Download (Start) und Installation (Beenden) könnte die Datei ersetzt
	// worden sein. Beim Windows-Setup (Adminrechte!) geschieht die Prüfung
	// über einen gesperrten Datei-Handle, der bis nach dem Start offen bleibt
	// (runVerifiedSetupInstaller) - so bleibt kein Zeitfenster zum Austausch.
	var err error
	switch pending.kind {
	case updateRunSetup:
		err = runVerifiedSetupInstaller(pending.path, pending.sha256)
	default:
		if err = verifyFileSHA256(pending.path, pending.sha256); err == nil {
			err = replaceExecutable(u.exePath, pending.path, u.goos == "windows")
		}
	}
	if err != nil {
		_ = os.Remove(pending.path)
	}
	return err
}

// verifyFileSHA256 vergleicht die SHA-256 einer Datei mit dem erwarteten Wert.
//
// @param path - Datei
// @param want - Erwartete Prüfsumme (hex)
// @return error - Fehler bei Abweichung oder Lesefehler
// @date   2026-10-16 10:00
func verifyFileSHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("Update-Datei: %w", err)
	}
	defer f.Close()
	return verifyReaderSHA256(f, want)
}

// verifyReaderSHA256 vergleicht die SHA-256 eines Datenstroms (z.B. eines
// bereits geöffneten, gesperrten Datei-Handles) mit dem erwarteten Wert.
//
// @param r - Datenquelle
// @param want - Erwartete Prüfsumme (hex)
// @return error - Fehler bei Abweichung oder Lesefehler
// @date   2026-10-16 10:00
func verifyReaderSHA256(r io.Reader, want string) error {
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(r, maxUpdateBytes+1)); err != nil {
		return fmt.Errorf("Update-Datei: %w", err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); want == "" || !strings.EqualFold(got, want) {
		return fmt.Errorf("Prüfsumme der Update-Datei stimmt nicht mehr - Installation abgebrochen")
	}
	return nil
}

// replaceExecutable ersetzt die Binary durch die heruntergeladene Datei.
//
// keepOld=false (Linux): Rename über die laufende Binary - unter Unix
// erlaubt und atomar, der laufende Prozess behält die alte Datei.
// keepOld=true (Windows): Eine laufende EXE kann nicht überschrieben, aber
// umbenannt werden: exe -> exe.old, neu -> exe. Die .old-Datei wird beim
// nächsten Start entfernt (cleanupUpdateLeftovers).
//
// @param exePath - Pfad der laufenden Binary
// @param newPath - Pfad der neuen Binary
// @param keepOld - true = Windows-Verfahren mit .old
// @return error - Fehler beim Umbenennen
// @date   2026-10-16 10:00
func replaceExecutable(exePath, newPath string, keepOld bool) error {
	if _, err := os.Stat(newPath); err != nil {
		return fmt.Errorf("Update-Datei fehlt: %w", err)
	}
	if !keepOld {
		return os.Rename(newPath, exePath)
	}
	oldPath := exePath + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("laufende Binary nicht umbenennbar: %w", err)
	}
	if err := os.Rename(newPath, exePath); err != nil {
		// Zurückrollen, damit das Programm startfähig bleibt
		_ = os.Rename(oldPath, exePath)
		return fmt.Errorf("neue Binary nicht installierbar: %w", err)
	}
	return nil
}

// cleanupUpdateLeftovers entfernt Reste früherer Updates beim Start:
// exe.old (Windows-Austausch), exe.new (abgebrochener Download) und
// heruntergeladene Setups.
//
// @param exePath - Pfad der laufenden Binary
// @param downloadDir - Ablage der Setups
// @date   2026-10-16 10:00
func cleanupUpdateLeftovers(exePath, downloadDir string) {
	if exePath != "" {
		_ = os.Remove(exePath + ".old")
		_ = os.Remove(exePath + ".new")
	}
	if downloadDir == "" {
		return
	}
	entries, err := os.ReadDir(downloadDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(downloadDir, e.Name())
		switch {
		case e.Type().IsRegular() && strings.HasPrefix(e.Name(), "ssh-easy-setup-"):
			// Altes Ablageformat (Setup direkt im Update-Ordner)
			_ = os.Remove(path)
		case e.IsDir() && strings.HasPrefix(e.Name(), "setup-"):
			// Unterordner eines früheren Setups (seit Build 49)
			_ = os.RemoveAll(path)
		}
	}
}

// checkSetupDirectoryClean prüft vor dem Start des Setups, dass in seinem
// Ordner NUR das Setup selbst liegt - keine untergeschobenen DLLs oder
// anderen Dateien (Schutz vor DLL-Hijacking beim Start mit Adminrechten).
//
// @param setupPath - Pfad des Setups
// @return error - Fehler wenn weitere Einträge im Ordner liegen
// @date   2026-10-16 11:00
func checkSetupDirectoryClean(setupPath string) error {
	dir := filepath.Dir(setupPath)
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() ||
		info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf("Setup-Ordner %s ist kein normaler Ordner", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("Setup-Ordner: %w", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(setupPath) {
			return fmt.Errorf("unerwartete Datei %q im Setup-Ordner - Installation abgebrochen", e.Name())
		}
	}
	return nil
}

// --- TUI-Anbindung ---

// SetUpdater verbindet den Updater mit dem Modell. Ist Auto-Update
// eingeschaltet, wird die Suche beim Start (Init) ausgelöst.
//
// @param u - Updater
// @date   2026-10-16 10:00
func (m *AppModel) SetUpdater(u *Updater) {
	m.updater = u
	m.updateChecking = m.autoUpdate && u != nil
	if m.updateChecking {
		m.updateStatus = m.tr(txtUpdateChecking)
	}
}

// updateCheckCmd sucht im Hintergrund nach Updates.
//
// @return tea.Cmd - Kommando, das updateResultMsg liefert
// @date   2026-10-16 10:00
func (m AppModel) updateCheckCmd() tea.Cmd {
	updater := m.updater
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		pending, err := updater.CheckAndDownload(ctx)
		return updateResultMsg{pending: pending, err: err}
	}
}

// handleUpdateResult zeigt das Ergebnis der Update-Suche an.
//
// @param msg - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Immer nil
// @date   2026-10-16 10:00
func (m AppModel) handleUpdateResult(msg updateResultMsg) (tea.Model, tea.Cmd) {
	m.updateChecking = false
	switch {
	case msg.err != nil:
		m.updateStatus = fmt.Sprintf(m.tr(txtUpdateFailed), msg.err.Error())
		if m.sshManager != nil {
			m.sshManager.logger.Warn("Auto-Update fehlgeschlagen: %v", msg.err)
		}
	case msg.pending != nil:
		m.updateStatus = fmt.Sprintf(m.tr(txtUpdateReady), msg.pending.build)
	default:
		m.updateStatus = m.tr(txtUpdateCurrent)
	}
	return m, nil
}

// toggleAutoUpdate schaltet Auto-Update um und speichert die Einstellung.
// Beim Einschalten wird sofort gesucht (sonst wirkte es erst nach zwei
// Neustarts: einmal laden, einmal installieren).
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Update-Suche oder nil
// @date   2026-10-16 10:00
func (m AppModel) toggleAutoUpdate() (tea.Model, tea.Cmd) {
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		m.errorMsg = err.Error()
		return m, nil
	}
	cfg.AutoUpdate = !m.autoUpdate
	if err := SaveConfig(m.configPath, cfg); err != nil {
		m.errorMsg = err.Error()
		return m, nil
	}
	m.configCache.Invalidate()
	m.autoUpdate = cfg.AutoUpdate
	m.errorMsg = ""

	if !m.autoUpdate {
		m.updateStatus = ""
		return m, nil
	}
	if m.updater == nil || m.updateChecking || m.updater.Pending() != nil {
		return m, nil
	}
	m.updateChecking = true
	m.updateStatus = m.tr(txtUpdateChecking)
	return m, m.updateCheckCmd()
}

// renderAutoUpdateLine rendert den Auto-Update-Schalter (letzte Menüzeile),
// bei eingeschaltetem Auto-Update mit dem Ergebnis der Suche.
//
// @return string - Gerenderte Zeile
// @date   2026-10-16 10:00
func (m AppModel) renderAutoUpdateLine() string {
	text := m.tr(txtAutoUpdateOff)
	if m.autoUpdate {
		text = m.tr(txtAutoUpdateOn)
		if m.updateStatus != "" {
			text += "  – " + m.updateStatus
		}
	}
	return helpStyle.UnsetMarginTop().Render(text)
}

// errUpdateUnsupported meldet eine auf der Plattform nicht mögliche Aktion.
var errUpdateUnsupported = errors.New("auf dieser Plattform nicht unterstützt")
