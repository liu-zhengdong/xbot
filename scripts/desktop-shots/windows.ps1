# Screenshots of the real XBot desktop window (WebView2 + native caption
# buttons) on a Windows CI runner, plus a font check through the WebView2
# DevTools protocol. Never run this on a user's PC: it changes the display
# mode, the scale and the app theme of the current session.
#   windows.ps1 -Exe XBot.exe -Seed seed.exe -Out shots -Label new [-Shots] [-Fonts]
param(
  [Parameter(Mandatory)] [string] $Exe,
  [Parameter(Mandatory)] [string] $Seed,
  [Parameter(Mandatory)] [string] $Out,
  [Parameter(Mandatory)] [string] $Label,
  [switch] $Shots,
  [switch] $Fonts,
  [switch] $Display
)
$ErrorActionPreference = 'Stop'
Add-Type -Path (Join-Path $PSScriptRoot 'Native.cs')
Add-Type -AssemblyName System.Drawing
[Native]::PerMonitorAware()
New-Item -ItemType Directory -Force $Out | Out-Null
$log = Join-Path $Out "$Label-log.txt"
function Say($m) { $line = "$(Get-Date -Format HH:mm:ss) $m"; Write-Host $line; Add-Content $log $line }

if ($Display) {
  Say "display before: $([Native]::Current())"
  Say ([Native]::Biggest(3840))
  Say ([Native]::Scale(150)) # 1920x1080 allows 175% at most, and only 150% fits a 1024x672 window
  Start-Sleep 3
}

$script:port = 8790
$script:cdp = 9230

function Set-AppTheme([string] $theme) {
  $v = if ($theme -eq 'dark') { 0 } else { 1 }
  $k = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize'
  New-Item -Force $k -ErrorAction SilentlyContinue | Out-Null
  Set-ItemProperty $k AppsUseLightTheme $v -Type DWord
  Set-ItemProperty $k SystemUsesLightTheme $v -Type DWord
}

function Cdp-Send($ws, [string] $method, $params) {
  $script:cdpId++
  $msg = @{ id = $script:cdpId; method = $method; params = $params } | ConvertTo-Json -Depth 10 -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($msg)
  $ws.SendAsync([ArraySegment[byte]]::new($bytes), 'Text', $true, [Threading.CancellationToken]::None).Wait()
  $buf = [byte[]]::new(1MB)
  while ($true) {
    $ms = [IO.MemoryStream]::new()
    do {
      $r = $ws.ReceiveAsync([ArraySegment[byte]]::new($buf), [Threading.CancellationToken]::None).GetAwaiter().GetResult()
      $ms.Write($buf, 0, $r.Count)
    } while (-not $r.EndOfMessage)
    $o = [Text.Encoding]::UTF8.GetString($ms.ToArray()) | ConvertFrom-Json -Depth 50
    if ($o.id -eq $script:cdpId) {
      if ($o.error) { throw "cdp $method : $($o.error.message)" }
      return $o.result
    }
  }
}

function Cdp-Eval($ws, [string] $expr) {
  (Cdp-Send $ws 'Runtime.evaluate' @{ expression = $expr; awaitPromise = $true; returnByValue = $true }).result.value
}

function Open-Cdp([int] $cdpPort, [int] $appPort) {
  $deadline = (Get-Date).AddSeconds(45)
  $last = ''
  while ((Get-Date) -lt $deadline) {
    try {
      $pages = Invoke-RestMethod "http://127.0.0.1:$cdpPort/json" -TimeoutSec 3
      $page = $pages | Where-Object { $_.type -eq 'page' -and $_.url -like "http://127.0.0.1:$appPort/*" -and $_.url -notlike '*/app/launch*' } | Select-Object -First 1
      if ($page) {
        $ws = [Net.WebSockets.ClientWebSocket]::new()
        $ws.ConnectAsync([Uri] $page.webSocketDebuggerUrl, [Threading.CancellationToken]::None).Wait()
        return $ws
      }
    } catch { $last = $_.Exception.Message }
    Start-Sleep -Milliseconds 500
  }
  Say "no DevTools page for port $appPort ($last)"
  $wv = @(Get-CimInstance Win32_Process -Filter "Name = 'msedgewebview2.exe'" | Where-Object { $_.CommandLine -notmatch '--type=' })
  foreach ($b in $wv) { Say "webview2 browser: $(($b.CommandLine -split ' ' | Where-Object { $_ -like '--remote*' -or $_ -like '--user-data-dir*' -or $_ -like '--embedded-browser-webview*' -or $_ -like '--webview-exe-name*' }) -join ' ')" }
  if (-not $wv) { Say 'no msedgewebview2.exe browser process' }
  $app = Get-Process -Name ([IO.Path]::GetFileNameWithoutExtension($Exe)) -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($app) { Say "loader in process: $(@($app.Modules | Where-Object { $_.ModuleName -eq 'WebView2Loader.dll' }).Count); env: $env:WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS" }
  Say "listening on ${cdpPort}: $(@(Get-NetTCPConnection -State Listen -LocalPort $cdpPort -ErrorAction SilentlyContinue).Count)"
  return $null
}

