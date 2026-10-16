@echo off
setlocal EnableExtensions
title PrismCat Restart
cd /d "%~dp0"

rem ---------------------------------------------------------------------------
rem  Restart the local PrismCat server (stop old instance, start a new one).
rem    - Kills only prismcat.exe located in the project root
rem    - Closes the leftover "start-prismcat.bat" console window (if any)
rem    - Relaunches via start-prismcat.bat in a fresh console window
rem
rem  Note: this does NOT rebuild. To pick up code changes, build first:
rem    go build -o prismcat.exe ./cmd/prismcat/
rem ---------------------------------------------------------------------------
set "PRISMCAT_EXE=%~dp0prismcat.exe"
set "PRISMCAT_LAUNCHER=\start-prismcat.bat"
set "PORT=8711"

if not exist "%PRISMCAT_EXE%" (
    echo [ERROR] prismcat.exe not found in the project root. Build it first:
    echo         go build -o prismcat.exe ./cmd/prismcat/
    timeout /t 8 >nul
    exit /b 1
)

echo [1/4] Stopping old PrismCat instance...
powershell -NoProfile -Command "$exe = $env:PRISMCAT_EXE; $launcher = $env:PRISMCAT_LAUNCHER; $procs = @(Get-CimInstance Win32_Process -Filter \"Name='prismcat.exe'\" | Where-Object { $_.ExecutablePath -eq $exe }); if ($procs.Count -gt 0) { $procs | ForEach-Object { Write-Host ('      Stopped server PID ' + $_.ProcessId); Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue } } else { Write-Host '      Not currently running.' }; Get-CimInstance Win32_Process -Filter \"Name='cmd.exe'\" | Where-Object { $_.CommandLine -and $_.CommandLine -like ('*' + $launcher + '*') } | ForEach-Object { Write-Host ('      Closed old launcher window PID ' + $_.ProcessId); Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"

echo [2/4] Waiting for port %PORT% to be released...
powershell -NoProfile -Command "$deadline = (Get-Date).AddSeconds(10); while ((Get-Date) -lt $deadline) { if (-not (Get-NetTCPConnection -LocalPort %PORT% -State Listen -ErrorAction SilentlyContinue)) { exit 0 }; Start-Sleep -Milliseconds 250 }; exit 1"
if errorlevel 1 (
    echo        [WARN] Port %PORT% is still busy, continuing anyway.
) else (
    echo        Port %PORT% released.
)

echo [3/4] Starting PrismCat...
start "PrismCat" "%~dp0start-prismcat.bat"

echo [4/4] Waiting for the server to come up...
powershell -NoProfile -Command "$deadline = (Get-Date).AddSeconds(30); while ((Get-Date) -lt $deadline) { if (Get-NetTCPConnection -LocalPort %PORT% -State Listen -ErrorAction SilentlyContinue) { exit 0 }; Start-Sleep -Milliseconds 250 }; exit 1"
if errorlevel 1 (
    echo        [WARN] UI is not listening on port %PORT% yet.
    echo        Check the PrismCat console window for startup errors.
) else (
    echo        PrismCat is up:  UI  http://localhost:%PORT%
    echo                         Traces:  http://localhost:%PORT%/traces
)

timeout /t 2 >nul
endlocal
exit /b 0