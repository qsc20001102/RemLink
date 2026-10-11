@echo off
setlocal EnableExtensions DisableDelayedExpansion
chcp 65001 >nul
set "REMLINK_BUILD_VERSION=1.1.0"
set "REMLINK_BUILD_STATUS=1"
set "REMLINK_BUILD_PWSH="

pushd "%~dp0"
if errorlevel 1 (
  echo ERROR: Cannot open the RemLink source directory.
  goto finish
)

for /f "delims=" %%P in ('where pwsh.exe 2^>nul') do if not defined REMLINK_BUILD_PWSH set "REMLINK_BUILD_PWSH=%%P"
if not defined REMLINK_BUILD_PWSH if exist "%ProgramFiles%\PowerShell\7\pwsh.exe" set "REMLINK_BUILD_PWSH=%ProgramFiles%\PowerShell\7\pwsh.exe"
if not defined REMLINK_BUILD_PWSH if exist "%USERPROFILE%\.cache\codex-runtimes\codex-primary-runtime\dependencies\native\powershell\pwsh.exe" set "REMLINK_BUILD_PWSH=%USERPROFILE%\.cache\codex-runtimes\codex-primary-runtime\dependencies\native\powershell\pwsh.exe"
if not defined REMLINK_BUILD_PWSH (
  echo ERROR: PowerShell 7 is required. Install PowerShell 7 and run this BAT again.
  goto restore_directory
)

echo RemLink v%REMLINK_BUILD_VERSION% - Engineer/Site ZIP, Server Docker image TAR.GZ
echo Output: dist\releases\v%REMLINK_BUILD_VERSION%-TIMESTAMP
echo.
"%REMLINK_BUILD_PWSH%" -NoProfile -ExecutionPolicy Bypass -Command "$ErrorActionPreference = 'Stop'; try { if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 or newer is required.' }; foreach ($toolName in @('git', 'go', 'node', 'npm')) { if (-not (Get-Command $toolName -ErrorAction SilentlyContinue)) { throw ('Required build tool is missing: ' + $toolName) } }; $outputDirectory = Join-Path 'dist/releases' ('v' + $env:REMLINK_BUILD_VERSION + '-' + (Get-Date -Format 'yyyyMMdd-HHmmss-fff')); Write-Host ('Release output: ' + [IO.Path]::GetFullPath($outputDirectory)); & './scripts/build-release.ps1' -Version $env:REMLINK_BUILD_VERSION -OutputDirectory $outputDirectory; if (-not $?) { exit 1 }; exit 0 } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }"
set "REMLINK_BUILD_STATUS=%ERRORLEVEL%"
if "%REMLINK_BUILD_STATUS%"=="0" (
  echo.
  echo SUCCESS: All three packages were built and verified. See the paths above.
) else (
  echo.
  echo FAILED: Build stopped. See the error above. Exit code: %REMLINK_BUILD_STATUS%
)

:restore_directory
popd
:finish
if /i not "%~1"=="--no-pause" pause
exit /b %REMLINK_BUILD_STATUS%
