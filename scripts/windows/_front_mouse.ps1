$ErrorActionPreference = "Continue"
$log = "C:\EasyRemote\frontmouse.log"
"start" | Out-File $log -Encoding utf8
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class W4 {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int n);
  [DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h, IntPtr after, int x, int y, int cx, int cy, uint flags);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  public struct RECT { public int Left, Top, Right, Bottom; }
}
"@
$p = Get-Process -Name "er-winapp","EasyRemote" -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $p) { "NO_APP" | Out-File $log -Append -Encoding utf8; exit 1 }
$h = $p.MainWindowHandle
"app=$($p.ProcessName) hwnd=$h" | Out-File $log -Append -Encoding utf8
[W4]::ShowWindow($h, 9) | Out-Null
$fg = [W4]::SetForegroundWindow($h)
$top = [W4]::SetWindowPos($h, [IntPtr](-1), 0, 0, 0, 0, 0x0003)
Start-Sleep -Milliseconds 900
$r = New-Object W4+RECT
[W4]::GetWindowRect($h, [ref]$r) | Out-Null
"foreground=$fg topmost=$top rect=$($r.Left),$($r.Top),$($r.Right),$($r.Bottom)" | Out-File $log -Append -Encoding utf8
$cx = [int](($r.Left + $r.Right) / 2)
$cy = [int](($r.Top + $r.Bottom) / 2)
$rx = [int](($r.Right - $r.Left) / 3)
$ry = [int](($r.Bottom - $r.Top) / 4)
"center=$cx,$cy radius=$rx,$ry" | Out-File $log -Append -Encoding utf8
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
for ($i = 0; $i -lt 40; $i++) {
  $x = $cx + [int]($rx * [Math]::Cos($i / 40.0 * 2 * [Math]::PI))
  $y = $cy + [int]($ry * [Math]::Sin($i / 40.0 * 2 * [Math]::PI))
  [System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point($x, $y)
  Start-Sleep -Milliseconds 90
}
"MOUSE_DONE" | Out-File $log -Append -Encoding utf8
