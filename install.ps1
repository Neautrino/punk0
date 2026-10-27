# Windows installer for punk0 (https://github.com/Neautrino/punk0)
#
# Usage, in PowerShell:
#   irm https://raw.githubusercontent.com/Neautrino/punk0/main/install.ps1 | iex
#
# Environment variables:
#   VERSION           - version to install (e.g. "0.1.0" or "latest", default: "latest")
#   PUNK0_INSTALL_DIR - target directory for punk0.exe (default: %LOCALAPPDATA%\punk0)
#   PUNK0_REPO        - GitHub repository (default: Neautrino/punk0)
#
# Run through `iex`, this script shares the caller's session: everything runs
# in a child scope so no variable or preference leaks, and failures throw
# instead of calling exit, which would close the caller's window.

& {
  $ErrorActionPreference = 'Stop'
  $ProgressPreference = 'SilentlyContinue' # Windows PowerShell's progress bar slows downloads to a crawl
  # Windows PowerShell 5.1 may not offer TLS 1.2, which GitHub requires.
  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

  # Glyphs every Windows console font has: Consolas lacks a check mark.
  function Step($msg) { Write-Host " › $msg" -ForegroundColor DarkYellow }
  function Ok($msg) { Write-Host " + $msg" -ForegroundColor Green }

  $repo = if ($env:PUNK0_REPO) { $env:PUNK0_REPO } else { 'Neautrino/punk0' }
  $version = if ($env:VERSION) { $env:VERSION.TrimStart('v') } else { 'latest' }

  # 1. Architecture: the OS's, not this process's (x64 PowerShell runs emulated on ARM64)
  $arch = $null
  try { $arch = [string][System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture } catch {}
  if (-not $arch) { $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE } }
  $arch = switch ($arch) {
    { $_ -in 'X64', 'AMD64' } { 'amd64'; break }
    { $_ -in 'Arm64', 'ARM64' } { 'arm64'; break }
    { $_ -in 'X86', 'x86' } { '386'; break }
    default { throw "Unsupported machine architecture: $arch" }
  }

  # 2. Version
  Step "Detecting latest release for $repo (windows/$arch)..."
  if ($version -eq 'latest') {
    try { $version = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest").tag_name.TrimStart('v') }
    catch { $version = '' }
    if (-not $version) {
      # The API is rate limited; the VERSION file on main is the fallback, as in install.sh.
      try { $version = (Invoke-RestMethod "https://raw.githubusercontent.com/$repo/main/VERSION").Trim() } catch {}
    }
    if (-not $version) { throw 'Could not determine the version to install. Check your network or set $env:VERSION = "x.y.z".' }
  }

  # 3. Target
  $dir = if ($env:PUNK0_INSTALL_DIR) { $env:PUNK0_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'punk0' }
  $target = Join-Path $dir 'punk0.exe'
  $current = $null
  if (Test-Path $target) {
    try { $current = ([regex]'\d+\.\d+(\.\d+)?').Match((& $target -version 2>&1 | Out-String)).Value } catch {}
    if ($current) { Step "Found existing punk0 v$current at $target" } else { Step "Replacing existing binary at $target..." }
  }

  # 4. Download and verify against the release's checksums.txt
  $name = "punk0-$version-windows-$arch.exe"
  $base = "https://github.com/$repo/releases/download/v$version"
  $tmp = Join-Path ([IO.Path]::GetTempPath()) ("punk0-install-" + [guid]::NewGuid())
  New-Item -ItemType Directory -Path $tmp | Out-Null
  try {
    Step "Downloading $name..."
    $exe = Join-Path $tmp $name
    $sums = Join-Path $tmp 'checksums.txt'
    try {
      Invoke-WebRequest "$base/$name" -OutFile $exe -UseBasicParsing
      Invoke-WebRequest "$base/checksums.txt" -OutFile $sums -UseBasicParsing
    } catch {
      throw "Failed to download $base/$name. If this version was just tagged, the release build may still be running."
    }
    $want = Get-Content $sums | ForEach-Object {
      $f = $_.Trim() -split '\s+'
      if ($f.Count -eq 2 -and $f[1].TrimStart('*') -eq $name) { $f[0].ToLower() }
    } | Select-Object -First 1
    if (-not $want) { throw "checksums.txt has no entry for $name" }
    $got = (Get-FileHash $exe -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $want) { throw "Checksum mismatch for ${name}: expected $want, got $got" }
    Ok 'Checksum verified'

    # 5. Install. A running punk0.exe cannot be overwritten but can be renamed,
    # so a busy old binary is moved aside instead.
    Step "Installing to $target..."
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $old = "$target.old"
    Remove-Item $old -Force -ErrorAction SilentlyContinue
    if (Test-Path $target) {
      try { Remove-Item $target -Force } catch { Rename-Item $target $old -Force }
    }
    Move-Item $exe $target -Force
  } finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
  }

  if ($current) { Ok "punk0 successfully updated (v$current -> v$version)!" } else { Ok "punk0 v$version installed successfully!" }

  # 6. PATH: the user's, persisted, and this session's
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  $parts = @($userPath -split ';' | Where-Object { $_ })
  if ($parts -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', (($parts + $dir) -join ';'), 'User')
    Ok "Added $dir to your PATH"
  }
  if (@($env:Path -split ';') -notcontains $dir) { $env:Path = "$env:Path;$dir" }

  Write-Host ''
  Write-Host 'Run punk0 to get started:'
  Write-Host ''
  Write-Host '  punk0                                       # open the agents page' -ForegroundColor DarkYellow
  Write-Host '  punk0 .                                     # add the current directory as a project' -ForegroundColor DarkYellow
  Write-Host '  punk0 https://github.com/org/repo/pull/123  # review PR in an isolated worktree' -ForegroundColor DarkYellow
  Write-Host ''
}
