// Paket main - Tests für geteilte automatische SSH-Schlüssel, Build 86
//
// Hintergrund: Fable-Review von Build 85. AutoDeployKey benennt den Schlüssel
// id_ed25519_<host>_<user>. Eine Kopie mit gleichem Host und Benutzer bekam
// dieselbe Datei; wer den Schlüssel der Kopie entfernte, löschte die Datei,
// die die Quelle noch nutzt.
//   - B1: autoKeyName() liefert einen noch freien Namen (_2, _3 ...).
//   - B2: connectionsSharingKey() findet andere Verbindungen mit gleichem
//     Schlüssel; RemoveDeployedKey() lässt dann die lokalen Dateien stehen.
//
// @author Kurt Ingwer
// @date   2026-10-28 13:09
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// setTestHome setzt das Home-Verzeichnis (Linux: HOME, Windows: USERPROFILE)
// auf ein Temp-Verzeichnis, damit ~/.ssh nie das echte Verzeichnis trifft.
//
// @param t - Testkontext
// @return string - Pfad des Temp-Homes
// @date 2026-10-27 17:59
func setTestHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

// keyConn erstellt eine Verbindung mit ID und Schlüsselpfad.
//
// @date 2026-10-28 13:09
func keyConn(id, host, user, keyPath string) Connection {
	conn := NewConnection(id, host, 22, user, AuthKey)
	conn.ID = id
	conn.KeyPath = keyPath
	return conn
}

// --- B1: eindeutiger Schlüsselname ---

// TestAutoKeyName prüft die Namenswahl: Grundname wie bisher, bei Belegung
// durch eine ANDERE Verbindung _2, _3 ...; Tilde und absoluter Pfad gelten
// als gleich; die eigene Verbindung blockiert nicht (sonst bekäme jeder
// zweite AutoDeployKey-Lauf einen neuen Namen).
//
// @date 2026-10-27 17:59
func TestAutoKeyName(t *testing.T) {
	home := setTestHome(t)
	base := "id_ed25519_srv.example.org_admin"
	conn := keyConn("me", "srv.example.org", "admin", "")
	abs := func(name string) string { return filepath.Join(home, ".ssh", name) }
	cases := []struct {
		label string
		conns []Connection
		want  string
	}{
		{"keine anderen Verbindungen", nil, base},
		{"nur die eigene (ohne Schlüssel)", []Connection{conn}, base},
		{"anderer Schlüsselname", []Connection{keyConn("o1", "x", "y", "~/.ssh/id_other")}, base},
		{"andere ohne Schlüssel", []Connection{keyConn("o1", "srv.example.org", "admin", "")}, base},
		{"andere nutzt Grundname (Tilde)", []Connection{keyConn("o1", "a", "b", "~/.ssh/"+base)}, base + "_2"},
		{"andere nutzt Grundname (absolut)", []Connection{keyConn("o1", "a", "b", abs(base))}, base + "_2"},
		{"eigene ID hat Grundname", []Connection{keyConn("me", "srv.example.org", "admin", "~/.ssh/"+base)}, base},
		{"eigene hat Grundname, andere _2", []Connection{
			keyConn("me", "a", "b", "~/.ssh/"+base),
			keyConn("o1", "a", "b", "~/.ssh/"+base+"_2")}, base},
		{"Grundname und _2 belegt", []Connection{
			keyConn("o1", "a", "b", "~/.ssh/"+base),
			keyConn("o2", "a", "b", abs(base+"_2"))}, base + "_3"},
		{"_2 belegt, Grundname frei", []Connection{
			keyConn("o1", "a", "b", "~/.ssh/"+base+"_2")}, base},
	}
	for _, c := range cases {
		if got := autoKeyName(conn, c.conns); got != c.want {
			t.Errorf("%s: autoKeyName = %q, erwartet %q", c.label, got, c.want)
		}
	}
}

