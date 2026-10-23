@echo off
cd /d "%~dp0"
if not exist wb2api.exe.new (
  echo No wb2api.exe.new to swap. Run build.bat first.
  pause
  exit /b 1
)
taskkill /f /im wb2api.exe >nul 2>&1
move /y wb2api.exe.new wb2api.exe >nul
if errorlevel 1 (
  echo Swap failed - wb2api.exe still locked?
  pause
  exit /b 1
)
start "" wb2api.exe
echo Swapped and restarted.
pause
