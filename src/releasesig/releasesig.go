// Paket releasesig - signierte Prüfsummenliste für Releases
//
// Jedes Release enthält zwei zusätzliche Dateien:
//   - ssh-easy-SHA256SUMS:     Kopfzeile "ssh-easy release <Tag>" und je
//     Datei eine Zeile "<sha256-hex>  <Dateiname>" (sortiert)
//   - ssh-easy-SHA256SUMS.sig: Ed25519-Signatur (Base64) über
//     signatureContext + Liste
//
// Der Auto-Updater prüft die Signatur mit dem im Programm eingebetteten
// öffentlichen Schlüssel. Der private Schlüssel liegt nur auf dem
// Build-Rechner - wer nur das GitHub-Konto bzw. den Token kontrolliert,
// kann deshalb keine Binary mehr unterschieben (CWE-494).
//
// Die Kopfzeile bindet die Liste an genau ein Release: Eine echte Liste
// eines alten Releases lässt sich nicht als neues Release ausgeben.
//
// @author Kurt Ingwer
// @date   2026-10-17 22:40
package releasesig

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	// ManifestName - Dateiname der Prüfsummenliste im Release
	ManifestName = "ssh-easy-SHA256SUMS"
	// SignatureName - Dateiname der Signatur im Release
	SignatureName = ManifestName + ".sig"
	// MaxManifestBytes - Obergrenze für Liste bzw. Signatur beim Download
	MaxManifestBytes = 64 << 10
	// manifestHeader - Anfang der Kopfzeile, gefolgt vom Release-Tag
	manifestHeader = "ssh-easy release "
	// signatureContext - Kontext-Präfix der Signatur (Domain Separation):
	// Eine Signatur für einen anderen Zweck gilt hier nicht
	signatureContext = "ssh-easy release manifest v1\n"
)

