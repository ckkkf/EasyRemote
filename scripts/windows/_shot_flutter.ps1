$ErrorActionPreference = "Continue"
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class W6 {
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int n);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h, IntPtr after, int x, int y, int cx, int cy, uint flags);
}
"@
$p = Get-Process -Name "easyremote_client" -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $p) { "NO_FLUTTER_APP" | Out-File C:\EasyRemote\shot_flutter.log -Encoding utf8; exit 1 }
[W6]::ShowWindow($p.MainWindowHandle, 3) | Out-Null
[W6]::SetWindowPos($p.MainWindowHandle, [IntPtr](-1), 0, 0, 0, 0, 0x0003) | Out-Null
[W6]::SetForegroundWindow($p.MainWindowHandle) | Out-Null
Start-Sleep -Milliseconds 1500
Add-Type -AssemblyName System.Windows.Forms,System.Drawing
$b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
$bmp.Save("C:\EasyRemote\win_screen.png", [System.Drawing.Imaging.ImageFormat]::Png)
"SHOT_OK app=$($p.ProcessName) title=$($p.MainWindowTitle)"