// TestAutoKeyNameSanitizesHostAndUser prüft, dass der Grundname weiterhin
// über sanitizeFilename gebildet wird: Pfadtrenner, Leerzeichen und ".."
// aus Host und Benutzer dürfen nie aus ~/.ssh herausführen.
//
// @date 2026-10-27 17:59
func TestAutoKeyNameSanitizesHostAndUser(t *testing.T) {
	setTestHome(t)
	conn := keyConn("me", "a/b c", "../x", "")
	want := "id_ed25519_" + sanitizeFilename("a/b c") + "_" + sanitizeFilename("../x")
	got := autoKeyName(conn, nil)
	if got != want {
		t.Errorf("autoKeyName = %q, erwartet %q", got, want)
	}
	if filepath.Base(got) != got {
		t.Errorf("Name %q enthält einen Pfadtrenner", got)
	}
}

// --- B2: geteilte Schlüssel ---

// TestConnectionsSharingKey prüft die Suche nach anderen Verbindungen mit
// demselben Schlüssel: Vergleich nach expandTilde (Tilde = absolut), die
// eigene ID zählt nicht, leerer KeyPath ergibt nil (sonst teilten alle
// Verbindungen ohne Schlüssel "den" leeren Schlüssel).
//
// @date 2026-10-27 17:59
func TestConnectionsSharingKey(t *testing.T) {
	home := setTestHome(t)
	abs := filepath.Join(home, ".ssh", "id_test")
	ids := func(conns []Connection) []string {
		var out []string
		for _, c := range conns {
			out = append(out, c.ID)
		}
		return out
	}
	cases := []struct {
		label string
		conn  Connection
		conns []Connection
		want  []string
	}{
		{"leerer KeyPath", keyConn("me", "h", "u", ""), []Connection{keyConn("o1", "h", "u", "")}, nil},
		{"keine anderen", keyConn("me", "h", "u", "~/.ssh/id_test"), []Connection{keyConn("me", "h", "u", "~/.ssh/id_test")}, nil},
		{"anderer Schlüssel", keyConn("me", "h", "u", "~/.ssh/id_test"), []Connection{keyConn("o1", "h", "u", "~/.ssh/id_other")}, nil},
		{"andere ohne Schlüssel", keyConn("me", "h", "u", "~/.ssh/id_test"), []Connection{keyConn("o1", "h", "u", "")}, nil},
		{"gleich (Tilde)", keyConn("me", "h", "u", "~/.ssh/id_test"), []Connection{keyConn("o1", "h", "u", "~/.ssh/id_test")}, []string{"o1"}},
		{"Tilde gegen absolut", keyConn("me", "h", "u", "~/.ssh/id_test"), []Connection{keyConn("o1", "h", "u", abs)}, []string{"o1"}},
		{"absolut gegen Tilde", keyConn("me", "h", "u", abs), []Connection{keyConn("o1", "h", "u", "~/.ssh/id_test")}, []string{"o1"}},
		{"mehrere, eigene ausgenommen", keyConn("me", "h", "u", "~/.ssh/id_test"), []Connection{
			keyConn("o1", "h", "u", "~/.ssh/id_test"),
			keyConn("me", "h", "u", "~/.ssh/id_test"),
			keyConn("o2", "h", "u", abs),
			keyConn("o3", "h", "u", "~/.ssh/id_other")}, []string{"o1", "o2"}},
	}
	for _, c := range cases {
		got := connectionsSharingKey(c.conn, c.conns)
		if len(c.want) == 0 {
			if got != nil {
				t.Errorf("%s: %v, erwartet nil", c.label, ids(got))
			}
			continue
		}
		if !reflect.DeepEqual(ids(got), c.want) {
			t.Errorf("%s: %v, erwartet %v", c.label, ids(got), c.want)
		}
	}
}

