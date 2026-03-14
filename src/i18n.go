// Paket main – Internationalisierung (i18n) für ssh-easy
//
// Definiert alle UI-seitig angezeigten Texte in 8 Sprachen.
// Neue Sprachen können einfach als weiterer Eintrag in allTranslations
// ergänzt werden.
//
// @author Kurt Ingwer
// @date   2026-03-14
package main

// Language identifiziert die gewählte Oberflächensprache
type Language string

const (
	LangDeutsch    Language = "de"
	LangEnglish    Language = "en"
	LangFrancais   Language = "fr"
	LangEspanol    Language = "es"
	LangItaliano   Language = "it"
	LangJapanese   Language = "ja"
	LangChinese    Language = "zh"
	LangPortugues  Language = "pt"
	LangHindi      Language = "hi"
	LangBengali    Language = "bn"
	LangRussian    Language = "ru"
	LangUrdu       Language = "ur"
	LangIndonesian Language = "id"
	LangArabic     Language = "ar"
)

// LanguageOption enthält Anzeigeinformationen für eine auswählbare Sprache
type LanguageOption struct {
	// Sprachcode (ISO 639-1)
	Code Language
	// Name in der jeweiligen Muttersprache (UTF-8)
	Name string
}

// AvailableLanguages listet alle unterstützten Sprachen in Anzeigereihenfolge
var AvailableLanguages = []LanguageOption{
	{LangDeutsch, "Deutsch"},
	{LangEnglish, "English"},
	{LangFrancais, "Français"},
	{LangEspanol, "Español"},
	{LangItaliano, "Italiano"},
	{LangPortugues, "Português"},
	{LangRussian, "Русский"},
	{LangIndonesian, "Bahasa Indonesia"},
	{LangHindi, "हिन्दी"},
	{LangBengali, "বাংলা"},
	{LangChinese, "中文"},
	{LangJapanese, "日本語"},
	{LangArabic, "العربية"},
	{LangUrdu, "اردو"},
}

// Translations enthält alle UI-seitig angezeigten Texte einer Sprache.
// Felder mit %s oder %d sind Format-Strings für fmt.Sprintf.
type Translations struct {
	// --- Sprachauswahl ---
	LangSelectTitle  string // Titel der Sprachauswahl
	LangSelectPrompt string // Aufforderungstext
	LangSelectHelp   string // Tastenbelegung der Auswahl

	// --- Listenansicht ---
	NoConnections   string // Meldung wenn keine Verbindungen vorhanden
	PressNToAdd     string // Hinweis zum Erstellen einer neuen Verbindung
	HelpList        string // Hilfezeile der Hauptliste

	// --- Verbindungsaufbau (Auto-Auth) ---
	ConnectingTitle string // Titel, enthält %s für den Verbindungsnamen
	TryingAutoAuth  string // Info-Text beim Auto-Connect
	PleaseWait      string // Warteanzeige

	// --- Löschbestätigung ---
	DeleteTitle     string // Titel des Löschdialogs
	DeleteConfirm   string // Bestätigungstext, enthält %s für den Namen
	DeleteYesNo     string // Tastenbelegung Ja/Nein
	DeletedMsg      string // Erfolgs-Meldung nach dem Löschen
	DisconnectedMsg string // Präfix für "getrennt von: <Name>"

	// --- Formular Erstellen / Bearbeiten ---
	FormTitleNew    string // Titel Neue Verbindung
	FormTitleEdit   string // Titel Verbindung bearbeiten
	LabelName       string // Feldbezeichnung Name
	LabelHost       string // Feldbezeichnung Host
	LabelPort       string // Feldbezeichnung Port
	LabelUser       string // Feldbezeichnung Benutzer
	LabelTunnels    string // Feldbezeichnung Tunnel-Ports
	PlaceholderName string // Platzhalter für das Name-Feld
	FormHelp        string // Hilfezeile im Formular
	SavedMsg        string // Erfolgs-Meldung nach dem Speichern

	// --- Passwort-Eingabe ---
	NoKeyFound   string // Meldung wenn kein SSH-Key gefunden wurde
	PlaceholderPW string // Platzhalter für das Passwort-Feld
	AfterPWHint  string // Hinweis auf automatischen Key-Deploy
	ConnectHelp  string // Hilfezeile bei Passwort-Eingabe
	ConnNotFound string // Meldung wenn Verbindung nicht in Liste gefunden

	// --- Statusansicht ---
	StatusTitle     string // Titel, enthält %s für den Verbindungsnamen
	LabelServer     string // Zeilenpräfix "Server:"
	LabelAuth       string // Zeilenpräfix "Auth:"
	LabelStatus     string // Zeilenpräfix "Status:"
	StatusConn      string // Text für "Verbunden"
	StatusDisconn   string // Text für "Getrennt"
	LabelTunnel     string // Abschnittsüberschrift Tunnel
	TunnelActive    string // Status "aktiv"
	TunnelErrPrefix string // Präfix für Tunnel-Fehlermeldungen
	StatusHelp      string // Hilfezeile der Statusansicht
	NoActiveConn    string // Meldung wenn kein aktiver Client
	DiscoMsg        string // Kurz-Meldung nach Trennung

	// --- Key-Generierung ---
	KeygenTitle     string // Formular-Titel
	LabelKeyPath    string // Feldbezeichnung Dateipfad
	LabelPassphrase string // Feldbezeichnung Passphrase
	PlaceholderPass string // Platzhalter für das Passphrase-Feld
	KeyPathRequired string // Fehler wenn Pfad leer
	KeygenHelp      string // Hilfezeile im Keygen-Formular
	KeygenDoneTitle string // Titel der Ergebnisanzeige
	KeyCreated      string // Erfolgs-Text
	LabelPublicKey  string // Bezeichnung "Public Key:"
	KeyAddToAuth    string // Hinweis zum Eintragen in authorized_keys
	BackHelp        string // Hilfezeile "Zurück"

	// --- Host-Key-Dialog ---
	HostKeyWarning  string // Hauptwarnung (fett rot)
	HostKeyBoxTitle string // Erste Zeile der Warnungsbox
	HostKeyBoxHost  string // Host-Zeile, enthält %s für den Hostnamen
	HostKeyReasons  string // Auflistung möglicher Ursachen
	HostKeyCaution  string // Abschlusssatz der Warnungsbox
	HostKeyAskYesNo string // Bestätigungsfrage + Tastenbelegung
	UnknownHost     string // Fallback wenn Hostname unbekannt

	// TunnelInfo ist der Format-String für die Tunnel-Anzeige in der Liste, enthält %s für die Ports
	TunnelInfo string

	// --- Laufzeit-Fehlermeldungen ---
	ErrPrefix        string // Präfix "  Fehler: " vor errorMsg in der Anzeige
	ErrLoading       string // Präfix beim Lade-Fehler
	ErrTerminal      string // Präfix beim Terminal-Fehler
	TerminalDone     string // Meldung nach Ende der Terminal-Session
	ConnectedMsg     string // Meldung nach erfolgreichem Verbindungsaufbau
	KeyDeployFailed  string // Präfix bei fehlgeschlagenem Key-Deploy
	KeyDeployedMsg   string // Erfolg Key-Deploy, enthält %s für den Pfad
	ConnErrPrefix    string // Präfix für Verbindungsfehler
	ErrPortMustBeNum string // Fehler bei ungültigem Port
	ErrTunnelPort    string // Fehler bei ungültigem Tunnel-Port, enthält %s
}

// GetTranslations gibt die Übersetzungen für die angegebene Sprache zurück.
// Fällt auf Englisch zurück wenn die Sprache unbekannt ist.
//
// @param lang - Sprachcode
// @return Translations - Texte der Sprache
// @date   2026-03-14
func GetTranslations(lang Language) Translations {
	if t, ok := allTranslations[lang]; ok {
		return t
	}
	return allTranslations[LangEnglish]
}

