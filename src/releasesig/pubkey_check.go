// Paket releasesig - Prüfung öffentlicher Ed25519-Schlüssel
//
// crypto/ed25519 prüft beim Verifizieren nur, ob der öffentliche Schlüssel
// ein Punkt der Kurve ist. Ein Punkt kleiner Ordnung (z. B. das neutrale
// Element, Kodierung 0100…00) macht [k]A für jede Nachricht trivial; dann
// lässt sich eine "gültige" Signatur ohne privaten Schlüssel bauen.
// ValidPublicKey lässt deshalb nur Schlüssel zu, die
//   - kanonisch kodiert sind (y < p, kein "-0" für x = 0),
//   - auf der Kurve liegen (Wurzel nach RFC 8032, Abschnitt 5.1.3),
//   - nicht kleine Ordnung haben ([8]A ist nicht das neutrale Element) und
//   - in der Untergruppe der Primordnung L liegen ([L]A ist neutral; echte
//     Schlüssel A = [a]B erfüllen das immer, Mischpunkte A + T nicht).
//
// Ohne neue Abhängigkeit (filippo.io/edwards25519 steht nicht in go.sum):
// gerechnet wird mit math/big in affinen Koordinaten. Langsam, aber nur
// für die wenigen eingebetteten Schlüssel nötig (einmal je Update-Prüfung,
// wenige Millisekunden). Kein Geheimnis im Spiel, daher ist konstante
// Laufzeit unnötig.
//
// @author Kurt Ingwer
// @date   2026-11-15 09:09
package releasesig

import (
	"crypto/ed25519"
	"math/big"
)

// edPoint ist ein Punkt der Kurve -x² + y² = 1 + d·x²·y² (affin).
type edPoint struct {
	x, y *big.Int
}

// Kurvenkonstanten von Ed25519 (RFC 8032, Abschnitt 5.1)
var (
	// edP - Primzahl p = 2^255 - 19
	edP = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	// edD - d = -121665 / 121666 mod p
	edD = func() *big.Int {
		num := new(big.Int).Neg(big.NewInt(121665))
		inv := new(big.Int).ModInverse(big.NewInt(121666), edP)
		return num.Mul(num, inv).Mod(num, edP)
	}()
	// edL - Ordnung der Primordnungs-Untergruppe 2^252 + 27742317777372353535851937790883648493
	edL = func() *big.Int {
		l, _ := new(big.Int).SetString("27742317777372353535851937790883648493", 10)
		return l.Add(l, new(big.Int).Lsh(big.NewInt(1), 252))
	}()
	// edSqrtM1 - Wurzel aus -1: 2^((p-1)/4) mod p
	edSqrtM1 = new(big.Int).Exp(big.NewInt(2), new(big.Int).Rsh(new(big.Int).Sub(edP, big.NewInt(1)), 2), edP)
)

// ValidPublicKey prüft einen öffentlichen Ed25519-Schlüssel (siehe
// Dateikopf).
//
// @param key - 32 Bytes
// @return bool - true = kanonisch, auf der Kurve, Primordnung, nicht neutral
// @date   2026-11-15 09:09
func ValidPublicKey(key []byte) bool {
	if len(key) != ed25519.PublicKeySize {
		return false
	}
	point, ok := decodeEdPoint(key)
	if !ok {
		return false
	}
	// Kleine Ordnung (1, 2, 4, 8): [8]A neutral
	if edScalarMult(point, big.NewInt(8)).isIdentity() {
		return false
	}
	// Nicht in der Primordnungs-Untergruppe (Mischpunkt A + T)
	return edScalarMult(point, edL).isIdentity()
}

