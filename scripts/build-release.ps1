[CmdletBinding()]
param(
    [string]$Version = "1.0.0",
    [string]$OutputDirectory = "dist"
)

$ErrorActionPreference = "Stop"
if ($Version -notmatch '^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$') {
    throw "Version must be a semantic version without path characters"
}

$repository = Split-Path -Parent $PSScriptRoot
$releaseRoot = Join-Path $repository $OutputDirectory
$engineerName = "RemLink-Engineer-v$Version-windows-amd64"
$siteName = "RemLink-Site-v$Version-windows-amd64"
$serverName = "RemLink-Server-v$Version-linux-amd64"
$engineerRoot = Join-Path $releaseRoot $engineerName
$siteRoot = Join-Path $releaseRoot $siteName
$serverRoot = Join-Path $releaseRoot $serverName
$linuxRoot = Join-Path $serverRoot "linux-amd64"
$validationScriptsRoot = Join-Path $serverRoot "scripts/validation"
$validationDocsRoot = Join-Path $serverRoot "docs/validation"
$commit = "unknown"
try { $commit = (git -C $repository rev-parse --short HEAD 2>$null).Trim() } catch {}
if (-not $commit) { $commit = "unknown" }
$linkerFlags = "-s -w -X remlink/internal/version.Version=$Version -X remlink/internal/version.Commit=$commit"

function Write-PackageMetadata {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Role,
        [Parameter(Mandatory = $true)][string]$Target
    )
    [ordered]@{
        version = $Version
        role = $Role
        commit = $commit
        built_at = (Get-Date).ToUniversalTime().ToString("o")
        go = (go version)
        node = (node --version)
        target = @($Target)
        portable_data_root = if ($Role -in @("Engineer", "Site")) { "executable_directory" } else { $null }
        wails_build_tags = if ($Role -eq "Engineer") { @("desktop", "production") } else { $null }
    } | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8 (Join-Path $Root "BUILD-INFO.json")

    $checksumLines = Get-ChildItem -LiteralPath $Root -Recurse -File | Sort-Object FullName | ForEach-Object {
        $relativePath = [System.IO.Path]::GetRelativePath($Root, $_.FullName).Replace('\', '/')
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant()
        "$hash  $relativePath"
    }
    $checksumLines | Set-Content -Encoding ascii (Join-Path $Root "SHA256SUMS.txt")
}

function New-PackageArchive {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Role
    )
    $archivePath = Join-Path $releaseRoot "$Name.zip"
    if (Test-Path -LiteralPath $archivePath) {
        Remove-Item -LiteralPath $archivePath -Force
    }
    Compress-Archive -Path $Root -DestinationPath $archivePath
    & (Join-Path $repository "scripts/validation/Test-ReleasePackage.ps1") -PackagePath $archivePath -Role $Role
    return $archivePath
}

New-Item -ItemType Directory -Force -Path $releaseRoot | Out-Null
$generatedTargets = @(
    $engineerRoot, $siteRoot, $serverRoot,
    (Join-Path $releaseRoot "RemLink-v$Version"),
    (Join-Path $releaseRoot "RemLink-v$Version.zip")
)
foreach ($target in $generatedTargets) {
    if (Test-Path -LiteralPath $target) {
        Remove-Item -LiteralPath $target -Recurse -Force
    }
}
New-Item -ItemType Directory -Force -Path (
    $engineerRoot,
    $siteRoot,
    $linuxRoot,
    (Join-Path $engineerRoot "docs"),
    (Join-Path $siteRoot "docs"),
    (Join-Path $engineerRoot "scripts/validation"),
    (Join-Path $siteRoot "scripts/validation"),
    (Join-Path $serverRoot "docs"),
    $validationScriptsRoot,
    $validationDocsRoot
) | Out-Null

