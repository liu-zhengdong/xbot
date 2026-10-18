# CI only: lets the screenshot builds open a DevTools port for the font check.
# MyGo's WebView2 environment options object answers "no extra browser
# arguments" and ignores what the loader or runtime tries to set, so
# WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS and the AdditionalBrowserArguments
# policy never reach the browser. This copies MyGo into the runner's temp
# directory, makes get_AdditionalBrowserArguments return that variable, and
# points the given module at the copy with a go.mod replace that is never
# committed. Shipped builds are untouched.
param([Parameter(Mandatory)] [string[]] $Modules)
$ErrorActionPreference = 'Stop'
$src = (go list -m -f '{{.Dir}}' github.com/egoist/mygo).Trim()
$dst = Join-Path $env:RUNNER_TEMP 'mygo-shots'
if (-not (Test-Path $dst)) {
  Copy-Item -Recurse $src $dst
  Get-ChildItem -Recurse -File $dst | ForEach-Object { $_.IsReadOnly = $false }
  $f = Join-Path $dst 'internal\windows\webview2_options.go'
  $old = 'str(func(*envOptions) []uint16 { return nil }), // get_AdditionalBrowserArguments'
  $s = [IO.File]::ReadAllText($f)
  if (-not $s.Contains($old)) { throw "MyGo changed: $old not found in $f" }
  [IO.File]::WriteAllText($f, $s.Replace($old, 'str(func(*envOptions) []uint16 { return shotsBrowserArgs() }), // get_AdditionalBrowserArguments'))
  [IO.File]::WriteAllText((Join-Path $dst 'internal\windows\shots_args.go'), @'
//go:build windows && (amd64 || arm64)

package windows

import "os"

// shotsBrowserArgs is the CI screenshot patch: extra browser arguments from
// the environment, or none.
func shotsBrowserArgs() []uint16 {
	if a := os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"); a != "" {
		return utf16z(a)
	}
	return nil
}
'@)
}
foreach ($m in $Modules) {
  Push-Location $m
  go mod edit "-replace=github.com/egoist/mygo=$dst"
  Pop-Location
  "patched MyGo for $m"
}