// BuildManifest erzeugt die Prüfsummenliste eines Releases.
//
// @param tag - Release-Tag (z.B. "v0.51")
// @param sums - Dateiname -> SHA-256 (hex, klein)
// @return []byte - Inhalt der Liste (deterministisch sortiert)
// @date   2026-10-17 22:40
func BuildManifest(tag string, sums map[string]string) []byte {
	names := make([]string, 0, len(sums))
	for name := range sums {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	buf.WriteString(manifestHeader + tag + "\n")
	for _, name := range names {
		buf.WriteString(sums[name] + "  " + name + "\n")
	}
	return buf.Bytes()
}

// ParseManifest liest eine Prüfsummenliste streng ein. Jede Abweichung vom
// Format ist ein Fehler - lieber kein Update als ein falsch verstandenes.
//
// @param data - Inhalt der Liste
// @param tag - Erwarteter Release-Tag (Kopfzeile muss genau passen)
// @return map[string]string - Dateiname -> SHA-256 (hex)
// @return error - Formatfehler oder Liste eines anderen Releases
// @date   2026-10-17 22:40
func ParseManifest(data []byte, tag string) (map[string]string, error) {
	text := string(data)
	if !strings.HasSuffix(text, "\n") {
		return nil, errors.New("Prüfsummenliste: Zeilenende fehlt")
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if lines[0] != manifestHeader+tag {
		return nil, fmt.Errorf("Prüfsummenliste gehört nicht zu Release %s", tag)
	}
	sums := make(map[string]string, len(lines)-1)
	for _, line := range lines[1:] {
		sum, name, ok := strings.Cut(line, "  ")
		if !ok || !validHexSum(sum) || !validFileName(name) {
			return nil, fmt.Errorf("Prüfsummenliste: ungültige Zeile %q", line)
		}
		if _, dup := sums[name]; dup {
			return nil, fmt.Errorf("Prüfsummenliste: %s doppelt", name)
		}
		sums[name] = sum
	}
	if len(sums) == 0 {
		return nil, errors.New("Prüfsummenliste ist leer")
	}
	return sums, nil
}

// validHexSum prüft eine SHA-256 als 64 kleine Hex-Zeichen.
//
// @param sum - Prüfsumme
// @return bool - true wenn gültig
// @date   2026-10-17 22:40
func validHexSum(sum string) bool {
	if len(sum) != sha256.Size*2 {
		return false
	}
	for _, r := range sum {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// validFileName erlaubt nur schlichte Dateinamen (Buchstaben, Ziffern,
// Punkt, Binde- und Unterstrich) - keine Pfade, Leer- oder Steuerzeichen.
//
// @param name - Dateiname
// @return bool - true wenn gültig
// @date   2026-10-17 22:40
func validFileName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// Sign signiert eine Prüfsummenliste.
//
// @param priv - Privater Schlüssel
// @param manifest - Inhalt der Liste
// @return []byte - Signatur als Base64-Text mit Zeilenende
// @date   2026-10-17 22:40
func Sign(priv ed25519.PrivateKey, manifest []byte) []byte {
	sig := ed25519.Sign(priv, signedMessage(manifest))
	return []byte(base64.StdEncoding.EncodeToString(sig) + "\n")
}

// Verify prüft die Signatur einer Prüfsummenliste.
//
// @param pub - Öffentlicher Schlüssel
// @param manifest - Inhalt der Liste
// @param sig - Inhalt der Signaturdatei (Base64)
// @return error - nil nur bei gültiger Signatur
// @date   2026-10-17 22:40
func Verify(pub ed25519.PublicKey, manifest, sig []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("Signaturschlüssel ungültig")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New("Signatur der Prüfsummenliste unlesbar")
	}
	if !ed25519.Verify(pub, signedMessage(manifest), raw) {
		return errors.New("Signatur der Prüfsummenliste ungültig")
	}
	return nil
}

// signedMessage liefert die tatsächlich signierten Bytes (Kontext + Liste).
//
// @param manifest - Inhalt der Liste
// @return []byte - Zu signierende Nachricht
// @date   2026-10-17 22:40
func signedMessage(manifest []byte) []byte {
	return append([]byte(signatureContext), manifest...)
}

// GenerateKey erzeugt einen neuen Signaturschlüssel. Eine vorhandene Datei
// wird nie überschrieben (der alte Schlüssel wäre sonst verloren und keine
// installierte Version könnte mehr Updates prüfen).
//
// @param path - Zieldatei des privaten Schlüssels (Rechte 0600)
// @return ed25519.PublicKey - Öffentlicher Schlüssel zum Einbetten
// @return error - Fehler beim Anlegen
// @date   2026-10-17 22:40
func GenerateKey(path string) (ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("Schlüsseldatei: %w", err)
	}
	// Gespeichert wird nur der 32-Byte-Seed (Base64)
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return pub, f.Close()
}

// LoadPrivateKey lädt den privaten Signaturschlüssel. Unter Unix wird ein
// für Gruppe oder andere lesbarer Schlüssel abgelehnt.
//
// @param path - Schlüsseldatei
// @return ed25519.PrivateKey - Privater Schlüssel
// @return error - Lese-, Rechte- oder Formatfehler
// @date   2026-10-17 22:40
func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("Schlüsseldatei %s ist für andere lesbar (chmod 600)", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("Schlüsseldatei %s ist ungültig", path)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// SignFiles berechnet die Prüfsummen der Dateien und schreibt Liste und
// Signatur nach outDir (Aufruf aus build.sh über cmd/release-sign).
//
// @param priv - Privater Schlüssel
// @param tag - Release-Tag
// @param outDir - Zielordner für Liste und Signatur
// @param files - Zu signierende Dateien (Dateinamen müssen eindeutig sein)
// @return error - Lese- oder Schreibfehler
// @date   2026-10-17 22:40
func SignFiles(priv ed25519.PrivateKey, tag, outDir string, files []string) error {
	sums := make(map[string]string, len(files))
	for _, path := range files {
		name := filepath.Base(path)
		if !validFileName(name) {
			return fmt.Errorf("ungültiger Dateiname %q", name)
		}
		if _, dup := sums[name]; dup {
			return fmt.Errorf("Dateiname %s doppelt", name)
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		sums[name] = sum
	}
	manifest := BuildManifest(tag, sums)
	if err := os.WriteFile(filepath.Join(outDir, ManifestName), manifest, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, SignatureName), Sign(priv, manifest), 0o644)
}

// fileSHA256 berechnet die SHA-256 einer Datei.
//
// @param path - Datei
// @return string - Prüfsumme (hex, klein)
// @return error - Lesefehler
// @date   2026-10-17 22:40
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
