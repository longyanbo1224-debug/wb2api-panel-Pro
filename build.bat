@echo off
cd /d "%~dp0"
rem Optional local Go install: set GO_BIN if go is not on PATH
rem (e.g.  setx GO_BIN C:\tools\go\bin), otherwise PATH is used as-is.
if defined GO_BIN set "PATH=%GO_BIN%;%PATH%"
set "GOPROXY=https://goproxy.cn,direct"

echo Building wb2api.exe ...
go build -ldflags "-s -w" -o wb2api.exe.new ./cmd/server
if errorlevel 1 (
  echo.
  echo BUILD FAILED
  pause
  exit /b 1
)
echo BUILD OK

rem Replace the old exe. Fails while the service is still running (Windows locks it).
move /y wb2api.exe.new wb2api.exe >nul 2>&1
if errorlevel 1 (
  echo.
  echo wb2api.exe is running - built as wb2api.exe.new instead.
  echo Stop the service, then run: move /y wb2api.exe.new wb2api.exe
) else (
  echo Replaced wb2api.exe. Restart the service to apply.
)
pause
