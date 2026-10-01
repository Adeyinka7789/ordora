# scripts\check.ps1
#
# Runs all pre-commit checks. Use this before every commit.

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot\..

Write-Host "==> gofmt" -ForegroundColor Cyan
$unformatted = gofmt -l . 2>&1
if ($unformatted) {
    Write-Host "Files not formatted:" -ForegroundColor Yellow
    Write-Host $unformatted
    Write-Host "Run: gofmt -w ." -ForegroundColor Yellow
    exit 1
}

Write-Host "==> go vet" -ForegroundColor Cyan
go vet ./...
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "==> go build" -ForegroundColor Cyan
go build ./...
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "==> go test" -ForegroundColor Cyan
go test ./...
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host ""
Write-Host "All checks passed." -ForegroundColor Green