// Paket main - Tests für die Prüfung öffentlicher Ed25519-Schlüssel
// (releasesig.ValidPublicKey, Audit Build 90)
//
// Hintergrund: crypto/ed25519 prüft beim Verifizieren nur, ob der öffentliche
// Schlüssel ein Punkt der Kurve ist. Ein Punkt kleiner Ordnung - allen voran
// das neutrale Element (Kodierung 0100…00) - macht [k]A für jede Nachricht
// trivial. Dann lässt sich OHNE privaten Schlüssel eine Signatur bauen, die
// ed25519.Verify akzeptiert. Ein solcher Eintrag in der Schlüsselliste des
// Updaters wäre ein Freibrief für gefälschte Releases. Deshalb müssen
// ValidPublicKey, ParsePublicKeysHex, Verify und VerifyAny solche Schlüssel
// ablehnen. Alle Punkte in diesen Tests sind öffentlich bekannte Konstanten
// (RFC 8032 / Ed25519-Testvektoren); der echte private Schlüssel wird nie
// benutzt.
//
// @author Kurt Ingwer
// @date   2026-11-15 14:55
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"ssh-easy/releasesig"
)

// pubkeyIdentityHex ist das neutrale Element (0, 1) der Kurve.
const pubkeyIdentityHex = "0100000000000000000000000000000000000000000000000000000000000000"

// pubkeyMustDecode wandelt einen Hex-Text in Bytes (Testfehler bei Tippfehler).
func pubkeyMustDecode(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(text)
	if err != nil {
		t.Fatalf("Hex %q ungültig: %v", text, err)
	}
	return raw
}

// pubkeyInvalidCases sind alle Schlüssel, die ValidPublicKey ablehnen muss
// (Name -> Hex; die falschen Längen stehen in TestValidPublicKeyRejectsWrongLength).
var pubkeyInvalidCases = []struct {
	name string
	hex  string
}{
	{"neutrales Element (Ordnung 1)", pubkeyIdentityHex},
	{"nur Nullen (y=0, Ordnung 4)", "0000000000000000000000000000000000000000000000000000000000000000"},
	{"Ordnung 2", "ecffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f"},
	{"Ordnung 8", "c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac037a"},
	{"y = p (nicht kanonisch)", "edffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f"},
	{"-0 (x=0 mit gesetztem Vorzeichenbit)", "0100000000000000000000000000000000000000000000000000000000000080"},
	{"nicht auf der Kurve (y=2)", "0200000000000000000000000000000000000000000000000000000000000000"},
	{"Mischpunkt (Hauptschlüssel + Ordnung-8-Punkt)", "5c66524a795d42b58b897140f25d44b032f4ed2b73639ef2467abbda7db09b0c"},
}

// TestValidPublicKeyRejectsInvalidPoints prüft jeden bekannten schlechten
// Punkt (kleine Ordnung, nicht kanonisch, nicht auf der Kurve, Mischpunkt):
// ValidPublicKey muss false liefern. Angriff: gefälschte Signaturen über
// einen Schlüssel kleiner Ordnung.
func TestValidPublicKeyRejectsInvalidPoints(t *testing.T) {
	for _, tc := range pubkeyInvalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if releasesig.ValidPublicKey(pubkeyMustDecode(t, tc.hex)) {
				t.Errorf("%s: ValidPublicKey akzeptiert %s", tc.name, tc.hex)
			}
		})
	}
}

// TestValidPublicKeyRejectsWrongLength prüft Randfälle der Länge: leer, nil,
// 31 und 33 Bytes sind nie gültig (auch nicht, wenn der Anfang ein echter
// Schlüssel ist).
func TestValidPublicKeyRejectsWrongLength(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !releasesig.ValidPublicKey(pub) {
		t.Fatal("Testaufbau falsch: frischer Schlüssel wird abgelehnt")
	}
	cases := map[string][]byte{
		"nil":      nil,
		"0 Bytes":  {},
		"31 Bytes": pub[:31],
		"33 Bytes": append(append([]byte{}, pub...), 0),
		"64 Bytes": append(append([]byte{}, pub...), pub...),
	}
	for name, key := range cases {
		if releasesig.ValidPublicKey(key) {
			t.Errorf("%s: ValidPublicKey akzeptiert falsche Länge", name)
		}
	}
}

