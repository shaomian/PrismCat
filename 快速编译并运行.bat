@echo off
setlocal EnableExtensions DisableDelayedExpansion
rem Windows double-click entry; shared workflow lives in scripts/build-and-run.mjs.
pushd "%~dp0"
if errorlevel 1 exit /b 1

where node >nul 2>nul
if errorlevel 1 (
    echo Node.js not found. Install Node.js ^(24 recommended^) and retry.
    popd
    pause
    exit /b 1
)

node "%~dp0scripts\build-and-run.mjs" %*
set "BUILD_STATUS=%ERRORLEVEL%"
popd
if not "%BUILD_STATUS%"=="0" pause
exit /b %BUILD_STATUS%
