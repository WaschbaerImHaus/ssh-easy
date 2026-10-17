// Paket main - Tests für die signierte Prüfsummenliste der Releases (Build 51)
//
// Hintergrund (Sicherheits-Scan, CWE-494): Bisher stammte die SHA-256 eines
// Updates aus derselben Quelle wie die Datei selbst (GitHub-API). Wer das
// Release-Konto bzw. den Token kontrolliert, hätte Binary und Prüfsumme
// gemeinsam austauschen können. Jetzt liegt jedem Release eine mit Ed25519
// signierte Prüfsummenliste bei; der öffentliche Schlüssel ist im Programm
// eingebettet, der private liegt nur auf dem Build-Rechner.
//
// @author Kurt Ingwer
// @date   2026-10-17 22:40
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ssh-easy/releasesig"
)

// testSigningKey ist ein fester Testschlüssel (deterministisch).
var testSigningKey = ed25519.NewKeyFromSeed([]byte("ssh-easy-test-signing-key-seed!!"))

// testSigningPub ist der passende öffentliche Schlüssel.
var testSigningPub = testSigningKey.Public().(ed25519.PublicKey)

// hexSum liefert die SHA-256 eines Inhalts als Hex-Text.
func hexSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestManifestRoundtrip prüft Erzeugen und Einlesen der Liste.
func TestManifestRoundtrip(t *testing.T) {
	sums := map[string]string{
		"ssh-easy":                   hexSum([]byte("a")),
		"ssh-easy-windows-amd64.exe": hexSum([]byte("b")),
	}
	data := releasesig.BuildManifest("v0.51", sums)
	if !strings.HasPrefix(string(data), "ssh-easy release v0.51\n") {
		t.Errorf("Kopfzeile fehlt: %q", data)
	}
	got, err := releasesig.ParseManifest(data, "v0.51")
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(got) != 2 || got["ssh-easy"] != sums["ssh-easy"] || got["ssh-easy-windows-amd64.exe"] != sums["ssh-easy-windows-amd64.exe"] {
		t.Errorf("Roundtrip = %v", got)
	}
	// Reihenfolge ist stabil (sortiert) - gleiche Eingabe, gleiche Bytes
	if string(releasesig.BuildManifest("v0.51", sums)) != string(data) {
		t.Error("Liste ist nicht deterministisch")
	}
}

// TestParseManifestRejectsOtherRelease prüft den Schutz vor dem
// Wiederverwenden einer (gültig signierten) Liste eines anderen Releases.
func TestParseManifestRejectsOtherRelease(t *testing.T) {
	data := releasesig.BuildManifest("v0.47", map[string]string{"ssh-easy": hexSum([]byte("alt"))})
	if _, err := releasesig.ParseManifest(data, "v0.51"); err == nil {
		t.Error("Liste von v0.47 wurde für v0.51 akzeptiert")
	}
}

// TestParseManifestRejectsMalformed prüft ungültige Inhalte.
func TestParseManifestRejectsMalformed(t *testing.T) {
	sum := hexSum([]byte("x"))
	cases := map[string]string{
		"leer":             "",
		"ohne Kopf":        sum + "  ssh-easy\n",
		"kurze Prüfsumme":  "ssh-easy release v0.51\nabc  ssh-easy\n",
		"kein Hex":         "ssh-easy release v0.51\n" + strings.Repeat("z", 64) + "  ssh-easy\n",
		"ein Leerzeichen":  "ssh-easy release v0.51\n" + sum + " ssh-easy\n",
		"ohne Namen":       "ssh-easy release v0.51\n" + sum + "  \n",
		"Pfad im Namen":    "ssh-easy release v0.51\n" + sum + "  ../ssh-easy\n",
		"Backslash":        "ssh-easy release v0.51\n" + sum + "  a\\ssh-easy\n",
		"doppelt":          "ssh-easy release v0.51\n" + sum + "  ssh-easy\n" + sum + "  ssh-easy\n",
		"ohne Einträge":    "ssh-easy release v0.51\n",
		"Kopf mit Zusatz":  "ssh-easy release v0.51 extra\n" + sum + "  ssh-easy\n",
		"Großbuchstaben":   "ssh-easy release v0.51\n" + strings.ToUpper(sum) + "  ssh-easy\n",
		"Leerzeile":        "ssh-easy release v0.51\n\n" + sum + "  ssh-easy\n",
		"ohne Zeilenende":  "ssh-easy release v0.51\n" + sum + "  ssh-easy",
		"Windows-Zeilen":   "ssh-easy release v0.51\r\n" + sum + "  ssh-easy\r\n",
		"Name mit Leerz.":  "ssh-easy release v0.51\n" + sum + "  ssh easy\n",
		"Name mit Steuerz": "ssh-easy release v0.51\n" + sum + "  ssh-easy\x00\n",
	}
	for name, data := range cases {
		if _, err := releasesig.ParseManifest([]byte(data), "v0.51"); err == nil {
			t.Errorf("%s: ungültige Liste akzeptiert", name)
		}
	}
}

