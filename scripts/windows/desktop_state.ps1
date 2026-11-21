$ErrorActionPreference = 'Continue'
Add-Type -MemberDefinition @'
[DllImport("user32.dll", SetLastError=true)] public static extern IntPtr OpenInputDesktop(uint dwFlags, bool fInherit, uint dwDesiredAccess);
[DllImport("user32.dll", SetLastError=true)] public static extern bool GetUserObjectInformation(IntPtr hObj, int nIndex, System.Text.StringBuilder pvInfo, uint nLength, out uint lpnLengthNeeded);
[DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
[DllImport("user32.dll")] public static extern IntPtr GetDesktopWindow();
[DllImport("user32.dll")] public static extern IntPtr GetShellWindow();
[DllImport("user32.dll")] public static extern bool RedrawWindow(IntPtr hWnd, IntPtr lprcUpdate, IntPtr hrgnUpdate, uint flags);
'@ -Name U -Namespace W
$h = [W.U]::OpenInputDesktop(0, $false, 0x0001)   # DESKTOP_READOBJECTS
if ($h -eq [IntPtr]::Zero) {
  "OpenInputDesktop FAILED err=" + [Runtime.InteropServices.Marshal]::GetLastWin32Error()
} else {
  $sb = New-Object System.Text.StringBuilder 256
  $need = 0
  $ok = [W.U]::GetUserObjectInformation($h, 2, $sb, 256, [ref]$need)  # UOI_NAME
  "input_desktop_name=" + $sb.ToString() + " ok=" + $ok
}
"foreground_window=" + [W.U]::GetForegroundWindow()
"desktop_window=" + [W.U]::GetDesktopWindow()
"shell_window=" + [W.U]::GetShellWindow()
"session_id=" + (Get-Process -Id $PID).SessionId
$r = [W.U]::RedrawWindow([W.U]::GetDesktopWindow(), [IntPtr]::Zero, [IntPtr]::Zero, 0x0001 -bor 0x0004 -bor 0x0100)
"redrawwindow=" + $r
"logonui=" + ((Get-Process LogonUI -ErrorAction SilentlyContinue) -ne $null)
