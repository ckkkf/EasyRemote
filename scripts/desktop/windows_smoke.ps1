# ER-900 Windows 桌面关键路径冒烟（在 Windows 主机执行）。
# 依赖：Rust toolchain、Flutter desktop、仓库根目录。
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..\..")

Write-Host "== host =="
Get-ComputerInfo | Select-Object WindowsProductName, WindowsVersion, OsHardwareAbstractionLayer

Write-Host "== rust platform tests =="
Push-Location core
cargo test -p er-capture -p er-input
cargo build -p er-ffi
Pop-Location

Write-Host "== flutter cross-process e2e (no skip) =="
Push-Location client
flutter test --concurrency=1 test\host_link_test.dart test\session_e2e_test.dart test\lossy_e2e_test.dart
Pop-Location

Write-Host "== done =="
