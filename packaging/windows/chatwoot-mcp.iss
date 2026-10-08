; Inno Setup script for Chatwoot MCP on Windows.
; Build with: ISCC.exe /DAppVersion=1.2.3 /DARCH=amd64 packaging\windows\chatwoot-mcp.iss
; The binary is expected at dist\chatwoot-mcp.exe and the installer is written to dist\.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef ARCH
  #define ARCH "amd64"
#endif

#define AppName "Chatwoot MCP"
#define AppPublisher "Chatwoot MCP"
#define AppURL "https://github.com/mhdsilva/chatwoot-mcp"
#define AppExe "chatwoot-mcp.exe"

[Setup]
AppId={{7E6C2B4A-6D9E-4C2F-9C1B-0123456789AB}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}
AppUpdatesURL={#AppURL}
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputDir=..\..\dist
OutputBaseFilename=chatwoot-mcp_{#AppVersion}_windows_{#ARCH}
SetupIconFile=..\app.ico
UninstallDisplayIcon={app}\{#AppExe}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesInstallIn64BitMode=x64compatible
LicenseFile=..\..\LICENSE

[Languages]
Name: "brazilianportuguese"; MessagesFile: "compiler:Languages\BrazilianPortuguese.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "autostart"; Description: "Iniciar o Chatwoot MCP com o Windows"; GroupDescription: "Opções:"; Flags: unchecked

[Files]
Source: "..\..\dist\{#AppExe}"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#AppName}"; Filename: "{app}\{#AppExe}"; Parameters: "app"
Name: "{group}\Desinstalar {#AppName}"; Filename: "{uninstallexe}"
Name: "{userstartup}\{#AppName}"; Filename: "{app}\{#AppExe}"; Parameters: "app"; Tasks: autostart

[Run]
Filename: "{app}\{#AppExe}"; Parameters: "app"; Description: "Abrir o Chatwoot MCP agora"; Flags: nowait postinstall skipifsilent
Filename: "{app}\{#AppExe}"; Parameters: "configure-client --client claude --yes"; Description: "Registrar no Claude Desktop"; Flags: postinstall skipifsilent unchecked
Filename: "{app}\{#AppExe}"; Parameters: "configure-client --client codex --yes"; Description: "Registrar no Codex"; Flags: postinstall skipifsilent unchecked
