$ErrorActionPreference = "Continue"
$out = "C:\EasyRemote\windiag.txt"
"=== windows with titles ===" | Out-File $out -Encoding utf8
Get-Process | Where-Object { $_.MainWindowTitle } | ForEach-Object { "$($_.Id) | $($_.ProcessName) | $($_.MainWindowTitle)" } | Out-File $out -Append -Encoding utf8

Add-Type @"
using System;
using System.Runtime.InteropServices;
public class W {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int n);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  public struct RECT { public int Left, Top, Right, Bottom; }
}
"@

$p = Get-Process er-winapp -ErrorAction SilentlyContinue | Select-Object -First 1
if ($p) {
  $r = New-Object W+RECT
  [W]::GetWindowRect($p.MainWindowHandle, [ref]$r) | Out-Null
  "er-winapp hwnd=$($p.MainWindowHandle) title=$($p.MainWindowTitle) rect=$($r.Left),$($r.Top) $($r.Right - $r.Left)x$($r.Bottom - $r.Top)" | Out-File $out -Append -Encoding utf8
  [W]::ShowWindow($p.MainWindowHandle, 9) | Out-Null
  [W]::SetForegroundWindow($p.MainWindowHandle) | Out-Null
  Start-Sleep -Milliseconds 900
} else {
  "NO_PROCESS" | Out-File $out -Append -Encoding utf8
}

Add-Type -AssemblyName System.Windows.Forms,System.Drawing
$b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
$bmp.Save("C:\EasyRemote\win_screen.png", [System.Drawing.Imaging.ImageFormat]::Png)
$g.Dispose(); $bmp.Dispose()
"SHOT_OK $($b.Width)x$($b.Height)" | Out-File $out -Append -Encoding utf8
