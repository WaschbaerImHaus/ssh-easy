// Programm release-sign - signiert die Release-Dateien von ssh-easy
//
// Aufruf aus build.sh:
//
//	release-sign -key <schlüssel> -tag v0.51 -out ../build <dateien...>
//
// Einmalig einen Schlüssel erzeugen (gibt den öffentlichen Schlüssel aus,
// der in src/updater.go als releaseSigningPublicKeyHex eingetragen wird):
//
//	release-sign -genkey -key ~/.config/ssh-easy/release-signing.key
//
// @author Kurt Ingwer
// @date   2026-10-17 22:40
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"ssh-easy/releasesig"
)

// main wertet die Argumente aus und signiert bzw. erzeugt den Schlüssel.
//
// @date   2026-10-17 22:40
func main() {
	keyPath := flag.String("key", "", "Datei des privaten Signaturschlüssels")
	tag := flag.String("tag", "", "Release-Tag, z.B. v0.51")
	outDir := flag.String("out", ".", "Zielordner für Prüfsummenliste und Signatur")
	genKey := flag.Bool("genkey", false, "neuen Schlüssel erzeugen (überschreibt nie)")
	flag.Parse()

	if *keyPath == "" {
		fail("-key fehlt")
	}
	if *genKey {
		pub, err := releasesig.GenerateKey(*keyPath)
		if err != nil {
			fail(err.Error())
		}
		fmt.Println(hex.EncodeToString(pub))
		return
	}
	if *tag == "" || flag.NArg() == 0 {
		fail("-tag und mindestens eine Datei sind nötig")
	}
	priv, err := releasesig.LoadPrivateKey(*keyPath)
	if err != nil {
		fail(err.Error())
	}
	if err := releasesig.SignFiles(priv, *tag, *outDir, flag.Args()); err != nil {
		fail(err.Error())
	}
	fmt.Printf("signiert: %d Dateien für %s\n", flag.NArg(), *tag)
}

// fail gibt eine Fehlermeldung aus und beendet mit Code 1.
//
// @param msg - Fehlermeldung
// @date   2026-10-17 22:40
func fail(msg string) {
	fmt.Fprintln(os.Stderr, "release-sign:", msg)
	os.Exit(1)
}
