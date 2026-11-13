param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$StageDir,
    [Parameter(Mandatory = $true)][string]$OutputDir
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
$trayDir = Join-Path $StageDir 'tray'
$null = New-Item -ItemType Directory -Path $portableDir, $trayDir

$desktopRelease = Join-Path $repoRoot 'desktop/src-tauri/target/release'
$trayBinary = Join-Path $repoRoot 'tray/target/release/cc-remote-tray.exe'
$backendBinary = Join-Path $repoRoot 'dist/native/cc-remote.exe'
$installers = @(Get-ChildItem -LiteralPath (Join-Path $desktopRelease 'bundle/nsis') -Filter "*_${Version}_x64-setup.exe")
if ($installers.Count -ne 1) { throw "Expected one NSIS installer for $Version, found $($installers.Count)" }

Copy-Item -LiteralPath (Join-Path $desktopRelease 'cc-remote-desktop.exe'), $trayBinary, $backendBinary -Destination $portableDir
Copy-Item -LiteralPath $trayBinary -Destination $trayDir
foreach ($directory in @($portableDir, $trayDir)) {
    Copy-Item -LiteralPath (Join-Path $repoRoot 'LICENSE'), (Join-Path $repoRoot 'tray/README.md') -Destination $directory
}
@'
CC Remote for Windows x64

1. Run cc-remote.exe service install to install or update the local backend.
2. Run cc-remote-tray.exe for the native tray. It does not load a WebView.
3. Choose detailed history in the tray to open cc-remote-desktop.exe.

Keep all three executables in this directory. The backend stores accounts and
history in its existing user data directory. Exiting the tray does not stop it.
The desktop client requires WebView2. Tray startup is currently manual.
'@ | Set-Content -LiteralPath (Join-Path $portableDir 'START-HERE.txt') -Encoding utf8

Copy-Item -LiteralPath $installers[0].FullName -Destination (Join-Path $OutputDir "cc-remote-desktop_${Version}_windows_amd64-setup.exe")
Compress-Archive -Path (Join-Path $portableDir '*') -DestinationPath (Join-Path $OutputDir "cc-remote-desktop_${Version}_windows_amd64.zip") -CompressionLevel Optimal
Compress-Archive -Path (Join-Path $trayDir '*') -DestinationPath (Join-Path $OutputDir "cc-remote-tray_${Version}_windows_amd64.zip") -CompressionLevel Optimal
Get-ChildItem -LiteralPath $OutputDir
