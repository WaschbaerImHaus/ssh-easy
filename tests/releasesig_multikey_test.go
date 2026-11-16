// Paket main - Tests für mehrere vertrauenswürdige Signaturschlüssel
//
// Hintergrund: Der Updater prüfte die signierte Prüfsummenliste bisher mit
// genau einem eingebetteten Schlüssel. Geht dieser verloren, könnte keine
// installierte Version mehr ein Update prüfen. Deshalb gibt es jetzt eine
// Liste vertrauenswürdiger Schlüssel (Hauptschlüssel + Ersatzschlüssel):
// Gültig ist eine Signatur von IRGENDEINEM Schlüssel der Liste
// (releasesig.VerifyAny). Alle Schlüssel in diesen Tests sind Wegwerf-
// Schlüssel aus festen Test-Seeds - die echten Schlüssel werden nie benutzt.
//
// @author Kurt Ingwer
// @date   2026-11-12 20:22
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"ssh-easy/releasesig"
)

// multikeyPair erzeugt aus einer Bezeichnung einen festen Wegwerf-Schlüssel
// (Seed = SHA-256 der Bezeichnung, deshalb reproduzierbar).
func multikeyPair(label string) (ed25519.PrivateKey, ed25519.PublicKey) {
	seed := sha256.Sum256([]byte("ssh-easy-multikey-test:" + label))
	priv := ed25519.NewKeyFromSeed(seed[:])
	return priv, priv.Public().(ed25519.PublicKey)
}

// testBackupSigningKey ist der Wegwerf-Ersatzschlüssel für die Updater-Tests.
var testBackupSigningKey, testBackupSigningPub = multikeyPair("ersatz")

// multikeyManifest liefert eine kleine gültige Prüfsummenliste.
func multikeyManifest() []byte {
	return releasesig.BuildManifest("v0.90", map[string]string{"ssh-easy": hexSum([]byte("a"))})
}

// multikeyHex liefert den Hex-Text des öffentlichen Schlüssels.
func multikeyHex(pub ed25519.PublicKey) string {
	return hex.EncodeToString(pub)
}

// --- releasesig.VerifyAny ---

// TestVerifyAnyAcceptsMainKey prüft den Normalfall: Die Signatur stammt vom
// ersten Schlüssel der Liste (Hauptschlüssel).
func TestVerifyAnyAcceptsMainKey(t *testing.T) {
	mainPriv, mainPub := multikeyPair("haupt")
	_, backupPub := multikeyPair("ersatz")
	manifest := multikeyManifest()
	sig := releasesig.Sign(mainPriv, manifest)
	if err := releasesig.VerifyAny([]ed25519.PublicKey{mainPub, backupPub}, manifest, sig); err != nil {
		t.Errorf("Signatur des Hauptschlüssels abgelehnt: %v", err)
	}
}

// TestVerifyAnyAcceptsBackupKey prüft den Zweck der Änderung: Eine Signatur
// des Ersatzschlüssels (nicht der erste der Liste) gilt ebenfalls.
func TestVerifyAnyAcceptsBackupKey(t *testing.T) {
	_, mainPub := multikeyPair("haupt")
	backupPriv, backupPub := multikeyPair("ersatz")
	manifest := multikeyManifest()
	sig := releasesig.Sign(backupPriv, manifest)
	if err := releasesig.VerifyAny([]ed25519.PublicKey{mainPub, backupPub}, manifest, sig); err != nil {
		t.Errorf("Signatur des Ersatzschlüssels abgelehnt: %v", err)
	}
}

// TestVerifyAnyKeyOrderIrrelevant prüft dass die Reihenfolge der Liste für
// das Ergebnis keine Rolle spielt (letzter Schlüssel passt, auch bei vielen).
func TestVerifyAnyKeyOrderIrrelevant(t *testing.T) {
	var keys []ed25519.PublicKey
	for _, label := range []string{"a", "b", "c", "d", "e"} {
		_, pub := multikeyPair(label)
		keys = append(keys, pub)
	}
	lastPriv, lastPub := multikeyPair("letzter")
	keys = append(keys, lastPub)
	manifest := multikeyManifest()
	sig := releasesig.Sign(lastPriv, manifest)
	if err := releasesig.VerifyAny(keys, manifest, sig); err != nil {
		t.Errorf("Signatur des letzten Schlüssels abgelehnt: %v", err)
	}
}

