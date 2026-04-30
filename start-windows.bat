@echo off
setlocal
cd /d "%~dp0"

echo [GoProxy] Windows one-click start
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0start-windows.ps1"

echo.
echo [GoProxy] Process exited. Press any key to close this window.
pause >nul