function Shot($hwnd, [string] $name) {
  $r = [Native]::Bounds($hwnd)
  $w = $r.Right - $r.Left; $h = $r.Bottom - $r.Top
  $bmp = [Drawing.Bitmap]::new($w, $h)
  $g = [Drawing.Graphics]::FromImage($bmp)
  $hdc = $g.GetHdc()
  $ok = [Native]::BltScreen($hdc, $r.Left, $r.Top, $w, $h)
  $g.ReleaseHdc($hdc)
  if (-not $ok) { $g.CopyFromScreen($r.Left, $r.Top, 0, 0, [Drawing.Size]::new($w, $h)) }
  $file = Join-Path $Out "$Label-$name.png"
  $bmp.Save($file, [Drawing.Imaging.ImageFormat]::Png)
  $g.Dispose(); $bmp.Dispose()
  Say "shot $file ${w}x$h"
}

# A small window of our own takes the focus away from XBot.
function Steal-Focus {
  Add-Type -AssemblyName System.Windows.Forms
  $f = [Windows.Forms.Form]::new()
  $f.StartPosition = 'Manual'; $f.Width = 220; $f.Height = 120; $f.Text = 'focus'
  $screen = [Windows.Forms.Screen]::PrimaryScreen.Bounds
  $f.Left = $screen.Width - 240; $f.Top = $screen.Height - 180
  $f.Show(); [Native]::Focus($f.Handle) | Out-Null; [Windows.Forms.Application]::DoEvents()
  return $f
}

$fontCheck = @'
(() => {
  const out = []; const seen = new Set();
  const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  while (walk.nextNode() && out.length < 16) {
    const t = walk.currentNode.nodeValue.trim(); const el = walk.currentNode.parentElement;
    if (!/[\u3000-\u9fff\uff00-\uffef]/.test(t) || !el || seen.has(el) || !el.getClientRects().length) continue;
    seen.add(el); el.setAttribute('data-fc', String(out.length));
    out.push({ i: out.length, text: t.slice(0, 40), cls: el.className && String(el.className), family: getComputedStyle(el).fontFamily });
  }
  return JSON.stringify({ fonts: [...document.fonts].map(f => f.family + ' ' + f.status), nodes: out });
})()
'@