// TestVerifyAnyRejectsForeignKey prüft dass die Signatur eines Schlüssels,
// der nicht in der Liste steht, abgelehnt wird (Angreifer ohne Schlüssel).
func TestVerifyAnyRejectsForeignKey(t *testing.T) {
	_, mainPub := multikeyPair("haupt")
	_, backupPub := multikeyPair("ersatz")
	evilPriv, _ := multikeyPair("angreifer")
	manifest := multikeyManifest()
	err := releasesig.VerifyAny([]ed25519.PublicKey{mainPub, backupPub}, manifest, releasesig.Sign(evilPriv, manifest))
	if err == nil {
		t.Fatal("Signatur eines fremden Schlüssels akzeptiert")
	}
	if !strings.Contains(err.Error(), "ungültig") {
		t.Errorf("Fehlertext %q enthält nicht \"ungültig\"", err.Error())
	}
}

// TestVerifyAnyRejectsBrokenSignature prüft kaputte Signaturdateien:
// Base64-Müll, falsche Länge, leer, nur Leerraum, Nullbytes.
func TestVerifyAnyRejectsBrokenSignature(t *testing.T) {
	_, mainPub := multikeyPair("haupt")
	_, backupPub := multikeyPair("ersatz")
	keys := []ed25519.PublicKey{mainPub, backupPub}
	manifest := multikeyManifest()
	unreadable := map[string][]byte{
		"leer":      nil,
		"nur Leerr": []byte(" \n\t"),
		"kein b64":  []byte("!!!kein-base64!!!"),
		"zu kurz":   []byte(base64.StdEncoding.EncodeToString([]byte("kurz"))),
		"zu lang":   []byte(base64.StdEncoding.EncodeToString(make([]byte, 65))),
		"eine Null": []byte(base64.StdEncoding.EncodeToString(make([]byte, 1))),
	}
	for name, bad := range unreadable {
		err := releasesig.VerifyAny(keys, manifest, bad)
		if err == nil {
			t.Errorf("%s: kaputte Signatur akzeptiert", name)
			continue
		}
		if !strings.Contains(err.Error(), "unlesbar") {
			t.Errorf("%s: Fehlertext %q enthält nicht \"unlesbar\"", name, err.Error())
		}
	}
	// Richtige Länge, aber nur Nullbytes: lesbar, doch ungültig
	zeros := []byte(base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)))
	err := releasesig.VerifyAny(keys, manifest, zeros)
	if err == nil {
		t.Error("Null-Signatur akzeptiert")
	} else if !strings.Contains(err.Error(), "ungültig") {
		t.Errorf("Null-Signatur: Fehlertext %q enthält nicht \"ungültig\"", err.Error())
	}
}

// TestVerifyAnyRejectsTamperedManifest prüft dass eine veränderte Liste auch
// bei einer sonst gültigen Signatur eines Listenschlüssels abgelehnt wird.
func TestVerifyAnyRejectsTamperedManifest(t *testing.T) {
	mainPriv, mainPub := multikeyPair("haupt")
	backupPriv, backupPub := multikeyPair("ersatz")
	keys := []ed25519.PublicKey{mainPub, backupPub}
	manifest := multikeyManifest()
	for name, priv := range map[string]ed25519.PrivateKey{"Haupt": mainPriv, "Ersatz": backupPriv} {
		sig := releasesig.Sign(priv, manifest)
		tampered := append([]byte(nil), manifest...)
		tampered[len(tampered)-2] ^= 1
		if releasesig.VerifyAny(keys, tampered, sig) == nil {
			t.Errorf("%sschlüssel: veränderte Liste akzeptiert", name)
		}
		// Angehängte Zeile
		extended := append(append([]byte(nil), manifest...), []byte(hexSum([]byte("x"))+"  evil\n")...)
		if releasesig.VerifyAny(keys, extended, sig) == nil {
			t.Errorf("%sschlüssel: erweiterte Liste akzeptiert", name)
		}
		// Leere Liste statt der signierten
		if releasesig.VerifyAny(keys, nil, sig) == nil {
			t.Errorf("%sschlüssel: leere Liste akzeptiert", name)
		}
	}
}

