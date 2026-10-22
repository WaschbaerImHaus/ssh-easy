// Paket main - Datei-Manager (MC-Modus): Fachlogik eines Dateifensters
//
// Ein Dateifenster (filePanel) zeigt das Verzeichnis einer entfernten
// Verbindung. Diese Datei enthält nur reine Logik ohne Bubbletea:
// Verzeichnis lesen und sortieren, Cursor bewegen, entscheiden was Enter
// bewirkt. Der Zugriff auf den Server läuft über das Interface remoteFS,
// damit die Logik ohne Netzwerk testbar ist.
//
// Entfernte Pfade sind immer POSIX-Pfade ("/" als Trenner) - deshalb wird
// hier das Paket "path" verwendet und NICHT "path/filepath" (das würde unter
// Windows "\" einsetzen).
//
// @author Kurt Ingwer
// @date   2026-10-21 21:16
package main

import (
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// remoteFS ist der Teil eines entfernten Dateisystems, den der
// Datei-Manager braucht. Die Implementierung sftpFS (filemanager_remote.go)
// leitet an *sftp.Client weiter und schließt beim Close() auch die
// SSH-Verbindung.
type remoteFS interface {
	// Getwd liefert das Startverzeichnis (bei SFTP: Home des Users)
	Getwd() (string, error)
	// ReadDir liest ein Verzeichnis; Symlinks werden NICHT aufgelöst
	ReadDir(p string) ([]os.FileInfo, error)
	// Stat liefert Infos zum Ziel eines Pfads (folgt Symlinks)
	Stat(p string) (os.FileInfo, error)
	// Lstat liefert Infos zum Pfad selbst (folgt Symlinks NICHT)
	Lstat(p string) (os.FileInfo, error)
	// ReadLink liefert das Ziel eines Symlinks
	ReadLink(p string) (string, error)
	// RealPath liefert den kanonischen Pfad (alle Symlinks im Pfad vom
	// Server aufgelöst; SFTP "realpath")
	RealPath(p string) (string, error)
	// OpenRead öffnet eine Datei zum Lesen
	OpenRead(p string) (io.ReadCloser, error)
	// CreateExclusive legt eine NEUE Datei an; existiert der Pfad schon
	// (auch als Symlink), schlägt es fehl - so wird nie durch einen
	// untergeschobenen Symlink hindurch geschrieben
	CreateExclusive(p string) (io.WriteCloser, error)
	// RenameOverwrite benennt um und ersetzt ein vorhandenes Ziel (Datei
	// oder Symlink - der Link selbst wird ersetzt, nicht sein Ziel)
	RenameOverwrite(oldPath, newPath string) error
	// Mkdir legt ein Verzeichnis an
	Mkdir(p string) error
	// Remove löscht eine Datei, einen Symlink oder ein leeres Verzeichnis
	Remove(p string) error
	// Symlink legt unter linkPath einen Symlink auf target an
	Symlink(target, linkPath string) error
	// Chmod setzt die Zugriffsrechte
	Chmod(p string, mode os.FileMode) error
	// Chtimes setzt Zugriffs- und Änderungszeit
	Chtimes(p string, atime, mtime time.Time) error
	// Close beendet die Verbindung
	Close() error
}

// fmEntry ist ein Eintrag eines Dateifensters.
type fmEntry struct {
	// Dateiname (unverändert, wie vom Server geliefert)
	name string
	// Größe in Bytes
	size int64
	// Letzte Änderung
	modTime time.Time
	// Verzeichnis (bei Symlinks: Ziel ist ein Verzeichnis)
	dir bool
	// Symbolischer Link
	symlink bool
	// Symlink mit nicht erreichbarem Ziel
	broken bool
	// Eintrag ".." (zum Elternverzeichnis)
	parent bool
	// Vom Nutzer markiert (Einfg / *); ".." ist nie markiert
	marked bool
}

// filePanel ist ein Dateifenster (linke oder rechte Seite).
type filePanel struct {
	// Verbindung, deren Dateisystem angezeigt wird
	conn Connection
	// Zugriff auf das entfernte Dateisystem (nil = nicht verbunden)
	fs remoteFS
	// Aktuelles Verzeichnis (absolut, POSIX)
	path string
	// Einträge des aktuellen Verzeichnisses, sortiert
	entries []fmEntry
	// Cursor-Position in entries
	cursor int
	// Erste sichtbare Zeile
	offset int
	// Läuft gerade ein Verzeichniswechsel?
	loading bool
}

// fmEnterAction beschreibt, was Enter auf einem Eintrag bewirkt.
type fmEnterAction int

const (
	// fmEnterNone - nichts tun (Datei; Ansehen kommt später mit F3)
	fmEnterNone fmEnterAction = iota
	// fmEnterChangeDir - in ein anderes Verzeichnis wechseln
	fmEnterChangeDir
	// fmEnterSymlinkDir - Symlink auf Verzeichnis: wird nicht verfolgt
	fmEnterSymlinkDir
)

// readPanelDir liest ein entferntes Verzeichnis und bereitet die Einträge
// für die Anzeige auf: Symlinks werden per Stat eingeordnet (zeigt das Ziel
// auf ein Verzeichnis?), außer im Wurzelverzeichnis steht ".." vorne.
//
// @param fsys - Entferntes Dateisystem
// @param dir - Absoluter Verzeichnispfad
// @return []fmEntry - Sortierte Einträge
// @return error - Lesefehler (z. B. os.ErrPermission)
// @date   2026-10-21 19:45
func readPanelDir(fsys remoteFS, dir string) ([]fmEntry, error) {
	infos, err := fsys.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]fmEntry, 0, len(infos)+1)
	if dir != "/" {
		entries = append(entries, fmEntry{name: "..", dir: true, parent: true})
	}
	for _, info := range infos {
		// "." und ".." liefert SFTP je nach Server mit - selbst erzeugen.
		// Namen mit "/" kann es in einem echten Verzeichnis nicht geben; ein
		// bösartiger Server könnte damit Pfade außerhalb erreichen -> weglassen
		if !safeEntryName(info.Name()) {
			continue
		}
		entry := fmEntry{
			name:    info.Name(),
			size:    info.Size(),
			modTime: info.ModTime(),
			dir:     info.IsDir(),
			symlink: info.Mode()&os.ModeSymlink != 0,
		}
		if entry.symlink {
			// Ziel ansehen (nur Metadaten, es wird nichts geöffnet)
			target, err := fsys.Stat(path.Join(dir, entry.name))
			if err != nil {
				entry.broken = true
			} else {
				entry.dir = target.IsDir()
			}
		}
		entries = append(entries, entry)
	}
	sortFMEntries(entries)
	return entries, nil
}

