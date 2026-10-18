# CI check for the Windows installer (packaging/windows/xbot.nsi).
# Records the user environment (every HKCU\Environment value with its registry
# type), the machine PATH and a sentinel variable, then installs silently,
# upgrades in place, uninstalls, and fails if anything in the environment
# changed or the user data in %USERPROFILE%\.xbot was touched.
# Usage: pwsh scripts/test-windows-installer.ps1 -Setup <XBot-...-setup.exe>
param([Parameter(Mandatory = $true)][string]$Setup)
$ErrorActionPreference = 'Stop'
$Setup = (Resolve-Path $Setup).Path

function Get-EnvSnapshot {
	$out = [ordered]@{}
	$user = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
	foreach ($n in ($user.GetValueNames() | Sort-Object)) {
		$out["HKCU:$n"] = '{0}|{1}' -f $user.GetValueKind($n), $user.GetValue($n, $null, 'DoNotExpandEnvironmentNames')
	}
	$m = [Microsoft.Win32.Registry]::LocalMachine.OpenSubKey('SYSTEM\CurrentControlSet\Control\Session Manager\Environment')
	$out['HKLM:Path'] = '{0}|{1}' -f $m.GetValueKind('Path'), $m.GetValue('Path', $null, 'DoNotExpandEnvironmentNames')
	return $out
}

function Assert-True([bool]$cond, [string]$what) {
	if (-not $cond) { throw "FAIL: $what" }
	Write-Host "ok: $what"
}

function Wait-Gone([string]$path, [string]$what) {
	for ($i = 0; $i -lt 60 -and (Test-Path $path); $i++) { Start-Sleep -Seconds 1 }
	Assert-True (-not (Test-Path $path)) $what
}

# A long REG_EXPAND_SZ user PATH (over setx's 1024-character limit) and a
# sentinel, so truncation, type changes or a rewrite would show up.
$envKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$oldPath = $envKey.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
$extra = (1..40 | ForEach-Object { "%USERPROFILE%\xbot-ci-path-entry-$_" }) -join ';'
$envKey.SetValue('Path', ($extra + ';' + $oldPath).TrimEnd(';'), 'ExpandString')
$envKey.SetValue('XBOT_CI_SENTINEL', '%USERPROFILE%\sentinel;keep me', 'ExpandString')
$envKey.Close()
$before = Get-EnvSnapshot
Assert-True ($before['HKCU:Path'].Length -gt 1100) 'user PATH is longer than 1024 characters before install'

$data = Join-Path $env:USERPROFILE '.xbot'
New-Item -ItemType Directory -Force $data | Out-Null
Set-Content -Path (Join-Path $data 'ci-keep.txt') -Value 'user data'

$dir = Join-Path $env:LOCALAPPDATA 'Programs\XBot'
$uninstKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\XBot'
$startMenu = Join-Path ([Environment]::GetFolderPath('Programs')) 'XBot.lnk'
$desktop = Join-Path ([Environment]::GetFolderPath('Desktop')) 'XBot.lnk'

foreach ($round in 'install', 'upgrade') {
	$p = Start-Process -FilePath $Setup -ArgumentList '/S' -Wait -PassThru
	Assert-True ($p.ExitCode -eq 0) "$round exits 0 (got $($p.ExitCode))"
	Assert-True (Test-Path (Join-Path $dir 'XBot.exe')) "$round puts XBot.exe in $dir"
	Assert-True (Test-Path (Join-Path $dir 'xbot-cli.exe')) "$round puts xbot-cli.exe in $dir"
	Assert-True (Test-Path $startMenu) "$round adds the Start menu shortcut"
	Assert-True (Test-Path $desktop) "$round adds the desktop shortcut"
	$entry = Get-ItemProperty $uninstKey
	Assert-True ($entry.DisplayName -eq 'XBot' -and $entry.UninstallString -like '*uninstall.exe*') "$round registers the uninstaller in Apps & Features"
	$ver = & (Join-Path $dir 'xbot-cli.exe') version 2>&1
	Assert-True ($LASTEXITCODE -eq 0 -and "$ver" -like 'v*') "$round installs a working CLI ($ver)"
	Assert-True ((Get-Content (Join-Path $data 'ci-keep.txt')) -eq 'user data') "$round keeps %USERPROFILE%\.xbot"
}

$p = Start-Process -FilePath (Join-Path $dir 'uninstall.exe') -ArgumentList '/S' -Wait -PassThru
Assert-True ($p.ExitCode -eq 0) "uninstall exits 0 (got $($p.ExitCode))"
Wait-Gone (Join-Path $dir 'XBot.exe') 'uninstall removes XBot.exe'
Wait-Gone $uninstKey 'uninstall removes the Apps & Features entry'
Wait-Gone $startMenu 'uninstall removes the Start menu shortcut'
Wait-Gone $desktop 'uninstall removes the desktop shortcut'
Assert-True ((Get-Content (Join-Path $data 'ci-keep.txt')) -eq 'user data') 'uninstall keeps %USERPROFILE%\.xbot'

$after = Get-EnvSnapshot
$diff = @()
foreach ($k in ($before.Keys + $after.Keys | Sort-Object -Unique)) {
	if ($before[$k] -cne $after[$k]) { $diff += $k }
}
if ($diff.Count -gt 0) {
	# Names only: values can be long and are not needed to see what moved.
	throw "FAIL: environment changed: $($diff -join ', ')"
}
Write-Host "ok: user environment ($($before.Count - 1) values, types included) and machine PATH unchanged after install, upgrade and uninstall"