// TestParsePublicKeysHexDropsInvalidPoints prüft, dass ParsePublicKeysHex
// jeden schlechten Punkt verwirft - einzeln und zwischen gültigen Einträgen
// (gültige bleiben in ihrer Reihenfolge erhalten). Ein stiller Fehler hier
// ließe einen Schlüssel kleiner Ordnung in die Updater-Liste.
func TestParsePublicKeysHexDropsInvalidPoints(t *testing.T) {
	for _, tc := range pubkeyInvalidCases {
		if keys := releasesig.ParsePublicKeysHex([]string{tc.hex}); len(keys) != 0 {
			t.Errorf("%s: ParsePublicKeysHex behält den Eintrag", tc.name)
		}
	}
	// Gemischt: gültig, schlecht, gültig - nur die zwei gültigen bleiben
	mixed := []string{releaseSigningPublicKeyHex}
	for _, tc := range pubkeyInvalidCases {
		mixed = append(mixed, tc.hex)
	}
	mixed = append(mixed, releaseBackupSigningPublicKeysHex...)
	keys := releasesig.ParsePublicKeysHex(mixed)
	if want := 1 + len(releaseBackupSigningPublicKeysHex); len(keys) != want {
		t.Fatalf("gemischte Liste: %d Schlüssel, erwartet %d", len(keys), want)
	}
	if hex.EncodeToString(keys[0]) != releaseSigningPublicKeyHex {
		t.Error("Reihenfolge der gültigen Schlüssel verändert")
	}
}

// TestValidPublicKeyAcceptsFreshKeys prüft, dass echte Schlüssel nie
// fälschlich abgelehnt werden (Gegenprobe zu den Ablehnungstests): 20
// frische Schlüssel, außerdem passt ParsePublicKeysHex sie durch.
func TestValidPublicKeyAcceptsFreshKeys(t *testing.T) {
	for i := 0; i < 20; i++ {
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if !releasesig.ValidPublicKey(pub) {
			t.Fatalf("frischer Schlüssel %d (%x) abgelehnt", i, []byte(pub))
		}
		if keys := releasesig.ParsePublicKeysHex([]string{hex.EncodeToString(pub)}); len(keys) != 1 {
			t.Fatalf("frischer Schlüssel %d von ParsePublicKeysHex verworfen", i)
		}
	}
}

// TestValidPublicKeyAcceptsEmbeddedKeys prüft die beiden tatsächlich
// eingebetteten Schlüssel (Haupt- und Ersatzschlüssel) als feste Werte:
// Würde die Prüfung sie ablehnen, könnte kein Update mehr verifiziert werden.
func TestValidPublicKeyAcceptsEmbeddedKeys(t *testing.T) {
	for _, entry := range []string{
		"e548b36992f95417e386183b3b57929b97fdd6801def9aee90b3489dccd7192f",
		"45cc3e61bdadc1e781cf33395ba1468eec05bced9dc61d062f220aeaad7bd964",
	} {
		if !releasesig.ValidPublicKey(pubkeyMustDecode(t, entry)) {
			t.Errorf("eingebetteter Schlüssel %s abgelehnt", entry)
		}
	}
}

// forgeIdentitySignature baut ohne privaten Schlüssel eine Signatur, die
// crypto/ed25519 für das neutrale Element als öffentlichen Schlüssel
// akzeptiert: Prüfgleichung [S]B = R + [k]A. Mit A = neutral gilt [k]A = O,
// also genügt R = [S]B. Wähle seed beliebig, a = geklemmte untere Hälfte von
// SHA-512(seed), S = a mod L; dann ist R = [a]B = NewKeyFromSeed(seed).Public().
//
// @param seed - beliebige 32 Byte (kein Geheimnis, nur Rechenhilfe)
// @return []byte - 64 Byte lange Signatur R||S, gültig für jede Nachricht
func forgeIdentitySignature(seed []byte) []byte {
	if len(seed) != ed25519.SeedSize {
		panic("seed muss 32 Byte lang sein")
	}
	h := sha512.Sum512(seed)
	h[0] &= 248
	h[31] &= 127
	h[31] |= 64
	// little-endian -> big.Int
	be := make([]byte, 32)
	for i := 0; i < 32; i++ {
		be[31-i] = h[i]
	}
	a := new(big.Int).SetBytes(be)
	l, _ := new(big.Int).SetString("27742317777372353535851937790883648493", 10)
	l.Add(l, new(big.Int).Lsh(big.NewInt(1), 252))
	s := a.Mod(a, l)
	sBE := s.FillBytes(make([]byte, 32))
	sLE := make([]byte, 32)
	for i := 0; i < 32; i++ {
		sLE[i] = sBE[31-i]
	}
	r := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	return append(append([]byte{}, r...), sLE...)
}