function Run-Case([string] $mode, [string] $theme, [string[]] $extras) {
  $script:port++; $script:cdp++
  $root = Join-Path $env:RUNNER_TEMP "xbot-shots-$Label-$mode-$theme"
  if (Test-Path $root) { Remove-Item -Recurse -Force $root }
  & $Seed $root $mode | ForEach-Object { Say $_ }
  Set-Content (Join-Path $root 'app-window.json') '{"main":{"x":16,"y":16,"width":1024,"height":640}}'
  Set-AppTheme $theme
  # Read by the CI build's patched MyGo (patch-mygo.ps1).
  $env:WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS = "--remote-debugging-port=$($script:cdp)"
  $p = Start-Process -FilePath $Exe -ArgumentList '--root', $root, 'app', '--port', $script:port -PassThru
  try {
    $hwnd = [IntPtr]::Zero
    for ($i = 0; $i -lt 120 -and $hwnd -eq [IntPtr]::Zero; $i++) { Start-Sleep -Milliseconds 500; $hwnd = [Native]::Find([uint32] $p.Id, 'XBot') }
    if ($hwnd -eq [IntPtr]::Zero) { throw "no XBot window ($mode $theme)" }
    $ws = Open-Cdp $script:cdp $script:port
    $ready = $false
    if (-not $ws) { Start-Sleep 8 }
    for ($i = 0; $ws -and $i -lt 120 -and -not $ready; $i++) {
      Start-Sleep -Milliseconds 500
      try { $ready = Cdp-Eval $ws "document.readyState === 'complete' && !document.getElementById('shell').hidden && document.fonts.status === 'loaded'" } catch {}
    }
    Say "case $mode $theme ready=$ready dpi=$([Native]::GetDpiForWindow($hwnd))"
    Start-Sleep 2
    if ($Fonts -and $ws) {
      $raw = Cdp-Eval $ws $fontCheck
      $info = $raw | ConvertFrom-Json
      Cdp-Send $ws 'DOM.enable' @{} | Out-Null
      Cdp-Send $ws 'CSS.enable' @{} | Out-Null
      $doc = Cdp-Send $ws 'DOM.getDocument' @{ depth = 0 }
      $ids = (Cdp-Send $ws 'DOM.querySelectorAll' @{ nodeId = $doc.root.nodeId; selector = '[data-fc]' }).nodeIds
      $rows = foreach ($id in $ids) {
        $attrs = (Cdp-Send $ws 'DOM.getAttributes' @{ nodeId = $id }).attributes
        $n = [int] $attrs[[array]::IndexOf($attrs, 'data-fc') + 1]
        $pf = (Cdp-Send $ws 'CSS.getPlatformFontsForNode' @{ nodeId = $id }).fonts
        [pscustomobject]@{ text = $info.nodes[$n].text; cls = $info.nodes[$n].cls; rendered = ($pf | ForEach-Object { "$($_.familyName) [$($_.postScriptName)] custom=$($_.isCustomFont) glyphs=$($_.glyphCount)" }) -join '; ' }
      }
      $report = [pscustomobject]@{ label = $Label; mode = $mode; theme = $theme; faces = $info.fonts; nodes = @($rows) }
      $report | ConvertTo-Json -Depth 6 | Set-Content -Encoding utf8 (Join-Path $Out "$Label-fonts-$mode-$theme.json")
      $yahei = @($rows | Where-Object { $_.rendered -match 'YaHei' }).Count
      Say "fonts: $($rows.Count) CJK nodes, $yahei rendered with YaHei; faces: $($info.fonts -join ', ')"
    }
    if ($Shots) {
      $sw = [Windows.Forms.Screen]::PrimaryScreen.Bounds
      [Native]::Hover($sw.Width - 5, $sw.Height - 60) | Out-Null
      [Native]::Focus($hwnd) | Out-Null; Start-Sleep 1
      Shot $hwnd "$mode-$theme"
      if ($extras -contains 'hover') {
        $r = [Native]::Bounds($hwnd); $s = [Native]::GetDpiForWindow($hwnd) / 96.0
        foreach ($b in @(@{ n = 'close'; x = 23 }, @{ n = 'max'; x = 69 }, @{ n = 'min'; x = 115 })) {
          Say "hover $($b.n): $([Native]::Hover([int]($r.Right - $b.x * $s), [int]($r.Top + 20 * $s)))"
          Start-Sleep -Milliseconds 700
          Shot $hwnd "$mode-$theme-hover-$($b.n)"
        }
        [Native]::Hover($sw.Width - 5, $sw.Height - 60) | Out-Null
      }
      if ($extras -contains 'unfocused') {
        $f = Steal-Focus; Start-Sleep 1
        Say "foreground is XBot: $([Native]::GetForegroundWindow() -eq $hwnd)"
        Shot $hwnd "$mode-$theme-unfocused"
        $f.Close()
      }
    }
    if ($ws) { $ws.Dispose() }
  } finally {
    Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
    # A WebView2 browser left running would be reused with its old arguments.
    Get-Process msedgewebview2 -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep 2
  }
}

Add-Type -AssemblyName System.Windows.Forms
if ($Shots) {
  Run-Case rich light @('hover', 'unfocused')
  Run-Case rich dark @('hover', 'unfocused')
  Run-Case empty light @()
  Run-Case chat dark @()
} elseif ($Fonts) {
  Run-Case chat light @()
}
Set-AppTheme light
