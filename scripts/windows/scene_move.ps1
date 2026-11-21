# scene_move.ps1 - create screen motion WITHOUT touching the mouse cursor.
#
# Previous round used jiggle.ps1 (cursor moved on a 3px circle) to force DXGI frames,
# which contaminated the injection-precision measurement: the landing error stayed
# inside the jiggle radius. This script moves a borderless always-on-top window
# instead. Window motion also produces DXGI Desktop Duplication frames, but the
# cursor never moves.
#
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File scene_move.ps1 -Seconds 8 -StepMs 60
param(
    [int]$Seconds = 8,
    [int]$StepMs = 60,
    [int]$Width = 360,
    [int]$Height = 220
)

Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

$form = New-Object System.Windows.Forms.Form
$form.FormBorderStyle = [System.Windows.Forms.FormBorderStyle]::None
$form.StartPosition = [System.Windows.Forms.FormStartPosition]::Manual
$form.TopMost = $true
$form.ShowInTaskbar = $false
$form.ControlBox = $false
$form.MinimizeBox = $false
$form.MaximizeBox = $false
$form.Width = $Width
$form.Height = $Height
$form.BackColor = [System.Drawing.Color]::FromArgb(30, 30, 160)
$form.Location = New-Object System.Drawing.Point(120, 120)

$label = New-Object System.Windows.Forms.Label
$label.AutoSize = $false
$label.Dock = [System.Windows.Forms.DockStyle]::Fill
$label.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
$label.Font = New-Object System.Drawing.Font('Segoe UI', 26, [System.Drawing.FontStyle]::Bold)
$label.ForeColor = [System.Drawing.Color]::White
$label.Text = 'SCENE'
$form.Controls.Add($label)

$script:step = 0
$script:ticks = [int]($Seconds * 1000 / [Math]::Max($StepMs, 10))

$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = [Math]::Max($StepMs, 10)
$timer.Add_Tick({
    $script:step++
    $phase = ($script:step % 40)
    $x = 120
    $y = 120
    if ($phase -lt 10) { $x = 120 + $phase * 90 }
    elseif ($phase -lt 20) { $x = 1020; $y = 120 + ($phase - 10) * 70 }
    elseif ($phase -lt 30) { $x = 1020 - ($phase - 20) * 90; $y = 820 }
    else { $x = 120; $y = 820 - ($phase - 30) * 70 }
    $form.Location = New-Object System.Drawing.Point($x, $y)
    $r = [int](($script:step * 37) % 200) + 30
    $g = [int](($script:step * 61) % 200) + 30
    $b = [int](($script:step * 97) % 200) + 30
    $form.BackColor = [System.Drawing.Color]::FromArgb($r, $g, $b)
    $label.Text = "SCENE $($script:step)"
    if ($script:step -ge $script:ticks) {
        $timer.Stop()
        $form.Close()
    }
})
$timer.Start()
$form.Show()
[System.Windows.Forms.Application]::Run($form)
Write-Output "SCENE_DONE steps=$($script:step)"
