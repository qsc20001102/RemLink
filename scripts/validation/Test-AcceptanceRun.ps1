[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$RunDirectory
)

$ErrorActionPreference = "Stop"
$runRoot = [System.IO.Path]::GetFullPath($RunDirectory).TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar)
$manifestPath = Join-Path $runRoot "acceptance-run.json"
if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
    throw "Acceptance manifest not found: $manifestPath"
}
$manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if ($manifest.schema_version -ne 2) {
    throw "Unsupported acceptance manifest schema version: $($manifest.schema_version)"
}
$wantedGates = @("Gate A", "Gate B", "Gate C", "Gate D")
$wantedScenarios = 1..18 | ForEach-Object { "T{0:D2}" -f $_ }
if (@($manifest.gates).Count -ne 4 -or @($manifest.scenarios).Count -ne 18) {
    throw "Manifest must contain four Gates and eighteen scenarios"
}
$actualGates = @($manifest.gates | ForEach-Object id | Sort-Object)
$actualScenarios = @($manifest.scenarios | ForEach-Object id | Sort-Object)
if (Compare-Object $wantedGates $actualGates -SyncWindow 0) { throw "Manifest Gate IDs are incomplete or duplicated" }
if (Compare-Object $wantedScenarios $actualScenarios -SyncWindow 0) { throw "Manifest scenario IDs are incomplete or duplicated" }

$allEntries = @($manifest.gates) + @($manifest.scenarios)
foreach ($entry in $allEntries) {
    if ($entry.status -notin @("PASS", "FAIL", "NOT_RUN")) {
        throw "$($entry.id) has invalid status $($entry.status)"
    }
    $evidence = @($entry.evidence)
    if ($entry.status -eq "NOT_RUN") {
        if ($evidence.Count -ne 0 -or $entry.completed_at) {
            throw "$($entry.id) is NOT_RUN but carries evidence or a completion time"
        }
        continue
    }
    if ($evidence.Count -eq 0 -or -not $entry.started_at -or -not $entry.completed_at) {
        throw "$($entry.id) requires evidence plus start/completion timestamps"
    }
    foreach ($record in $evidence) {
        if (-not $record.path -or $record.sha256 -notmatch '^[0-9a-f]{64}$' -or $record.bytes -lt 0) {
            throw "$($entry.id) contains an invalid evidence record"
        }
        $fullPath = [System.IO.Path]::GetFullPath((Join-Path $runRoot $record.path))
        $rootPrefix = $runRoot + [System.IO.Path]::DirectorySeparatorChar
        if (-not $fullPath.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase) -or
            -not (Test-Path -LiteralPath $fullPath -PathType Leaf)) {
            throw "$($entry.id) evidence is missing or outside the run directory: $($record.path)"
        }
        $item = Get-Item -LiteralPath $fullPath
        $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $fullPath).Hash.ToLowerInvariant()
        if ($actualHash -ne $record.sha256 -or $item.Length -ne $record.bytes) {
            throw "$($entry.id) evidence hash/size mismatch: $($record.path)"
        }
    }
}

$dependenciesByGate = [ordered]@{
    "Gate A" = @("T01", "T02")
    "Gate B" = @("Gate A")
    "Gate C" = @("Gate B", "T08", "T09")
    "Gate D" = @("Gate C", "T07", "T08", "T09")
}
foreach ($gateID in $dependenciesByGate.Keys) {
    $gate = @($manifest.gates | Where-Object id -eq $gateID)[0]
    if ($gate.status -ne "PASS") { continue }
    foreach ($dependency in $dependenciesByGate[$gateID]) {
        $dependencyEntry = if ($dependency.StartsWith("Gate ")) {
            @($manifest.gates | Where-Object id -eq $dependency)[0]
        } else {
            @($manifest.scenarios | Where-Object id -eq $dependency)[0]
        }
        if ($dependencyEntry.status -ne "PASS") {
            throw "$gateID is PASS while dependency $dependency is not PASS"
        }
    }
}
Write-Host "Acceptance manifest and evidence hashes verified: $manifestPath"
