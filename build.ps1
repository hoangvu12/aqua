# Aqua build script (Phase 6 packaging).
# Builds the phone SPA and the single static Aqua.exe. Run from anywhere:
#   powershell -ExecutionPolicy Bypass -File build.ps1
#
# Toolchain: Bun (npm breaks here on sharp's postinstall) + Go. Deploying the
# Worker+SPA to Cloudflare is a separate step (printed at the end).

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

Write-Host "==> Building phone SPA (web/)" -ForegroundColor Cyan
Push-Location "$root/web"
try {
    bun install
    bun run build   # tsc -b && vite build  ->  web/dist (served by the Worker)
} finally { Pop-Location }

Write-Host "==> Building Aqua.exe (pc/)" -ForegroundColor Cyan
Push-Location "$root/pc"
try {
    # Best-effort Windows icon embed. Any *.syso in the main package is linked
    # automatically by `go build`. If goversioninfo can't be fetched (offline),
    # we still produce a working exe, just without the custom icon.
    $syso = "cmd/aqua/resource_windows.syso"
    try {
        go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1 `
            -64 -o $syso -icon "$root/brand/favicon/favicon.ico" cmd/aqua/versioninfo.json
        Write-Host "    embedded icon + version info" -ForegroundColor DarkGray
    } catch {
        Write-Warning "icon embed skipped (goversioninfo unavailable): $($_.Exception.Message)"
    }

    # Console app: do NOT pass -H windowsgui — the UI needs a terminal.
    go build -o "$root/Aqua.exe" ./cmd/aqua
    Remove-Item $syso -ErrorAction SilentlyContinue
} finally { Pop-Location }

Write-Host ""
Write-Host "==> Done. Built:" -ForegroundColor Green
Write-Host "      $root/Aqua.exe   (run it, then open VALORANT)"
Write-Host "      $root/web/dist   (SPA bundle, served by the Worker)"
Write-Host ""
Write-Host "Deploy the relay + SPA to Cloudflare:" -ForegroundColor Cyan
Write-Host "      cd cloud/aqua-agent-picker-worker; bun run deploy"
