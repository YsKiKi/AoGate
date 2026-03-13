@echo off
setlocal

echo Building for Linux x86_64 (Debian 13)...
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0

if not exist build\linux mkdir build\linux

echo [1/4] Building Server (build/linux/server_linux)...
go build -ldflags="-s -w" -o build/linux/server_linux ./cmd/server
if %errorlevel% neq 0 (
    echo Failed to build server!
    exit /b %errorlevel%
)

echo [2/4] Building Keygen (build/linux/keygen_linux)...
go build -ldflags="-s -w" -o build/linux/keygen_linux ./cmd/keygen
if %errorlevel% neq 0 (
    echo Failed to build keygen!
    exit /b %errorlevel%
)

echo [3/4] Building Launcher (build/linux/launcher_linux)...
go build -ldflags="-s -w" -o build/linux/launcher_linux ./cmd/launcher
if %errorlevel% neq 0 (
    echo Failed to build launcher!
    exit /b %errorlevel%
)

echo [4/4] Building Packager (build/linux/packager_linux)...
go build -ldflags="-s -w" -o build/linux/packager_linux ./cmd/packager
if %errorlevel% neq 0 (
    echo Failed to build packager!
    exit /b %errorlevel%
)

echo.
echo ==============================================
echo Build Success! (Linux x86_64)
echo Files generated in 'build/linux' folder:
echo   - server_linux
echo   - keygen_linux
echo   - launcher_linux
echo   - packager_linux
echo.
echo Instructions for Debian Server:
echo 1. Upload 'server_linux' and 'keys' folder
echo 2. Run: chmod +x server_linux
echo 3. Run: ./server_linux
echo    (Config will be auto-generated on first run)
echo ==============================================
pause