// decodeEdPoint dekodiert einen Punkt nach RFC 8032, Abschnitt 5.1.3, und
// lehnt nicht kanonische Kodierungen ab.
//
// @param key - 32 Bytes, little-endian
// @return edPoint - Punkt
// @return bool - false = keine gültige, kanonische Kodierung
// @date   2026-11-15 09:09
func decodeEdPoint(key []byte) (edPoint, bool) {
	// Little-endian -> big.Int; oberstes Bit ist das Vorzeichen von x
	be := make([]byte, len(key))
	for i, b := range key {
		be[len(key)-1-i] = b
	}
	sign := be[0] >> 7
	be[0] &= 0x7f
	y := new(big.Int).SetBytes(be)
	if y.Cmp(edP) >= 0 {
		// Nicht kanonisch (y >= p)
		return edPoint{}, false
	}
	// x² = (y² - 1) / (d·y² + 1)
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, edP)
	u := new(big.Int).Sub(y2, big.NewInt(1))
	u.Mod(u, edP)
	v := new(big.Int).Mul(edD, y2)
	v.Add(v, big.NewInt(1)).Mod(v, edP)
	// Kandidat x = u·v³·(u·v⁷)^((p-5)/8)
	v3 := new(big.Int).Exp(v, big.NewInt(3), edP)
	v7 := new(big.Int).Exp(v, big.NewInt(7), edP)
	uv7 := new(big.Int).Mul(u, v7)
	uv7.Mod(uv7, edP)
	exp := new(big.Int).Rsh(new(big.Int).Sub(edP, big.NewInt(5)), 3)
	x := new(big.Int).Exp(uv7, exp, edP)
	x.Mul(x, u).Mod(x, edP)
	x.Mul(x, v3).Mod(x, edP)
	// v·x² == u: passt; == -u: mit Wurzel aus -1 korrigieren; sonst keine Wurzel
	vx2 := new(big.Int).Mul(x, x)
	vx2.Mul(vx2, v).Mod(vx2, edP)
	negU := new(big.Int).Sub(edP, u)
	negU.Mod(negU, edP)
	switch {
	case vx2.Cmp(u) == 0:
	case vx2.Cmp(negU) == 0:
		x.Mul(x, edSqrtM1).Mod(x, edP)
	default:
		return edPoint{}, false
	}
	if x.Sign() == 0 && sign == 1 {
		// "-0" ist nicht kanonisch
		return edPoint{}, false
	}
	if uint(x.Bit(0)) != uint(sign) {
		x.Sub(edP, x)
	}
	return edPoint{x: x, y: y}, true
}

// isIdentity meldet, ob der Punkt das neutrale Element (0, 1) ist.
//
// @return bool - true = neutral
// @date   2026-11-15 09:09
func (p edPoint) isIdentity() bool {
	return p.x.Sign() == 0 && p.y.Cmp(big.NewInt(1)) == 0
}

// edAdd addiert zwei Punkte (vollständige Formel für a = -1; die Nenner
// sind für Punkte der Kurve nie 0, weil d kein Quadrat ist).
//
// @param a - erster Punkt
// @param b - zweiter Punkt
// @return edPoint - Summe
// @date   2026-11-15 09:09
func edAdd(a, b edPoint) edPoint {
	x1y2 := new(big.Int).Mul(a.x, b.y)
	y1x2 := new(big.Int).Mul(a.y, b.x)
	y1y2 := new(big.Int).Mul(a.y, b.y)
	x1x2 := new(big.Int).Mul(a.x, b.x)
	t := new(big.Int).Mul(x1x2, y1y2)
	t.Mul(t, edD).Mod(t, edP)
	// x3 = (x1·y2 + y1·x2) / (1 + d·x1·x2·y1·y2)
	numX := new(big.Int).Add(x1y2, y1x2)
	denX := new(big.Int).Add(big.NewInt(1), t)
	denX.Mod(denX, edP)
	// y3 = (y1·y2 + x1·x2) / (1 - d·x1·x2·y1·y2)
	numY := new(big.Int).Add(y1y2, x1x2)
	denY := new(big.Int).Sub(big.NewInt(1), t)
	denY.Mod(denY, edP)
	x3 := numX.Mul(numX, new(big.Int).ModInverse(denX, edP))
	x3.Mod(x3, edP)
	y3 := numY.Mul(numY, new(big.Int).ModInverse(denY, edP))
	y3.Mod(y3, edP)
	return edPoint{x: x3, y: y3}
}

// edScalarMult berechnet [k]P (Verdoppeln und Addieren).
//
// @param p - Punkt
// @param k - Faktor (>= 0)
// @return edPoint - Ergebnis
// @date   2026-11-15 09:09
func edScalarMult(p edPoint, k *big.Int) edPoint {
	result := edPoint{x: big.NewInt(0), y: big.NewInt(1)}
	for i := k.BitLen() - 1; i >= 0; i-- {
		result = edAdd(result, result)
		if k.Bit(i) == 1 {
			result = edAdd(result, p)
		}
	}
	return result
}
