[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$TargetIPv4,
    [Parameter(Mandatory = $true)]
    [string]$OutputDirectory,
    [int[]]$TCPPorts = @(102, 502, 80, 3389),
    [int]$UDPPort = 0,
    [string]$UDPPayload = "RemLink-T09",
    [int]$TimeoutMilliseconds = 3000
)

$ErrorActionPreference = "Stop"
$address = [System.Net.IPAddress]::Parse($TargetIPv4)
if ($address.AddressFamily -ne [System.Net.Sockets.AddressFamily]::InterNetwork) {
    throw "TargetIPv4 must be IPv4"
}
$resolved = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $resolved | Out-Null
$results = [ordered]@{
    captured_at = (Get-Date).ToUniversalTime().ToString("o")
    target = $TargetIPv4
    ping = $null
    tcp = @()
    udp = $null
}

$ping = Test-Connection -TargetName $TargetIPv4 -Count 4 -ErrorAction SilentlyContinue
$results.ping = @($ping | Select-Object Address, Latency, Status)
foreach ($port in $TCPPorts) {
    $probe = Test-NetConnection -ComputerName $TargetIPv4 -Port $port -InformationLevel Detailed -WarningAction SilentlyContinue
    $results.tcp += [ordered]@{ port = $port; success = [bool]$probe.TcpTestSucceeded; remote_address = "$($probe.RemoteAddress)" }
}
if ($UDPPort -gt 0) {
    $client = [System.Net.Sockets.UdpClient]::new([System.Net.Sockets.AddressFamily]::InterNetwork)
    try {
        $client.Client.ReceiveTimeout = $TimeoutMilliseconds
        $payload = [Text.Encoding]::UTF8.GetBytes($UDPPayload)
        [void]$client.Send($payload, $payload.Length, $TargetIPv4, $UDPPort)
        $remote = [System.Net.IPEndPoint]::new([System.Net.IPAddress]::Any, 0)
        $reply = $client.Receive([ref]$remote)
        $replyText = [Text.Encoding]::UTF8.GetString($reply)
        $results.udp = [ordered]@{ port = $UDPPort; success = ($replyText -eq $UDPPayload); reply = $replyText; remote = "$remote" }
    } catch {
        $results.udp = [ordered]@{ port = $UDPPort; success = $false; error = $_.Exception.Message }
    } finally {
        $client.Dispose()
    }
}
$path = Join-Path $resolved ((Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ") + "-target-$TargetIPv4.json")
$results | ConvertTo-Json -Depth 8 | Set-Content -Encoding utf8 $path
Get-FileHash -Algorithm SHA256 -LiteralPath $path | Format-List | Out-File -Encoding utf8 "$path.sha256.txt"
Write-Host "Remote target evidence: $path"
