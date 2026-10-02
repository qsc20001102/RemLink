[CmdletBinding()]
param(
    [string]$Version = "1.0.8",
    [string]$OutputDirectory = "dist"
)

$ErrorActionPreference = "Stop"
if ($Version -notmatch '^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$') {
    throw "Version must be a semantic version without path characters"
}
$repository = Split-Path -Parent $PSScriptRoot
$releaseRoot = [IO.Path]::GetFullPath((Join-Path $repository $OutputDirectory))
$commit = (& git -C $repository rev-parse --short HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw "Cannot determine Git commit" }
$image = "remlink/server:$Version"
$packageName = "RemLink-Server-v$Version-docker-build"
$packageRoot = Join-Path $releaseRoot $packageName
$contextRoot = $packageRoot

function Assert-NativeSuccess([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed with exit code $LASTEXITCODE" }
}
function Write-Utf8LF([string]$Path, [string]$Content) {
    [IO.File]::WriteAllText($Path, $Content.Replace("`r`n", "`n"), [Text.UTF8Encoding]::new($false))
}

if (Test-Path -LiteralPath $packageRoot) { throw "Output already exists: $packageRoot; archive or remove the old generated directory first" }
New-Item -ItemType Directory -Path (Join-Path $packageRoot "linux-amd64"), (Join-Path $packageRoot "docker") -Force | Out-Null
Push-Location $repository
$savedGOOS = $env:GOOS
$savedGOARCH = $env:GOARCH
$savedCGO = $env:CGO_ENABLED
$savedFrontendVersion = $env:VITE_APP_VERSION
try {
    # Tests run for the host before cross-compiling the Linux binary.
    $env:GOOS = $null
    $env:GOARCH = $null
    $env:CGO_ENABLED = "0"
    npm ci --prefix frontend
    Assert-NativeSuccess "npm ci"
    npm run typecheck --prefix frontend
    Assert-NativeSuccess "frontend typecheck"
    $env:VITE_APP_VERSION = $Version
    npm run build --prefix frontend
    Assert-NativeSuccess "frontend build"
    & (Join-Path $repository "scripts/validation/Test-FrontendProduction.ps1")
    go mod verify
    Assert-NativeSuccess "go mod verify"
    go test ./...
    Assert-NativeSuccess "go test"
    go vet ./...
    Assert-NativeSuccess "go vet"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    go build -trimpath -ldflags "-s -w -X remlink/internal/version.Version=$Version -X remlink/internal/version.Commit=$commit" -o (Join-Path $contextRoot "linux-amd64/remlink-server") ./cmd/server
    Assert-NativeSuccess "Linux server build"

    Copy-Item THIRD_PARTY_NOTICES.md (Join-Path $packageRoot "linux-amd64")
    Write-Utf8LF (Join-Path $packageRoot "Dockerfile") ([IO.File]::ReadAllText((Join-Path $repository "deploy/docker/Dockerfile.release")))
    foreach ($mapping in @(@("preflight.sh", "docker/preflight.sh"), @("build.sh", "build.sh"), @("README.1panel.md", "README.md"), @("server.yaml", "server.example.yaml"), @(".env.china.example", ".env.example"))) {
        Write-Utf8LF (Join-Path $packageRoot $mapping[1]) ([IO.File]::ReadAllText((Join-Path $repository ("deploy/docker/" + $mapping[0]))))
    }
    $compose = [IO.File]::ReadAllText((Join-Path $repository "deploy/docker/compose.image.yaml"))
    Write-Utf8LF (Join-Path $packageRoot "compose.yaml") ($compose.Replace("remlink/server:1.0.8", $image))
    Write-Utf8LF (Join-Path $packageRoot "image.txt") "$image`n"
    Write-Utf8LF (Join-Path $packageRoot ".dockerignore") "*`n!Dockerfile`n!linux-amd64/`n!linux-amd64/remlink-server`n!linux-amd64/THIRD_PARTY_NOTICES.md`n!docker/`n!docker/preflight.sh`n"
    $checksumLines = Get-ChildItem -LiteralPath $packageRoot -Recurse -File -Force | Sort-Object FullName | ForEach-Object {
        $relativePath = [IO.Path]::GetRelativePath($packageRoot, $_.FullName).Replace('\', '/')
        "$((Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant())  $relativePath"
    }
    Write-Utf8LF (Join-Path $packageRoot "SHA256SUMS.txt") (($checksumLines -join "`n") + "`n")
    $archive = Join-Path $releaseRoot "$packageName.tar"
    tar -cf "$archive.partial" -C $releaseRoot $packageName
    Assert-NativeSuccess "Build context tar"
    Move-Item -LiteralPath "$archive.partial" -Destination $archive -Force
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant()
    Write-Utf8LF "$archive.sha256" "$hash  $packageName.tar`n"
    Write-Host "Docker build context (extract and build on the server): $archive"
    Write-Host "Image tag after server build: $image"
} finally {
    $env:GOOS = $savedGOOS
    $env:GOARCH = $savedGOARCH
    $env:CGO_ENABLED = $savedCGO
    $env:VITE_APP_VERSION = $savedFrontendVersion
    Pop-Location
}
