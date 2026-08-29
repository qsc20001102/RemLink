[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$root = Join-Path ([System.IO.Path]::GetTempPath()) ("remlink-acceptance-tools-" + [guid]::NewGuid().ToString("N"))
try {
    & (Join-Path $PSScriptRoot "New-AcceptanceRun.ps1") -OutputDirectory $root | Out-Null
    $missingEvidenceRejected = $false
    try {
        & (Join-Path $PSScriptRoot "Set-AcceptanceResult.ps1") -RunDirectory $root -ID T01 -Status PASS | Out-Null
    } catch {
        $missingEvidenceRejected = $true
    }
    if (-not $missingEvidenceRejected) { throw "PASS without evidence was accepted" }

    "T01 physical evidence fixture" | Set-Content -Encoding utf8 -LiteralPath (Join-Path $root "t01.txt")
    "T02 physical evidence fixture" | Set-Content -Encoding utf8 -LiteralPath (Join-Path $root "t02.txt")
    "Gate A evidence fixture" | Set-Content -Encoding utf8 -LiteralPath (Join-Path $root "gate-a.txt")
    & (Join-Path $PSScriptRoot "Set-AcceptanceResult.ps1") -RunDirectory $root -ID T01 -Status PASS -EvidencePath t01.txt | Out-Null
    & (Join-Path $PSScriptRoot "Set-AcceptanceResult.ps1") -RunDirectory $root -ID T02 -Status PASS -EvidencePath t02.txt | Out-Null
    & (Join-Path $PSScriptRoot "Set-AcceptanceResult.ps1") -RunDirectory $root -ID "Gate A" -Status PASS -EvidencePath gate-a.txt | Out-Null
    & (Join-Path $PSScriptRoot "Test-AcceptanceRun.ps1") -RunDirectory $root | Out-Null

    "tampered" | Add-Content -Encoding utf8 -LiteralPath (Join-Path $root "t01.txt")
    $tamperRejected = $false
    try {
        & (Join-Path $PSScriptRoot "Test-AcceptanceRun.ps1") -RunDirectory $root | Out-Null
    } catch {
        $tamperRejected = $true
    }
    if (-not $tamperRejected) { throw "Tampered acceptance evidence was accepted" }
    Write-Host "Acceptance tooling self-test passed"
} finally {
    $resolvedTemp = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath()).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    $resolvedRoot = [System.IO.Path]::GetFullPath($root)
    if ($resolvedRoot.StartsWith($resolvedTemp + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase) -and
        [System.IO.Path]::GetFileName($resolvedRoot).StartsWith("remlink-acceptance-tools-")) {
        Remove-Item -LiteralPath $resolvedRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}
