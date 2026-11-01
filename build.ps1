# Builds mw-watch for Windows and macOS into .\dist. No cgo, so all targets build from any OS.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
New-Item -ItemType Directory -Force dist | Out-Null

$targets = @(
    @{ GOOS = 'windows'; GOARCH = 'amd64'; Out = 'dist/mw-watch.exe';            LdFlags = '-s -w -H=windowsgui' },
    @{ GOOS = 'darwin';  GOARCH = 'arm64'; Out = 'dist/mw-watch-darwin-arm64';  LdFlags = '-s -w' },
    @{ GOOS = 'darwin';  GOARCH = 'amd64'; Out = 'dist/mw-watch-darwin-amd64';  LdFlags = '-s -w' }
)

$env:CGO_ENABLED = '0'
try {
    foreach ($t in $targets) {
        $env:GOOS = $t.GOOS
        $env:GOARCH = $t.GOARCH
        go build -trimpath -ldflags $t.LdFlags -o $t.Out .
        if ($LASTEXITCODE -ne 0) { throw "build failed for $($t.GOOS)/$($t.GOARCH)" }
        Write-Host "built $($t.Out)"
    }
} finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
}
