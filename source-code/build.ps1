# Build Glance.exe on Windows (requires Go 1.24+: winget install GoLang.Go)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
$goCommand = Get-Command go -ErrorAction SilentlyContinue
if ($goCommand) {
    $goExe = $goCommand.Source
} else {
    $toolsDir = Join-Path (Split-Path $PSScriptRoot -Parent) 'tools'
    $goExe = Join-Path $toolsDir 'go\bin\go.exe'
    if (-not (Test-Path -LiteralPath $goExe)) { throw '需要 Go 1.24+；请安装 Go 或放到 ../tools/go。' }
    $env:GOCACHE = Join-Path $toolsDir 'gocache'
    $env:GOPATH = Join-Path $toolsDir 'gopath'
}
$env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
& $goExe test ./internal/...
if ($LASTEXITCODE -ne 0) { throw "go test 失败 ($LASTEXITCODE)" }
& $goExe vet -unsafeptr=false ./...
if ($LASTEXITCODE -ne 0) { throw "go vet 失败 ($LASTEXITCODE)" }
New-Item -ItemType Directory -Force dist | Out-Null
& $goExe build -trimpath -ldflags "-s -w -H windowsgui" -o dist\Glance.exe .
if ($LASTEXITCODE -ne 0) { throw "go build 失败 ($LASTEXITCODE)" }
Get-Item dist\Glance.exe | Select-Object Name, Length
Write-Host "To update a running Glance: copy dist\Glance.exe to the install folder as Glance.new.exe, then use the tray menu item."