// TestSignVerify prüft Signieren und Prüfen inkl. Manipulationen.
func TestSignVerify(t *testing.T) {
	manifest := releasesig.BuildManifest("v0.51", map[string]string{"ssh-easy": hexSum([]byte("a"))})
	sig := releasesig.Sign(testSigningKey, manifest)
	if err := releasesig.Verify(testSigningPub, manifest, sig); err != nil {
		t.Fatalf("gültige Signatur abgelehnt: %v", err)
	}

	// Ein verändertes Byte in der Liste
	tampered := append([]byte(nil), manifest...)
	tampered[len(tampered)-2] ^= 1
	if releasesig.Verify(testSigningPub, tampered, sig) == nil {
		t.Error("veränderte Liste akzeptiert")
	}

	// Fremder Schlüssel
	other := ed25519.NewKeyFromSeed([]byte("ein-ganz-anderer-schluessel-1234"))
	if releasesig.Verify(testSigningPub, manifest, releasesig.Sign(other, manifest)) == nil {
		t.Error("Signatur eines fremden Schlüssels akzeptiert")
	}

	// Kaputte Signaturdateien
	for name, bad := range map[string][]byte{
		"leer":      nil,
		"kein b64":  []byte("!!!kein-base64!!!"),
		"zu kurz":   []byte(base64.StdEncoding.EncodeToString([]byte("kurz"))),
		"zu lang":   []byte(base64.StdEncoding.EncodeToString(make([]byte, 65))),
		"Nullbytes": []byte(base64.StdEncoding.EncodeToString(make([]byte, 64))),
	} {
		if releasesig.Verify(testSigningPub, manifest, bad) == nil {
			t.Errorf("%s: kaputte Signatur akzeptiert", name)
		}
	}

	// Ungültiger öffentlicher Schlüssel darf nicht in Panik enden
	if releasesig.Verify(ed25519.PublicKey([]byte("kurz")), manifest, sig) == nil {
		t.Error("ungültiger öffentlicher Schlüssel akzeptiert")
	}
}

// TestSignatureIsDomainSeparated prüft dass die Signatur nicht einfach die
// rohe Ed25519-Signatur der Liste ist (Kontext-Präfix): Eine mit demselben
// Schlüssel für einen anderen Zweck erstellte Signatur gilt nicht.
func TestSignatureIsDomainSeparated(t *testing.T) {
	manifest := releasesig.BuildManifest("v0.51", map[string]string{"ssh-easy": hexSum([]byte("a"))})
	raw := ed25519.Sign(testSigningKey, manifest)
	encoded := []byte(base64.StdEncoding.EncodeToString(raw))
	if releasesig.Verify(testSigningPub, manifest, encoded) == nil {
		t.Error("Signatur ohne Kontext-Präfix akzeptiert")
	}
}

