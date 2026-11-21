$ErrorActionPreference = "Stop"
$root = "C:\EasyRemote"
$src = "$root\client\build\windows\x64\runner\Release"
$stage = "$root\dist\EasyRemote-Flutter"
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item -ItemType Directory -Path $stage | Out-Null
Copy-Item "$src\*" $stage -Recurse -Force
# 运行时必须与 exe 同目录：Rust core 动态库
Copy-Item "$root\core\target\release\er_ffi.dll" "$stage\er_ffi.dll" -Force
Copy-Item "$root\dist\README.txt" "$stage\README.txt" -Force
# 启动器：双击即开（无需额外配置；网关地址已烧进构建）
@"
@echo off
cd /d "%~dp0"
start "" easyremote_client.exe
"@ | Set-Content "$stage\Start-EasyRemote.cmd" -Encoding ASCII
$zip = "$root\dist\EasyRemote-Flutter.zip"
if (Test-Path $zip) { Remove-Item $zip -Force }
Compress-Archive -Path "$stage\*" -DestinationPath $zip -CompressionLevel Optimal
$desk = "F:\Users\ckkk\Desktop"
if (-not (Test-Path $desk)) { $desk = [Environment]::GetFolderPath("Desktop") }
Copy-Item $zip "$desk\EasyRemote-Flutter.zip" -Force
$len = (Get-Item "$desk\EasyRemote-Flutter.zip").Length
"PACK_OK $desk\EasyRemote-Flutter.zip $len bytes"
Get-ChildItem $stage | ForEach-Object { "  $($_.Name)" }