// TestVerifyAnyEmptyKeyList prüft dass ohne vertrauenswürdige Schlüssel nie
// etwas gilt (leere und nil-Liste) - auch nicht bei korrekter Signatur.
func TestVerifyAnyEmptyKeyList(t *testing.T) {
	priv, _ := multikeyPair("haupt")
	manifest := multikeyManifest()
	sig := releasesig.Sign(priv, manifest)
	if releasesig.VerifyAny(nil, manifest, sig) == nil {
		t.Error("nil-Liste akzeptiert")
	}
	if releasesig.VerifyAny([]ed25519.PublicKey{}, manifest, sig) == nil {
		t.Error("leere Liste akzeptiert")
	}
	// Auch bei kaputter Signatur: Fehler, kein Panic
	if releasesig.VerifyAny(nil, manifest, nil) == nil {
		t.Error("nil-Liste mit leerer Signatur akzeptiert")
	}
}

// TestVerifyAnyOnlyInvalidKeys prüft eine Liste, die nur aus unbrauchbaren
// Schlüsseln besteht (zu kurz, zu lang, leer, nil): kein Panic und nie ein
// Treffer - sonst würde ein kaputter Eintrag das Update freigeben.
func TestVerifyAnyOnlyInvalidKeys(t *testing.T) {
	priv, pub := multikeyPair("haupt")
	manifest := multikeyManifest()
	sig := releasesig.Sign(priv, manifest)
	lists := map[string][]ed25519.PublicKey{
		"zu kurz":        {ed25519.PublicKey([]byte("kurz"))},
		"31 Bytes":       {pub[:31]},
		"33 Bytes":       {append(append(ed25519.PublicKey(nil), pub...), 0)},
		"leerer Eintrag": {ed25519.PublicKey{}},
		"nil-Eintrag":    {nil},
		"alle gemischt":  {nil, ed25519.PublicKey{}, pub[:16], append(append(ed25519.PublicKey(nil), pub...), 7)},
	}
	for name, keys := range lists {
		if releasesig.VerifyAny(keys, manifest, sig) == nil {
			t.Errorf("%s: Liste ohne gültigen Schlüssel akzeptiert", name)
		}
	}
}

// TestVerifyAnySkipsInvalidKeyButAcceptsValid prüft eine gemischte Liste:
// Ungültige Einträge werden übersprungen, der gültige Schlüssel zählt.
// Der ungültige Eintrag steht vorn, in der Mitte und hinten.
func TestVerifyAnySkipsInvalidKeyButAcceptsValid(t *testing.T) {
	priv, pub := multikeyPair("haupt")
	manifest := multikeyManifest()
	sig := releasesig.Sign(priv, manifest)
	bad := ed25519.PublicKey([]byte("kaputt"))
	lists := map[string][]ed25519.PublicKey{
		"ungültig vorn":   {bad, pub},
		"ungültig mitten": {nil, pub, bad},
		"ungültig hinten": {pub, bad},
	}
	for name, keys := range lists {
		if err := releasesig.VerifyAny(keys, manifest, sig); err != nil {
			t.Errorf("%s: gültiger Schlüssel nicht erkannt: %v", name, err)
		}
	}
}

// TestVerifyAnyDomainSeparation prüft dass eine rohe Ed25519-Signatur der
// Liste (ohne Kontext-Präfix) abgelehnt wird - für jeden Schlüssel der Liste.
// Sonst ließe sich eine Signatur aus einem anderen Zweck wiederverwenden.
func TestVerifyAnyDomainSeparation(t *testing.T) {
	mainPriv, mainPub := multikeyPair("haupt")
	backupPriv, backupPub := multikeyPair("ersatz")
	keys := []ed25519.PublicKey{mainPub, backupPub}
	manifest := multikeyManifest()
	for name, priv := range map[string]ed25519.PrivateKey{"Haupt": mainPriv, "Ersatz": backupPriv} {
		raw := ed25519.Sign(priv, manifest)
		encoded := []byte(base64.StdEncoding.EncodeToString(raw))
		if releasesig.VerifyAny(keys, manifest, encoded) == nil {
			t.Errorf("%sschlüssel: Signatur ohne Kontext-Präfix akzeptiert", name)
		}
	}
}

