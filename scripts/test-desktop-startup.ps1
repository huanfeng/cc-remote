param(
    [string]$ExecutablePath = "$PSScriptRoot/../desktop/src-tauri/target/release/cc-remote-desktop.exe",
    [int]$TimeoutSeconds = 15
)
$ErrorActionPreference = 'Stop'
$executable = (Resolve-Path -LiteralPath $ExecutablePath).Path
if (Get-Process -Name cc-remote-desktop -ErrorAction SilentlyContinue) {
    throw 'Exit running desktop clients before running this smoke check.'
}

$process = $null
try {
    $process = Start-Process -FilePath $executable -ArgumentList '--tray' -WindowStyle Normal -PassThru
    Start-Sleep -Seconds 2
    $process.Refresh()
    if ($process.HasExited) { throw 'Tray-only startup exited unexpectedly.' }
    if (Get-CimInstance Win32_Process -Filter "ParentProcessId = $($process.Id)" | Where-Object Name -like '*webview*') {
        throw 'Tray-only startup created a WebView.'
    }
    Start-Process -FilePath $executable -WindowStyle Normal | Out-Null
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $ready = $false
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 500
        $process.Refresh()
        if ($process.HasExited) { throw 'Opening the main window exited the desktop.' }
        if ($process.MainWindowTitle -ne 'CC Remote') { continue }
        if (-not $process.Responding) { throw 'Main window is not responding after opening from tray mode.' }
        $webviews = @(Get-CimInstance Win32_Process -Filter "ParentProcessId = $($process.Id)" | Where-Object Name -like '*webview*')
        if ($webviews.Count) { $ready = $true; break }
    }
    if (-not $ready) { throw 'Main window did not create a responsive WebView before the deadline.' }
    # Repeated ordinary launches must reuse the first responsive window.
    1..2 | ForEach-Object {
        Start-Process -FilePath $executable -WindowStyle Normal | Out-Null
        Start-Sleep -Milliseconds 500
        $process.Refresh()
        if ($process.HasExited -or -not $process.Responding) { throw 'Repeated launch hung the desktop.' }
    }
    Write-Host 'PASS: tray-only mode has no WebView; opening and reopening the main window stays responsive.'
} finally {
    if ($process -and -not $process.HasExited) { Stop-Process -Id $process.Id -ErrorAction SilentlyContinue }
}
