<#
.SYNOPSIS
  Install or update cc-remote on Windows.

.DESCRIPTION
  Downloads a cc-remote release, verifies its sha256 against checksums.txt and
  runs "cc-remote service install", which copies the binary to
  %LOCALAPPDATA%\cc-remote\bin and (re)starts the background service.
  Re-running the script updates an existing installation in place.
  -Desktop also installs the Windows x64 desktop client and native tray, adds
  Start Menu shortcuts and starts only the tray (no WebView). Use -AutoStart
  for tray startup at sign-in, or -NoLaunch to leave it stopped.
  Account, history and application settings remain in their existing locations.

  Public releases download anonymously. For a private fork (CC_REMOTE_REPO),
  credentials are used when available:
    1. an authenticated GitHub CLI ("gh auth login"), or
    2. $env:GITHUB_TOKEN / $env:GH_TOKEN with "Contents: Read" on the repository.

  Works in Windows PowerShell 5.1 and PowerShell 7.

.EXAMPLE
  gh api repos/huanfeng/cc-remote/contents/scripts/install.ps1 -H "Accept: application/vnd.github.raw" | Out-String | Invoke-Expression

.EXAMPLE
  $s = gh api repos/huanfeng/cc-remote/contents/scripts/install.ps1 -H "Accept: application/vnd.github.raw" | Out-String
  & ([scriptblock]::Create($s)) -Listen lan:8787 -Version v0.2.0 -Lang zh
#>
[CmdletBinding()]
param(
    # Release tag to install (default: latest release).
    [string]$Version = "",
    # Listen address(es) passed to "service install --listen". Repeat or comma separate.
    [string[]]$Listen = @(),
    # Language of messages and the web UI: auto, zh or en, passed to "service install --lang".
    # Default: $env:CC_REMOTE_LANG; when neither is set the installed setting is kept.
    [string]$Lang = "",
    # Install the Windows x64 desktop client and native tray alongside the backend.
    [switch]$Desktop,
    # Start the tray at sign-in (desktop mode only; existing startup is kept on update).
    [switch]$AutoStart,
    # Do not launch or restart the tray after installing (desktop mode only).
    [switch]$NoLaunch,
    # Skip the background service: copy the binary to -InstallDir only.
    [switch]$NoService,
    # Target directory for desktop/tray and for the backend with -NoService.
    [string]$InstallDir = "",
    # Create the "ccr" shim (default on). Use -NoAlias to skip.
    [bool]$Alias = $true,
    [switch]$NoAlias,
    # Resolve the release and print the plan without downloading or installing.
    [switch]$DryRun,
    # Repository owner/name (default: $env:CC_REMOTE_REPO or huanfeng/cc-remote).
    # $env:CC_REMOTE_API_URL overrides the GitHub API base (default https://api.github.com).
    [string]$Repo = ""
)

