[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$engineerSource = Get-Content -Raw -LiteralPath (Join-Path $repository "frontend/engineer/src/api.ts")
if ($engineerSource -notmatch 'import\.meta\.env\.DEV' -or $engineerSource -notmatch 'production Demo fallback is disabled') {
    throw "Engineer API does not fail closed when the production Native binding is absent"
}
if ($engineerSource -match '\\d\{1,3\}.*CIDR_INVALID') {
    throw "Engineer frontend duplicates CIDR validation instead of calling the Go Core"
}
$serverAppSource = Get-Content -Raw -LiteralPath (Join-Path $repository "frontend/server/src/App.vue")
if ($serverAppSource -notmatch 'Array\.isArray' -or $serverAppSource -notmatch 'logs\.value=arrayOrEmpty') {
    throw "Server frontend does not normalize nullable legacy Admin list responses"
}

$checks = @(
    [ordered]@{
        name = "Engineer"
        root = Join-Path $repository "frontend/engineer/dist"
        required = @("GetState", "CreateSession", "CheckCIDRs", "production Demo fallback is disabled")
        forbidden = @("4488624737516445881", "site-qingdao", "dev-request", "青岛现场 01")
    },
    [ordered]@{
        name = "Server"
        root = Join-Path $repository "frontend/server/dist"
        required = @("/api/v1/admin/nodes", "/api/v1/admin/sessions", "/api/v1/admin/network", "/api/v1/admin/logs")
        forbidden = @("8648912340291133", "demo-join-token-after-rotation", "节点上线，Overlay")
    }
)
foreach ($check in $checks) {
    if (-not (Test-Path -LiteralPath $check.root -PathType Container)) {
        throw "$($check.name) production bundle is missing; run npm build first"
    }
    $bundle = (Get-ChildItem -LiteralPath $check.root -Recurse -File | ForEach-Object { Get-Content -Raw -LiteralPath $_.FullName }) -join "`n"
    foreach ($required in $check.required) {
        if (-not $bundle.Contains($required)) {
            throw "$($check.name) production bundle is missing required runtime marker: $required"
        }
    }
    foreach ($forbidden in $check.forbidden) {
        if ($bundle.Contains($forbidden)) {
            throw "$($check.name) production bundle contains development fixture data: $forbidden"
        }
    }
}
Write-Host "Frontend production bundles contain live adapters and no Demo fixtures"
