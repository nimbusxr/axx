# Install axx on Windows: https://axx.nimbusxr.us
#
#   irm https://axx.nimbusxr.us/install.ps1 | iex
#
# Environment: AXX_VERSION (default newest release), AXX_INSTALL_DIR
# (default %LOCALAPPDATA%\axx\bin), GITHUB_TOKEN (optional).
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
  # Pre-releases included: /releases/latest skips them during the 0.x beta.
  $release = (Invoke-RestMethod -Headers $headers "$api/releases?per_page=20") |
    Where-Object { -not $_.draft -and $_.tag_name -ne 'nightly' } | Select-Object -First 1
  if (-not $release) { throw 'could not determine the newest release; set AXX_VERSION' }
  $version = $release.tag_name
}
$tag = if ($version.StartsWith('v')) { $version } else { "v$version" }
$plain = $tag.TrimStart('v')
$archive = "axx_${plain}_windows_${arch}.zip"
$base = "https://github.com/$repo/releases/download/$tag"

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("axx-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Write-Host "axx-install: downloading $archive ($tag)"
  Invoke-WebRequest -Headers $headers -Uri "$base/$archive" -OutFile (Join-Path $tmp $archive)
  Invoke-WebRequest -Headers $headers -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

  $line = Select-String -Path (Join-Path $tmp 'checksums.txt') -Pattern " $([regex]::Escape($archive))$"
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