// TestVerifyAnyAgreesWithVerify prüft die Vorgabe "verhält sich wie Verify":
// Bei einer Ein-Schlüssel-Liste stimmen Ergebnis (nil/Fehler) und Fehlertext
// mit Verify überein.
func TestVerifyAnyAgreesWithVerify(t *testing.T) {
	priv, pub := multikeyPair("haupt")
	evilPriv, _ := multikeyPair("angreifer")
	manifest := multikeyManifest()
	sigs := map[string][]byte{
		"gültig":  releasesig.Sign(priv, manifest),
		"fremd":   releasesig.Sign(evilPriv, manifest),
		"kaputt":  []byte("%%%"),
		"leer":    nil,
		"zu kurz": []byte(base64.StdEncoding.EncodeToString([]byte("kurz"))),
	}
	for name, sig := range sigs {
		want := releasesig.Verify(pub, manifest, sig)
		got := releasesig.VerifyAny([]ed25519.PublicKey{pub}, manifest, sig)
		if (want == nil) != (got == nil) {
			t.Errorf("%s: Verify=%v, VerifyAny=%v", name, want, got)
			continue
		}
		if want != nil && want.Error() != got.Error() {
			t.Errorf("%s: Fehlertext Verify %q, VerifyAny %q", name, want.Error(), got.Error())
		}
	}
}

// TestVerifyAnyDoesNotModifyKeys prüft dass die übergebene Liste unverändert
// bleibt (kein Sortieren oder Überschreiben der Schlüssel).
func TestVerifyAnyDoesNotModifyKeys(t *testing.T) {
	priv, pub := multikeyPair("haupt")
	_, other := multikeyPair("ersatz")
	keys := []ed25519.PublicKey{other, nil, pub}
	manifest := multikeyManifest()
	if err := releasesig.VerifyAny(keys, manifest, releasesig.Sign(priv, manifest)); err != nil {
		t.Fatal(err)
	}
	if !keys[0].Equal(other) || keys[1] != nil || !keys[2].Equal(pub) || len(keys) != 3 {
		t.Error("VerifyAny hat die Schlüsselliste verändert")
	}
}

// --- releasesig.ParsePublicKeysHex ---

// TestParsePublicKeysHexValid prüft den Normalfall: gültige Einträge werden
// dekodiert, die Reihenfolge bleibt erhalten.
func TestParsePublicKeysHexValid(t *testing.T) {
	_, a := multikeyPair("a")
	_, b := multikeyPair("b")
	_, c := multikeyPair("c")
	got := releasesig.ParsePublicKeysHex([]string{multikeyHex(c), multikeyHex(a), multikeyHex(b)})
	if len(got) != 3 || !got[0].Equal(c) || !got[1].Equal(a) || !got[2].Equal(b) {
		t.Errorf("Ergebnis/Reihenfolge falsch: %x", got)
	}
	for i, key := range got {
		if len(key) != ed25519.PublicKeySize {
			t.Errorf("Schlüssel %d hat %d Bytes", i, len(key))
		}
	}
}

// TestParsePublicKeysHexEmptyInput prüft nil und leere Liste: leeres
// Ergebnis ohne Panic.
func TestParsePublicKeysHexEmptyInput(t *testing.T) {
	if got := releasesig.ParsePublicKeysHex(nil); len(got) != 0 {
		t.Errorf("nil-Liste liefert %d Schlüssel", len(got))
	}
	if got := releasesig.ParsePublicKeysHex([]string{}); len(got) != 0 {
		t.Errorf("leere Liste liefert %d Schlüssel", len(got))
	}
}

// TestParsePublicKeysHexSkipsInvalid prüft dass fehlerhafte Einträge einzeln
// übersprungen werden (kein Abbruch der ganzen Liste): kein Hex, ungerade
// Länge, zu kurz, zu lang, leer, nur Leerraum, Steuerzeichen, Hex-Präfix.
func TestParsePublicKeysHexSkipsInvalid(t *testing.T) {
	_, good := multikeyPair("gut")
	goodHex := multikeyHex(good)
	invalid := map[string]string{
		"kein Hex":       strings.Repeat("zz", 32),
		"ungerade Länge": goodHex[:63],
		"zu kurz":        goodHex[:62],
		"zu lang":        goodHex + "00",
		"leer":           "",
		"nur Leerraum":   "   ",
		"Steuerzeichen":  goodHex[:60] + "\x00\x00",
		"Hex-Präfix":     "0x" + goodHex,
		"Hälfte":         goodHex[:32],
		"doppelt lang":   goodHex + goodHex,
	}
	for name, entry := range invalid {
		// Allein: leeres Ergebnis
		if got := releasesig.ParsePublicKeysHex([]string{entry}); len(got) != 0 {
			t.Errorf("%s: ungültiger Eintrag lieferte %d Schlüssel", name, len(got))
		}
		// Zwischen gültigen Einträgen: nur der gültige bleibt
		got := releasesig.ParsePublicKeysHex([]string{entry, goodHex})
		if len(got) != 1 || !got[0].Equal(good) {
			t.Errorf("%s: gültiger Nachbar nicht erhalten: %x", name, got)
		}
	}
}

