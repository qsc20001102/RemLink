[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$PackagePath,

    [ValidateSet("Engineer", "Site", "Server")]
    [string]$Role
)

$ErrorActionPreference = "Stop"
$resolvedPackage = [System.IO.Path]::GetFullPath($PackagePath)
$temporaryRoot = $null
$packageRoot = $resolvedPackage
try {
    if (Test-Path -LiteralPath $resolvedPackage -PathType Leaf) {
        if ([System.IO.Path]::GetExtension($resolvedPackage) -ne ".zip") {
            throw "Release package file must be a ZIP: $resolvedPackage"
        }
        $temporaryRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("remlink-release-verify-" + [guid]::NewGuid().ToString("N"))
        New-Item -ItemType Directory -Path $temporaryRoot | Out-Null
        Expand-Archive -LiteralPath $resolvedPackage -DestinationPath $temporaryRoot
        $roots = @(Get-ChildItem -LiteralPath $temporaryRoot -Directory)
        $looseFiles = @(Get-ChildItem -LiteralPath $temporaryRoot -File)
        if ($roots.Count -ne 1 -or $looseFiles.Count -ne 0) {
            throw "Release ZIP must contain exactly one top-level package directory"
        }
        $packageRoot = $roots[0].FullName
    } elseif (-not (Test-Path -LiteralPath $resolvedPackage -PathType Container)) {
        throw "Release package not found: $resolvedPackage"
    }

    $roleMarkers = [ordered]@{
        Engineer = "RemLinkEngineer.exe"
        Site = "RemLinkSite.exe"
        Server = "linux-amd64/remlink-server"
    }
    $detectedRoles = @($roleMarkers.GetEnumerator() | Where-Object {
        Test-Path -LiteralPath (Join-Path $packageRoot $_.Value) -PathType Leaf
    } | ForEach-Object Key)
    if (-not $Role) {
        if ($detectedRoles.Count -ne 1) {
            throw "Unable to infer one package role; found markers for: $($detectedRoles -join ', ')"
        }
        $Role = $detectedRoles[0]
    }
    if ($detectedRoles.Count -ne 1 -or $detectedRoles[0] -ne $Role) {
        throw "$Role package must contain only its own executable; found markers for: $($detectedRoles -join ', ')"
    }
    $forbiddenExecutableNames = switch ($Role) {
        Engineer { @("RemLinkSite.exe", "remlink-server") }
        Site { @("RemLinkEngineer.exe", "remlink-server") }
        Server { @("RemLinkEngineer.exe", "RemLinkSite.exe") }
    }
    $foreignExecutables = @(Get-ChildItem -LiteralPath $packageRoot -Recurse -File | Where-Object { $_.Name -in $forbiddenExecutableNames })
    if ($foreignExecutables.Count -ne 0) {
        throw "$Role package contains a foreign executable: $($foreignExecutables[0].FullName)"
    }

    $required = @(
        "README.md", "BUILD-INFO.json", "SHA256SUMS.txt", "docs/deployment-and-usage.md",
        "scripts/validation/Test-ReleasePackage.ps1"
    )
    switch ($Role) {
        Engineer {
            $required += @("RemLinkEngineer.exe", "RemLinkEngineer.svg", "RemLinkEngineer.ico", "engineer.yaml", "THIRD_PARTY_NOTICES.md")
        }
        Site {
            $required += @("RemLinkSite.exe", "RemLinkSite.svg", "RemLinkSite.ico", "site.yaml", "THIRD_PARTY_NOTICES.md")
        }
        Server {
            $required += @(
                "linux-amd64/remlink-server", "linux-amd64/server.yaml", "linux-amd64/THIRD_PARTY_NOTICES.md",
                "docker/compose.yaml", "docker/Dockerfile", "docker/compose.release.yaml", "docker/Dockerfile.release",
                "docker/.env.example", "docker/.env.china.example", "docker/preflight.sh", "docker/server.yaml", "docker/README.md",
                "docs/implementation-status.md",
                "scripts/validation/New-AcceptanceRun.ps1", "scripts/validation/Set-AcceptanceResult.ps1",
                "scripts/validation/Test-AcceptanceRun.ps1", "scripts/validation/Test-AcceptanceTools.ps1",
                "scripts/validation/Test-ReleasePackage.ps1", "scripts/validation/Collect-WindowsEvidence.ps1",
                "scripts/validation/Collect-ServerEvidence.sh", "scripts/validation/Test-RemoteTargets.ps1",
                "docs/validation/T01-T18-runbook.md", "docs/validation/requirements-evidence.md"
            )
        }
    }
    foreach ($relative in $required) {
        $target = Join-Path $packageRoot $relative
        if (-not (Test-Path -LiteralPath $target -PathType Leaf)) {
            throw "Required $Role release entry is missing: $relative"
        }
        if ((Get-Item -LiteralPath $target).Length -eq 0) {
            throw "Required $Role release entry is empty: $relative"
        }
    }
    if ($Role -in @("Engineer", "Site")) {
        $clientConfigName = if ($Role -eq "Engineer") { "engineer.yaml" } else { "site.yaml" }
        $clientConfig = Get-Content -Raw -LiteralPath (Join-Path $packageRoot $clientConfigName)
        if ($clientConfig -notmatch '(?m)^join_token:\s*') {
            throw "$Role package YAML does not expose the join_token convenience field"
        }
        if ($clientConfig -match '(?im)^\s*(node_token|wg_private_key)\s*:') {
            throw "$Role package YAML contains a durable Node secret field"
        }
    }

    $checksumPath = Join-Path $packageRoot "SHA256SUMS.txt"
    $checksums = @{}
    foreach ($line in Get-Content -LiteralPath $checksumPath) {
        if ($line -notmatch '^([0-9a-f]{64})  (.+)$') {
            throw "Malformed checksum line: $line"
        }
        $relative = $Matches[2].Replace('\', '/')
        if ([System.IO.Path]::IsPathRooted($relative) -or $relative -match '(^|/)\.\.(/|$)' -or $relative -eq "SHA256SUMS.txt") {
            throw "Unsafe or self-referential checksum path: $relative"
        }
        if ($checksums.ContainsKey($relative)) {
            throw "Duplicate checksum path: $relative"
        }
        $checksums[$relative] = $Matches[1]
    }
    $actualFiles = @(Get-ChildItem -LiteralPath $packageRoot -Recurse -File | ForEach-Object {
        [System.IO.Path]::GetRelativePath($packageRoot, $_.FullName).Replace('\', '/')
    } | Where-Object { $_ -ne "SHA256SUMS.txt" })
    if ($checksums.Count -ne $actualFiles.Count) {
        throw "Checksum count $($checksums.Count) does not match package file count $($actualFiles.Count)"
    }
    foreach ($relative in $actualFiles) {
        if (-not $checksums.ContainsKey($relative)) {
            throw "Package file is not covered by SHA256SUMS.txt: $relative"
        }
        $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $packageRoot $relative)).Hash.ToLowerInvariant()
        if ($actualHash -ne $checksums[$relative]) {
            throw "Checksum mismatch: $relative"
        }
    }

    $buildInfo = Get-Content -Raw -LiteralPath (Join-Path $packageRoot "BUILD-INFO.json") | ConvertFrom-Json
    if (-not $buildInfo.version -or $buildInfo.role -ne $Role -or @($buildInfo.target).Count -ne 1) {
        throw "BUILD-INFO.json does not describe one $Role target"
    }
    if ($Role -in @("Engineer", "Site") -and $buildInfo.portable_data_root -ne "executable_directory") {
        throw "$Role BUILD-INFO.json does not declare its portable executable data root"
    }
    if ($Role -eq "Engineer" -and (@($buildInfo.wails_build_tags) -join ',') -ne 'desktop,production') {
        throw "Engineer BUILD-INFO.json does not declare the mandatory Wails desktop,production tags"
    }

    if ($Role -eq "Server") {
        $acceptanceRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("remlink-acceptance-smoke-" + [guid]::NewGuid().ToString("N"))
        try {
            & (Join-Path $packageRoot "scripts/validation/New-AcceptanceRun.ps1") -OutputDirectory $acceptanceRoot
            $manifest = Get-Content -Raw -LiteralPath (Join-Path $acceptanceRoot "acceptance-run.json") | ConvertFrom-Json
            if ($manifest.schema_version -ne 2 -or @($manifest.gates).Count -ne 4 -or @($manifest.scenarios).Count -ne 18) {
                throw "Packaged acceptance initializer produced an invalid manifest"
            }
            if (@($manifest.gates + $manifest.scenarios | Where-Object status -ne "NOT_RUN").Count -ne 0) {
                throw "Packaged acceptance initializer fabricated a completed result"
            }
            if (-not (Test-Path -LiteralPath (Join-Path $acceptanceRoot "T01-T18-runbook.md") -PathType Leaf)) {
                throw "Packaged acceptance initializer did not copy its runbook"
            }
        } finally {
            $tempBase = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
            if ($acceptanceRoot -and $acceptanceRoot.StartsWith($tempBase, [System.StringComparison]::OrdinalIgnoreCase) -and
                [System.IO.Path]::GetFileName($acceptanceRoot).StartsWith("remlink-acceptance-smoke-")) {
                Remove-Item -LiteralPath $acceptanceRoot -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
    }
    Write-Host "$Role release package verified: $packageRoot ($($checksums.Count) checksums)"
} finally {
    if ($temporaryRoot) {
        $tempBase = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
        if ($temporaryRoot.StartsWith($tempBase, [System.StringComparison]::OrdinalIgnoreCase) -and
            [System.IO.Path]::GetFileName($temporaryRoot).StartsWith("remlink-release-verify-")) {
            Remove-Item -LiteralPath $temporaryRoot -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
}