// TestGenerateAndLoadKey prüft Schlüsselerzeugung, Rechte und das
// Verbot, einen vorhandenen Schlüssel zu überschreiben.
func TestGenerateAndLoadKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "release-signing.key")
	pub, err := releasesig.GenerateKey(path)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("öffentlicher Schlüssel hat %d Bytes", len(pub))
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("Schlüsseldatei hat Rechte %v, erwartet 0600", info.Mode().Perm())
		}
	}
	if _, err := releasesig.GenerateKey(path); err == nil {
		t.Error("vorhandener Schlüssel wurde überschrieben")
	}

	priv, err := releasesig.LoadPrivateKey(path)
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}
	if !priv.Public().(ed25519.PublicKey).Equal(pub) {
		t.Error("geladener Schlüssel passt nicht zum erzeugten")
	}
}

// TestLoadPrivateKeyRejectsOpenPermissions prüft dass ein für andere
// lesbarer Schlüssel nicht verwendet wird.
func TestLoadPrivateKeyRejectsOpenPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-Dateirechte")
	}
	path := filepath.Join(t.TempDir(), "k.key")
	if _, err := releasesig.GenerateKey(path); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(path, 0o644)
	if _, err := releasesig.LoadPrivateKey(path); err == nil {
		t.Error("für andere lesbarer Schlüssel wurde akzeptiert")
	}
}

// TestLoadPrivateKeyRejectsGarbage prüft kaputte Schlüsseldateien.
func TestLoadPrivateKeyRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"leer":     "",
		"kein b64": "%%%",
		"zu kurz":  base64.StdEncoding.EncodeToString([]byte("kurz")),
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_"))
		_ = os.WriteFile(path, []byte(content), 0o600)
		if _, err := releasesig.LoadPrivateKey(path); err == nil {
			t.Errorf("%s: kaputter Schlüssel akzeptiert", name)
		}
	}
	if _, err := releasesig.LoadPrivateKey(filepath.Join(dir, "fehlt")); err == nil {
		t.Error("fehlende Datei akzeptiert")
	}
}

// TestSignFiles prüft das Signieren der Build-Dateien (wie build.sh es tut).
func TestSignFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{"ssh-easy": []byte("\x7fELFlinux"), "ssh-easy-setup-amd64.exe": []byte("MZsetup")}
	var paths []string
	for name, content := range files {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, content, 0o755)
		paths = append(paths, p)
	}
	if err := releasesig.SignFiles(testSigningKey, "v0.51", dir, paths); err != nil {
		t.Fatalf("SignFiles: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, releasesig.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(filepath.Join(dir, releasesig.SignatureName))
	if err != nil {
		t.Fatal(err)
	}
	if err := releasesig.Verify(testSigningPub, manifest, sig); err != nil {
		t.Fatalf("erzeugte Signatur ungültig: %v", err)
	}
	sums, err := releasesig.ParseManifest(manifest, "v0.51")
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if sums[name] != hexSum(content) {
			t.Errorf("%s: Prüfsumme %q, erwartet %q", name, sums[name], hexSum(content))
		}
	}

	// Doppelte Dateinamen (z.B. aus zwei Ordnern) sind ein Fehler
	other := filepath.Join(t.TempDir(), "ssh-easy")
	_ = os.WriteFile(other, []byte("anders"), 0o755)
	if err := releasesig.SignFiles(testSigningKey, "v0.51", dir, append(paths, other)); err == nil {
		t.Error("doppelter Dateiname wurde akzeptiert")
	}
	// Fehlende Datei
	if err := releasesig.SignFiles(testSigningKey, "v0.51", dir, []string{filepath.Join(dir, "fehlt")}); err == nil {
		t.Error("fehlende Datei wurde akzeptiert")
	}
}

// TestEmbeddedReleaseKey prüft den im Programm eingebetteten Schlüssel.
func TestEmbeddedReleaseKey(t *testing.T) {
	key := releaseSigningKey()
	if len(key) != ed25519.PublicKeySize {
		t.Fatalf("eingebetteter Schlüssel hat %d Bytes", len(key))
	}
	if strings.Trim(hex.EncodeToString(key), "0") == "" {
		t.Error("eingebetteter Schlüssel ist ein Platzhalter (nur Nullen)")
	}
	if NewUpdater("50").signingKey == nil {
		t.Error("NewUpdater setzt den Signaturschlüssel nicht")
	}
}
