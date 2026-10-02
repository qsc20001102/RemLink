[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$goFiles = Get-ChildItem -Path $repository -Recurse -File -Filter *.go | Where-Object {
    $_.FullName -notmatch '[\\/]frontend[\\/]node_modules[\\/]' -and $_.Name -notlike '*_test.go'
}
$violations = @()
foreach ($file in $goFiles) {
    $relative = [System.IO.Path]::GetRelativePath($repository, $file.FullName)
    $content = Get-Content -Raw $file.FullName
    if ($relative -notlike 'internal\platform\windows\*' -and ($content -match 'powershell\.exe|Set-NetIPInterface|New-NetRoute|Remove-NetRoute')) {
        $violations += "$relative contains a Windows network command outside internal/platform/windows"
    }
    if ($content -match 'New-NetNat|Get-NetNat|Remove-NetNat|Set-NetNat|IPEnableRouter|Disable-NetFirewall|Set-NetFirewallProfile|netsh\s+advfirewall') {
        $violations += "$relative contains forbidden WinNAT, Windows forwarding, or firewall-disabling code"
    }
    if ($content -match 'S7Proxy|ModbusProxy|HTTPProxy|RDPProxy|WireGuardNT|TransitCIDR|TransitPrefix|SecondSubnetAdapter|SubnetAdapterName|\bP2P\b|\bSTUN\b|\bTURN\b|TAPDevice|EthernetFrame') {
        $violations += "$relative contains a forbidden architecture symbol"
    }
}
$siteConfig = Get-Content -Raw (Join-Path $repository 'internal/config/client.go')
if ($siteConfig -match '(?i)remote[_ ]?(cidr|subnet)|transit[_ ]?(cidr|prefix)') {
    $violations += 'Site YAML configuration contains a forbidden Remote/Transit CIDR field'
}
$packetMux = Get-Content -Raw (Join-Path $repository 'internal/overlay/clientwg/packetmux.go')
if ($packetMux -match 'gopacket|tcpip/header|tcprelay|udprelay|pingrelay') {
    $violations += 'PacketMux contains protocol parsing beyond IPv4 destination CIDR classification'
}
$serverSources = (Get-Content -Raw (Join-Path $repository 'cmd/server/main.go')) + (Get-Content -Raw (Join-Path $repository 'internal/overlay/serverwg/manager_linux.go'))
if ($serverSources -match 'wireguard/device|internal/overlay/clientwg|NewPacketMux|NewMuxTun') {
    $violations += 'Server data plane imports client wireguard-go or PacketMux code'
}
$composePaths = @('deploy/docker/compose.yaml', 'deploy/docker/compose.release.yaml', 'deploy/docker/compose.image.yaml')
$composes = @{}
foreach ($composePath in $composePaths) {
    $compose = Get-Content -Raw (Join-Path $repository $composePath)
    $composes[$composePath] = $compose
    if ($compose -match '(?m)^\s*privileged\s*:') { $violations += "$composePath enables privileged mode" }
    if ($compose -notmatch 'NET_ADMIN') { $violations += "$composePath does not grant NET_ADMIN" }
    if ($compose -notmatch '/dev/net/tun') { $violations += "$composePath does not map /dev/net/tun" }
    if ($compose -match '(?m)^\s*-\s*"?(7001|6200):') { $violations += "$composePath publicly publishes an Overlay-only Control or Session port" }
    if ($compose -match '(?i)MASQUERADE|\bSNAT\b') { $violations += "$composePath configures forbidden Overlay NAT" }
    if ($compose -notmatch '\./data:/app/data') { $violations += "$composePath does not use the required ./data:/app/data persistence mount" }
    if ($compose -notmatch '51820:51820/udp') {
        $violations += "$composePath does not publish the default WireGuard UDP port"
    }
    if ($compose -match 'REMLINK_WG_ENDPOINT|REMLINK_ADMIN_TOKEN') { $violations += "$composePath still supplies application settings outside server.yaml" }
}
$compose = $composes['deploy/docker/compose.yaml']
$dockerServerConfig = Get-Content -Raw (Join-Path $repository 'deploy/docker/server.yaml')
$dockerfile = Get-Content -Raw (Join-Path $repository 'deploy/docker/Dockerfile')
if ($dockerServerConfig -notmatch '(?m)^\s*directory:\s*["'']?/app/data["'']?\s*$' -or $dockerfile -notmatch 'VOLUME \["/app/data"\]') {
    $violations += 'Docker Server data directory does not target the required ./data:/app/data persistence mount'
}
$releaseDockerfile = Get-Content -Raw (Join-Path $repository 'deploy/docker/Dockerfile.release')
if ($releaseDockerfile -notmatch 'COPY linux-amd64/remlink-server' -or $releaseDockerfile -notmatch 'VOLUME \["/app/data"\]') {
    $violations += 'Release Dockerfile does not package the released Server binary with persistent data'
}
$identityPathSource = Get-Content -Raw (Join-Path $repository 'internal/identity/path_windows.go')
$wintunRuntimeSource = Get-Content -Raw (Join-Path $repository 'internal/platform/windows/wintunruntime/runtime_windows.go')
if (($identityPathSource + $wintunRuntimeSource) -match 'ProgramData') {
    $violations += 'Portable Windows identity or Wintun runtime still depends on ProgramData'
}
$releaseScript = Get-Content -Raw (Join-Path $repository 'scripts/build-release.ps1')
foreach ($packageName in @('RemLink-Engineer-v', 'RemLink-Site-v', 'RemLink-Server-v')) {
    if ($releaseScript -notmatch [regex]::Escape($packageName)) {
        $violations += "Release build does not define the independent $packageName package"
    }
}
if ($releaseScript -match '\$windowsRoot') {
    $violations += 'Release build still combines Engineer and Site under one Windows package root'
}
if ($releaseScript -notmatch 'go build[^\r\n]+-tags\s+"desktop,production"[^\r\n]+RemLinkEngineer\.exe') {
    $violations += 'Engineer release build does not use the mandatory Wails desktop,production tags'
}
foreach ($dockerfileEntry in @{
    'deploy/docker/Dockerfile' = $dockerfile
    'deploy/docker/Dockerfile.release' = $releaseDockerfile
}.GetEnumerator()) {
    if ($dockerfileEntry.Value -notmatch 'Acquire::Retries=5' -or
        $dockerfileEntry.Value -notmatch 'Acquire::http::Timeout=30' -or
        $dockerfileEntry.Value -notmatch 'APT_FORCE_IPV4' -or
        $dockerfileEntry.Value -notmatch 'APT_DEBIAN_MIRROR' -or
        $dockerfileEntry.Value -notmatch 'APT_SECURITY_MIRROR') {
        $violations += "$($dockerfileEntry.Key) does not bound apt network waits and expose the IPv4 fallback"
    }
}
$moduleText = Get-Content -Raw (Join-Path $repository 'go.mod')
$packageText = Get-Content -Raw (Join-Path $repository 'frontend/package.json')
if ($moduleText -match '@latest' -or $packageText -match '"(latest|\*)"') { $violations += 'A floating dependency version was found' }
$serverCollector = Get-Content -Raw (Join-Path $repository 'scripts/validation/Collect-ServerEvidence.sh')
if ($serverCollector -match '(?im)wg\s+show[^\r\n]*\bdump\b|show[^\r\n]*(private-key|preshared-key)') {
    $violations += 'Server evidence collector may export WireGuard private or preshared keys'
}
$preflight = Get-Content -Raw (Join-Path $repository 'deploy/docker/preflight.sh')
if ($preflight -notmatch 'probe_interface="([^"]+)"') {
    $violations += 'Docker preflight does not define a fixed WireGuard probe interface'
} elseif ($Matches[1].Length -gt 15) {
    $violations += "Docker preflight interface '$($Matches[1])' exceeds the Linux 15-character interface-name limit"
}
if ($preflight -notmatch 'ip link error') {
    $violations += 'Docker preflight suppresses the underlying ip link diagnostic'
}
if ($violations.Count -gt 0) {
    $violations | ForEach-Object { Write-Error $_ }
    exit 1
}
Write-Host "Architecture policy checks passed ($($goFiles.Count) production Go files inspected)."
