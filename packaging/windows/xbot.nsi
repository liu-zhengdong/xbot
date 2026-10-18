; XBot per-user installer, built by scripts/package-desktop.sh with makensis.
; makensis -DVERSION=v0.3.1 -DNUMERIC=0.3.1 -DARCH=amd64 -DSRC=<dir with XBot.exe> -DICON=<XBot.ico> -DOUT=<setup.exe> xbot.nsi
;
; Installs to %LOCALAPPDATA%\Programs\XBot without admin rights. It never reads
; or writes PATH or any other environment variable, and it never touches the
; user data in %USERPROFILE%\.xbot, so upgrading in place keeps it.
Unicode true
ManifestDPIAware true
RequestExecutionLevel user
SetCompressor /SOLID lzma

!include MUI2.nsh
!include LogicLib.nsh
!include x64.nsh

!define APP "XBot"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\XBot"
!define WEBVIEW2_GUID "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
!define WEBVIEW2_URL "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

Name "${APP}"
OutFile "${OUT}"
InstallDir "$LOCALAPPDATA\Programs\XBot"
InstallDirRegKey HKCU "Software\XBot" "InstallDir"
BrandingText "XBot ${VERSION}"
VIProductVersion "${NUMERIC}.0"
VIAddVersionKey /LANG=0 "ProductName" "XBot"
VIAddVersionKey /LANG=0 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=0 "FileDescription" "XBot Setup"
VIAddVersionKey /LANG=0 "FileVersion" "${NUMERIC}.0"
VIAddVersionKey /LANG=0 "LegalCopyright" "XBot contributors"

!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_FINISHPAGE_RUN "$INSTDIR\XBot.exe"
!define MUI_FINISHPAGE_RUN_TEXT "打开 XBot"
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

Function .onInit
	SetShellVarContext current
!if "${ARCH}" == "arm64"
	${IfNot} ${IsNativeARM64}
		MessageBox MB_ICONSTOP "这是 ARM 版安装包，这台电脑请用 amd64 版。" /SD IDOK
		Abort
	${EndIf}
!endif
FunctionEnd

Function un.onInit
	SetShellVarContext current
FunctionEnd

; WebView2 is present when any of its EdgeUpdate client keys has a version.
Function HasWebView2
	Push $0
	StrCpy $0 ""
	SetRegView 64
	ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\${WEBVIEW2_GUID}" "pv"
	${If} $0 == ""
	${OrIf} $0 == "0.0.0.0"
		ReadRegStr $0 HKLM "SOFTWARE\Microsoft\EdgeUpdate\Clients\${WEBVIEW2_GUID}" "pv"
	${EndIf}
	SetRegView 32
	${If} $0 == ""
	${OrIf} $0 == "0.0.0.0"
		ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\${WEBVIEW2_GUID}" "pv"
	${EndIf}
	${If} $0 == "0.0.0.0"
		StrCpy $0 ""
	${EndIf}
	Exch $0
FunctionEnd

; The app holds its exe open while it runs, so ask the user to close it first.
!macro CloseApp
	retry:
	ClearErrors
	${If} ${FileExists} "$INSTDIR\XBot.exe"
		FileOpen $0 "$INSTDIR\XBot.exe" a
		${If} ${Errors}
			MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "XBot 正在运行，请先关掉 XBot 窗口再继续。" /SD IDCANCEL IDRETRY retry
			Abort
		${EndIf}
		FileClose $0
	${EndIf}
!macroend

Section "XBot（必需）" SecApp
	SectionIn RO
	!insertmacro CloseApp
	SetOutPath "$INSTDIR"
	File "${SRC}\XBot.exe"
	File "${SRC}\xbot-cli.exe"
	WriteUninstaller "$INSTDIR\uninstall.exe"
	CreateShortcut "$SMPROGRAMS\XBot.lnk" "$INSTDIR\XBot.exe"
	WriteRegStr HKCU "Software\XBot" "InstallDir" "$INSTDIR"
	WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "XBot"
	WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
	WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "XBot"
	WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\XBot.exe"
	WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
	WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
	WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
	WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
	WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
	WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" 40000

	Call HasWebView2
	Pop $1
	${If} $1 == ""
		MessageBox MB_YESNO|MB_ICONQUESTION "XBot 的窗口需要微软 WebView2 运行库，这台电脑还没有。现在下载并安装吗？" /SD IDYES IDNO skipwv2
		InitPluginsDir
		DetailPrint "正在下载 WebView2 运行库…"
		nsExec::ExecToLog `powershell.exe -NoProfile -NonInteractive -Command "[Net.ServicePointManager]::SecurityProtocol='Tls12'; Invoke-WebRequest -UseBasicParsing -Uri '${WEBVIEW2_URL}' -OutFile '$PLUGINSDIR\webview2-setup.exe'"`
		Pop $2
		${If} $2 == 0
		${AndIf} ${FileExists} "$PLUGINSDIR\webview2-setup.exe"
			ExecWait '"$PLUGINSDIR\webview2-setup.exe" /silent /install' $2
		${EndIf}
		Call HasWebView2
		Pop $1
		${If} $1 == ""
			MessageBox MB_ICONEXCLAMATION "WebView2 没装上。请从微软官网下载「WebView2 Runtime（常青版）」安装后再打开 XBot。" /SD IDOK
		${EndIf}
		skipwv2:
	${EndIf}
SectionEnd

Section "桌面快捷方式" SecDesktop
	CreateShortcut "$DESKTOP\XBot.lnk" "$INSTDIR\XBot.exe"
SectionEnd

Section "Uninstall"
	!insertmacro CloseApp
	Delete "$INSTDIR\XBot.exe"
	Delete "$INSTDIR\xbot-cli.exe"
	Delete "$INSTDIR\uninstall.exe"
	RMDir "$INSTDIR"
	Delete "$SMPROGRAMS\XBot.lnk"
	Delete "$DESKTOP\XBot.lnk"
	DeleteRegKey HKCU "${UNINST_KEY}"
	DeleteRegKey HKCU "Software\XBot"
	; %USERPROFILE%\.xbot holds the user's bots and keys and stays.
SectionEnd
