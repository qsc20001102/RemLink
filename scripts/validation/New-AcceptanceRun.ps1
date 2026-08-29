[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$OutputDirectory,
    [string]$EngineerA = "Engineer-A",
    [string]$SiteA = "Site-A",
    [string]$EngineerB = "Engineer-B",
    [string]$SiteB = "Site-B"
)

$ErrorActionPreference = "Stop"
$resolved = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $resolved | Out-Null
$scenarios = 1..18 | ForEach-Object {
    [ordered]@{
        id = "T{0:D2}" -f $_
        status = "NOT_RUN"
        started_at = $null
        completed_at = $null
        operator = $env:USERNAME
        evidence = @()
        notes = ""
    }
}
$manifest = [ordered]@{
    schema_version = 2
    created_at = (Get-Date).ToUniversalTime().ToString("o")
    topology = [ordered]@{
        engineer_a = $EngineerA
        site_a = $SiteA
        engineer_b = $EngineerB
        site_b = $SiteB
    }
    gates = @(
        [ordered]@{ id = "Gate A"; status = "NOT_RUN"; started_at = $null; completed_at = $null; operator = $env:USERNAME; evidence = @(); notes = "" },
        [ordered]@{ id = "Gate B"; status = "NOT_RUN"; started_at = $null; completed_at = $null; operator = $env:USERNAME; evidence = @(); notes = "" },
        [ordered]@{ id = "Gate C"; status = "NOT_RUN"; started_at = $null; completed_at = $null; operator = $env:USERNAME; evidence = @(); notes = "" },
        [ordered]@{ id = "Gate D"; status = "NOT_RUN"; started_at = $null; completed_at = $null; operator = $env:USERNAME; evidence = @(); notes = "" }
    )
    scenarios = $scenarios
}
$manifestPath = Join-Path $resolved "acceptance-run.json"
$manifest | ConvertTo-Json -Depth 8 | Set-Content -Encoding utf8 $manifestPath
Copy-Item (Join-Path $PSScriptRoot "..\..\docs\validation\T01-T18-runbook.md") (Join-Path $resolved "T01-T18-runbook.md")
Write-Host "Acceptance run initialized: $manifestPath"