// sortFMEntries sortiert wie der mc: ".." zuerst, dann Verzeichnisse (inkl.
// Symlinks auf Verzeichnisse), dann Dateien - jeweils alphabetisch ohne
// Beachtung der Groß-/Kleinschreibung.
//
// @param entries - Zu sortierende Einträge (wird verändert)
// @date   2026-10-21 19:45
func sortFMEntries(entries []fmEntry) {
	rank := func(e fmEntry) int {
		switch {
		case e.parent:
			return 0
		case e.dir:
			return 1
		}
		return 2
	}
	sort.SliceStable(entries, func(i, j int) bool {
		ri, rj := rank(entries[i]), rank(entries[j])
		if ri != rj {
			return ri < rj
		}
		li, lj := strings.ToLower(entries[i].name), strings.ToLower(entries[j].name)
		if li != lj {
			return li < lj
		}
		return entries[i].name < entries[j].name
	})
}

// remoteParentDir liefert das Elternverzeichnis eines entfernten Pfads.
// "/" bleibt "/"; ein leerer Pfad gilt als "/".
//
// @param p - Absoluter POSIX-Pfad
// @return string - Elternverzeichnis
// @date   2026-10-21 19:45
func remoteParentDir(p string) string {
	if p == "" {
		return "/"
	}
	return path.Dir(path.Clean(p))
}

