@echo off
setlocal enabledelayedexpansion

set BUILD_DIR=.build
set BUILD_LOG=%BUILD_DIR%\run.log

if not exist "%BUILD_DIR%" mkdir "%BUILD_DIR%"

:: Install UI dependencies
call :log "Installing UI dependencies..."
pushd ui
call npm install
popd

:: Build React UI
call :log "UI sources changed, rebuilding React..."
pushd ui
call npm run build 2>&1
if %errorlevel% equ 0 (
    call :log "UI build succeeded."
) else (
    call :log "WARNING: UI build failed — continuing with existing ui\dist\"
)
popd

:: Remove old binaries
if exist "%BUILD_DIR%\hyperax.exe" del "%BUILD_DIR%\hyperax.exe"
if exist "%BUILD_DIR%\hyperax-bridge.exe" del "%BUILD_DIR%\hyperax-bridge.exe"

:: Build Go binaries
call :log "Building hyperax... to: %BUILD_DIR%\hyperax.exe"
go build -o "%BUILD_DIR%\hyperax.exe" ./cmd/hyperax
if %errorlevel% neq 0 (
    call :log "ERROR: hyperax build failed."
    exit /b 1
)
call :log "hyperax build succeeded."

call :log "Building hyperax-bridge... to: %BUILD_DIR%\hyperax-bridge.exe"
go build -o "%BUILD_DIR%\hyperax-bridge.exe" ./cmd/hyperax-bridge
if %errorlevel% neq 0 (
    call :log "ERROR: hyperax-bridge build failed."
    exit /b 1
)
call :log "hyperax-bridge build succeeded."

call :log "Done. Binaries: %BUILD_DIR%\hyperax.exe, %BUILD_DIR%\hyperax-bridge.exe"
exit /b 0

:log
echo [build.bat %TIME:~0,8%] %~1
echo [build.bat %TIME:~0,8%] %~1 >> "%BUILD_LOG%"
goto :eof