// TestParsePublicKeysHexSkipsDuplicates prüft dass doppelte Schlüssel nur
// einmal vorkommen (der erste Treffer bleibt, die Reihenfolge bleibt).
func TestParsePublicKeysHexSkipsDuplicates(t *testing.T) {
	_, a := multikeyPair("a")
	_, b := multikeyPair("b")
	got := releasesig.ParsePublicKeysHex([]string{
		multikeyHex(a), multikeyHex(b), multikeyHex(a), multikeyHex(b), multikeyHex(a),
	})
	if len(got) != 2 || !got[0].Equal(a) || !got[1].Equal(b) {
		t.Errorf("Duplikate nicht entfernt: %x", got)
	}
}

// TestParsePublicKeysHexIgnoresCase prüft dass Groß-/Kleinschreibung des Hex
// egal ist - auch für die Duplikaterkennung ("AB.." und "ab.." sind
// derselbe Schlüssel).
func TestParsePublicKeysHexIgnoresCase(t *testing.T) {
	_, a := multikeyPair("a")
	lower := multikeyHex(a)
	upper := strings.ToUpper(lower)
	got := releasesig.ParsePublicKeysHex([]string{upper})
	if len(got) != 1 || !got[0].Equal(a) {
		t.Errorf("Großbuchstaben nicht dekodiert: %x", got)
	}
	// Gemischte Schreibweise
	mixed := upper[:32] + lower[32:]
	got = releasesig.ParsePublicKeysHex([]string{mixed})
	if len(got) != 1 || !got[0].Equal(a) {
		t.Errorf("gemischte Schreibweise nicht dekodiert: %x", got)
	}
	// Gleicher Schlüssel in beiden Schreibweisen zählt einmal
	got = releasesig.ParsePublicKeysHex([]string{lower, upper, mixed})
	if len(got) != 1 {
		t.Errorf("Duplikate in unterschiedlicher Schreibweise: %d Schlüssel", len(got))
	}
}

// TestParsePublicKeysHexResultVerifies prüft Ende-zu-Ende dass dekodierte
// Schlüssel tatsächlich zum Prüfen taugen (Hex -> Schlüssel -> VerifyAny).
func TestParsePublicKeysHexResultVerifies(t *testing.T) {
	_, mainPub := multikeyPair("haupt")
	backupPriv, backupPub := multikeyPair("ersatz")
	keys := releasesig.ParsePublicKeysHex([]string{multikeyHex(mainPub), "kaputt", strings.ToUpper(multikeyHex(backupPub))})
	manifest := multikeyManifest()
	if err := releasesig.VerifyAny(keys, manifest, releasesig.Sign(backupPriv, manifest)); err != nil {
		t.Errorf("aus Hex dekodierter Ersatzschlüssel prüft nicht: %v", err)
	}
}

// TestParsePublicKeysHexDoesNotAliasInput prüft dass das Ergebnis eigene
// Speicherbereiche hat: Der Aufrufer darf es verändern, ohne dass ein
// späterer Aufruf andere Schlüssel liefert.
func TestParsePublicKeysHexDoesNotAliasInput(t *testing.T) {
	_, a := multikeyPair("a")
	entries := []string{multikeyHex(a)}
	first := releasesig.ParsePublicKeysHex(entries)
	if len(first) != 1 {
		t.Fatalf("Ergebnis: %x", first)
	}
	first[0][0] ^= 0xff
	second := releasesig.ParsePublicKeysHex(entries)
	if len(second) != 1 || !second[0].Equal(a) {
		t.Error("zweiter Aufruf liefert veränderten Schlüssel")
	}
}

// --- Eingebettete Schlüssel im Programm ---