// writeTestKeyPair legt ~/.ssh/id_test und id_test.pub im Temp-Home an
// (die .pub im gültigen Format "Typ Base64 Kommentar") und liefert beide Pfade.
//
// @param t - Testkontext
// @param home - Temp-Home
// @return string - Pfad des privaten Schlüssels
// @return string - Pfad des öffentlichen Schlüssels
// @date 2026-10-27 17:59
func writeTestKeyPair(t *testing.T, home string) (string, string) {
	t.Helper()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	priv := filepath.Join(dir, "id_test")
	pub := priv + ".pub"
	if err := os.WriteFile(priv, []byte("PRIVATE-TEST-KEY\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(pub, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItestkey kommentar\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return priv, pub
}

// writeKeyConfig speichert eine Konfiguration mit den angegebenen
// Verbindungen und liefert den Pfad.
//
// @date 2026-10-28 13:09
func writeKeyConfig(t *testing.T, conns ...Connection) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "connections.json")
	cfg := NewAppConfig()
	cfg.Connections = conns
	if err := SaveConfig(cfgPath, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return cfgPath
}

// findConn sucht eine Verbindung in der gespeicherten Konfiguration.
//
// @date 2026-10-28 13:09
func findConn(t *testing.T, cfgPath, id string) Connection {
	t.Helper()
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	for _, c := range cfg.Connections {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("Verbindung %q nicht in der Konfiguration", id)
	return Connection{}
}

// TestRemoveDeployedKeyKeepsSharedFiles prüft den Kern des Fixes: Nutzt noch
// eine andere Verbindung denselben Schlüssel, bleiben Private- und
// Public-Key-Datei liegen; nur KeyPath und AuthType der entfernenden
// Verbindung werden zurückgesetzt, die Quelle bleibt unverändert.
// Läuft mit client=nil (nur lokal). Die zweite Verbindung nutzt den absoluten
// Pfad, die Kopie die Tilde-Form - beide müssen als gleich gelten.
//
// @date 2026-10-27 17:59
func TestRemoveDeployedKeyKeepsSharedFiles(t *testing.T) {
	home := setTestHome(t)
	priv, pub := writeTestKeyPair(t, home)
	src := keyConn("src-id", "srv.example.org", "admin", priv)
	cpy := keyConn("copy-id", "srv.example.org", "admin", "~/.ssh/id_test")
	cfgPath := writeKeyConfig(t, src, cpy)

	if err := RemoveDeployedKey(cpy, nil, cfgPath); err != nil {
		t.Fatalf("RemoveDeployedKey: %v", err)
	}
	for _, p := range []string{priv, pub} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("Datei %s fehlt, obwohl die Quelle den Schlüssel noch nutzt: %v", p, err)
		}
	}
	gotCopy := findConn(t, cfgPath, "copy-id")
	if gotCopy.KeyPath != "" || gotCopy.AuthType == AuthKey {
		t.Errorf("Kopie nicht zurückgesetzt: KeyPath=%q AuthType=%q", gotCopy.KeyPath, gotCopy.AuthType)
	}
	gotSrc := findConn(t, cfgPath, "src-id")
	if gotSrc.KeyPath != priv || gotSrc.AuthType != AuthKey {
		t.Errorf("Quelle verändert: KeyPath=%q AuthType=%q", gotSrc.KeyPath, gotSrc.AuthType)
	}
}

// TestRemoveDeployedKeyDeletesUnsharedFiles ist die Gegenprobe: Ohne zweite
// Verbindung mit demselben Schlüssel werden beide Dateien wie bisher gelöscht
// und die Verbindung zurückgesetzt. Eine andere Verbindung mit ANDEREM
// Schlüssel ändert daran nichts.
//
// @date 2026-10-27 17:59
func TestRemoveDeployedKeyDeletesUnsharedFiles(t *testing.T) {
	home := setTestHome(t)
	priv, pub := writeTestKeyPair(t, home)
	mine := keyConn("copy-id", "srv.example.org", "admin", "~/.ssh/id_test")
	other := keyConn("src-id", "other.example.org", "admin", "~/.ssh/id_other")
	cfgPath := writeKeyConfig(t, mine, other)

	if err := RemoveDeployedKey(mine, nil, cfgPath); err != nil {
		t.Fatalf("RemoveDeployedKey: %v", err)
	}
	for _, p := range []string{priv, pub} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("Datei %s müsste gelöscht sein (Fehler: %v)", p, err)
		}
	}
	got := findConn(t, cfgPath, "copy-id")
	if got.KeyPath != "" || got.AuthType == AuthKey {
		t.Errorf("Verbindung nicht zurückgesetzt: KeyPath=%q AuthType=%q", got.KeyPath, got.AuthType)
	}
	if o := findConn(t, cfgPath, "src-id"); o.KeyPath != "~/.ssh/id_other" || o.AuthType != AuthKey {
		t.Errorf("fremde Verbindung verändert: %+v", o)
	}
}

