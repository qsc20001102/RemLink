[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("Engineer", "Site")]
    [string]$Role,
    [Parameter(Mandatory = $true)]
    [string]$OutputDirectory
)

$ErrorActionPreference = "Stop"
$resolved = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $resolved | Out-Null
$stamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
$prefix = "$stamp-$($Role.ToLowerInvariant())"

$adapters = Get-NetAdapter -IncludeHidden | Select-Object Name, InterfaceDescription, ifIndex, Status, MacAddress, LinkSpeed
$remlinkAdapters = @($adapters | Where-Object Name -eq "RemLink")
$ipConfiguration = Get-NetIPConfiguration -All | Select-Object InterfaceAlias, InterfaceIndex, IPv4Address, IPv4DefaultGateway, DNSServer
$routes = Get-NetRoute -AddressFamily IPv4 | Select-Object DestinationPrefix, NextHop, InterfaceAlias, InterfaceIndex, RouteMetric, Protocol, PolicyStore
$nat = @(Get-NetNat -ErrorAction SilentlyContinue | Select-Object Name, InternalIPInterfaceAddressPrefix, ExternalIPInterfaceAddressPrefix, Active)
$processes = Get-Process | Where-Object ProcessName -Match "RemLink|wireguard" | Select-Object ProcessName, Id, Path, StartTime

$inventory = [ordered]@{
    captured_at = (Get-Date).ToUniversalTime().ToString("o")
    computer = $env:COMPUTERNAME
    role = $Role
    elevated = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    remlink_adapter_count = $remlinkAdapters.Count
    remlink_adapters = $remlinkAdapters
    adapters = $adapters
    ip_configuration = $ipConfiguration
    ipv4_routes = $routes
    existing_nat_read_only_snapshot = $nat
    processes = $processes
}
$path = Join-Path $resolved "$prefix-network.json"
$inventory | ConvertTo-Json -Depth 8 | Set-Content -Encoding utf8 $path
Get-FileHash -Algorithm SHA256 $path | Format-List | Out-File -Encoding utf8 (Join-Path $resolved "$prefix-network.sha256.txt")

if ($remlinkAdapters.Count -ne 1) {
    throw "Expected exactly one adapter named RemLink; found $($remlinkAdapters.Count). Evidence was saved to $path"
}
if (@($adapters | Where-Object { $_.Name -Match "WireGuard" -or $_.InterfaceDescription -Match "WireGuardNT" }).Count -ne 0) {
    throw "Unexpected independent WireGuard/WireGuardNT adapter detected. Evidence was saved to $path"
}
Write-Host "Windows evidence captured: $path"
