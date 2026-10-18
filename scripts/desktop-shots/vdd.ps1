# CI only: adds a virtual display so the window can be shot at 2x.
# The hosted runner's only display tops out at 1920x1080 (175%), too small for a 2x 1024x672 window.
# Best effort: any failure leaves the runner's display as it was.
param([string] $Tag = '25.7.23')
$ErrorActionPreference = 'Stop'
$dir = Join-Path $env:RUNNER_TEMP 'vdd'
New-Item -ItemType Directory -Force $dir | Out-Null
Invoke-WebRequest "https://github.com/VirtualDrivers/Virtual-Display-Driver/releases/download/$Tag/VirtualDisplayDriver-x86.Driver.Only.zip" -OutFile "$dir\vdd.zip"
Expand-Archive "$dir\vdd.zip" "$dir\x" -Force
$inf = Get-ChildItem "$dir\x" -Recurse -Filter *.inf | Where-Object { $_.FullName -match 'amd64|x64' } | Select-Object -First 1
if (-not $inf) { $inf = Get-ChildItem "$dir\x" -Recurse -Filter *.inf | Select-Object -First 1 }
$hwid = ([regex]::Match((Get-Content $inf.FullName -Raw), '(?i)Root\\[A-Za-z0-9_]+')).Value
Write-Host "inf $($inf.FullName) hwid $hwid"
# Trust the driver's signer first, or the install waits on a publisher prompt nobody can answer.
Get-ChildItem $inf.DirectoryName -Filter *.cat | ForEach-Object {
  $cert = (Get-AuthenticodeSignature $_.FullName).SignerCertificate
  if ($cert) {
    foreach ($store in 'TrustedPublisher', 'Root') {
      $st = [Security.Cryptography.X509Certificates.X509Store]::new($store, 'LocalMachine'); $st.Open('ReadWrite'); $st.Add($cert); $st.Close()
    }
    Write-Host "trusted $($cert.Subject)"
  }
}
# The driver reads its modes from here; the defaults may not list 4K.
New-Item -ItemType Directory -Force 'C:\VirtualDisplayDriver' | Out-Null
@'
<?xml version="1.0" encoding="utf-8"?>
<vdd_settings>
  <monitors><count>1</count></monitors>
  <gpu><friendlyname>default</friendlyname></gpu>
  <global><g_refresh_rate>60</g_refresh_rate></global>
  <resolutions>
    <resolution><width>2560</width><height>1440</height><refresh_rate>60</refresh_rate></resolution>
    <resolution><width>3840</width><height>2160</height><refresh_rate>60</refresh_rate></resolution>
  </resolutions>
</vdd_settings>
'@ | Set-Content -Encoding utf8 'C:\VirtualDisplayDriver\vdd_settings.xml'
Add-Type -TypeDefinition @'
using System; using System.Runtime.InteropServices;
public static class Vdd {
  [StructLayout(LayoutKind.Sequential)] public struct SP_DEVINFO_DATA { public int cbSize; public Guid ClassGuid; public int DevInst; public IntPtr Reserved; }
  [DllImport("setupapi.dll", SetLastError = true)] static extern IntPtr SetupDiCreateDeviceInfoList(ref Guid g, IntPtr hwnd);
  [DllImport("setupapi.dll", SetLastError = true, CharSet = CharSet.Unicode)] static extern bool SetupDiCreateDeviceInfoW(IntPtr set, string name, ref Guid g, string desc, IntPtr hwnd, int flags, ref SP_DEVINFO_DATA d);
  [DllImport("setupapi.dll", SetLastError = true)] static extern bool SetupDiSetDeviceRegistryPropertyW(IntPtr set, ref SP_DEVINFO_DATA d, int prop, byte[] buf, int size);
  [DllImport("setupapi.dll", SetLastError = true)] static extern bool SetupDiCallClassInstaller(int fn, IntPtr set, ref SP_DEVINFO_DATA d);
  [DllImport("newdev.dll", SetLastError = true, CharSet = CharSet.Unicode)] static extern bool UpdateDriverForPlugAndPlayDevicesW(IntPtr hwnd, string hwid, string inf, int flags, out bool reboot);
  public static string Install(string hwid, string inf) {
    Guid g = new Guid("4d36e968-e325-11ce-bfc1-08002be10318");
    IntPtr set = SetupDiCreateDeviceInfoList(ref g, IntPtr.Zero);
    var d = new SP_DEVINFO_DATA(); d.cbSize = Marshal.SizeOf(d);
    if (!SetupDiCreateDeviceInfoW(set, "Display", ref g, null, IntPtr.Zero, 1, ref d)) return "create " + Marshal.GetLastWin32Error();
    byte[] id = System.Text.Encoding.Unicode.GetBytes(hwid + "\0\0");
    if (!SetupDiSetDeviceRegistryPropertyW(set, ref d, 1, id, id.Length)) return "hwid " + Marshal.GetLastWin32Error();
    if (!SetupDiCallClassInstaller(0x19, set, ref d)) return "register " + Marshal.GetLastWin32Error();
    bool reboot;
    if (!UpdateDriverForPlugAndPlayDevicesW(IntPtr.Zero, hwid, inf, 1, out reboot)) return "driver " + Marshal.GetLastWin32Error();
    return "ok reboot=" + reboot;
  }
}
'@
$job = Start-ThreadJob -ScriptBlock { param($h, $i) [Vdd]::Install($h, $i) } -ArgumentList $hwid, $inf.FullName
if (Wait-Job $job -Timeout 90) { Write-Host "install: $(Receive-Job $job)" } else { Write-Host 'install: still waiting after 90s; giving up'; exit 0 }
Start-Sleep 8
Get-PnpDevice -Class Display | ForEach-Object { Write-Host "display device: $($_.FriendlyName) [$($_.Status)] $($_.InstanceId)" }
Add-Type -AssemblyName System.Windows.Forms
[Windows.Forms.Screen]::AllScreens | ForEach-Object { Write-Host "screen $($_.DeviceName) $($_.Bounds) primary=$($_.Primary)" }
# Leave only the virtual display, so the app and the shots land on it.
$virt = Get-PnpDevice -Class Display | Where-Object { $_.FriendlyName -eq 'Virtual Display Driver' -and $_.Status -eq 'OK' }
if ($virt -and ([Windows.Forms.Screen]::AllScreens.Count -gt 1)) {
  # "Second screen only": the system topology switch, as Win+P does.
  & "$env:WINDIR\System32\DisplaySwitch.exe" /external
  Start-Sleep 6
  [Windows.Forms.Screen]::AllScreens | ForEach-Object { Write-Host "after switch: screen $($_.DeviceName) $($_.Bounds) primary=$($_.Primary)" }
  if ([Windows.Forms.Screen]::AllScreens.Count -gt 1) {
    Get-PnpDevice -Class Display | Where-Object { $_.FriendlyName -ne 'Virtual Display Driver' -and $_.Status -eq 'OK' } | ForEach-Object {
      Write-Host "disabling $($_.FriendlyName)"; Disable-PnpDevice -InstanceId $_.InstanceId -Confirm:$false
    }
    Start-Sleep 6
    [Windows.Forms.Screen]::AllScreens | ForEach-Object { Write-Host "after disable: screen $($_.DeviceName) $($_.Bounds) primary=$($_.Primary)" }
  }
} else { Write-Host 'virtual display not attached; keeping the runner display' }
