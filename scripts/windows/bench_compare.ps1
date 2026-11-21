# bench_compare.ps1 - turn dxgi_nvenc_bench METRIC lines into a copy-vs-zero-copy table
# Usage: powershell -ExecutionPolicy Bypass -File bench_compare.ps1 <log path>
param([Parameter(Mandatory=$true)][string]$LogPath)

if (-not (Test-Path $LogPath)) { Write-Output "MISSING LOG: $LogPath"; exit 1 }
$lines = Get-Content $LogPath | Where-Object { $_ -match '^METRIC ' }
if ($lines.Count -eq 0) { Write-Output "NO METRIC LINES in $LogPath"; exit 1 }

Write-Output ""
Write-Output ("{0,-28} {1,>12} {2,>12} {3,>10} {4,>9}" -f "path","capture_ms","encode_ms","e2e_fps","decode")
Write-Output ("{0,-28} {1,>12} {2,>12} {3,>10} {4,>9}" -f "----","----------","---------","-------","------")

$copyCapture = $null; $copyEncode = $null; $copyFps = $null
$zeroCapture = $null; $zeroEncode = $null; $zeroFps = $null

foreach ($l in $lines) {
    if ($l -match '^METRIC (\S+) capture_ms=([0-9.]+|n/a) encode_ms=([0-9.]+|n/a) .*?e2e_fps=([0-9.]+|n/a) .*?decode=(\S+)') {
        $name = $matches[1]; $cap = $matches[2]; $enc = $matches[3]; $fps = $matches[4]; $dec = $matches[5]
        Write-Output ("{0,-28} {1,>12} {2,>12} {3,>10} {4,>9}" -f $name,$cap,$enc,$fps,$dec)
        if ($name -eq 'dxgi+NVENC(copy)')      { $copyCapture=$cap; $copyEncode=$enc; $copyFps=$fps }
        if ($name -eq 'dxgi+NVENC(zero-copy)') { $zeroCapture=$cap; $zeroEncode=$enc; $zeroFps=$fps }
    }
}

Write-Output ""
if ($null -ne $copyEncode -and $null -ne $zeroEncode) {
    Write-Output "COMPARE copy_vs_zero_copy:"
    Write-Output ("  capture_ms : copy={0}  zero-copy={1}" -f $copyCapture, $zeroCapture)
    Write-Output ("  encode_ms  : copy={0}  zero-copy={1}" -f $copyEncode, $zeroEncode)
    Write-Output ("  e2e_fps    : copy={0}  zero-copy={1}" -f $copyFps, $zeroFps)
    Write-Output "NOTE: numbers are per-run; zero-copy mainly saves upload bandwidth, not always fps."
} else {
    Write-Output "WARN: missing copy or zero-copy METRIC line (bench may have failed midway)"
}
