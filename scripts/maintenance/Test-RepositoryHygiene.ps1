[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$violations = [System.Collections.Generic.List[string]]::new()

Push-Location $repository
try {
    $gitRoot = (& git rev-parse --show-toplevel 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $gitRoot) {
        throw "Repository hygiene check must run inside a Git worktree"
    }
    # Disable Git's C-style quoting so paths containing Chinese characters are
    # inspected as real filesystem paths instead of skipped quoted strings.
    $candidateFiles = @(& git -c core.quotePath=false ls-files --cached --others --exclude-standard)
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to enumerate Git candidate files"
    }
} finally {
    Pop-Location
}

$allowedDLLs = @(
    "internal/platform/windows/wintunruntime/assets/amd64/wintun.dll"
)
$forbiddenDirectoryPattern = '(^|/)(build|dist|node_modules|data|runtime|evidence|\.codex-qa|\.cache|\.local-deploy|__pycache__)(/|$)'
$forbiddenRuntimeNamePattern = '(^|/)(identity\.json|site-profiles\.json)$'
$forbiddenExtensions = @(".exe", ".zip", ".tar", ".gz", ".tgz", ".7z", ".pyc", ".pyo", ".db", ".sqlite", ".log", ".jsonl", ".key", ".pem", ".p12", ".pfx")
$textExtensions = @(".go", ".ts", ".vue", ".css", ".html", ".md", ".yaml", ".yml", ".json", ".ps1", ".sh", ".py", ".txt", ".mod", ".sum")
$maximumSourceFileBytes = 10MB

foreach ($candidate in $candidateFiles) {
    $relative = $candidate.Replace('\', '/')
    $absolute = Join-Path $repository $candidate
    if (-not [System.IO.File]::Exists($absolute)) {
        continue
    }

    $name = [System.IO.Path]::GetFileName($relative)
    $extension = [System.IO.Path]::GetExtension($relative).ToLowerInvariant()
    if ($relative -match $forbiddenDirectoryPattern) {
        $violations.Add("generated/runtime directory is a Git candidate: $relative")
    }
    if ($relative -eq "deploy-server.cmd") {
        $violations.Add("local deployment helper is a Git candidate: $relative")
    }
    if ($relative -match $forbiddenRuntimeNamePattern) {
        $violations.Add("runtime identity/profile is a Git candidate: $relative")
    }
    if ($name -match '^\.env(?:\..+)?$' -and $name -notmatch '^\.env(?:\.[A-Za-z0-9_-]+)?\.example$') {
        $violations.Add("real environment file is a Git candidate: $relative")
    }
    if ($relative -match '^config/.*\.yaml$' -and $relative -notmatch '\.example\.yaml$') {
        $violations.Add("real client/server YAML is a Git candidate: $relative")
    }
    if ($extension -eq ".dll" -and $relative -notin $allowedDLLs) {
        $violations.Add("unapproved DLL is a Git candidate: $relative")
    }
    if ($extension -in $forbiddenExtensions) {
        $violations.Add("binary, runtime data, or private-key file is a Git candidate: $relative")
    }

    # PowerShell treats dotfiles as hidden on Linux. Provider cmdlets such as
    # Get-Item/Get-Content may require -Force there even for an explicit path,
    # while the same files are ordinary on Windows. System.IO has consistent
    # behavior on both platforms when checking .dockerignore,
    # .gitattributes, and .gitignore.
    $file = [System.IO.FileInfo]::new($absolute)
    if ($file.Length -gt $maximumSourceFileBytes) {
        $violations.Add("source candidate exceeds 10 MiB: $relative ($($file.Length) bytes)")
    }

    $isText = $extension -in $textExtensions -or $name -like ".env*" -or $name -in @("Dockerfile", "go.mod", "go.sum", ".gitignore", ".gitattributes", ".dockerignore")
    if (-not $isText -or $file.Length -gt 2MB) {
        continue
    }

    $content = [System.IO.File]::ReadAllText($absolute)
    if ($content -match '(?m)[ \t]+$') {
        $violations.Add("trailing whitespace found in: $relative")
    }
    if ($content -match '(?m)^(?:<<<<<<< .+|=======|>>>>>>> .+)$') {
        $violations.Add("unresolved merge marker found in: $relative")
    }
    if ($content -match '-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----') {
        $violations.Add("private-key material found in: $relative")
    }

    if ($extension -in @(".yaml", ".yml") -or $name -like ".env*") {
        $secretAssignments = [regex]::Matches(
            $content,
            '(?im)^\s*(?<key>(?:REMLINK_)?(?:JOIN_TOKEN|NODE_TOKEN|ADMIN_TOKEN|PASSWORD|PRIVATE_KEY|API_KEY))\s*[:=]\s*["'']?(?<value>[^\s"''#]+)'
        )
        foreach ($assignment in $secretAssignments) {
            $value = $assignment.Groups["value"].Value
            $isSafeExample = $value.StartsWith('${') -or $value -match '(?i)replace|change|example|placeholder|random|your|替换|粘贴'
            if (-not $isSafeExample) {
                $violations.Add("non-placeholder $($assignment.Groups['key'].Value) found in: $relative")
            }
        }
    }
}

$markdownFiles = $candidateFiles | Where-Object { $_ -match '\.md$' }
foreach ($markdownFile in $markdownFiles) {
    $absolute = Join-Path $repository $markdownFile
    if (-not [System.IO.File]::Exists($absolute)) {
        continue
    }
    $content = [System.IO.File]::ReadAllText($absolute)
    $links = [regex]::Matches($content, '!?(?:\[[^\]]*\])\((?<target>[^)]+)\)')
    foreach ($link in $links) {
        $target = $link.Groups["target"].Value.Trim().Trim('<', '>')
        if (-not $target -or $target.StartsWith('#') -or $target -match '^[A-Za-z][A-Za-z0-9+.-]*:') {
            continue
        }
        $pathPart = ($target -split '[?#]', 2)[0]
        if ($pathPart -match '\s+["'']') {
            $pathPart = ($pathPart -split '\s+["'']', 2)[0]
        }
        $decodedPath = [Uri]::UnescapeDataString($pathPart)
        $resolved = Join-Path (Split-Path -Parent $absolute) $decodedPath
        if (-not (Test-Path -LiteralPath $resolved)) {
            $relativeMarkdown = $markdownFile.Replace('\', '/')
            $violations.Add("broken relative Markdown link in ${relativeMarkdown}: $target")
        }
    }
}

if ($violations.Count -gt 0) {
    $violations | Sort-Object -Unique | ForEach-Object { Write-Error $_ }
    exit 1
}

Write-Host "Repository hygiene checks passed ($($candidateFiles.Count) Git candidate files inspected)."
