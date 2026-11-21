Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$start = [System.Windows.Forms.Cursor]::Position
for ($i = 0; $i -lt 40; $i++) {
  $x = 600 + [int](260 * [Math]::Cos($i / 40.0 * 2 * [Math]::PI))
  $y = 420 + [int](170 * [Math]::Sin($i / 40.0 * 2 * [Math]::PI))
  [System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point($x, $y)
  Start-Sleep -Milliseconds 90
}
[System.Windows.Forms.Cursor]::Position = $start
"MOUSE_SIM_DONE"