Push-Location $repository
try {
    & (Join-Path $repository "scripts/maintenance/Test-RepositoryHygiene.ps1")
    npm ci --prefix frontend
    npm run typecheck --prefix frontend
    npm run build --prefix frontend
    & (Join-Path $repository "scripts/validation/Test-FrontendProduction.ps1")
    go mod verify
    go test -count=1 ./...
    go vet ./...
    & (Join-Path $repository "scripts/validation/Test-Architecture.ps1")
    & (Join-Path $repository "scripts/validation/Test-AcceptanceTools.ps1")

    $savedGOOS = $env:GOOS
    $savedGOARCH = $env:GOARCH
    $savedCGO = $env:CGO_ENABLED
    try {
        $env:GOOS = "windows"
        $env:GOARCH = "amd64"
        $env:CGO_ENABLED = "0"
        # Wails desktop production tags are mandatory. Without them Wails
        # intentionally compiles app_default_windows.go, which only shows an
        # error dialog instructing the operator to use `wails build`.
        go build -trimpath -tags "desktop,production" -ldflags "$linkerFlags -H windowsgui" -o (Join-Path $engineerRoot "RemLinkEngineer.exe") ./cmd/engineer
        go build -trimpath -ldflags $linkerFlags -o (Join-Path $siteRoot "RemLinkSite.exe") ./cmd/site

        $env:GOOS = "linux"
        $env:GOARCH = "amd64"
        $env:CGO_ENABLED = "0"
        go build -trimpath -ldflags $linkerFlags -o (Join-Path $linuxRoot "remlink-server") ./cmd/server
    } finally {
        $env:GOOS = $savedGOOS
        $env:GOARCH = $savedGOARCH
        $env:CGO_ENABLED = $savedCGO
    }

    # Engineer and Site are intentionally self-contained portable packages.
    Copy-Item config/engineer.example.yaml (Join-Path $engineerRoot "engineer.yaml")
    Copy-Item config/site.example.yaml (Join-Path $siteRoot "site.yaml")
    Copy-Item THIRD_PARTY_NOTICES.md $engineerRoot
    Copy-Item THIRD_PARTY_NOTICES.md $siteRoot
    Copy-Item docs/packages/engineer-readme.md (Join-Path $engineerRoot "README.md")
    Copy-Item docs/packages/site-readme.md (Join-Path $siteRoot "README.md")
    Copy-Item docs/deployment-and-usage.md (Join-Path $engineerRoot "docs/deployment-and-usage.md")
    Copy-Item docs/deployment-and-usage.md (Join-Path $siteRoot "docs/deployment-and-usage.md")
    Copy-Item scripts/validation/Test-ReleasePackage.ps1 (Join-Path $engineerRoot "scripts/validation/Test-ReleasePackage.ps1")
    Copy-Item scripts/validation/Test-ReleasePackage.ps1 (Join-Path $siteRoot "scripts/validation/Test-ReleasePackage.ps1")

    # Server has its own native binary and Docker deployment context; it never
    # contains either Windows client executable.
    Copy-Item config/server.example.yaml (Join-Path $linuxRoot "server.yaml")
    Copy-Item THIRD_PARTY_NOTICES.md $linuxRoot
    Copy-Item README.md $serverRoot
    Copy-Item -Recurse -Force deploy/docker (Join-Path $serverRoot "docker")
    Copy-Item docs/deployment-and-usage.md (Join-Path $serverRoot "docs/deployment-and-usage.md")
    Copy-Item docs/implementation-status.md (Join-Path $serverRoot "docs/implementation-status.md")
    Copy-Item scripts/validation/* $validationScriptsRoot
    Copy-Item docs/validation/* $validationDocsRoot

    Write-PackageMetadata -Root $engineerRoot -Role Engineer -Target "windows/amd64"
    Write-PackageMetadata -Root $siteRoot -Role Site -Target "windows/amd64"
    Write-PackageMetadata -Root $serverRoot -Role Server -Target "linux/amd64"

    $engineerArchive = New-PackageArchive -Root $engineerRoot -Name $engineerName -Role Engineer
    $siteArchive = New-PackageArchive -Root $siteRoot -Name $siteName -Role Site
    $serverArchive = New-PackageArchive -Root $serverRoot -Name $serverName -Role Server
} finally {
    Pop-Location
}

Write-Host "Engineer package: $engineerArchive"
Write-Host "Site package: $siteArchive"
Write-Host "Server package: $serverArchive"
