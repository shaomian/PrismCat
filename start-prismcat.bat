@echo off
title PrismCat
cd /d "%~dp0"

rem Runs prismcat.exe from the project root (built by the build script in docs\).
rem No -config flag: the exe defaults to data\config.yaml relative to this
rem directory (start script and build script use the same config/data).

if not exist "prismcat.exe" (
    echo [ERROR] prismcat.exe not found in the project root. Build it first:
    echo         go build -o prismcat.exe ./cmd/prismcat/
    pause
    exit /b 1
)

echo ============================================
echo  Starting PrismCat...
echo  UI:     http://localhost:8711
echo  Proxy:  http://localhost:8711/_proxy/axonhub/v1
echo  Close this window (or tray icon) to stop.
echo ============================================
echo.

"prismcat.exe"

echo.
echo PrismCat exited.
pause