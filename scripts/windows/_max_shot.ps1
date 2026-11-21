$ErrorActionPreference = "Continue"
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class W5 {
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int n);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
}
"@
$p = Get-Process EasyRemote -ErrorAction SilentlyContinue | Select-Object -First 1
if ($p) { [W5]::ShowWindow($p.MainWindowHandle, 3) | Out-Null; [W5]::SetForegroundWindow($p.MainWindowHandle) | Out-Null; "MAXIMIZED" } else { "NO_APP" }
Start-Sleep -Milliseconds 1200
Add-Type -AssemblyName System.Windows.Forms,System.Drawing
$b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
$bmp.Save("C:\EasyRemote\win_screen.png", [System.Drawing.Imaging.ImageFormat]::Png)
"SHOT_OK"