// TestReleaseSigningKeysList prüft die eingebettete Schlüsselliste: Der
// Hauptschlüssel steht an Position 0 und entspricht releaseSigningKey(),
// danach folgen genau die gültigen Ersatzschlüssel.
func TestReleaseSigningKeysList(t *testing.T) {
	keys := releaseSigningKeys()
	if want := 1 + len(releaseBackupSigningPublicKeysHex); len(keys) != want {
		t.Fatalf("releaseSigningKeys() liefert %d Schlüssel, erwartet %d", len(keys), want)
	}
	if !keys[0].Equal(releaseSigningKey()) {
		t.Error("Position 0 ist nicht der Hauptschlüssel")
	}
	if len(releaseBackupSigningPublicKeysHex) == 0 {
		t.Error("kein Ersatzschlüssel eingebettet")
	}
	// Ersatzschlüssel in der Reihenfolge der Hex-Liste
	for i, hexKey := range releaseBackupSigningPublicKeysHex {
		raw, err := hex.DecodeString(hexKey)
		if err != nil {
			t.Fatalf("Ersatzschlüssel %d ist kein Hex: %v", i, err)
		}
		if !keys[i+1].Equal(ed25519.PublicKey(raw)) {
			t.Errorf("Position %d ist nicht Ersatzschlüssel %d", i+1, i)
		}
	}
}

// TestReleaseSigningKeysValid prüft jeden eingebetteten Schlüssel: 32 Byte,
// kein Platzhalter aus Nullen, keine Duplikate, Ersatz ungleich Haupt.
func TestReleaseSigningKeysValid(t *testing.T) {
	keys := releaseSigningKeys()
	seen := map[string]int{}
	for i, key := range keys {
		if len(key) != ed25519.PublicKeySize {
			t.Errorf("Schlüssel %d hat %d Bytes", i, len(key))
		}
		if strings.Trim(hex.EncodeToString(key), "0") == "" {
			t.Errorf("Schlüssel %d ist ein Platzhalter (nur Nullen)", i)
		}
		if first, dup := seen[string(key)]; dup {
			t.Errorf("Schlüssel %d ist ein Duplikat von Schlüssel %d", i, first)
		}
		seen[string(key)] = i
	}
	mainKey := releaseSigningKey()
	for i, key := range keys[1:] {
		if key.Equal(mainKey) {
			t.Errorf("Ersatzschlüssel %d ist gleich dem Hauptschlüssel", i)
		}
	}
}

// TestReleaseBackupKeysHexWellFormed prüft den eingebetteten Hex-Text: Jeder
// Eintrag muss gültig sein (sonst würde er beim Start stillschweigend
// verschwinden und der Ersatzschlüssel fehlte im Ernstfall).
func TestReleaseBackupKeysHexWellFormed(t *testing.T) {
	for i, entry := range releaseBackupSigningPublicKeysHex {
		if len(entry) != ed25519.PublicKeySize*2 {
			t.Errorf("Ersatzschlüssel %d: %d Hex-Zeichen, erwartet %d", i, len(entry), ed25519.PublicKeySize*2)
		}
		if entry != strings.ToLower(entry) {
			t.Errorf("Ersatzschlüssel %d: Hex nicht in Kleinbuchstaben", i)
		}
	}
	parsed := releasesig.ParsePublicKeysHex(releaseBackupSigningPublicKeysHex)
	if len(parsed) != len(releaseBackupSigningPublicKeysHex) {
		t.Errorf("nur %d von %d Ersatzschlüsseln gültig", len(parsed), len(releaseBackupSigningPublicKeysHex))
	}
}

// TestReleaseSigningKeysFreshCopy prüft dass jeder Aufruf eine neue Liste
// mit neuen Schlüsselbytes liefert: Eine Änderung am Ergebnis darf den
// nächsten Aufruf nicht verfälschen.
func TestReleaseSigningKeysFreshCopy(t *testing.T) {
	first := releaseSigningKeys()
	want := append(ed25519.PublicKey(nil), first[0]...)
	first[0][0] ^= 0xff
	first[len(first)-1] = nil
	second := releaseSigningKeys()
	if len(second) != len(first) || !second[0].Equal(want) || len(second[len(second)-1]) != ed25519.PublicKeySize {
		t.Error("Änderung am Ergebnis wirkt auf den nächsten Aufruf")
	}
}

// TestNewUpdaterTrustsAllEmbeddedKeys prüft dass NewUpdater alle
// eingebetteten Schlüssel übernimmt (nicht nur den Hauptschlüssel).
func TestNewUpdaterTrustsAllEmbeddedKeys(t *testing.T) {
	got := NewUpdater("50").signingKeys
	want := releaseSigningKeys()
	if len(got) != len(want) {
		t.Fatalf("NewUpdater: %d Schlüssel, erwartet %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("NewUpdater: Schlüssel %d weicht ab", i)
		}
	}
}
