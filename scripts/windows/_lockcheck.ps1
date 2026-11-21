$ErrorActionPreference = "Continue"
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class DL {
  [DllImport("user32.dll")] public static extern IntPtr OpenInputDesktop(uint f, bool inherit, uint access);
  [DllImport("user32.dll")] public static extern bool CloseDesktop(IntPtr h);
}
"@
$h = [DL]::OpenInputDesktop(0, $false, 0x0001)
if ($h -eq [IntPtr]::Zero) { "DESKTOP_LOCKED" } else { [DL]::CloseDesktop($h) | Out-Null; "DESKTOP_UNLOCKED" }