// allTranslations enthält alle Übersetzungen in einer Map nach Sprachcode
var allTranslations = map[Language]Translations{

	// ===================== DEUTSCH =====================
	LangDeutsch: {
		LangSelectTitle:  "  Sprachauswahl",
		LangSelectPrompt: "  Bitte wähle deine Sprache:",
		LangSelectHelp:   "  ↑/↓ oder j/k: Navigieren   Enter: Auswählen   Esc: Abbrechen",

		NoConnections: "  Keine Verbindungen gespeichert.",
		PressNToAdd:   "  Drücke 'n' um eine neue Verbindung anzulegen.",
		HelpList:      "  n:Neu  e:Bearbeiten  d:Löschen  Enter:Verbinden  x:Trennen  g:Key-Gen  l:Sprache  q:Beenden",

		ConnectingTitle: "  Verbinde mit: %s",
		TryingAutoAuth:  "  Probiere SSH-Agent und verfügbare Schlüssel...",
		PleaseWait:      "  Bitte warten",

		DeleteTitle:     "  Verbindung löschen?",
		DeleteConfirm:   "  Soll die Verbindung '%s' wirklich gelöscht werden?\n\n",
		DeleteYesNo:     "  [j/y] Ja   [n/Esc] Nein",
		DeletedMsg:      "Verbindung gelöscht!",
		DisconnectedMsg: "Verbindung getrennt: ",

		FormTitleNew:    "Neue Verbindung erstellen",
		FormTitleEdit:   "Verbindung bearbeiten",
		LabelName:       "Name:",
		LabelHost:       "Host:",
		LabelPort:       "Port:",
		LabelUser:       "Benutzer:",
		LabelTunnels:    "Tunnel-Ports (kommagetrennt):",
		PlaceholderName: "Mein Server",
		FormHelp:        "  Tab:Nächstes Feld  Enter:Speichern  Esc:Abbrechen",
		SavedMsg:        "Verbindung gespeichert!",

		NoKeyFound:   "  Kein passender SSH-Key gefunden. Bitte Passwort eingeben:",
		PlaceholderPW: "Passwort/Passphrase eingeben",
		AfterPWHint:  "  Nach erfolgreicher Anmeldung wird automatisch ein SSH-Schlüssel erstellt.",
		ConnectHelp:  "  Enter:Verbinden  Esc:Abbrechen",
		ConnNotFound: "Verbindung nicht gefunden",

		StatusTitle:     "  Status: %s",
		LabelServer:     "Server:  ",
		LabelAuth:       "Auth:    ",
		LabelStatus:     "Status:  ",
		StatusConn:      "Verbunden",
		StatusDisconn:   "Getrennt",
		LabelTunnel:     "Tunnel:",
		TunnelActive:    "aktiv",
		TunnelErrPrefix: "Fehler: ",
		StatusHelp:      "  t:Terminal  x:Trennen  Esc:Zurück",
		NoActiveConn:    "Keine aktive Verbindung",
		DiscoMsg:        "Verbindung getrennt",

		KeygenTitle:     "  SSH-Key generieren (Ed25519)",
		LabelKeyPath:    "Dateipfad:",
		LabelPassphrase: "Passphrase (optional):",
		PlaceholderPass: "leer = ohne Passphrase",
		KeyPathRequired: "Dateipfad darf nicht leer sein",
		KeygenHelp:      "  Tab:Nächstes Feld  Enter:Generieren  Esc:Abbrechen",
		KeygenDoneTitle: "  SSH-Key generiert!",
		KeyCreated:      "  Schlüssel erfolgreich erstellt.",
		LabelPublicKey:  "Public Key:",
		KeyAddToAuth:    "  Diesen Public Key auf dem Zielserver in ~/.ssh/authorized_keys eintragen.",
		BackHelp:        "  Enter/Esc:Zurück",

		HostKeyWarning:  "  SICHERHEITSWARNUNG: SSH HOST-KEY GEÄNDERT!",
		HostKeyBoxTitle: "Der SSH-Schlüssel des Servers hat sich geändert!\n\n",
		HostKeyBoxHost:  "Host: %s\n\n",
		HostKeyReasons:  "MÖGLICHE URSACHEN:\n  - Server wurde neu installiert (legitim)\n  - Server-Key wurde erneuert (legitim)\n  - Man-in-the-Middle-Angriff (GEFÄHRLICH!)\n\n",
		HostKeyCaution:  "Nur fortfahren wenn du weißt dass sich\nder Server-Key geändert hat!",
		HostKeyAskYesNo: "  Alten Host-Key entfernen und neu verbinden?\n\n  [j/y] Ja, ich weiß was ich tue   [n/Esc] Nein, abbrechen",
		UnknownHost:     "(unbekannt)",

		ErrPrefix:        "  Fehler: ",
		ErrLoading:       "Fehler beim Laden: ",
		ErrTerminal:      "Terminal-Fehler: ",
		TerminalDone:     "Terminal-Session beendet",
		ConnectedMsg:     "Verbindung hergestellt!",
		KeyDeployFailed:  "Key-Deployment fehlgeschlagen: ",
		KeyDeployedMsg:   "SSH-Key deployed! Nächste Verbindung ohne Passwort: %s",
		ConnErrPrefix:    "Verbindungsfehler: ",
		TunnelInfo:       " [Tunnel: %s]",
		ErrPortMustBeNum: "Port muss eine Zahl sein",
		ErrTunnelPort:    "Tunnel-Port '%s' ist keine gültige Zahl",
	},

	// ===================== ENGLISH =====================
	LangEnglish: {
		LangSelectTitle:  "  Language Selection",
		LangSelectPrompt: "  Please select your language:",
		LangSelectHelp:   "  ↑/↓ or j/k: Navigate   Enter: Select   Esc: Cancel",

		NoConnections: "  No connections saved.",
		PressNToAdd:   "  Press 'n' to add a new connection.",
		HelpList:      "  n:New  e:Edit  d:Delete  Enter:Connect  x:Disconnect  g:Key-Gen  l:Language  q:Quit",

		ConnectingTitle: "  Connecting to: %s",
		TryingAutoAuth:  "  Trying SSH agent and available keys...",
		PleaseWait:      "  Please wait",

		DeleteTitle:     "  Delete connection?",
		DeleteConfirm:   "  Really delete connection '%s'?\n\n",
		DeleteYesNo:     "  [j/y] Yes   [n/Esc] No",
		DeletedMsg:      "Connection deleted!",
		DisconnectedMsg: "Disconnected: ",

		FormTitleNew:    "New Connection",
		FormTitleEdit:   "Edit Connection",
		LabelName:       "Name:",
		LabelHost:       "Host:",
		LabelPort:       "Port:",
		LabelUser:       "User:",
		LabelTunnels:    "Tunnel Ports (comma-separated):",
		PlaceholderName: "My Server",
		FormHelp:        "  Tab:Next Field  Enter:Save  Esc:Cancel",
		SavedMsg:        "Connection saved!",

		NoKeyFound:   "  No matching SSH key found. Please enter password:",
		PlaceholderPW: "Enter password/passphrase",
		AfterPWHint:  "  After login, an SSH key will be created automatically.",
		ConnectHelp:  "  Enter:Connect  Esc:Cancel",
		ConnNotFound: "Connection not found",

		StatusTitle:     "  Status: %s",
		LabelServer:     "Server:  ",
		LabelAuth:       "Auth:    ",
		LabelStatus:     "Status:  ",
		StatusConn:      "Connected",
		StatusDisconn:   "Disconnected",
		LabelTunnel:     "Tunnel:",
		TunnelActive:    "active",
		TunnelErrPrefix: "Error: ",
		StatusHelp:      "  t:Terminal  x:Disconnect  Esc:Back",
		NoActiveConn:    "No active connection",
		DiscoMsg:        "Disconnected",

		KeygenTitle:     "  Generate SSH Key (Ed25519)",
		LabelKeyPath:    "File path:",
		LabelPassphrase: "Passphrase (optional):",
		PlaceholderPass: "empty = no passphrase",
		KeyPathRequired: "File path must not be empty",
		KeygenHelp:      "  Tab:Next Field  Enter:Generate  Esc:Cancel",
		KeygenDoneTitle: "  SSH Key generated!",
		KeyCreated:      "  Key successfully created.",
		LabelPublicKey:  "Public Key:",
		KeyAddToAuth:    "  Add this public key to ~/.ssh/authorized_keys on the target server.",
		BackHelp:        "  Enter/Esc:Back",

		HostKeyWarning:  "  SECURITY WARNING: SSH HOST KEY CHANGED!",
		HostKeyBoxTitle: "The SSH key of the server has changed!\n\n",
		HostKeyBoxHost:  "Host: %s\n\n",
		HostKeyReasons:  "POSSIBLE CAUSES:\n  - Server was reinstalled (legitimate)\n  - Server key was renewed (legitimate)\n  - Man-in-the-middle attack (DANGEROUS!)\n\n",
		HostKeyCaution:  "Only proceed if you know the server key has changed.",
		HostKeyAskYesNo: "  Remove old host key and reconnect?\n\n  [j/y] Yes, I know what I'm doing   [n/Esc] No, cancel",
		UnknownHost:     "(unknown)",

		ErrPrefix:        "  Error: ",
		ErrLoading:       "Error loading: ",
		ErrTerminal:      "Terminal error: ",
		TerminalDone:     "Terminal session ended",
		ConnectedMsg:     "Connection established!",
		KeyDeployFailed:  "Key deployment failed: ",
		KeyDeployedMsg:   "SSH key deployed! Next connection without password: %s",
		ConnErrPrefix:    "Connection error: ",
		TunnelInfo:       " [Tunnel: %s]",
		ErrPortMustBeNum: "Port must be a number",
		ErrTunnelPort:    "Tunnel port '%s' is not a valid number",
	},

	// ===================== FRANÇAIS =====================
	LangFrancais: {
		LangSelectTitle:  "  Sélection de la langue",
		LangSelectPrompt: "  Veuillez sélectionner votre langue :",
		LangSelectHelp:   "  ↑/↓ ou j/k : Naviguer   Entrée : Sélectionner   Éch : Annuler",

		NoConnections: "  Aucune connexion enregistrée.",
		PressNToAdd:   "  Appuyez sur 'n' pour ajouter une connexion.",
		HelpList:      "  n:Nouveau  e:Modifier  d:Supprimer  Entrée:Connecter  x:Déconnecter  g:Clé SSH  l:Langue  q:Quitter",

		ConnectingTitle: "  Connexion à : %s",
		TryingAutoAuth:  "  Tentative avec l'agent SSH et les clés disponibles...",
		PleaseWait:      "  Veuillez patienter",

		DeleteTitle:     "  Supprimer la connexion ?",
		DeleteConfirm:   "  Supprimer vraiment la connexion '%s' ?\n\n",
		DeleteYesNo:     "  [j/y] Oui   [n/Éch] Non",
		DeletedMsg:      "Connexion supprimée !",
		DisconnectedMsg: "Déconnecté : ",

		FormTitleNew:    "Nouvelle connexion",
		FormTitleEdit:   "Modifier la connexion",
		LabelName:       "Nom :",
		LabelHost:       "Hôte :",
		LabelPort:       "Port :",
		LabelUser:       "Utilisateur :",
		LabelTunnels:    "Ports tunnel (séparés par des virgules) :",
		PlaceholderName: "Mon serveur",
		FormHelp:        "  Tab:Champ suivant  Entrée:Enregistrer  Éch:Annuler",
		SavedMsg:        "Connexion enregistrée !",

		NoKeyFound:   "  Aucune clé SSH correspondante. Veuillez entrer le mot de passe :",
		PlaceholderPW: "Entrer le mot de passe/la phrase secrète",
		AfterPWHint:  "  Après la connexion, une clé SSH sera créée automatiquement.",
		ConnectHelp:  "  Entrée:Connecter  Éch:Annuler",
		ConnNotFound: "Connexion introuvable",

		StatusTitle:     "  Statut : %s",
		LabelServer:     "Serveur :  ",
		LabelAuth:       "Auth :     ",
		LabelStatus:     "Statut :   ",
		StatusConn:      "Connecté",
		StatusDisconn:   "Déconnecté",
		LabelTunnel:     "Tunnel :",
		TunnelActive:    "actif",
		TunnelErrPrefix: "Erreur : ",
		StatusHelp:      "  t:Terminal  x:Déconnecter  Éch:Retour",
		NoActiveConn:    "Aucune connexion active",
		DiscoMsg:        "Déconnecté",

		KeygenTitle:     "  Générer une clé SSH (Ed25519)",
		LabelKeyPath:    "Chemin du fichier :",
		LabelPassphrase: "Phrase secrète (optionnel) :",
		PlaceholderPass: "vide = sans phrase secrète",
		KeyPathRequired: "Le chemin du fichier ne peut pas être vide",
		KeygenHelp:      "  Tab:Champ suivant  Entrée:Générer  Éch:Annuler",
		KeygenDoneTitle: "  Clé SSH générée !",
		KeyCreated:      "  Clé créée avec succès.",
		LabelPublicKey:  "Clé publique :",
		KeyAddToAuth:    "  Ajoutez cette clé publique dans ~/.ssh/authorized_keys sur le serveur cible.",
		BackHelp:        "  Entrée/Éch:Retour",

		HostKeyWarning:  "  AVERTISSEMENT DE SÉCURITÉ : CLÉ SSH DE L'HÔTE MODIFIÉE !",
		HostKeyBoxTitle: "La clé SSH du serveur a changé !\n\n",
		HostKeyBoxHost:  "Hôte : %s\n\n",
		HostKeyReasons:  "CAUSES POSSIBLES :\n  - Le serveur a été réinstallé (légitime)\n  - La clé du serveur a été renouvelée (légitime)\n  - Attaque man-in-the-middle (DANGEREUX !)\n\n",
		HostKeyCaution:  "Ne continuez que si vous savez que la clé du serveur a changé.",
		HostKeyAskYesNo: "  Supprimer l'ancienne clé et se reconnecter ?\n\n  [j/y] Oui, je sais ce que je fais   [n/Éch] Non, annuler",
		UnknownHost:     "(inconnu)",

		ErrPrefix:        "  Erreur : ",
		ErrLoading:       "Erreur de chargement : ",
		ErrTerminal:      "Erreur terminal : ",
		TerminalDone:     "Session terminal terminée",
		ConnectedMsg:     "Connexion établie !",
		KeyDeployFailed:  "Déploiement de clé échoué : ",
		KeyDeployedMsg:   "Clé SSH déployée ! Prochaine connexion sans mot de passe : %s",
		ConnErrPrefix:    "Erreur de connexion : ",
		TunnelInfo:       " [Tunnel : %s]",
		ErrPortMustBeNum: "Le port doit être un nombre",
		ErrTunnelPort:    "Le port tunnel '%s' n'est pas un nombre valide",
	},

	// ===================== ESPAÑOL =====================
	LangEspanol: {
		LangSelectTitle:  "  Selección de idioma",
		LangSelectPrompt: "  Por favor selecciona tu idioma:",
		LangSelectHelp:   "  ↑/↓ o j/k: Navegar   Enter: Seleccionar   Esc: Cancelar",

		NoConnections: "  No hay conexiones guardadas.",
		PressNToAdd:   "  Pulsa 'n' para añadir una nueva conexión.",
		HelpList:      "  n:Nuevo  e:Editar  d:Eliminar  Enter:Conectar  x:Desconectar  g:Clave SSH  l:Idioma  q:Salir",

		ConnectingTitle: "  Conectando a: %s",
		TryingAutoAuth:  "  Probando agente SSH y claves disponibles...",
		PleaseWait:      "  Por favor espera",

		DeleteTitle:     "  ¿Eliminar conexión?",
		DeleteConfirm:   "  ¿Eliminar realmente la conexión '%s'?\n\n",
		DeleteYesNo:     "  [j/y] Sí   [n/Esc] No",
		DeletedMsg:      "¡Conexión eliminada!",
		DisconnectedMsg: "Desconectado: ",

		FormTitleNew:    "Nueva conexión",
		FormTitleEdit:   "Editar conexión",
		LabelName:       "Nombre:",
		LabelHost:       "Host:",
		LabelPort:       "Puerto:",
		LabelUser:       "Usuario:",
		LabelTunnels:    "Puertos de túnel (separados por comas):",
		PlaceholderName: "Mi servidor",
		FormHelp:        "  Tab:Siguiente campo  Enter:Guardar  Esc:Cancelar",
		SavedMsg:        "¡Conexión guardada!",

		NoKeyFound:   "  No se encontró clave SSH. Por favor introduce la contraseña:",
		PlaceholderPW: "Introducir contraseña/frase de paso",
		AfterPWHint:  "  Tras el inicio de sesión, se creará automáticamente una clave SSH.",
		ConnectHelp:  "  Enter:Conectar  Esc:Cancelar",
		ConnNotFound: "Conexión no encontrada",

		StatusTitle:     "  Estado: %s",
		LabelServer:     "Servidor:  ",
		LabelAuth:       "Auth:      ",
		LabelStatus:     "Estado:    ",
		StatusConn:      "Conectado",
		StatusDisconn:   "Desconectado",
		LabelTunnel:     "Túnel:",
		TunnelActive:    "activo",
		TunnelErrPrefix: "Error: ",
		StatusHelp:      "  t:Terminal  x:Desconectar  Esc:Volver",
		NoActiveConn:    "Sin conexión activa",
		DiscoMsg:        "Desconectado",

		KeygenTitle:     "  Generar clave SSH (Ed25519)",
		LabelKeyPath:    "Ruta del archivo:",
		LabelPassphrase: "Frase de paso (opcional):",
		PlaceholderPass: "vacío = sin frase de paso",
		KeyPathRequired: "La ruta del archivo no puede estar vacía",
		KeygenHelp:      "  Tab:Siguiente campo  Enter:Generar  Esc:Cancelar",
		KeygenDoneTitle: "  ¡Clave SSH generada!",
		KeyCreated:      "  Clave creada con éxito.",
		LabelPublicKey:  "Clave pública:",
		KeyAddToAuth:    "  Añade esta clave pública a ~/.ssh/authorized_keys en el servidor de destino.",
		BackHelp:        "  Enter/Esc:Volver",

		HostKeyWarning:  "  ADVERTENCIA DE SEGURIDAD: ¡CLAVE SSH DEL HOST CAMBIADA!",
		HostKeyBoxTitle: "¡La clave SSH del servidor ha cambiado!\n\n",
		HostKeyBoxHost:  "Host: %s\n\n",
		HostKeyReasons:  "POSIBLES CAUSAS:\n  - El servidor fue reinstalado (legítimo)\n  - La clave del servidor fue renovada (legítimo)\n  - Ataque man-in-the-middle (¡PELIGROSO!)\n\n",
		HostKeyCaution:  "Solo continúa si sabes que la clave del servidor ha cambiado.",
		HostKeyAskYesNo: "  ¿Eliminar la clave antigua y reconectar?\n\n  [j/y] Sí, sé lo que hago   [n/Esc] No, cancelar",
		UnknownHost:     "(desconocido)",

		ErrPrefix:        "  Error: ",
		ErrLoading:       "Error al cargar: ",
		ErrTerminal:      "Error de terminal: ",
		TerminalDone:     "Sesión de terminal finalizada",
		ConnectedMsg:     "¡Conexión establecida!",
		KeyDeployFailed:  "Despliegue de clave fallido: ",
		KeyDeployedMsg:   "¡Clave SSH desplegada! Próxima conexión sin contraseña: %s",
		ConnErrPrefix:    "Error de conexión: ",
		TunnelInfo:       " [Túnel: %s]",
		ErrPortMustBeNum: "El puerto debe ser un número",
		ErrTunnelPort:    "El puerto de túnel '%s' no es un número válido",
	},

	// ===================== ITALIANO =====================
	LangItaliano: {
		LangSelectTitle:  "  Selezione della lingua",
		LangSelectPrompt: "  Seleziona la tua lingua:",
		LangSelectHelp:   "  ↑/↓ o j/k: Naviga   Invio: Seleziona   Esc: Annulla",

		NoConnections: "  Nessuna connessione salvata.",
		PressNToAdd:   "  Premi 'n' per aggiungere una nuova connessione.",
		HelpList:      "  n:Nuovo  e:Modifica  d:Elimina  Invio:Connetti  x:Disconnetti  g:Chiave SSH  l:Lingua  q:Esci",

		ConnectingTitle: "  Connessione a: %s",
		TryingAutoAuth:  "  Provo agente SSH e chiavi disponibili...",
		PleaseWait:      "  Attendere prego",

		DeleteTitle:     "  Eliminare la connessione?",
		DeleteConfirm:   "  Eliminare davvero la connessione '%s'?\n\n",
		DeleteYesNo:     "  [j/y] Sì   [n/Esc] No",
		DeletedMsg:      "Connessione eliminata!",
		DisconnectedMsg: "Disconnesso: ",

		FormTitleNew:    "Nuova connessione",
		FormTitleEdit:   "Modifica connessione",
		LabelName:       "Nome:",
		LabelHost:       "Host:",
		LabelPort:       "Porta:",
		LabelUser:       "Utente:",
		LabelTunnels:    "Porte tunnel (separate da virgola):",
		PlaceholderName: "Il mio server",
		FormHelp:        "  Tab:Campo successivo  Invio:Salva  Esc:Annulla",
		SavedMsg:        "Connessione salvata!",

		NoKeyFound:   "  Nessuna chiave SSH trovata. Inserisci la password:",
		PlaceholderPW: "Inserisci password/frase d'accesso",
		AfterPWHint:  "  Dopo il login, verrà creata automaticamente una chiave SSH.",
		ConnectHelp:  "  Invio:Connetti  Esc:Annulla",
		ConnNotFound: "Connessione non trovata",

		StatusTitle:     "  Stato: %s",
		LabelServer:     "Server:  ",
		LabelAuth:       "Auth:    ",
		LabelStatus:     "Stato:   ",
		StatusConn:      "Connesso",
		StatusDisconn:   "Disconnesso",
		LabelTunnel:     "Tunnel:",
		TunnelActive:    "attivo",
		TunnelErrPrefix: "Errore: ",
		StatusHelp:      "  t:Terminale  x:Disconnetti  Esc:Indietro",
		NoActiveConn:    "Nessuna connessione attiva",
		DiscoMsg:        "Disconnesso",

		KeygenTitle:     "  Genera chiave SSH (Ed25519)",
		LabelKeyPath:    "Percorso file:",
		LabelPassphrase: "Frase d'accesso (opzionale):",
		PlaceholderPass: "vuoto = senza frase d'accesso",
		KeyPathRequired: "Il percorso del file non può essere vuoto",
		KeygenHelp:      "  Tab:Campo successivo  Invio:Genera  Esc:Annulla",
		KeygenDoneTitle: "  Chiave SSH generata!",
		KeyCreated:      "  Chiave creata con successo.",
		LabelPublicKey:  "Chiave pubblica:",
		KeyAddToAuth:    "  Aggiungi questa chiave pubblica a ~/.ssh/authorized_keys sul server di destinazione.",
		BackHelp:        "  Invio/Esc:Indietro",

		HostKeyWarning:  "  AVVISO DI SICUREZZA: CHIAVE SSH DELL'HOST CAMBIATA!",
		HostKeyBoxTitle: "La chiave SSH del server è cambiata!\n\n",
		HostKeyBoxHost:  "Host: %s\n\n",
		HostKeyReasons:  "POSSIBILI CAUSE:\n  - Il server è stato reinstallato (legittimo)\n  - La chiave del server è stata rinnovata (legittimo)\n  - Attacco man-in-the-middle (PERICOLOSO!)\n\n",
		HostKeyCaution:  "Procedi solo se sai che la chiave del server è cambiata.",
		HostKeyAskYesNo: "  Rimuovere la vecchia chiave e riconnettersi?\n\n  [j/y] Sì, so cosa sto facendo   [n/Esc] No, annulla",
		UnknownHost:     "(sconosciuto)",

		ErrPrefix:        "  Errore: ",
		ErrLoading:       "Errore di caricamento: ",
		ErrTerminal:      "Errore terminale: ",
		TerminalDone:     "Sessione terminale terminata",
		ConnectedMsg:     "Connessione stabilita!",
		KeyDeployFailed:  "Distribuzione chiave fallita: ",
		KeyDeployedMsg:   "Chiave SSH distribuita! Prossima connessione senza password: %s",
		ConnErrPrefix:    "Errore di connessione: ",
		TunnelInfo:       " [Tunnel: %s]",
		ErrPortMustBeNum: "La porta deve essere un numero",
		ErrTunnelPort:    "La porta tunnel '%s' non è un numero valido",
	},

	// ===================== 日本語 =====================
	LangJapanese: {
		LangSelectTitle:  "  言語選択",
		LangSelectPrompt: "  言語を選択してください：",
		LangSelectHelp:   "  ↑/↓ または j/k：ナビゲート   Enter：選択   Esc：キャンセル",

		NoConnections: "  保存済みの接続がありません。",
		PressNToAdd:   "  'n' を押して新しい接続を追加してください。",
		HelpList:      "  n:新規  e:編集  d:削除  Enter:接続  x:切断  g:SSH鍵  l:言語  q:終了",

		ConnectingTitle: "  接続中：%s",
		TryingAutoAuth:  "  SSHエージェントと利用可能な鍵を試行中...",
		PleaseWait:      "  しばらくお待ちください",

		DeleteTitle:     "  接続を削除しますか？",
		DeleteConfirm:   "  接続 '%s' を本当に削除しますか？\n\n",
		DeleteYesNo:     "  [j/y] はい   [n/Esc] いいえ",
		DeletedMsg:      "接続を削除しました！",
		DisconnectedMsg: "切断しました：",

		FormTitleNew:    "新しい接続",
		FormTitleEdit:   "接続を編集",
		LabelName:       "名前：",
		LabelHost:       "ホスト：",
		LabelPort:       "ポート：",
		LabelUser:       "ユーザー：",
		LabelTunnels:    "トンネルポート（カンマ区切り）：",
		PlaceholderName: "マイサーバー",
		FormHelp:        "  Tab:次のフィールド  Enter:保存  Esc:キャンセル",
		SavedMsg:        "接続を保存しました！",

		NoKeyFound:   "  SSHキーが見つかりません。パスワードを入力してください：",
		PlaceholderPW: "パスワード/パスフレーズを入力",
		AfterPWHint:  "  ログイン後、SSHキーが自動的に作成されます。",
		ConnectHelp:  "  Enter:接続  Esc:キャンセル",
		ConnNotFound: "接続が見つかりません",

		StatusTitle:     "  ステータス：%s",
		LabelServer:     "サーバー：  ",
		LabelAuth:       "認証：      ",
		LabelStatus:     "状態：      ",
		StatusConn:      "接続済み",
		StatusDisconn:   "切断済み",
		LabelTunnel:     "トンネル：",
		TunnelActive:    "有効",
		TunnelErrPrefix: "エラー：",
		StatusHelp:      "  t:ターミナル  x:切断  Esc:戻る",
		NoActiveConn:    "アクティブな接続がありません",
		DiscoMsg:        "切断しました",

		KeygenTitle:     "  SSH鍵を生成（Ed25519）",
		LabelKeyPath:    "ファイルパス：",
		LabelPassphrase: "パスフレーズ（任意）：",
		PlaceholderPass: "空白 = パスフレーズなし",
		KeyPathRequired: "ファイルパスは空にできません",
		KeygenHelp:      "  Tab:次のフィールド  Enter:生成  Esc:キャンセル",
		KeygenDoneTitle: "  SSH鍵を生成しました！",
		KeyCreated:      "  鍵の作成に成功しました。",
		LabelPublicKey:  "公開鍵：",
		KeyAddToAuth:    "  この公開鍵をターゲットサーバーの ~/.ssh/authorized_keys に追加してください。",
		BackHelp:        "  Enter/Esc:戻る",

		HostKeyWarning:  "  セキュリティ警告：SSHホスト鍵が変更されました！",
		HostKeyBoxTitle: "サーバーのSSH鍵が変更されました！\n\n",
		HostKeyBoxHost:  "ホスト：%s\n\n",
		HostKeyReasons:  "考えられる原因：\n  - サーバーが再インストールされた（正当）\n  - サーバー鍵が更新された（正当）\n  - 中間者攻撃（危険！）\n\n",
		HostKeyCaution:  "サーバー鍵が変更されたことが確かな場合のみ続行してください。",
		HostKeyAskYesNo: "  古いホスト鍵を削除して再接続しますか？\n\n  [j/y] はい、承知しています   [n/Esc] いいえ、キャンセル",
		UnknownHost:     "（不明）",

		ErrPrefix:        "  エラー：",
		ErrLoading:       "読み込みエラー：",
		ErrTerminal:      "ターミナルエラー：",
		TerminalDone:     "ターミナルセッションが終了しました",
		ConnectedMsg:     "接続しました！",
		KeyDeployFailed:  "鍵のデプロイに失敗しました：",
		KeyDeployedMsg:   "SSH鍵をデプロイしました！次回からパスワードなしで接続：%s",
		ConnErrPrefix:    "接続エラー：",
		TunnelInfo:       " [トンネル: %s]",
		ErrPortMustBeNum: "ポートは数字でなければなりません",
		ErrTunnelPort:    "トンネルポート '%s' は有効な数字ではありません",
	},

	// ===================== 中文 =====================
	LangChinese: {
		LangSelectTitle:  "  语言选择",
		LangSelectPrompt: "  请选择您的语言：",
		LangSelectHelp:   "  ↑/↓ 或 j/k：导航   Enter：选择   Esc：取消",

		NoConnections: "  没有已保存的连接。",
		PressNToAdd:   "  按 'n' 添加新连接。",
		HelpList:      "  n:新建  e:编辑  d:删除  Enter:连接  x:断开  g:SSH密钥  l:语言  q:退出",

		ConnectingTitle: "  正在连接：%s",
		TryingAutoAuth:  "  正在尝试 SSH 代理和可用密钥...",
		PleaseWait:      "  请稍候",

		DeleteTitle:     "  删除连接？",
		DeleteConfirm:   "  确定要删除连接 '%s' 吗？\n\n",
		DeleteYesNo:     "  [j/y] 是   [n/Esc] 否",
		DeletedMsg:      "连接已删除！",
		DisconnectedMsg: "已断开：",

		FormTitleNew:    "新建连接",
		FormTitleEdit:   "编辑连接",
		LabelName:       "名称：",
		LabelHost:       "主机：",
		LabelPort:       "端口：",
		LabelUser:       "用户：",
		LabelTunnels:    "隧道端口（逗号分隔）：",
		PlaceholderName: "我的服务器",
		FormHelp:        "  Tab:下一字段  Enter:保存  Esc:取消",
		SavedMsg:        "连接已保存！",

		NoKeyFound:   "  未找到匹配的 SSH 密钥。请输入密码：",
		PlaceholderPW: "输入密码/密钥短语",
		AfterPWHint:  "  登录后将自动创建 SSH 密钥。",
		ConnectHelp:  "  Enter:连接  Esc:取消",
		ConnNotFound: "未找到连接",

		StatusTitle:     "  状态：%s",
		LabelServer:     "服务器：  ",
		LabelAuth:       "认证：    ",
		LabelStatus:     "状态：    ",
		StatusConn:      "已连接",
		StatusDisconn:   "已断开",
		LabelTunnel:     "隧道：",
		TunnelActive:    "活跃",
		TunnelErrPrefix: "错误：",
		StatusHelp:      "  t:终端  x:断开  Esc:返回",
		NoActiveConn:    "没有活跃连接",
		DiscoMsg:        "已断开",

		KeygenTitle:     "  生成 SSH 密钥（Ed25519）",
		LabelKeyPath:    "文件路径：",
		LabelPassphrase: "密钥短语（可选）：",
		PlaceholderPass: "空白 = 无密钥短语",
		KeyPathRequired: "文件路径不能为空",
		KeygenHelp:      "  Tab:下一字段  Enter:生成  Esc:取消",
		KeygenDoneTitle: "  SSH 密钥已生成！",
		KeyCreated:      "  密钥创建成功。",
		LabelPublicKey:  "公钥：",
		KeyAddToAuth:    "  将此公钥添加到目标服务器的 ~/.ssh/authorized_keys 中。",
		BackHelp:        "  Enter/Esc:返回",

		HostKeyWarning:  "  安全警告：SSH 主机密钥已更改！",
		HostKeyBoxTitle: "服务器的 SSH 密钥已更改！\n\n",
		HostKeyBoxHost:  "主机：%s\n\n",
		HostKeyReasons:  "可能原因：\n  - 服务器被重新安装（合法）\n  - 服务器密钥已更新（合法）\n  - 中间人攻击（危险！）\n\n",
		HostKeyCaution:  "仅在确认服务器密钥已更改的情况下继续。",
		HostKeyAskYesNo: "  删除旧主机密钥并重新连接？\n\n  [j/y] 是，我知道我在做什么   [n/Esc] 否，取消",
		UnknownHost:     "（未知）",

		ErrPrefix:        "  错误：",
		ErrLoading:       "加载错误：",
		ErrTerminal:      "终端错误：",
		TerminalDone:     "终端会话已结束",
		ConnectedMsg:     "连接已建立！",
		KeyDeployFailed:  "密钥部署失败：",
		KeyDeployedMsg:   "SSH 密钥已部署！下次连接无需密码：%s",
		ConnErrPrefix:    "连接错误：",
		TunnelInfo:       " [隧道: %s]",
		ErrPortMustBeNum: "端口必须是数字",
		ErrTunnelPort:    "隧道端口 '%s' 不是有效数字",
	},

	// ===================== PORTUGUÊS =====================
	LangPortugues: {
		LangSelectTitle:  "  Seleção de idioma",
		LangSelectPrompt: "  Por favor selecione o seu idioma:",
		LangSelectHelp:   "  ↑/↓ ou j/k: Navegar   Enter: Selecionar   Esc: Cancelar",

		NoConnections: "  Nenhuma conexão salva.",
		PressNToAdd:   "  Pressione 'n' para adicionar uma nova conexão.",
		HelpList:      "  n:Novo  e:Editar  d:Excluir  Enter:Conectar  x:Desconectar  g:Chave SSH  l:Idioma  q:Sair",

		ConnectingTitle: "  Conectando a: %s",
		TryingAutoAuth:  "  Tentando agente SSH e chaves disponíveis...",
		PleaseWait:      "  Aguarde por favor",

		DeleteTitle:     "  Excluir conexão?",
		DeleteConfirm:   "  Realmente excluir a conexão '%s'?\n\n",
		DeleteYesNo:     "  [j/y] Sim   [n/Esc] Não",
		DeletedMsg:      "Conexão excluída!",
		DisconnectedMsg: "Desconectado: ",

		FormTitleNew:    "Nova conexão",
		FormTitleEdit:   "Editar conexão",
		LabelName:       "Nome:",
		LabelHost:       "Host:",
		LabelPort:       "Porta:",
		LabelUser:       "Usuário:",
		LabelTunnels:    "Portas de túnel (separadas por vírgula):",
		PlaceholderName: "Meu servidor",
		FormHelp:        "  Tab:Próximo campo  Enter:Salvar  Esc:Cancelar",
		SavedMsg:        "Conexão salva!",

		NoKeyFound:   "  Nenhuma chave SSH encontrada. Por favor insira a senha:",
		PlaceholderPW: "Inserir senha/frase-senha",
		AfterPWHint:  "  Após o login, uma chave SSH será criada automaticamente.",
		ConnectHelp:  "  Enter:Conectar  Esc:Cancelar",
		ConnNotFound: "Conexão não encontrada",

		StatusTitle:     "  Status: %s",
		LabelServer:     "Servidor:  ",
		LabelAuth:       "Auth:      ",
		LabelStatus:     "Status:    ",
		StatusConn:      "Conectado",
		StatusDisconn:   "Desconectado",
		LabelTunnel:     "Túnel:",
		TunnelActive:    "ativo",
		TunnelErrPrefix: "Erro: ",
		StatusHelp:      "  t:Terminal  x:Desconectar  Esc:Voltar",
		NoActiveConn:    "Sem conexão ativa",
		DiscoMsg:        "Desconectado",

		KeygenTitle:     "  Gerar chave SSH (Ed25519)",
		LabelKeyPath:    "Caminho do arquivo:",
		LabelPassphrase: "Frase-senha (opcional):",
		PlaceholderPass: "vazio = sem frase-senha",
		KeyPathRequired: "O caminho do arquivo não pode estar vazio",
		KeygenHelp:      "  Tab:Próximo campo  Enter:Gerar  Esc:Cancelar",
		KeygenDoneTitle: "  Chave SSH gerada!",
		KeyCreated:      "  Chave criada com sucesso.",
		LabelPublicKey:  "Chave pública:",
		KeyAddToAuth:    "  Adicione esta chave pública ao ~/.ssh/authorized_keys no servidor de destino.",
		BackHelp:        "  Enter/Esc:Voltar",

		HostKeyWarning:  "  AVISO DE SEGURANÇA: CHAVE SSH DO HOST ALTERADA!",
		HostKeyBoxTitle: "A chave SSH do servidor foi alterada!\n\n",
		HostKeyBoxHost:  "Host: %s\n\n",
		HostKeyReasons:  "POSSÍVEIS CAUSAS:\n  - O servidor foi reinstalado (legítimo)\n  - A chave do servidor foi renovada (legítimo)\n  - Ataque man-in-the-middle (PERIGOSO!)\n\n",
		HostKeyCaution:  "Prossiga somente se souber que a chave do servidor foi alterada.",
		HostKeyAskYesNo: "  Remover a chave antiga e reconectar?\n\n  [j/y] Sim, sei o que estou fazendo   [n/Esc] Não, cancelar",
		UnknownHost:     "(desconhecido)",

		ErrPrefix:        "  Erro: ",
		ErrLoading:       "Erro ao carregar: ",
		ErrTerminal:      "Erro de terminal: ",
		TerminalDone:     "Sessão de terminal encerrada",
		ConnectedMsg:     "Conexão estabelecida!",
		KeyDeployFailed:  "Implantação de chave falhou: ",
		KeyDeployedMsg:   "Chave SSH implantada! Próxima conexão sem senha: %s",
		ConnErrPrefix:    "Erro de conexão: ",
		TunnelInfo:       " [Túnel: %s]",
		ErrPortMustBeNum: "A porta deve ser um número",
		ErrTunnelPort:    "A porta de túnel '%s' não é um número válido",
	},

	// ===================== РУССКИЙ =====================
	LangRussian: {
		LangSelectTitle:  "  Выбор языка",
		LangSelectPrompt: "  Пожалуйста, выберите ваш язык:",
		LangSelectHelp:   "  ↑/↓ или j/k: Навигация   Enter: Выбрать   Esc: Отмена",

		NoConnections: "  Нет сохранённых подключений.",
		PressNToAdd:   "  Нажмите 'n' чтобы добавить новое подключение.",
		HelpList:      "  n:Новое  e:Изменить  d:Удалить  Enter:Подключить  x:Отключить  g:SSH-ключ  l:Язык  q:Выход",

		ConnectingTitle: "  Подключение к: %s",
		TryingAutoAuth:  "  Пробуем SSH-агент и доступные ключи...",
		PleaseWait:      "  Пожалуйста, подождите",

		DeleteTitle:     "  Удалить подключение?",
		DeleteConfirm:   "  Действительно удалить подключение '%s'?\n\n",
		DeleteYesNo:     "  [j/y] Да   [n/Esc] Нет",
		DeletedMsg:      "Подключение удалено!",
		DisconnectedMsg: "Отключено: ",

		FormTitleNew:    "Новое подключение",
		FormTitleEdit:   "Изменить подключение",
		LabelName:       "Имя:",
		LabelHost:       "Хост:",
		LabelPort:       "Порт:",
		LabelUser:       "Пользователь:",
		LabelTunnels:    "Порты туннеля (через запятую):",
		PlaceholderName: "Мой сервер",
		FormHelp:        "  Tab:Следующее поле  Enter:Сохранить  Esc:Отмена",
		SavedMsg:        "Подключение сохранено!",

		NoKeyFound:    "  SSH-ключ не найден. Пожалуйста, введите пароль:",
		PlaceholderPW: "Введите пароль/парольную фразу",
		AfterPWHint:   "  После входа SSH-ключ будет создан автоматически.",
		ConnectHelp:   "  Enter:Подключить  Esc:Отмена",
		ConnNotFound:  "Подключение не найдено",

		StatusTitle:     "  Статус: %s",
		LabelServer:     "Сервер:        ",
		LabelAuth:       "Аутентификация:",
		LabelStatus:     "Состояние:     ",
		StatusConn:      "Подключено",
		StatusDisconn:   "Отключено",
		LabelTunnel:     "Туннель:",
		TunnelActive:    "активен",
		TunnelErrPrefix: "Ошибка: ",
		StatusHelp:      "  t:Терминал  x:Отключить  Esc:Назад",
		NoActiveConn:    "Нет активного подключения",
		DiscoMsg:        "Отключено",

		KeygenTitle:     "  Создать SSH-ключ (Ed25519)",
		LabelKeyPath:    "Путь к файлу:",
		LabelPassphrase: "Парольная фраза (необязательно):",
		PlaceholderPass: "пусто = без парольной фразы",
		KeyPathRequired: "Путь к файлу не может быть пустым",
		KeygenHelp:      "  Tab:Следующее поле  Enter:Создать  Esc:Отмена",
		KeygenDoneTitle: "  SSH-ключ создан!",
		KeyCreated:      "  Ключ успешно создан.",
		LabelPublicKey:  "Публичный ключ:",
		KeyAddToAuth:    "  Добавьте этот публичный ключ в ~/.ssh/authorized_keys на целевом сервере.",
		BackHelp:        "  Enter/Esc:Назад",

		HostKeyWarning:  "  ПРЕДУПРЕЖДЕНИЕ БЕЗОПАСНОСТИ: SSH-КЛЮЧ ХОСТА ИЗМЕНЁН!",
		HostKeyBoxTitle: "SSH-ключ сервера изменился!\n\n",
		HostKeyBoxHost:  "Хост: %s\n\n",
		HostKeyReasons:  "ВОЗМОЖНЫЕ ПРИЧИНЫ:\n  - Сервер был переустановлен (законно)\n  - Ключ сервера был обновлён (законно)\n  - Атака «человек посередине» (ОПАСНО!)\n\n",
		HostKeyCaution:  "Продолжайте только если вы знаете, что ключ сервера изменился.",
		HostKeyAskYesNo: "  Удалить старый ключ хоста и переподключиться?\n\n  [j/y] Да, я знаю что делаю   [n/Esc] Нет, отмена",
		UnknownHost:     "(неизвестно)",

		ErrPrefix:        "  Ошибка: ",
		ErrLoading:       "Ошибка загрузки: ",
		ErrTerminal:      "Ошибка терминала: ",
		TerminalDone:     "Сеанс терминала завершён",
		ConnectedMsg:     "Подключение установлено!",
		KeyDeployFailed:  "Развёртывание ключа не удалось: ",
		KeyDeployedMsg:   "SSH-ключ развёрнут! Следующее подключение без пароля: %s",
		ConnErrPrefix:    "Ошибка подключения: ",
		TunnelInfo:       " [Туннель: %s]",
		ErrPortMustBeNum: "Порт должен быть числом",
		ErrTunnelPort:    "Порт туннеля '%s' не является допустимым числом",
	},

	// ===================== BAHASA INDONESIA =====================
	LangIndonesian: {
		LangSelectTitle:  "  Pilihan Bahasa",
		LangSelectPrompt: "  Silakan pilih bahasa Anda:",
		LangSelectHelp:   "  ↑/↓ atau j/k: Navigasi   Enter: Pilih   Esc: Batal",

		NoConnections: "  Tidak ada koneksi yang tersimpan.",
		PressNToAdd:   "  Tekan 'n' untuk menambahkan koneksi baru.",
		HelpList:      "  n:Baru  e:Edit  d:Hapus  Enter:Hubungkan  x:Putuskan  g:Kunci SSH  l:Bahasa  q:Keluar",

		ConnectingTitle: "  Menghubungkan ke: %s",
		TryingAutoAuth:  "  Mencoba agen SSH dan kunci yang tersedia...",
		PleaseWait:      "  Harap tunggu",

		DeleteTitle:     "  Hapus koneksi?",
		DeleteConfirm:   "  Benar-benar hapus koneksi '%s'?\n\n",
		DeleteYesNo:     "  [j/y] Ya   [n/Esc] Tidak",
		DeletedMsg:      "Koneksi dihapus!",
		DisconnectedMsg: "Terputus: ",

		FormTitleNew:    "Koneksi Baru",
		FormTitleEdit:   "Edit Koneksi",
		LabelName:       "Nama:",
		LabelHost:       "Host:",
		LabelPort:       "Port:",
		LabelUser:       "Pengguna:",
		LabelTunnels:    "Port terowongan (dipisah koma):",
		PlaceholderName: "Server saya",
		FormHelp:        "  Tab:Bidang berikutnya  Enter:Simpan  Esc:Batal",
		SavedMsg:        "Koneksi disimpan!",

		NoKeyFound:    "  Kunci SSH tidak ditemukan. Masukkan kata sandi:",
		PlaceholderPW: "Masukkan kata sandi/frasa sandi",
		AfterPWHint:   "  Setelah masuk, kunci SSH akan dibuat secara otomatis.",
		ConnectHelp:   "  Enter:Hubungkan  Esc:Batal",
		ConnNotFound:  "Koneksi tidak ditemukan",

		StatusTitle:     "  Status: %s",
		LabelServer:     "Server:  ",
		LabelAuth:       "Auth:    ",
		LabelStatus:     "Status:  ",
		StatusConn:      "Terhubung",
		StatusDisconn:   "Terputus",
		LabelTunnel:     "Terowongan:",
		TunnelActive:    "aktif",
		TunnelErrPrefix: "Kesalahan: ",
		StatusHelp:      "  t:Terminal  x:Putuskan  Esc:Kembali",
		NoActiveConn:    "Tidak ada koneksi aktif",
		DiscoMsg:        "Terputus",

		KeygenTitle:     "  Buat Kunci SSH (Ed25519)",
		LabelKeyPath:    "Jalur file:",
		LabelPassphrase: "Frasa sandi (opsional):",
		PlaceholderPass: "kosong = tanpa frasa sandi",
		KeyPathRequired: "Jalur file tidak boleh kosong",
		KeygenHelp:      "  Tab:Bidang berikutnya  Enter:Buat  Esc:Batal",
		KeygenDoneTitle: "  Kunci SSH dibuat!",
		KeyCreated:      "  Kunci berhasil dibuat.",
		LabelPublicKey:  "Kunci publik:",
		KeyAddToAuth:    "  Tambahkan kunci publik ini ke ~/.ssh/authorized_keys di server tujuan.",
		BackHelp:        "  Enter/Esc:Kembali",

		HostKeyWarning:  "  PERINGATAN KEAMANAN: KUNCI SSH HOST BERUBAH!",
		HostKeyBoxTitle: "Kunci SSH server telah berubah!\n\n",
		HostKeyBoxHost:  "Host: %s\n\n",
		HostKeyReasons:  "KEMUNGKINAN PENYEBAB:\n  - Server diinstal ulang (sah)\n  - Kunci server diperbarui (sah)\n  - Serangan man-in-the-middle (BERBAHAYA!)\n\n",
		HostKeyCaution:  "Lanjutkan hanya jika Anda tahu kunci server telah berubah.",
		HostKeyAskYesNo: "  Hapus kunci host lama dan hubungkan kembali?\n\n  [j/y] Ya, saya tahu apa yang saya lakukan   [n/Esc] Tidak, batal",
		UnknownHost:     "(tidak diketahui)",

		ErrPrefix:        "  Kesalahan: ",
		ErrLoading:       "Kesalahan memuat: ",
		ErrTerminal:      "Kesalahan terminal: ",
		TerminalDone:     "Sesi terminal berakhir",
		ConnectedMsg:     "Koneksi berhasil!",
		KeyDeployFailed:  "Penerapan kunci gagal: ",
		KeyDeployedMsg:   "Kunci SSH diterapkan! Koneksi berikutnya tanpa kata sandi: %s",
		ConnErrPrefix:    "Kesalahan koneksi: ",
		TunnelInfo:       " [Terowongan: %s]",
		ErrPortMustBeNum: "Port harus berupa angka",
		ErrTunnelPort:    "Port terowongan '%s' bukan angka yang valid",
	},

	// ===================== हिन्दी =====================
	LangHindi: {
		LangSelectTitle:  "  भाषा चयन",
		LangSelectPrompt: "  कृपया अपनी भाषा चुनें:",
		LangSelectHelp:   "  ↑/↓ या j/k: नेविगेट   Enter: चुनें   Esc: रद्द करें",

		NoConnections: "  कोई कनेक्शन सहेजा नहीं गया।",
		PressNToAdd:   "  नया कनेक्शन जोड़ने के लिए 'n' दबाएँ।",
		HelpList:      "  n:नया  e:संपादित  d:हटाएँ  Enter:कनेक्ट  x:डिस्कनेक्ट  g:SSH-कुंजी  l:भाषा  q:बाहर",

		ConnectingTitle: "  कनेक्ट हो रहा है: %s",
		TryingAutoAuth:  "  SSH एजेंट और उपलब्ध कुंजियाँ आज़मा रहे हैं...",
		PleaseWait:      "  कृपया प्रतीक्षा करें",

		DeleteTitle:     "  कनेक्शन हटाएँ?",
		DeleteConfirm:   "  क्या वाकई कनेक्शन '%s' हटाना है?\n\n",
		DeleteYesNo:     "  [j/y] हाँ   [n/Esc] नहीं",
		DeletedMsg:      "कनेक्शन हटा दिया गया!",
		DisconnectedMsg: "डिस्कनेक्ट: ",

		FormTitleNew:    "नया कनेक्शन",
		FormTitleEdit:   "कनेक्शन संपादित करें",
		LabelName:       "नाम:",
		LabelHost:       "होस्ट:",
		LabelPort:       "पोर्ट:",
		LabelUser:       "उपयोगकर्ता:",
		LabelTunnels:    "टनल पोर्ट (अल्पविराम से अलग):",
		PlaceholderName: "मेरा सर्वर",
		FormHelp:        "  Tab:अगला फ़ील्ड  Enter:सहेजें  Esc:रद्द करें",
		SavedMsg:        "कनेक्शन सहेजा गया!",

		NoKeyFound:    "  कोई SSH कुंजी नहीं मिली। कृपया पासवर्ड दर्ज करें:",
		PlaceholderPW: "पासवर्ड/पासफ़्रेज़ दर्ज करें",
		AfterPWHint:   "  लॉगिन के बाद SSH कुंजी स्वचालित रूप से बनाई जाएगी।",
		ConnectHelp:   "  Enter:कनेक्ट  Esc:रद्द करें",
		ConnNotFound:  "कनेक्शन नहीं मिला",

		StatusTitle:     "  स्थिति: %s",
		LabelServer:     "सर्वर:       ",
		LabelAuth:       "प्रमाणीकरण: ",
		LabelStatus:     "स्थिति:      ",
		StatusConn:      "कनेक्टेड",
		StatusDisconn:   "डिस्कनेक्टेड",
		LabelTunnel:     "टनल:",
		TunnelActive:    "सक्रिय",
		TunnelErrPrefix: "त्रुटि: ",
		StatusHelp:      "  t:टर्मिनल  x:डिस्कनेक्ट  Esc:वापस",
		NoActiveConn:    "कोई सक्रिय कनेक्शन नहीं",
		DiscoMsg:        "डिस्कनेक्ट हो गया",

		KeygenTitle:     "  SSH कुंजी बनाएँ (Ed25519)",
		LabelKeyPath:    "फ़ाइल पथ:",
		LabelPassphrase: "पासफ़्रेज़ (वैकल्पिक):",
		PlaceholderPass: "खाली = कोई पासफ़्रेज़ नहीं",
		KeyPathRequired: "फ़ाइल पथ खाली नहीं हो सकता",
		KeygenHelp:      "  Tab:अगला फ़ील्ड  Enter:बनाएँ  Esc:रद्द करें",
		KeygenDoneTitle: "  SSH कुंजी बनाई गई!",
		KeyCreated:      "  कुंजी सफलतापूर्वक बनाई गई।",
		LabelPublicKey:  "सार्वजनिक कुंजी:",
		KeyAddToAuth:    "  इस सार्वजनिक कुंजी को लक्ष्य सर्वर पर ~/.ssh/authorized_keys में जोड़ें।",
		BackHelp:        "  Enter/Esc:वापस",

		HostKeyWarning:  "  सुरक्षा चेतावनी: SSH होस्ट कुंजी बदल गई!",
		HostKeyBoxTitle: "सर्वर की SSH कुंजी बदल गई है!\n\n",
		HostKeyBoxHost:  "होस्ट: %s\n\n",
		HostKeyReasons:  "संभावित कारण:\n  - सर्वर पुनः स्थापित किया गया (वैध)\n  - सर्वर कुंजी नवीनीकृत की गई (वैध)\n  - मैन-इन-द-मिडल हमला (खतरनाक!)\n\n",
		HostKeyCaution:  "केवल तभी आगे बढ़ें जब आप जानते हों कि सर्वर कुंजी बदली है।",
		HostKeyAskYesNo: "  पुरानी होस्ट कुंजी हटाएँ और फिर से कनेक्ट करें?\n\n  [j/y] हाँ, मुझे पता है   [n/Esc] नहीं, रद्द करें",
		UnknownHost:     "(अज्ञात)",

		ErrPrefix:        "  त्रुटि: ",
		ErrLoading:       "लोड त्रुटि: ",
		ErrTerminal:      "टर्मिनल त्रुटि: ",
		TerminalDone:     "टर्मिनल सत्र समाप्त",
		ConnectedMsg:     "कनेक्शन स्थापित!",
		KeyDeployFailed:  "कुंजी तैनाती विफल: ",
		KeyDeployedMsg:   "SSH कुंजी तैनात! अगला कनेक्शन बिना पासवर्ड के: %s",
		ConnErrPrefix:    "कनेक्शन त्रुटि: ",
		TunnelInfo:       " [टनल: %s]",
		ErrPortMustBeNum: "पोर्ट एक संख्या होनी चाहिए",
		ErrTunnelPort:    "टनल पोर्ट '%s' एक वैध संख्या नहीं है",
	},

	// ===================== বাংলা =====================
	LangBengali: {
		LangSelectTitle:  "  ভাষা নির্বাচন",
		LangSelectPrompt: "  অনুগ্রহ করে আপনার ভাষা নির্বাচন করুন:",
		LangSelectHelp:   "  ↑/↓ বা j/k: নেভিগেট   Enter: নির্বাচন   Esc: বাতিল",

		NoConnections: "  কোনো সংযোগ সংরক্ষিত নেই।",
		PressNToAdd:   "  নতুন সংযোগ যোগ করতে 'n' চাপুন।",
		HelpList:      "  n:নতুন  e:সম্পাদনা  d:মুছুন  Enter:সংযুক্ত  x:বিচ্ছিন্ন  g:SSH-কী  l:ভাষা  q:বের হন",

		ConnectingTitle: "  সংযোগ হচ্ছে: %s",
		TryingAutoAuth:  "  SSH এজেন্ট এবং উপলব্ধ কী চেষ্টা করা হচ্ছে...",
		PleaseWait:      "  অনুগ্রহ করে অপেক্ষা করুন",

		DeleteTitle:     "  সংযোগ মুছবেন?",
		DeleteConfirm:   "  সত্যিই '%s' সংযোগটি মুছবেন?\n\n",
		DeleteYesNo:     "  [j/y] হ্যাঁ   [n/Esc] না",
		DeletedMsg:      "সংযোগ মুছে ফেলা হয়েছে!",
		DisconnectedMsg: "বিচ্ছিন্ন: ",

		FormTitleNew:    "নতুন সংযোগ",
		FormTitleEdit:   "সংযোগ সম্পাদনা",
		LabelName:       "নাম:",
		LabelHost:       "হোস্ট:",
		LabelPort:       "পোর্ট:",
		LabelUser:       "ব্যবহারকারী:",
		LabelTunnels:    "টানেল পোর্ট (কমা দিয়ে আলাদা):",
		PlaceholderName: "আমার সার্ভার",
		FormHelp:        "  Tab:পরবর্তী ক্ষেত্র  Enter:সংরক্ষণ  Esc:বাতিল",
		SavedMsg:        "সংযোগ সংরক্ষিত হয়েছে!",

		NoKeyFound:    "  কোনো SSH কী পাওয়া যায়নি। পাসওয়ার্ড লিখুন:",
		PlaceholderPW: "পাসওয়ার্ড/পাসফ্রেজ লিখুন",
		AfterPWHint:   "  লগইনের পরে SSH কী স্বয়ংক্রিয়ভাবে তৈরি হবে।",
		ConnectHelp:   "  Enter:সংযুক্ত  Esc:বাতিল",
		ConnNotFound:  "সংযোগ পাওয়া যায়নি",

		StatusTitle:     "  অবস্থা: %s",
		LabelServer:     "সার্ভার:    ",
		LabelAuth:       "প্রমাণীকরণ:",
		LabelStatus:     "অবস্থা:     ",
		StatusConn:      "সংযুক্ত",
		StatusDisconn:   "বিচ্ছিন্ন",
		LabelTunnel:     "টানেল:",
		TunnelActive:    "সক্রিয়",
		TunnelErrPrefix: "ত্রুটি: ",
		StatusHelp:      "  t:টার্মিনাল  x:বিচ্ছিন্ন  Esc:ফিরে যান",
		NoActiveConn:    "কোনো সক্রিয় সংযোগ নেই",
		DiscoMsg:        "বিচ্ছিন্ন হয়েছে",

		KeygenTitle:     "  SSH কী তৈরি করুন (Ed25519)",
		LabelKeyPath:    "ফাইলের পথ:",
		LabelPassphrase: "পাসফ্রেজ (ঐচ্ছিক):",
		PlaceholderPass: "খালি = পাসফ্রেজ নেই",
		KeyPathRequired: "ফাইলের পথ খালি হতে পারে না",
		KeygenHelp:      "  Tab:পরবর্তী ক্ষেত্র  Enter:তৈরি  Esc:বাতিল",
		KeygenDoneTitle: "  SSH কী তৈরি হয়েছে!",
		KeyCreated:      "  কী সফলভাবে তৈরি হয়েছে।",
		LabelPublicKey:  "পাবলিক কী:",
		KeyAddToAuth:    "  এই পাবলিক কীটি লক্ষ্য সার্ভারের ~/.ssh/authorized_keys-এ যোগ করুন।",
		BackHelp:        "  Enter/Esc:ফিরে যান",

		HostKeyWarning:  "  নিরাপত্তা সতর্কতা: SSH হোস্ট কী পরিবর্তিত হয়েছে!",
		HostKeyBoxTitle: "সার্ভারের SSH কী পরিবর্তিত হয়েছে!\n\n",
		HostKeyBoxHost:  "হোস্ট: %s\n\n",
		HostKeyReasons:  "সম্ভাব্য কারণ:\n  - সার্ভার পুনরায় ইনস্টল করা হয়েছে (বৈধ)\n  - সার্ভার কী নবায়ন করা হয়েছে (বৈধ)\n  - ম্যান-ইন-দ্য-মিডল আক্রমণ (বিপজ্জনক!)\n\n",
		HostKeyCaution:  "শুধুমাত্র তখনই এগিয়ে যান যখন আপনি জানেন যে সার্ভার কী পরিবর্তিত হয়েছে।",
		HostKeyAskYesNo: "  পুরানো হোস্ট কী মুছে পুনরায় সংযুক্ত হবেন?\n\n  [j/y] হ্যাঁ, আমি জানি   [n/Esc] না, বাতিল",
		UnknownHost:     "(অজানা)",

		ErrPrefix:        "  ত্রুটি: ",
		ErrLoading:       "লোড ত্রুটি: ",
		ErrTerminal:      "টার্মিনাল ত্রুটি: ",
		TerminalDone:     "টার্মিনাল সেশন শেষ হয়েছে",
		ConnectedMsg:     "সংযোগ স্থাপিত!",
		KeyDeployFailed:  "কী স্থাপনা ব্যর্থ: ",
		KeyDeployedMsg:   "SSH কী স্থাপিত! পরবর্তী সংযোগ পাসওয়ার্ড ছাড়া: %s",
		ConnErrPrefix:    "সংযোগ ত্রুটি: ",
		TunnelInfo:       " [টানেল: %s]",
		ErrPortMustBeNum: "পোর্ট একটি সংখ্যা হতে হবে",
		ErrTunnelPort:    "টানেল পোর্ট '%s' একটি বৈধ সংখ্যা নয়",
	},

	// ===================== اردو =====================
	LangUrdu: {
		LangSelectTitle:  "  زبان کا انتخاب",
		LangSelectPrompt: "  براہ کرم اپنی زبان منتخب کریں:",
		LangSelectHelp:   "  ↑/↓ یا j/k: نیویگیٹ   Enter: منتخب   Esc: منسوخ",

		NoConnections: "  کوئی کنکشن محفوظ نہیں۔",
		PressNToAdd:   "  نیا کنکشن شامل کرنے کے لیے 'n' دبائیں۔",
		HelpList:      "  n:نیا  e:ترمیم  d:حذف  Enter:کنکٹ  x:منقطع  g:SSH-کلید  l:زبان  q:خروج",

		ConnectingTitle: "  کنکٹ ہو رہا ہے: %s",
		TryingAutoAuth:  "  SSH ایجنٹ اور دستیاب کلیدیں آزما رہے ہیں...",
		PleaseWait:      "  براہ کرم انتظار کریں",

		DeleteTitle:     "  کنکشن حذف کریں؟",
		DeleteConfirm:   "  کیا واقعی کنکشن '%s' حذف کرنا ہے؟\n\n",
		DeleteYesNo:     "  [j/y] ہاں   [n/Esc] نہیں",
		DeletedMsg:      "کنکشن حذف ہو گیا!",
		DisconnectedMsg: "منقطع: ",

		FormTitleNew:    "نیا کنکشن",
		FormTitleEdit:   "کنکشن میں ترمیم",
		LabelName:       "نام:",
		LabelHost:       "ہوسٹ:",
		LabelPort:       "پورٹ:",
		LabelUser:       "صارف:",
		LabelTunnels:    "ٹنل پورٹس (کاما سے الگ):",
		PlaceholderName: "میرا سرور",
		FormHelp:        "  Tab:اگلا خانہ  Enter:محفوظ  Esc:منسوخ",
		SavedMsg:        "کنکشن محفوظ ہو گیا!",

		NoKeyFound:    "  کوئی SSH کلید نہیں ملی۔ پاس ورڈ درج کریں:",
		PlaceholderPW: "پاس ورڈ/پاس فریز درج کریں",
		AfterPWHint:   "  لاگ ان کے بعد SSH کلید خودکار بنائی جائے گی۔",
		ConnectHelp:   "  Enter:کنکٹ  Esc:منسوخ",
		ConnNotFound:  "کنکشن نہیں ملا",

		StatusTitle:     "  حالت: %s",
		LabelServer:     "سرور:     ",
		LabelAuth:       "توثیق:    ",
		LabelStatus:     "حالت:     ",
		StatusConn:      "متصل",
		StatusDisconn:   "منقطع",
		LabelTunnel:     "ٹنل:",
		TunnelActive:    "فعال",
		TunnelErrPrefix: "خطا: ",
		StatusHelp:      "  t:ٹرمینل  x:منقطع  Esc:واپس",
		NoActiveConn:    "کوئی فعال کنکشن نہیں",
		DiscoMsg:        "منقطع ہو گیا",

		KeygenTitle:     "  SSH کلید بنائیں (Ed25519)",
		LabelKeyPath:    "فائل کا راستہ:",
		LabelPassphrase: "پاس فریز (اختیاری):",
		PlaceholderPass: "خالی = بغیر پاس فریز",
		KeyPathRequired: "فائل کا راستہ خالی نہیں ہو سکتا",
		KeygenHelp:      "  Tab:اگلا خانہ  Enter:بنائیں  Esc:منسوخ",
		KeygenDoneTitle: "  SSH کلید بن گئی!",
		KeyCreated:      "  کلید کامیابی سے بنائی گئی۔",
		LabelPublicKey:  "عوامی کلید:",
		KeyAddToAuth:    "  اس عوامی کلید کو ہدف سرور پر ~/.ssh/authorized_keys میں شامل کریں۔",
		BackHelp:        "  Enter/Esc:واپس",

		HostKeyWarning:  "  سیکیورٹی انتباہ: SSH ہوسٹ کلید بدل گئی!",
		HostKeyBoxTitle: "سرور کی SSH کلید بدل گئی ہے!\n\n",
		HostKeyBoxHost:  "ہوسٹ: %s\n\n",
		HostKeyReasons:  "ممکنہ وجوہات:\n  - سرور دوبارہ انسٹال کیا گیا (جائز)\n  - سرور کلید تجدید ہوئی (جائز)\n  - مین-ان-دا-مڈل حملہ (خطرناک!)\n\n",
		HostKeyCaution:  "صرف اسی صورت آگے بڑھیں جب آپ جانتے ہوں کہ سرور کلید بدلی ہے۔",
		HostKeyAskYesNo: "  پرانی ہوسٹ کلید حذف کریں اور دوبارہ کنکٹ کریں؟\n\n  [j/y] ہاں، مجھے معلوم ہے   [n/Esc] نہیں، منسوخ",
		UnknownHost:     "(نامعلوم)",

		ErrPrefix:        "  خطا: ",
		ErrLoading:       "لوڈ خطا: ",
		ErrTerminal:      "ٹرمینل خطا: ",
		TerminalDone:     "ٹرمینل سیشن ختم ہوا",
		ConnectedMsg:     "کنکشن قائم ہو گیا!",
		KeyDeployFailed:  "کلید تعیناتی ناکام: ",
		KeyDeployedMsg:   "SSH کلید تعینات! اگلا کنکشن بغیر پاس ورڈ: %s",
		ConnErrPrefix:    "کنکشن خطا: ",
		TunnelInfo:       " [ٹنل: %s]",
		ErrPortMustBeNum: "پورٹ ایک عدد ہونا چاہیے",
		ErrTunnelPort:    "ٹنل پورٹ '%s' ایک درست عدد نہیں ہے",
	},

	// ===================== العربية =====================
	LangArabic: {
		LangSelectTitle:  "  اختيار اللغة",
		LangSelectPrompt: "  يرجى اختيار لغتك:",
		LangSelectHelp:   "  ↑/↓ أو j/k: تنقل   Enter: اختيار   Esc: إلغاء",

		NoConnections: "  لا توجد اتصالات محفوظة.",
		PressNToAdd:   "  اضغط 'n' لإضافة اتصال جديد.",
		HelpList:      "  n:جديد  e:تعديل  d:حذف  Enter:اتصال  x:قطع  g:مفتاح SSH  l:لغة  q:خروج",

		ConnectingTitle: "  جارٍ الاتصال بـ: %s",
		TryingAutoAuth:  "  جارٍ تجربة عميل SSH والمفاتيح المتاحة...",
		PleaseWait:      "  يرجى الانتظار",

		DeleteTitle:     "  حذف الاتصال؟",
		DeleteConfirm:   "  هل تريد حقاً حذف الاتصال '%s'؟\n\n",
		DeleteYesNo:     "  [j/y] نعم   [n/Esc] لا",
		DeletedMsg:      "تم حذف الاتصال!",
		DisconnectedMsg: "تم القطع: ",

		FormTitleNew:    "اتصال جديد",
		FormTitleEdit:   "تعديل الاتصال",
		LabelName:       "الاسم:",
		LabelHost:       "المضيف:",
		LabelPort:       "المنفذ:",
		LabelUser:       "المستخدم:",
		LabelTunnels:    "منافذ النفق (مفصولة بفاصلة):",
		PlaceholderName: "خادمي",
		FormHelp:        "  Tab:الحقل التالي  Enter:حفظ  Esc:إلغاء",
		SavedMsg:        "تم حفظ الاتصال!",

		NoKeyFound:    "  لم يُعثر على مفتاح SSH. يرجى إدخال كلمة المرور:",
		PlaceholderPW: "أدخل كلمة المرور/عبارة المرور",
		AfterPWHint:   "  بعد تسجيل الدخول، سيُنشأ مفتاح SSH تلقائياً.",
		ConnectHelp:   "  Enter:اتصال  Esc:إلغاء",
		ConnNotFound:  "الاتصال غير موجود",

		StatusTitle:     "  الحالة: %s",
		LabelServer:     "الخادم:    ",
		LabelAuth:       "التوثيق:   ",
		LabelStatus:     "الحالة:    ",
		StatusConn:      "متصل",
		StatusDisconn:   "منقطع",
		LabelTunnel:     "النفق:",
		TunnelActive:    "نشط",
		TunnelErrPrefix: "خطأ: ",
		StatusHelp:      "  t:طرفية  x:قطع  Esc:رجوع",
		NoActiveConn:    "لا يوجد اتصال نشط",
		DiscoMsg:        "تم القطع",

		KeygenTitle:     "  إنشاء مفتاح SSH (Ed25519)",
		LabelKeyPath:    "مسار الملف:",
		LabelPassphrase: "عبارة المرور (اختياري):",
		PlaceholderPass: "فارغ = بدون عبارة مرور",
		KeyPathRequired: "لا يمكن أن يكون مسار الملف فارغاً",
		KeygenHelp:      "  Tab:الحقل التالي  Enter:إنشاء  Esc:إلغاء",
		KeygenDoneTitle: "  تم إنشاء مفتاح SSH!",
		KeyCreated:      "  تم إنشاء المفتاح بنجاح.",
		LabelPublicKey:  "المفتاح العام:",
		KeyAddToAuth:    "  أضف هذا المفتاح العام إلى ~/.ssh/authorized_keys على الخادم الهدف.",
		BackHelp:        "  Enter/Esc:رجوع",

		HostKeyWarning:  "  تحذير أمني: تم تغيير مفتاح SSH للمضيف!",
		HostKeyBoxTitle: "تغيّر مفتاح SSH للخادم!\n\n",
		HostKeyBoxHost:  "المضيف: %s\n\n",
		HostKeyReasons:  "الأسباب المحتملة:\n  - تمت إعادة تثبيت الخادم (مشروع)\n  - تم تجديد مفتاح الخادم (مشروع)\n  - هجوم الوسيط (خطير!)\n\n",
		HostKeyCaution:  "تابع فقط إذا كنت تعلم أن مفتاح الخادم قد تغيّر.",
		HostKeyAskYesNo: "  حذف المفتاح القديم وإعادة الاتصال؟\n\n  [j/y] نعم، أعلم ما أفعله   [n/Esc] لا، إلغاء",
		UnknownHost:     "(غير معروف)",

		ErrPrefix:        "  خطأ: ",
		ErrLoading:       "خطأ في التحميل: ",
		ErrTerminal:      "خطأ في الطرفية: ",
		TerminalDone:     "انتهت جلسة الطرفية",
		ConnectedMsg:     "تم إنشاء الاتصال!",
		KeyDeployFailed:  "فشل نشر المفتاح: ",
		KeyDeployedMsg:   "تم نشر مفتاح SSH! الاتصال التالي بدون كلمة مرور: %s",
		ConnErrPrefix:    "خطأ في الاتصال: ",
		TunnelInfo:       " [نفق: %s]",
		ErrPortMustBeNum: "يجب أن يكون المنفذ رقماً",
		ErrTunnelPort:    "منفذ النفق '%s' ليس رقماً صالحاً",
	},
}
