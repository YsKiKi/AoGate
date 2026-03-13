@echo off
setlocal

echo Building for Windows (amd64)...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

if not exist build\windows mkdir build\windows

echo [1/4] Building Keygen (build/windows/keygen.exe)...
go build -ldflags="-s -w" -o build/windows/keygen.exe ./cmd/keygen
if %errorlevel% neq 0 (
    echo Failed to build keygen!
    exit /b %errorlevel%
)

echo [2/4] Building Gate Server (build/windows/server.exe)...
go build -ldflags="-s -w" -o build/windows/server.exe ./cmd/server
if %errorlevel% neq 0 (
    echo Failed to build server!
    exit /b %errorlevel%
)

echo [3/4] Building Launcher (build/windows/launcher.exe)...
go build -o build/windows/launcher.exe -ldflags "-s -w" ./cmd/launcher
if %errorlevel% neq 0 (
    echo Failed to build launcher!
    exit /b %errorlevel%
)

echo [4/4] Building Packager (build/windows/packager.exe)...
go build -ldflags="-s -w" -o build/windows/packager.exe ./cmd/packager
if %errorlevel% neq 0 (
    echo Failed to build packager!
    exit /b %errorlevel%
)

echo.
echo ==============================================
echo Build Complete! Output directory: build/windows/
echo Files generated:
echo   - keygen.exe
echo   - server.exe
echo   - launcher.exe
echo   - packager.exe
echo ==============================================
pause
