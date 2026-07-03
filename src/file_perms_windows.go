// Paket main - Datei-Berechtigungen (Windows-Variante)
//
// Setzt restriktive DACLs auf Windows-Dateien. Nur der Besitzer (aktueller
// User) behält Zugriff; Administratoren-Gruppe, SYSTEM und Vererbung vom
// übergeordneten Ordner werden explizit entfernt. Pendant zu chmod 0600
// auf Unix – ohne diesen Aufruf ignoriert Windows das 0600-FileMode-Flag
// komplett und sensible Dateien wären für andere Konten lesbar.
//
// Benötigt keinen CGO – nutzt ausschließlich golang.org/x/sys/windows
// welches direkt gegen die Win32-API per Syscall aufruft.
//
// @author Kurt Ingwer
// @date   2026-04-19 12:00

//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// restrictFilePermissions setzt eine DACL die nur dem aktuellen User Vollzugriff
// gewährt und verhindert Vererbung von der Ordner-DACL.
//
// Ablauf:
//  1. SID des aktuellen Prozess-Users ermitteln
//  2. EXPLICIT_ACCESS-Eintrag bauen: User = GENERIC_ALL, keine Vererbung
//  3. DACL aus dem Eintrag erzeugen
//  4. DACL via SetNamedSecurityInfo auf Datei anwenden
//     + PROTECTED_DACL_SECURITY_INFORMATION unterbindet Vererbung
//
// @param path - Pfad zur Datei (UTF-8)
// @return error - Fehler bei API-Aufrufen
// @date   2026-04-19 12:00
func restrictFilePermissions(path string) error {
	// Aktuellen Prozess-Token lesen (Pseudo-Handle, kein Close nötig)
	token := windows.GetCurrentProcessToken()
	tokenUser, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("TokenUser konnte nicht ermittelt werden: %w", err)
	}

	// Ein einziger ACL-Eintrag: nur der aktuelle User bekommt Vollzugriff.
	// GENERIC_ALL entspricht dem Unix rwx für den Besitzer.
	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(tokenUser.User.Sid),
		},
	}}

	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return fmt.Errorf("DACL konnte nicht erstellt werden: %w", err)
	}

	// DACL auf die Datei anwenden.
	// PROTECTED_DACL_SECURITY_INFORMATION blockiert geerbte Rechte –
	// ohne das würden vom Home-Verzeichnis geerbte Rechte weiterhin gelten.
	secInfo := windows.SECURITY_INFORMATION(
		windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		secInfo,
		nil, // Owner nicht ändern
		nil, // Group nicht ändern
		acl,
		nil, // SACL nicht ändern
	); err != nil {
		return fmt.Errorf("SetNamedSecurityInfo fehlgeschlagen: %w", err)
	}
	return nil
}