# Everything runs inside a script block so that "Invoke-Expression" usage does
# not leak variables or $ErrorActionPreference into the caller's session, and
# errors are thrown instead of calling "exit" (which would close that session).
& {
    $ErrorActionPreference = "Stop"
    $ProgressPreference = "SilentlyContinue"
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    if (-not $Repo) { $Repo = $env:CC_REMOTE_REPO }
    if (-not $Repo) { $Repo = "huanfeng/cc-remote" }
    $api = $env:CC_REMOTE_API_URL
    if (-not $api) { $api = "https://api.github.com" }
    if ($NoAlias) { $Alias = $false }
    if (-not $InstallDir) {
        $base = $env:LOCALAPPDATA
        if (-not $base) { $base = Join-Path $HOME "AppData\Local" }
        $InstallDir = Join-Path $base "cc-remote\bin"
    }
    if ($Version -and -not $Version.StartsWith("v")) { $Version = "v" + $Version }
    if (-not $Lang) { $Lang = $env:CC_REMOTE_LANG }
    if ($Lang) {
        $Lang = $Lang.Trim().ToLower()
        if ($Lang -match "^zh([-_]|$)") { $Lang = "zh" }
        elseif ($Lang -match "^en([-_]|$)") { $Lang = "en" }
        elseif ($Lang -ne "auto") { throw "-Lang must be auto, zh or en (got '$Lang')" }
    }
    $listenList = @()
    foreach ($item in $Listen) {
        foreach ($part in ($item -split ",")) {
            if ($part.Trim()) { $listenList += $part.Trim() }
        }
    }

    $authHelp = @"
cannot access releases of $Repo.
The repository may be private or the request was rate-limited. Either:
  1. install the GitHub CLI and run: gh auth login
  2. or set `$env:GITHUB_TOKEN to a fine-grained token with "Contents: Read" on $Repo
"@

    # Runs a native command without PowerShell 5.1 turning stderr into
    # terminating errors; returns exit code and combined output.
    function Invoke-Native([string]$exe, [string[]]$argv) {
        $old = $ErrorActionPreference
        $ErrorActionPreference = "Continue"
        try {
            $out = & $exe @argv 2>&1 | ForEach-Object { "$_" }
            return @{ Code = $LASTEXITCODE; Out = (@($out) -join "`n") }
        } finally {
            $ErrorActionPreference = $old
        }
    }

    # --- platform ---------------------------------------------------------
    $cpu = $env:PROCESSOR_ARCHITEW6432
    if (-not $cpu) { $cpu = $env:PROCESSOR_ARCHITECTURE }
    switch ($cpu) {
        "AMD64" { $arch = "amd64" }
        "ARM64" { $arch = "arm64" }
        default { throw "unsupported architecture '$cpu'" }
    }
    if ($Desktop -and $arch -ne "amd64") { throw "desktop and tray packages currently support Windows x64 only" }
    if (($AutoStart -or $NoLaunch) -and -not $Desktop) { throw "-AutoStart and -NoLaunch require -Desktop" }
    $InstallDir = [IO.Path]::GetFullPath($InstallDir)

    # --- credentials ------------------------------------------------------
    $token = $env:GITHUB_TOKEN
    if (-not $token) { $token = $env:GH_TOKEN }
    $method = "anonymous"
    $gh = Get-Command gh -ErrorAction SilentlyContinue
    if ($gh -and (Invoke-Native $gh.Source @("auth", "status", "--hostname", "github.com")).Code -eq 0) {
        $method = "gh"
    } elseif ($token) {
        $method = "token"
    }

    $apiHeaders = @{ "X-GitHub-Api-Version" = "2022-11-28"; "User-Agent" = "cc-remote-installer" }
    if ($token -and $method -ne "gh") { $apiHeaders["Authorization"] = "Bearer $token" }

    function Get-StatusCode($err) {
        try { return [int]$err.Exception.Response.StatusCode } catch { return 0 }
    }

    # --- resolve release ----------------------------------------------------
    $assets = @{}   # name -> asset id (id unused with gh)
    if ($method -eq "gh") {
        $argv = @("release", "view")
        if ($Version) { $argv += $Version }
        $argv += @("-R", $Repo, "--json", "tagName,assets", "-q", ".tagName, (.assets[] | .name)")
        $r = Invoke-Native $gh.Source $argv
        if ($r.Code -ne 0) {
            if ($r.Out -match "not found") {
                # GitHub's "latest" release never includes prereleases.
                if ($Version) { throw "no release $Version found in $Repo" }
                throw "no stable release found in $Repo (prereleases are skipped; use -Version <tag>)"
            }
            throw "gh release view failed: $($r.Out)"
        }
        $lines = @($r.Out -split "`n" | Where-Object { $_ })
        $tag = $lines[0]
        foreach ($n in ($lines | Select-Object -Skip 1)) { $assets[$n] = "" }
    } else {
        $path = "/repos/$Repo/releases/latest"
        if ($Version) { $path = "/repos/$Repo/releases/tags/$Version" }
        try {
            $hdr = $apiHeaders.Clone(); $hdr["Accept"] = "application/vnd.github+json"
            $rel = Invoke-RestMethod -UseBasicParsing -Uri ($api + $path) -Headers $hdr
        } catch {
            $code = Get-StatusCode $_
            if ($method -eq "anonymous") { throw $authHelp }
            if ($code -eq 404) {
                # GitHub's "latest" release never includes prereleases.
                if ($Version) { throw "no release $Version found in $Repo" }
                throw "no stable release found in $Repo (prereleases are skipped; use -Version <tag>)"
            }
            if ($code -eq 401 -or $code -eq 403) { throw "GitHub API returned $code; check the token's permissions.`n$authHelp" }
            throw
        }
        $tag = $rel.tag_name
        foreach ($a in $rel.assets) { $assets[$a.name] = $a.id }
    }
    if (-not $tag) { throw "could not determine the release tag" }

    $suffix = "_windows_$arch.zip"
    $prefix = "cc-remote_"
    if ($Desktop) { $prefix = "cc-remote-desktop_" }
    $asset = $assets.Keys | Where-Object { $_.StartsWith($prefix) -and $_.EndsWith($suffix) } | Select-Object -First 1
    if (-not $asset) { throw "release $tag has no archive for windows/$arch" }
    if (-not $assets.ContainsKey("checksums.txt")) { throw "release $tag has no checksums.txt; refusing to install unverified files" }

    $serviceArgs = @("service", "install")
    foreach ($l in $listenList) { $serviceArgs += @("--listen", $l) }
    if ($Lang) { $serviceArgs += @("--lang", $Lang) }
    if ($Alias) { $serviceArgs += "--alias" }
    if ($NoService) {
        $plan = "copy cc-remote.exe to $InstallDir"
        if ($Alias) { $plan += "; run: cc-remote alias install" }
    } else {
        $plan = "run: cc-remote " + ($serviceArgs -join " ")
    }
    if ($Desktop) {
        $plan += "; install desktop and tray to $InstallDir; create Start Menu shortcuts"
        if ($AutoStart) { $plan += "; start tray at sign-in" }
        if (-not $NoLaunch) { $plan += "; start/restart tray (no WebView)" }
    }

    Write-Host "repository : $Repo (via $method)"
    Write-Host "release    : $tag"
    Write-Host "asset      : $asset"
    Write-Host "plan       : $plan"
    if ($DryRun) {
        Write-Host "dry run: nothing downloaded or installed"
        return
    }

    # --- download + verify --------------------------------------------------
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("cc-remote-install-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        foreach ($name in @($asset, "checksums.txt")) {
            Write-Host "downloading $name"
            if ($method -eq "gh") {
                $r = Invoke-Native $gh.Source @("release", "download", $tag, "-R", $Repo, "-p", $name, "-D", $tmp, "--clobber")
                if ($r.Code -ne 0) { throw "gh release download failed: $($r.Out)" }
            } else {
                $hdr = $apiHeaders.Clone(); $hdr["Accept"] = "application/octet-stream"
                # Both PowerShell editions drop Authorization when GitHub
                # redirects to its storage host.
                Invoke-WebRequest -UseBasicParsing -Uri "$api/repos/$Repo/releases/assets/$($assets[$name])" -Headers $hdr -OutFile (Join-Path $tmp $name)
            }
        }
        $want = $null
        foreach ($line in Get-Content (Join-Path $tmp "checksums.txt")) {
            $f = @($line.Trim() -split "\s+")
            if ($f.Count -eq 2 -and $f[1].TrimStart("*") -eq $asset) { $want = $f[0].ToLower() }
        }
        if (-not $want) { throw "checksums.txt has no entry for $asset" }
        $got = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $asset)).Hash.ToLower()
        if ($got -ne $want) { throw "sha256 mismatch for $asset (got $got, want $want)" }
        Write-Host "sha256 verified"

        $x = Join-Path $tmp "x"
        # Extract only the required root entries, never paths supplied by the archive.
        $required = @("cc-remote.exe")
        if ($Desktop) { $required += @("cc-remote-desktop.exe", "cc-remote-tray.exe") }
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $zip = [IO.Compression.ZipFile]::OpenRead((Join-Path $tmp $asset))
        try {
            foreach ($name in $required) {
                $matchesInZip = @($zip.Entries | Where-Object { $_.FullName -ceq $name })
                if ($matchesInZip.Count -ne 1 -or $matchesInZip[0].Length -eq 0) { throw "archive must contain one nonempty $name at its root" }
            }
            New-Item -ItemType Directory -Path $x | Out-Null
            foreach ($name in $required) { [IO.Compression.ZipFileExtensions]::ExtractToFile($zip.GetEntry($name), (Join-Path $x $name)) }
        } finally { $zip.Dispose() }
        $bin = Join-Path $x "cc-remote.exe"
        if (-not (Test-Path $bin)) { throw "archive does not contain cc-remote.exe" }
        Unblock-File -Path $bin -ErrorAction SilentlyContinue

        # --- install ----------------------------------------------------------
        function Install-Executable([string]$name) {
            $target = Join-Path $InstallDir $name
            $backup = $null
            # Running programs can be renamed, but not overwritten. Unique
            # backups also allow another update while an older image is open.
            if (Test-Path -LiteralPath $target) {
                $backup = $target + ".old-" + [guid]::NewGuid().ToString("N")
                Move-Item -LiteralPath $target -Destination $backup
            }
            try {
                Copy-Item -LiteralPath (Join-Path $x $name) -Destination $target
            } catch {
                if ($backup) {
                    Remove-Item -LiteralPath $target -Force -ErrorAction SilentlyContinue
                    Move-Item -LiteralPath $backup -Destination $target
                }
                throw
            }
            Unblock-File -LiteralPath $target -ErrorAction SilentlyContinue
            # Locked backups stay until a later update after the app closes.
            Get-ChildItem -LiteralPath $InstallDir -Filter "$name.old-*" -File | ForEach-Object {
                Remove-Item -LiteralPath $_.FullName -Force -ErrorAction SilentlyContinue
            }
        }
        if ($NoService) {
            New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
            $target = Join-Path $InstallDir "cc-remote.exe"
            Install-Executable "cc-remote.exe"
            Write-Host "installed $target ($tag)"
            if ($Alias) {
                & $target alias install
                if ($LASTEXITCODE -ne 0) { throw "cc-remote alias install failed ($LASTEXITCODE)" }
            }
            $pathDirs = $env:PATH -split ";"
            if ($pathDirs -notcontains $InstallDir) { Write-Host "note: $InstallDir is not on PATH" }
        } else {
            & $bin @serviceArgs
            if ($LASTEXITCODE -ne 0) { throw "cc-remote service install failed ($LASTEXITCODE)" }
            & $bin service status
        }
        if ($Desktop) {
            New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
            $trayTarget = Join-Path $InstallDir "cc-remote-tray.exe"
            # Only stop our own installed tray; other copies and the backend keep running.
            if (-not $NoLaunch) {
                Get-Process -Name "cc-remote-tray" -ErrorAction SilentlyContinue | Where-Object {
                    $_.Path -and [IO.Path]::GetFullPath($_.Path) -eq $trayTarget
                } | ForEach-Object { Stop-Process -Id $_.Id -ErrorAction Stop; $_.WaitForExit(10000) | Out-Null }
            }
            foreach ($name in @("cc-remote-desktop.exe", "cc-remote-tray.exe")) {
                Install-Executable $name
            }
            # WScript.Shell rejects target paths outside the system ANSI code
            # page. Use the Unicode Shell link interface for every path instead.
            # Interface order follows shobjidl_core.h / IShellLinkW.
            # https://learn.microsoft.com/windows/win32/api/shobjidl_core/nn-shobjidl_core-ishelllinkw
            if (-not ('CCRemote.NativeShortcut' -as [type])) {
                Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Text;
using System.Runtime.InteropServices;
using System.Runtime.InteropServices.ComTypes;

namespace CCRemote {
    public static class NativeShortcut {
        [ComImport, Guid("000214F9-0000-0000-C000-000000000046"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
        private interface IShellLinkW {
            void GetPath([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder path, int count, IntPtr findData, uint flags);
            void GetIDList(out IntPtr idList);
            void SetIDList(IntPtr idList);
            void GetDescription([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder text, int count);
            void SetDescription([MarshalAs(UnmanagedType.LPWStr)] string text);
            void GetWorkingDirectory([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder path, int count);
            void SetWorkingDirectory([MarshalAs(UnmanagedType.LPWStr)] string path);
            void GetArguments([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder arguments, int count);
            void SetArguments([MarshalAs(UnmanagedType.LPWStr)] string arguments);
            void GetHotkey(out ushort hotkey);
            void SetHotkey(ushort hotkey);
            void GetShowCmd(out int command);
            void SetShowCmd(int command);
            void GetIconLocation([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder path, int count, out int index);
            void SetIconLocation([MarshalAs(UnmanagedType.LPWStr)] string path, int index);
            void SetRelativePath([MarshalAs(UnmanagedType.LPWStr)] string path, uint reserved);
            void Resolve(IntPtr window, uint flags);
            void SetPath([MarshalAs(UnmanagedType.LPWStr)] string path);
        }

        public static void Save(string path, string target) {
            object instance = Activator.CreateInstance(Type.GetTypeFromCLSID(new Guid("00021401-0000-0000-C000-000000000046")));
            try {
                IShellLinkW link = (IShellLinkW)instance;
                link.SetPath(target);
                link.SetWorkingDirectory(Path.GetDirectoryName(target));
                link.SetIconLocation(target, 0);
                ((IPersistFile)instance).Save(path, true);
            } finally {
                Marshal.FinalReleaseComObject(instance);
            }
        }
    }
}
'@
            }
            function Save-Shortcut([string]$path, [string]$target) {
                New-Item -ItemType Directory -Force -Path (Split-Path $path -Parent) | Out-Null
                [CCRemote.NativeShortcut]::Save($path, $target)
            }
            $menu = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\CC Remote"
            Save-Shortcut (Join-Path $menu "CC Remote.lnk") (Join-Path $InstallDir "cc-remote-desktop.exe")
            Save-Shortcut (Join-Path $menu "CC Remote Tray.lnk") $trayTarget
            $startup = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Startup\CC Remote Tray.lnk"
            if ($AutoStart -or (Test-Path -LiteralPath $startup)) { Save-Shortcut $startup $trayTarget }
            if (-not $NoLaunch) { Start-Process -FilePath $trayTarget -WorkingDirectory $InstallDir -WindowStyle Hidden | Out-Null }
            Write-Host "desktop and tray installed to $InstallDir ($tag)"
            Write-Host "repeat this installer to update all three programs; an open desktop client uses the update after closing and reopening"
        }
        Write-Host "done: cc-remote $tag"
    } finally {
        # The only recursive cleanup target is our own GUID directory under TEMP.
        if ([IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($tmp)).TrimEnd('\', '/') -eq [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\', '/')) {
            Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
}
