// Hilfsprogramm für debugging/installer_wine_check.sh (nur Windows/Wine).
//
// Ahmt die Lage beim Setup nach, in der ssh-easy.exe noch benutzt wird:
//   - "lock <datei> <sekunden>": hält die Datei offen, nur Lesen erlaubt
//     (Löschen und Umbenennen sind dann gesperrt), und gibt sie nach
//     <sekunden> frei - wie ssh-easy, das sich beim Auto-Update beendet.
//   - "sleep <sekunden>": wartet nur. Als ssh-easy.exe kopiert und gestartet
//     ist das eine echte laufende EXE (Löschen verboten, Umbenennen erlaubt).
//
// @author Kurt Ingwer
// @date   2026-10-27 15:50
package main

import (
	"os"
	"strconv"
	"syscall"
	"time"
)

// main wertet die Kommandozeile aus und sperrt bzw. wartet.
//
// @date 2026-10-27 15:50
func main() {
	if len(os.Args) < 3 {
		println("usage: locker lock <file> <seconds> | sleep <seconds>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "sleep":
		seconds, _ := strconv.Atoi(os.Args[2])
		time.Sleep(time.Duration(seconds) * time.Second)
	case "lock":
		seconds := 60
		if len(os.Args) > 3 {
			seconds, _ = strconv.Atoi(os.Args[3])
		}
		path, _ := syscall.UTF16PtrFromString(os.Args[2])
		handle, err := syscall.CreateFile(path, syscall.GENERIC_READ, syscall.FILE_SHARE_READ,
			nil, syscall.OPEN_EXISTING, 0, 0)
		if err != nil {
			println("lock failed:", err.Error())
			os.Exit(1)
		}
		println("locked")
		time.Sleep(time.Duration(seconds) * time.Second)
		syscall.CloseHandle(handle)
		println("released")
	}
}
