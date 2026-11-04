@echo off
setlocal
cd /d "%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build-android.ps1" %*
set "RC=%errorlevel%"
if not defined NOPAUSE pause
exit /b %RC%
