# Install axx on Windows: https://axx.nimbusxr.us
#
#   irm https://axx.nimbusxr.us/install.ps1 | iex
#
# Environment: AXX_VERSION (e.g. 0.1.0, v0.1.0, nightly; default newest release),
# AXX_INSTALL_DIR (default %LOCALAPPDATA%\axx\bin), GITHUB_TOKEN (optional).
$ErrorActionPreference = 'Stop'
$repo = 'nimbusxr/axx'
$api = "https://api.github.com/repos/$repo"
$headers = @{ 'User-Agent' = 'axx-install' }
if ($env:GITHUB_TOKEN) { $headers['Authorization'] = "Bearer $env:GITHUB_TOKEN" }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$version = $env:AXX_VERSION
if (-not $version) {
  # Pre-releases included: /releases/latest skips them during the 0.x beta. The newest release
  # whose files are up: its checksums and this platform's archive. A release is published
  # before its build uploads them, so the newest one can have none yet. The CLI's tags are
  # v*; the WireMock extension, the Mailpit image and the IntelliJ plugin have releases of
  # their own (wiremock-openapi-v*, mailpit-chaos-v*, intellij-v*), and nightly.
  $release = (Invoke-RestMethod -Headers $headers "$api/releases?per_page=20") |
    Where-Object {
      $names = @($_.assets.name)
      -not $_.draft -and $_.tag_name -match '^v\d' -and $names -contains 'checksums.txt' -and
        $names -contains "axx_$($_.tag_name.TrimStart('v'))_windows_${arch}.zip"
    } | Select-Object -First 1
  if (-not $release) { throw "no release has a build for windows/$arch yet; set AXX_VERSION" }
  $version = $release.tag_name
}
$tag = if ($version -eq 'nightly') { 'nightly' } elseif ($version.StartsWith('v')) { $version } else { "v$version" }
$base = "https://github.com/$repo/releases/download/$tag"

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("axx-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $checksums = Join-Path $tmp 'checksums.txt'
  Invoke-WebRequest -Headers $headers -Uri "$base/checksums.txt" -OutFile $checksums
  if ($tag -eq 'nightly') {
    # The nightly's archives are named after its build (axx_0.1.0-SNAPSHOT-<commit>_...), and
    # the release can keep older builds' archives: take the one its checksums list.
    $archive = Get-Content $checksums | ForEach-Object { ($_ -split '\s+')[1] } |
      Where-Object { $_ -like "axx_*_windows_${arch}.zip" } | Select-Object -First 1
    if (-not $archive) { throw "no nightly build for windows/$arch" }
  } else {
    $archive = "axx_$($tag.TrimStart('v'))_windows_${arch}.zip"
  }

  Write-Host "axx-install: downloading $archive ($tag)"
  Invoke-WebRequest -Headers $headers -Uri "$base/$archive" -OutFile (Join-Path $tmp $archive)

  $line = Select-String -Path $checksums -Pattern " $([regex]::Escape($archive))$"
  if (-not $line) { throw "$archive is not listed in checksums.txt" }
  $expected = ($line.Line -split '\s+')[0]
  $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $archive)).Hash.ToLower()
  if ($expected -ne $actual) { throw "checksum mismatch for $archive" }
  Write-Host 'axx-install: checksum verified'

  Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath $tmp -Force
  $dir = if ($env:AXX_INSTALL_DIR) { $env:AXX_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'axx\bin' }
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  Copy-Item (Join-Path $tmp 'axx.exe') (Join-Path $dir 'axx.exe') -Force
  Copy-Item (Join-Path $tmp 'axx.exe') (Join-Path $dir 'axxeptance.exe') -Force

  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (($userPath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
    Write-Host "axx-install: added $dir to your user PATH (open a new terminal)"
  }
  Write-Host "axx-install: installed $(& (Join-Path $dir 'axx.exe') version) to $dir"
  Write-Host 'axx-install: next: axx init   (docs: https://axx.nimbusxr.us)'
} finally {
  Remove-Item -Recurse -Force $tmp
}