// current liefert den Eintrag unter dem Cursor.
//
// @return fmEntry - Eintrag
// @return bool - false bei leerem Verzeichnis
// @date   2026-10-21 19:45
func (p *filePanel) current() (fmEntry, bool) {
	if p.cursor < 0 || p.cursor >= len(p.entries) {
		return fmEntry{}, false
	}
	return p.entries[p.cursor], true
}

// enterTarget entscheidet, was Enter auf dem aktuellen Eintrag bewirkt.
// Bei einem Verzeichniswechsel liefert es das Zielverzeichnis und den
// Namen, auf den der Cursor danach soll ("" = erster Eintrag; bei ".."
// das Verzeichnis, aus dem man kommt - wie im mc).
//
// @return fmEnterAction - Was zu tun ist
// @return string - Zielverzeichnis (nur fmEnterChangeDir)
// @return string - Name für den Cursor nach dem Wechsel
// @date   2026-10-21 19:45
func (p *filePanel) enterTarget() (fmEnterAction, string, string) {
	entry, ok := p.current()
	if !ok {
		return fmEnterNone, "", ""
	}
	switch {
	case entry.parent:
		return fmEnterChangeDir, remoteParentDir(p.path), path.Base(p.path)
	case entry.symlink && entry.dir:
		return fmEnterSymlinkDir, "", ""
	case entry.dir && !entry.symlink:
		return fmEnterChangeDir, path.Join(p.path, entry.name), ""
	}
	return fmEnterNone, "", ""
}

// applyListing übernimmt ein frisch gelesenes Verzeichnis und setzt den
// Cursor auf den Eintrag focus (falls vorhanden, sonst an den Anfang).
//
// @param dir - Gelesenes Verzeichnis
// @param entries - Einträge
// @param focus - Name, auf den der Cursor soll ("" = Anfang)
// @date   2026-10-21 19:45
func (p *filePanel) applyListing(dir string, entries []fmEntry, focus string) {
	p.path = dir
	p.entries = entries
	p.cursor = 0
	p.offset = 0
	if focus == "" {
		return
	}
	for i, e := range entries {
		if e.name == focus && !e.parent {
			p.cursor = i
			return
		}
	}
}

// moveCursor bewegt den Cursor um delta Zeilen und hält ihn im Bereich.
//
// @param delta - Zeilen (negativ = nach oben)
// @date   2026-10-21 19:45
func (p *filePanel) moveCursor(delta int) {
	p.setCursor(p.cursor + delta)
}

// setCursor setzt den Cursor auf eine Position und hält ihn im Bereich.
//
// @param pos - Neue Position
// @date   2026-10-21 19:45
func (p *filePanel) setCursor(pos int) {
	if pos >= len(p.entries) {
		pos = len(p.entries) - 1
	}
	if pos < 0 {
		pos = 0
	}
	p.cursor = pos
}

// toggleMark markiert bzw. entmarkiert den Eintrag unter dem Cursor und
// setzt den Cursor eine Zeile weiter (wie Einfg im mc). ".." wird nicht
// markiert, der Cursor springt trotzdem weiter.
//
// @date   2026-10-21 20:10
func (p *filePanel) toggleMark() {
	if p.cursor < 0 || p.cursor >= len(p.entries) {
		return
	}
	if entry := &p.entries[p.cursor]; !entry.parent {
		entry.marked = !entry.marked
	}
	p.moveCursor(1)
}

// invertMarks kehrt die Markierung aller Einträge um - Dateien und
// Verzeichnisse, nur ".." bleibt unmarkiert (wie * im mc).
//
// @date   2026-10-21 20:10
func (p *filePanel) invertMarks() {
	for i := range p.entries {
		if !p.entries[i].parent {
			p.entries[i].marked = !p.entries[i].marked
		}
	}
}

