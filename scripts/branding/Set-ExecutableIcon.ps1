[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ExecutablePath,
    [Parameter(Mandatory = $true)][string]$IconPath
)

$ErrorActionPreference = "Stop"
$executable = (Resolve-Path -LiteralPath $ExecutablePath).Path
$icon = (Resolve-Path -LiteralPath $IconPath).Path

if (-not ([System.Management.Automation.PSTypeName]"RemLinkIconResources").Type) {
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

public static class RemLinkIconResources {
    [DllImport("kernel32.dll", EntryPoint = "BeginUpdateResourceW", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern IntPtr Begin(string path, bool deleteExisting);

    [DllImport("kernel32.dll", EntryPoint = "UpdateResourceW", SetLastError = true)]
    public static extern bool Update(IntPtr handle, IntPtr type, IntPtr name, ushort language, byte[] data, uint size);

    [DllImport("kernel32.dll", EntryPoint = "EndUpdateResourceW", SetLastError = true)]
    public static extern bool Finish(IntPtr handle, bool discard);
}
'@
}

$reader = [System.IO.BinaryReader]::new([System.IO.File]::OpenRead($icon))
try {
    if ($reader.ReadUInt16() -ne 0 -or $reader.ReadUInt16() -ne 1) {
        throw "Invalid ICO header: $icon"
    }
    $count = $reader.ReadUInt16()
    if ($count -eq 0) { throw "ICO has no image entries: $icon" }
    $entries = @(
        for ($index = 0; $index -lt $count; $index++) {
            [pscustomobject]@{
                Width = $reader.ReadByte()
                Height = $reader.ReadByte()
                Colors = $reader.ReadByte()
                Reserved = $reader.ReadByte()
                Planes = $reader.ReadUInt16()
                BitCount = $reader.ReadUInt16()
                Size = $reader.ReadUInt32()
                Offset = $reader.ReadUInt32()
                ID = $index + 1
            }
        }
    )
    $images = @(
        foreach ($entry in $entries) {
            $reader.BaseStream.Position = $entry.Offset
            $data = $reader.ReadBytes([int]$entry.Size)
            if ($data.Length -ne $entry.Size) { throw "ICO image is truncated: $icon" }
            ,$data
        }
    )
} finally {
    $reader.Dispose()
}

$groupStream = [System.IO.MemoryStream]::new()
$groupWriter = [System.IO.BinaryWriter]::new($groupStream)
try {
    $groupWriter.Write([uint16]0)
    $groupWriter.Write([uint16]1)
    $groupWriter.Write([uint16]$count)
    foreach ($entry in $entries) {
        $groupWriter.Write([byte]$entry.Width)
        $groupWriter.Write([byte]$entry.Height)
        $groupWriter.Write([byte]$entry.Colors)
        $groupWriter.Write([byte]$entry.Reserved)
        $groupWriter.Write([uint16]$entry.Planes)
        $groupWriter.Write([uint16]$entry.BitCount)
        $groupWriter.Write([uint32]$entry.Size)
        $groupWriter.Write([uint16]$entry.ID)
    }
    $groupWriter.Flush()
    $group = $groupStream.ToArray()
} finally {
    $groupWriter.Dispose()
    $groupStream.Dispose()
}

$handle = [RemLinkIconResources]::Begin($executable, $false)
if ($handle -eq [IntPtr]::Zero) {
    throw "Cannot open PE resources: $([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
}
$completed = $false
try {
    for ($index = 0; $index -lt $entries.Count; $index++) {
        $data = [byte[]]$images[$index]
        if (-not [RemLinkIconResources]::Update($handle, [IntPtr]::new(3), [IntPtr]::new($entries[$index].ID), 0, $data, [uint32]$data.Length)) {
            throw "Cannot write icon image $($entries[$index].ID): $([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
        }
    }
    if (-not [RemLinkIconResources]::Update($handle, [IntPtr]::new(14), [IntPtr]::new(1), 0, $group, [uint32]$group.Length)) {
        throw "Cannot write icon group: $([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
    }
    if (-not [RemLinkIconResources]::Finish($handle, $false)) {
        throw "Cannot commit icon resources: $([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
    }
    $completed = $true
} finally {
    if (-not $completed) { [RemLinkIconResources]::Finish($handle, $true) | Out-Null }
}
