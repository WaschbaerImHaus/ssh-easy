; ssh-easy Windows Installer
; Built with NSIS (Nullsoft Scriptable Install System)
;
; Installs ssh-easy with a Start Menu entry and optional Desktop shortcut.
; Automatically detects and silently removes any previous installation.
; Supports both x64 and ARM64 binaries via /DARCH= define.
;
; Usage:
;   makensis /DARCH=amd64 /DVERSION=0.15 /DBUILD=15 ssh-easy-setup.nsi
;   makensis /DARCH=arm64 /DVERSION=0.15 /DBUILD=15 ssh-easy-setup.nsi
;
; @author Kurt Ingwer
; @date   2026-03-14

;----------------------------------------------------------------------
; Default values (overridden by command-line /D defines)
;----------------------------------------------------------------------
!ifndef PRODUCT_VERSION
  !define PRODUCT_VERSION "0.15.0"
!endif
!ifndef PRODUCT_BUILD
  !define PRODUCT_BUILD "15"
!endif
!ifndef ARCH
  !define ARCH "amd64"
!endif

;----------------------------------------------------------------------
; Derived names
;----------------------------------------------------------------------
!if "${ARCH}" == "arm64"
  !define BINARY_FILE "build\ssh-easy-windows-arm64.exe"
  !define INSTALLER_NAME "build\ssh-easy-setup-arm64.exe"
  !define ARCH_LABEL "ARM64"
!else
  !define BINARY_FILE "build\ssh-easy-windows-amd64.exe"
  !define INSTALLER_NAME "build\ssh-easy-setup-amd64.exe"
  !define ARCH_LABEL "x64"
!endif

;----------------------------------------------------------------------
; General settings
;----------------------------------------------------------------------

Name "ssh-easy"
OutFile "${INSTALLER_NAME}"

; Default install directory (from registry if already installed)
InstallDir "$PROGRAMFILES64\ssh-easy"
InstallDirRegKey HKLM "Software\ssh-easy" "InstallDir"

; Administrator rights required (Program Files + HKLM registry)
RequestExecutionLevel admin

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"

;----------------------------------------------------------------------
; MUI appearance
;----------------------------------------------------------------------

!define MUI_ICON "assets\icon.ico"
!define MUI_UNICON "assets\icon.ico"

!define MUI_WELCOMEPAGE_TITLE "Welcome to ssh-easy Setup"
!define MUI_WELCOMEPAGE_TEXT "This wizard will install ssh-easy ${PRODUCT_VERSION} (Build ${PRODUCT_BUILD}) for Windows ${ARCH_LABEL}.$\n$\nssh-easy is an SSH connection manager with a terminal UI.$\nIt supports automatic authentication via SSH agent, key files and password.$\n$\nAny existing installation will be replaced automatically.$\n$\nClick Next to continue."

!define MUI_FINISHPAGE_RUN "$INSTDIR\ssh-easy.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Launch ssh-easy now"

; Installer pages
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
Page custom DesktopIconPage DesktopIconLeave
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

; Uninstaller pages
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; Languages (English first = default)
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "German"

;----------------------------------------------------------------------
; Variables
;----------------------------------------------------------------------

Var DesktopIconCheckbox
Var DesktopIconState

;----------------------------------------------------------------------
; Optional Desktop shortcut page
;----------------------------------------------------------------------

Function DesktopIconPage
  nsDialogs::Create 1018
  Pop $0

  ${NSD_CreateLabel} 0 0 100% 24u "Additional tasks:"
  Pop $0

  ${NSD_CreateCheckbox} 10u 30u 100% 14u "Create Desktop shortcut"
  Pop $DesktopIconCheckbox
  ${NSD_SetState} $DesktopIconCheckbox ${BST_UNCHECKED}

  nsDialogs::Show
FunctionEnd

Function DesktopIconLeave
  ${NSD_GetState} $DesktopIconCheckbox $DesktopIconState
FunctionEnd

;----------------------------------------------------------------------
; Detect and silently remove any previous installation
;----------------------------------------------------------------------

