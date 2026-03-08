# ssh-easy - Projektkontext

## Was ist ssh-easy?
Ein Go-basierter SSH-Verbindungsmanager mit Terminal-UI (Bubbletea).
Ermoeglicht SSH-Verbindungen mit Local Port Forwarding zu speichern und aufzubauen.
Unterstuetzt Passwort, SSH-Key und SSH-Agent Authentifizierung.

## Technologie
- Go 1.26.0
- Bubbletea + Lipgloss + Bubbles (TUI)
- golang.org/x/crypto/ssh (SSH-Client)
- golang.org/x/crypto/ssh/knownhosts (Host-Key-Verifizierung)
- golang.org/x/crypto/ssh/agent (SSH-Agent)
- JSON-Config unter ~/.ssh-easy/connections.json
- Log-Datei unter ~/.ssh-easy/ssh-easy.log

## Dateistruktur (Build 3)
- src/ - Quellcode:
  - main.go - Einstiegspunkt
  - types.go - Datenstrukturen
  - config.go - JSON Config + ConfigCache
  - ssh.go - Tunnel-Helfer, Key-Gen, Disconnect
  - ssh_manager.go - SSHManager (Connect, Reconnect, Keepalive)
  - logger.go - Datei-basiertes Logging
  - tui.go - TUI-Core (Model, Styles)
  - tui_list.go, tui_form.go, tui_status.go, tui_keygen.go - Views
- build/ - Kompilierte Binaries (4 Plattformen)
- tests/ - Test-Dateien (werden auch nach src/ kopiert fuer go test)

## Build
```bash
cd src && /usr/local/go/bin/go build -o ../build/ssh-easy .
```

## Cross-Compile
```bash
GOOS=linux GOARCH=arm64 go build -o ../build/ssh-easy-linux-arm64 .
GOOS=windows GOARCH=amd64 go build -o ../build/ssh-easy-windows-amd64.exe .
GOOS=windows GOARCH=arm64 go build -o ../build/ssh-easy-windows-arm64.exe .
```

## Tests
```bash
cd src && go test -v ./...
```

## Wichtig
- Build-Nummer in src/build.txt bei jeder Aenderung hochzaehlen
- Passwoerter werden NICHT gespeichert
- Tunnel nur auf 127.0.0.1 binden
- Kein InsecureIgnoreHostKey - known_hosts ist Pflicht
- Tests muessen vor jedem Build durchlaufen
- 47 Tests in 6 Test-Dateien
