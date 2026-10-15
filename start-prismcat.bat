@echo off
title PrismCat
cd /d "%~dp0"

if not exist ".tools\prismcat.exe" (
    echo [ERROR] .tools\prismcat.exe not found. Build it first:
    echo         go build -o .tools\prismcat.exe ./cmd/prismcat/
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

".tools\prismcat.exe" -config ".tools\data\config.yaml"

echo.
echo PrismCat exited.
pause