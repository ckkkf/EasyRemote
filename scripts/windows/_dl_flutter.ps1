$ErrorActionPreference = "Continue"
$url = "https://storage.flutter-io.cn/flutter_infra_release/releases/stable/windows/flutter_windows_3.50.0-stable.zip"
$out = "C:\EasyRemote\flutter.zip"
$target = 1868535132
"START $(Get-Date -Format o)" | Out-File C:\EasyRemote\flutter_dl.log -Encoding utf8
for ($i = 0; $i -lt 60; $i++) {
  if ((Test-Path $out) -and ((Get-Item $out).Length -ge $target)) { break }
  & curl.exe -L -sS -C - -o $out $url
  $len = if (Test-Path $out) { (Get-Item $out).Length } else { 0 }
  "attempt $i size=$len" | Out-File C:\EasyRemote\flutter_dl.log -Append -Encoding utf8
  if ($len -ge $target) { break }
  Start-Sleep -Seconds 3
}
$final = if (Test-Path $out) { (Get-Item $out).Length } else { 0 }
"DL_FINAL=$final" | Out-File C:\EasyRemote\flutter_dl.log -Append -Encoding utf8
"DL_DONE" | Out-File C:\EasyRemote\flutter_dl.done -Encoding utf8
