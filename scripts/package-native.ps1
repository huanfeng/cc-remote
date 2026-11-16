param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$StageDir,
    [Parameter(Mandatory = $true)][string]$OutputDir,
    [switch]$UpdaterEnabled
)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path $PSScriptRoot -Parent
if ($Version -notmatch '^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$') {
    throw "Invalid release version: $Version"
}

# Fresh directories prevent a previous build's files from entering an archive.
$null = New-Item -ItemType Directory -Path $StageDir
$null = New-Item -ItemType Directory -Path $OutputDir
$portableDir = Join-Path $StageDir 'portable'
$null = New-Item -ItemType Directory -Path $portableDir

$desktopRelease = Join-Path $repoRoot 'desktop/src-tauri/target/release'
$backendBinary = Join-Path $repoRoot 'dist/native/cc-remote.exe'
$installers = @(Get-ChildItem -LiteralPath (Join-Path $desktopRelease 'bundle/nsis') -Filter "*_${Version}_x64-setup.exe")
if ($installers.Count -ne 1) { throw "Expected one NSIS installer for $Version, found $($installers.Count)" }

Copy-Item -LiteralPath (Join-Path $desktopRelease 'cc-remote-desktop.exe'), $backendBinary -Destination $portableDir
Copy-Item -LiteralPath (Join-Path $repoRoot 'LICENSE'), (Join-Path $repoRoot 'docs/desktop.md') -Destination $portableDir
@'
CC Remote for Windows x64

For installation and future updates of the backend and desktop app, run in PowerShell:
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1 | iex
This creates Start Menu shortcuts and starts the desktop app in tray mode. Add
-AutoStart using the parameter syntax documented in desktop.md for sign-in startup.
Signed releases also check for desktop updates automatically. Use Settings > This app
or the tray's update menu to download and install. The local backend updates separately.
Updating a manually extracted client through the app registers an NSIS installation.

To use this extracted archive manually:
1. Run cc-remote.exe service install to install or update the local backend.
2. Run cc-remote-desktop.exe --tray for the desktop's built-in tray.
3. Hover over the tray to view usage; click it to open CC Remote. History is in its menu.

Keep both executables in this directory. The backend stores accounts and
history in its existing user data directory. Exiting the tray does not stop it.
The desktop client requires WebView2. Manual archive use does not register tray startup.
'@ | Set-Content -LiteralPath (Join-Path $portableDir 'START-HERE.txt') -Encoding utf8

Copy-Item -LiteralPath $installers[0].FullName -Destination (Join-Path $OutputDir "cc-remote-desktop_${Version}_windows_amd64-setup.exe")
if ($UpdaterEnabled) {
    $signature = "$($installers[0].FullName).sig"
    if (-not (Test-Path -LiteralPath $signature)) { throw 'Missing signed desktop updater artifact' }
    Copy-Item -LiteralPath $signature -Destination (Join-Path $OutputDir "cc-remote-desktop_${Version}_windows_amd64-setup.exe.sig")
}
Compress-Archive -Path (Join-Path $portableDir '*') -DestinationPath (Join-Path $OutputDir "cc-remote-desktop_${Version}_windows_amd64.zip") -CompressionLevel Optimal
Get-ChildItem -LiteralPath $OutputDir
