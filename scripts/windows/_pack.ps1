$ErrorActionPreference = "Stop"
$root = "C:\EasyRemote"
$stage = "$root\dist\EasyRemote-Windows"
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item -ItemType Directory -Path $stage | Out-Null
Copy-Item "$root\core\target\release\er-winapp.exe" "$stage\EasyRemote.exe"
Copy-Item "$root\udptun.exe" "$stage\udptun.exe"
Copy-Item "$root\dist\er.conf" "$stage\er.conf"
Copy-Item "$root\dist\Start-EasyRemote.cmd" "$stage\Start-EasyRemote.cmd"
Copy-Item "$root\dist\README.txt" "$stage\README.txt"
$zip = "$root\dist\EasyRemote-Windows.zip"
if (Test-Path $zip) { Remove-Item $zip -Force }
Compress-Archive -Path "$stage\*" -DestinationPath $zip -CompressionLevel Optimal
$desk = [Environment]::GetFolderPath("Desktop")
Copy-Item $zip "$desk\EasyRemote-Windows.zip" -Force
$len = (Get-Item "$desk\EasyRemote-Windows.zip").Length
"PACK_OK $desk\EasyRemote-Windows.zip $len bytes"
Get-ChildItem $stage | ForEach-Object { "  $($_.Name) $($_.Length)" }
