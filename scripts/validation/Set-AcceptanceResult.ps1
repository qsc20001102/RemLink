[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$RunDirectory,
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^(T(0[1-9]|1[0-8])|Gate [A-D])$')]
    [string]$ID,
    [Parameter(Mandatory = $true)]
    [ValidateSet("PASS", "FAIL", "NOT_RUN")]
    [string]$Status,
    [string[]]$EvidencePath = @(),
    [string]$Notes = "",
    [string]$Operator = $env:USERNAME
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
$entry = if ($ID.StartsWith("Gate ")) {
    @($manifest.gates | Where-Object id -eq $ID)
} else {
    @($manifest.scenarios | Where-Object id -eq $ID)
}
if ($entry.Count -ne 1) {
    throw "Manifest must contain exactly one entry for $ID"
}
$entry = $entry[0]

$records = @()
foreach ($rawPath in $EvidencePath) {
    $candidate = if ([System.IO.Path]::IsPathRooted($rawPath)) { $rawPath } else { Join-Path $runRoot $rawPath }
    $fullPath = [System.IO.Path]::GetFullPath($candidate)
    $rootPrefix = $runRoot + [System.IO.Path]::DirectorySeparatorChar
    if (-not $fullPath.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Evidence must be stored inside the acceptance run directory: $fullPath"
    }
    if (-not (Test-Path -LiteralPath $fullPath -PathType Leaf)) {
        throw "Evidence file not found: $fullPath"
    }
    if ($fullPath -eq $manifestPath) {
        throw "The acceptance manifest cannot be used as its own evidence"
    }
    $item = Get-Item -LiteralPath $fullPath
    $records += [ordered]@{
        path = [System.IO.Path]::GetRelativePath($runRoot, $fullPath).Replace('\', '/')
        sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $fullPath).Hash.ToLowerInvariant()
        bytes = $item.Length
    }
}
if (($Status -eq "PASS" -or $Status -eq "FAIL") -and $records.Count -eq 0) {
    throw "$Status requires at least one evidence file"
}
if ($Status -eq "NOT_RUN" -and $records.Count -ne 0) {
    throw "NOT_RUN must not carry evidence; use PASS or FAIL after execution"
}

if ($Status -eq "PASS" -and $ID.StartsWith("Gate ")) {
    $dependencies = switch ($ID) {
        "Gate A" { @("T01", "T02") }
        "Gate B" { @("Gate A") }
        "Gate C" { @("Gate B", "T08", "T09") }
        "Gate D" { @("Gate C", "T07", "T08", "T09") }
    }
    foreach ($dependency in $dependencies) {
        $dependencyEntry = if ($dependency.StartsWith("Gate ")) {
            @($manifest.gates | Where-Object id -eq $dependency)
        } else {
            @($manifest.scenarios | Where-Object id -eq $dependency)
        }
        if ($dependencyEntry.Count -ne 1 -or $dependencyEntry[0].status -ne "PASS") {
            throw "$ID cannot be marked PASS until $dependency is PASS"
        }
    }
}

foreach ($property in @("started_at", "completed_at", "operator", "notes")) {
    if (-not $entry.PSObject.Properties[$property]) {
        $entry | Add-Member -NotePropertyName $property -NotePropertyValue $null
    }
}
$now = (Get-Date).ToUniversalTime().ToString("o")
$entry.status = $Status
$entry.evidence = $records
$entry.notes = $Notes
$entry.operator = $Operator
if ($Status -eq "NOT_RUN") {
    $entry.started_at = $null
    $entry.completed_at = $null
} else {
    if (-not $entry.started_at) { $entry.started_at = $now }
    $entry.completed_at = $now
}

$temporary = Join-Path $runRoot (".acceptance-run-" + [guid]::NewGuid().ToString("N") + ".tmp")
try {
    $manifest | ConvertTo-Json -Depth 12 | Set-Content -Encoding utf8 -LiteralPath $temporary
    [System.IO.File]::Move($temporary, $manifestPath, $true)
} finally {
    Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
}
Write-Host "Acceptance result recorded: $ID=$Status"