// TestIdentityKeyForgeryIsRejected ist die Fälschungs-Gegenprobe (Kern):
// Mit dem neutralen Element als Schlüssel entsteht OHNE privaten Schlüssel
// eine Signatur, die crypto/ed25519 akzeptiert. Zuerst wird das belegt (sonst
// wäre der Test falsch gebaut), dann müssen Verify, VerifyAny und
// ParsePublicKeysHex den Schlüssel trotzdem abweisen.
func TestIdentityKeyForgeryIsRejected(t *testing.T) {
	identity := ed25519.PublicKey(pubkeyMustDecode(t, pubkeyIdentityHex))
	manifest := releasesig.BuildManifest("v0.999", map[string]string{"ssh-easy": hexSum([]byte("boese"))})
	// Nachricht wie in releasesig.signedMessage: Kontext + Liste
	message := append([]byte("ssh-easy release manifest v1\n"), manifest...)
	seed := sha256.Sum256([]byte("ssh-easy-forgery-test-seed"))
	sig := forgeIdentitySignature(seed[:])
	if !ed25519.Verify(identity, message, sig) {
		t.Fatal("Testaufbau falsch: crypto/ed25519 akzeptiert die Fälschung nicht")
	}
	sigFile := []byte(base64.StdEncoding.EncodeToString(sig) + "\n")

	if err := releasesig.VerifyAny([]ed25519.PublicKey{identity}, manifest, sigFile); err == nil {
		t.Error("VerifyAny akzeptiert eine Fälschung mit dem neutralen Element als Schlüssel")
	}
	if err := releasesig.Verify(identity, manifest, sigFile); err == nil {
		t.Error("Verify akzeptiert eine Fälschung mit dem neutralen Element als Schlüssel")
	}
	if keys := releasesig.ParsePublicKeysHex([]string{pubkeyIdentityHex}); len(keys) != 0 {
		t.Errorf("ParsePublicKeysHex behält das neutrale Element (%d Schlüssel)", len(keys))
	}
	// Mit weiteren echten Schlüsseln in der Liste darf die Fälschung auch
	// nicht über den schlechten Schlüssel durchrutschen
	_, realPub := multikeyPair("haupt")
	if err := releasesig.VerifyAny([]ed25519.PublicKey{realPub, identity}, manifest, sigFile); err == nil {
		t.Error("VerifyAny akzeptiert die Fälschung in einer gemischten Liste")
	}
}

// TestVerifyRejectsInvalidPointsWithValidLookingSignature prüft Verify und
// VerifyAny für jeden schlechten Punkt mit einer beliebigen (hier echten,
// aber fremden) Signatur: nie Erfolg, nie Absturz.
func TestVerifyRejectsInvalidPointsWithValidLookingSignature(t *testing.T) {
	priv, _ := multikeyPair("haupt")
	manifest := multikeyManifest()
	sigFile := releasesig.Sign(priv, manifest)
	for _, tc := range pubkeyInvalidCases {
		key := ed25519.PublicKey(pubkeyMustDecode(t, tc.hex))
		if err := releasesig.Verify(key, manifest, sigFile); err == nil {
			t.Errorf("%s: Verify akzeptiert", tc.name)
		}
		if err := releasesig.VerifyAny([]ed25519.PublicKey{key}, manifest, sigFile); err == nil {
			t.Errorf("%s: VerifyAny akzeptiert", tc.name)
		}
	}
}

// TestValidPublicKeyIsFastEnough prüft, dass die Punktprüfung (math/big,
// kein Spezialcode) schnell genug für den Programmstart ist: 10 Aufrufe
// unter 500 ms (großzügig, gemessen sind es wenige Millisekunden).
func TestValidPublicKeyIsFastEnough(t *testing.T) {
	key := pubkeyMustDecode(t, releaseSigningPublicKeyHex)
	start := time.Now()
	for i := 0; i < 10; i++ {
		if !releasesig.ValidPublicKey(key) {
			t.Fatal("Hauptschlüssel abgelehnt")
		}
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("10 Aufrufe dauern %v (Grenze 500 ms)", elapsed)
	}
}