// markedSummary liefert Anzahl und Gesamtgröße der markierten Einträge.
// Die Größe zählt nur Dateien (die "Größe" eines Verzeichnisses sagt
// nichts über seinen Inhalt).
//
// @return int - Anzahl markierter Einträge
// @return int64 - Summe der Dateigrößen in Bytes
// @date   2026-10-21 20:10
func (p *filePanel) markedSummary() (int, int64) {
	count, size := 0, int64(0)
	for _, e := range p.entries {
		if !e.marked {
			continue
		}
		count++
		if !e.dir {
			size += e.size
		}
	}
	return count, size
}

// title liefert die Titelzeile des Fensters, z. B. "root@host:/home/root"
// bzw. "Lokaler PC:/home/a".
//
// @return string - Titel (bereinigt, ohne Steuerzeichen)
// @date   2026-10-22 06:40
func (p *filePanel) title() string {
	return p.location(p.path)
}

// location beschreibt einen Pfad dieses Fensters für Titel (Fenster,
// Betrachter, Editor): "user@host:pfad" bzw. "Lokaler PC:pfad".
//
// @param filePath - Pfad im Fenster
// @return string - Text (bereinigt, ohne Steuerzeichen)
// @date   2026-10-22 06:40
func (p *filePanel) location(filePath string) string {
	if p.conn.ID == fmLocalConnID {
		return sanitizeFileName(p.conn.Name + ":" + filePath)
	}
	return sanitizeFileName(p.conn.User + "@" + p.conn.Host + ":" + filePath)
}

// sanitizeFileName ersetzt Steuerzeichen, unsichtbare Formatzeichen und
// ungültiges UTF-8 durch "?". Dateinamen kommen vom Server und sind nicht
// vertrauenswürdig: Ein Name mit ESC-Sequenz könnte sonst das lokale
// Terminal steuern, eine Richtungsumkehr (U+202E) täuscht einen anderen
// Namen vor ("rechnung<RLO>fdp.exe" erscheint als "rechnungexe.pdf").
// Die Joiner U+200C/U+200D bleiben: Sie gehören z. B. zur persischen Schrift.
//
// @param name - Name vom Server
// @return string - Gefahrlos anzeigbarer Name
// @date   2026-10-21 19:45
func sanitizeFileName(name string) string {
	var out strings.Builder
	for i := 0; i < len(name); {
		r, size := utf8.DecodeRuneInString(name[i:])
		i += size
		if r == utf8.RuneError && size <= 1 {
			out.WriteByte('?')
			continue
		}
		if unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d') {
			out.WriteByte('?')
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// formatFileSize liefert die Größe in höchstens 7 Zeichen: bis 9999999
// in Bytes, darüber in K, M oder G (abgerundet, Basis 1024).
//
// @param size - Größe in Bytes
// @return string - Anzeige, z. B. "2048", "9766K", "500M"
// @date   2026-10-21 19:45
func formatFileSize(size int64) string {
	if size < 0 {
		size = 0
	}
	if size <= 9999999 {
		return strconv.FormatInt(size, 10)
	}
	for _, unit := range []string{"K", "M", "G", "T"} {
		size /= 1024
		if size <= 99999 {
			return strconv.FormatInt(size, 10) + unit
		}
	}
	return strconv.FormatInt(size, 10) + "P"
}

// formatSizeWithUnit liefert die Größe wie formatFileSize, aber mit
// Einheit: "12 B", "9765 KB", "500 MB" (für die Markierungsinfo, wo die
// Zahl ohne Spaltenkopf steht).
//
// @param size - Größe in Bytes
// @return string - Anzeige mit Einheit
// @date   2026-10-21 20:12
func formatSizeWithUnit(size int64) string {
	text := formatFileSize(size)
	last := text[len(text)-1]
	if last >= '0' && last <= '9' {
		return text + " B"
	}
	return text[:len(text)-1] + " " + string(last) + "B"
}