Function .onInit
  ; $R0 = UninstallString aus der Registry (bereits gequotet: "C:\...\Uninstall.exe")
  ReadRegStr $R0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" "UninstallString"
  ${If} $R0 != ""
    ; WICHTIG: $R0 ist bereits mit Anführungszeichen gespeichert, daher KEINE
    ; zusätzlichen Quotes und kein _?= (Pfad könnte von $INSTDIR abweichen).
    ; /S = silent, NSIS kopiert den Deinstaller selbst in ein Temp-Verzeichnis.
    ExecWait '$R0 /S'
    ; Warten bis der Deinstaller vollständig abgeschlossen ist
    Sleep 1500
  ${EndIf}
FunctionEnd

;----------------------------------------------------------------------
; Main install section
;----------------------------------------------------------------------

Section "ssh-easy (required)" SecMain
  SectionIn RO

  SetOutPath "$INSTDIR"

  ; Alte Programmdatei entfernen bevor die neue kopiert wird (Fallback falls
  ; der Deinstaller in .onInit die Datei nicht entfernt hat, z.B. wenn das
  ; Programm noch läuft und die EXE gesperrt ist).
  Delete "$INSTDIR\ssh-easy.exe"

  ; Binärdatei installieren und umbenennen
  File "${BINARY_FILE}"
  !if "${ARCH}" == "arm64"
    Rename "$INSTDIR\ssh-easy-windows-arm64.exe" "$INSTDIR\ssh-easy.exe"
  !else
    Rename "$INSTDIR\ssh-easy-windows-amd64.exe" "$INSTDIR\ssh-easy.exe"
  !endif

  ; Store install path and version in registry
  WriteRegStr HKLM "Software\ssh-easy" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "Software\ssh-easy" "Version" "${PRODUCT_VERSION}"

  ; Add/Programs entry for Windows Apps & Features
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "DisplayName" "ssh-easy"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "DisplayVersion" "${PRODUCT_VERSION} (Build ${PRODUCT_BUILD})"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "Publisher" "Kurt Ingwer"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "UninstallString" "$\"$INSTDIR\Uninstall.exe$\""
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "QuietUninstallString" "$\"$INSTDIR\Uninstall.exe$\" /S"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "DisplayIcon" "$INSTDIR\ssh-easy.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "URLInfoAbout" "https://github.com/WaschbaerImHaus/ssh-easy"
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "NoRepair" 1

  ; Start Menu entry
  CreateDirectory "$SMPROGRAMS\ssh-easy"
  CreateShortcut "$SMPROGRAMS\ssh-easy\ssh-easy.lnk" \
    "$INSTDIR\ssh-easy.exe" "" \
    "$INSTDIR\ssh-easy.exe" 0 \
    SW_SHOWNORMAL "" "SSH Connection Manager"
  CreateShortcut "$SMPROGRAMS\ssh-easy\Uninstall ssh-easy.lnk" \
    "$INSTDIR\Uninstall.exe"

  ; Optional Desktop shortcut
  ${If} $DesktopIconState == ${BST_CHECKED}
    CreateShortcut "$DESKTOP\ssh-easy.lnk" \
      "$INSTDIR\ssh-easy.exe" "" \
      "$INSTDIR\ssh-easy.exe" 0 \
      SW_SHOWNORMAL "" "SSH Connection Manager"
  ${EndIf}

SectionEnd

;----------------------------------------------------------------------
; Uninstaller
;----------------------------------------------------------------------

; Support silent uninstall via /S flag
Function un.onInit
  ${GetParameters} $0
  ${If} $0 == "/S"
    SetSilent silent
  ${EndIf}
FunctionEnd

Section "Uninstall"

  Delete "$INSTDIR\ssh-easy.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\ssh-easy\ssh-easy.lnk"
  Delete "$SMPROGRAMS\ssh-easy\Uninstall ssh-easy.lnk"
  RMDir "$SMPROGRAMS\ssh-easy"

  Delete "$DESKTOP\ssh-easy.lnk"

  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy"
  DeleteRegKey HKLM "Software\ssh-easy"

SectionEnd