// --- Nachträge Build 86: Server-Eintrag teilen, defekte Konfiguration ---

// TestSharesServerEntry prüft, wann der Server-Eintrag in authorized_keys
// stehen bleiben muss: gleicher Host (Groß-/Kleinschreibung egal) UND gleicher
// Benutzer. Der Port zählt nicht (zweiter sshd, gleiche authorized_keys);
// anderer Benutzer oder anderer Host dürfen den Eintrag NICHT schützen,
// sonst bliebe ein Schlüssel auf einem Server zurück, den keine Verbindung
// mehr nutzt. Leere Liste ergibt false.
//
// @date 2026-10-27 18:30
func TestSharesServerEntry(t *testing.T) {
	mk := func(host string, port int, user string) Connection {
		c := NewConnection("n", host, port, user, AuthKey)
		c.KeyPath = "~/.ssh/id_test"
		return c
	}
	conn := mk("srv.example.org", 22, "admin")
	cases := []struct {
		label  string
		others []Connection
		want   bool
	}{
		{"keine anderen (nil)", nil, false},
		{"leere Liste", []Connection{}, false},
		{"gleicher Host, Benutzer und Port", []Connection{mk("srv.example.org", 22, "admin")}, true},
		{"anderer Port bleibt geteilt", []Connection{mk("srv.example.org", 2222, "admin")}, true},
		{"anderer Benutzer nicht geteilt", []Connection{mk("srv.example.org", 22, "root")}, false},
		{"anderer Host nicht geteilt", []Connection{mk("other.example.org", 22, "admin")}, false},
		{"Host ohne Groß-/Kleinschreibung gleich", []Connection{mk("SRV.Example.ORG", 22, "admin")}, true},
		{"Benutzer ist groß-/kleinschreibungsabhängig", []Connection{mk("srv.example.org", 22, "Admin")}, false},
		{"einer von mehreren passt", []Connection{
			mk("other.example.org", 22, "admin"),
			mk("srv.example.org", 22, "root"),
			mk("srv.example.org", 2200, "admin")}, true},
		{"keiner von mehreren passt", []Connection{
			mk("other.example.org", 22, "admin"),
			mk("srv.example.org", 22, "root")}, false},
	}
	for _, c := range cases {
		if got := sharesServerEntry(conn, c.others); got != c.want {
			t.Errorf("%s: sharesServerEntry = %v, erwartet %v", c.label, got, c.want)
		}
	}
}

// TestRemoveDeployedKeyBrokenConfigKeepsFiles prüft den Abbruch bei defekter
// Konfiguration: Ohne lesbare Liste ist unbekannt, ob eine andere Verbindung
// den Schlüssel nutzt. Darum Fehler zurückgeben und BEIDE Schlüsseldateien
// liegen lassen (sonst könnte eine fremde Schlüsseldatei verloren gehen).
// Die defekte Konfiguration bleibt unverändert (kein stilles Überschreiben).
//
// @date 2026-10-27 18:30
func TestRemoveDeployedKeyBrokenConfigKeepsFiles(t *testing.T) {
	home := setTestHome(t)
	priv, pub := writeTestKeyPair(t, home)
	mine := keyConn("copy-id", "srv.example.org", "admin", "~/.ssh/id_test")
	cfgPath := filepath.Join(t.TempDir(), "connections.json")
	broken := []byte("{ das ist kein JSON ")
	if err := os.WriteFile(cfgPath, broken, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := RemoveDeployedKey(mine, nil, cfgPath); err == nil {
		t.Fatal("RemoveDeployedKey liefert keinen Fehler bei defekter Konfiguration")
	}
	for _, p := range []string{priv, pub} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("Datei %s fehlt nach Abbruch: %v", p, err)
		}
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil || string(got) != string(broken) {
		t.Errorf("defekte Konfiguration wurde verändert: %q (Fehler: %v)", got, err)
	}
}
