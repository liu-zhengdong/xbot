// Win32 helpers for scripts/desktop-shots/windows.ps1: find, focus and measure
// the XBot window, move the mouse, and try a bigger display at a higher scale.
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;

public static class Native {
  [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr v);
  [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
  public delegate bool EnumProc(IntPtr h, IntPtr l);
  [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc f, IntPtr l);
  [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
  [DllImport("dwmapi.dll")] static extern int DwmGetWindowAttribute(IntPtr h, int attr, out RECT r, int size);
  [DllImport("user32.dll")] static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] static extern void keybd_event(byte vk, byte scan, uint flags, UIntPtr extra);
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();

  public static void PerMonitorAware() { SetProcessDpiAwarenessContext(new IntPtr(-4)); }

  // The visible top-level window of pid whose title is title, or zero.
  public static IntPtr Find(uint pid, string title) {
    IntPtr found = IntPtr.Zero;
    EnumWindows((h, l) => {
      uint p; GetWindowThreadProcessId(h, out p);
      if (p != pid || !IsWindowVisible(h)) return true;
      var sb = new StringBuilder(256); GetWindowText(h, sb, 256);
      if (sb.ToString() == title) { found = h; return false; }
      return true;
    }, IntPtr.Zero);
    return found;
  }

  // Bounds without the invisible resize border, in physical pixels.
  public static RECT Bounds(IntPtr h) {
    RECT r; DwmGetWindowAttribute(h, 9, out r, Marshal.SizeOf(typeof(RECT))); return r;
  }

  // An Alt tap lets a background process take the foreground.
  public static bool Focus(IntPtr h) {
    keybd_event(0x12, 0, 0, UIntPtr.Zero); bool ok = SetForegroundWindow(h); keybd_event(0x12, 0, 2, UIntPtr.Zero); return ok;
  }

  [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)] public struct DEVMODE {
    [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)] public string dmDeviceName;
    public short dmSpecVersion, dmDriverVersion, dmSize, dmDriverExtra; public int dmFields;
    public int dmPositionX, dmPositionY, dmDisplayOrientation, dmDisplayFixedOutput;
    public short dmColor, dmDuplex, dmYResolution, dmTTOption, dmCollate;
    [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)] public string dmFormName;
    public short dmLogPixels; public int dmBitsPerPel, dmPelsWidth, dmPelsHeight, dmDisplayFlags, dmDisplayFrequency;
    public int dmICMMethod, dmICMIntent, dmMediaType, dmDitherType, dmReserved1, dmReserved2, dmPanningWidth, dmPanningHeight;
  }
  [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern bool EnumDisplaySettings(string dev, int mode, ref DEVMODE dm);
  [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern int ChangeDisplaySettings(ref DEVMODE dm, int flags);

  static DEVMODE NewMode() { var d = new DEVMODE(); d.dmSize = (short)Marshal.SizeOf(typeof(DEVMODE)); return d; }

  public static string Current() { var d = NewMode(); EnumDisplaySettings(null, -1, ref d); return d.dmPelsWidth + "x" + d.dmPelsHeight; }

  // Switch to the largest mode no wider than maxW; returns what happened.
  public static string Biggest(int maxW) {
    DEVMODE best = NewMode(); bool any = false; var seen = new List<string>();
    for (int i = 0; ; i++) {
      var d = NewMode(); if (!EnumDisplaySettings(null, i, ref d)) break;
      string m = d.dmPelsWidth + "x" + d.dmPelsHeight; if (!seen.Contains(m)) seen.Add(m);
      if (d.dmPelsWidth <= maxW && (!any || d.dmPelsWidth * d.dmPelsHeight > best.dmPelsWidth * best.dmPelsHeight)) { best = d; any = true; }
    }
    string modes = "modes " + string.Join(",", seen.ToArray());
    if (!any) return modes + "; none";
    best.dmFields = 0x80000 | 0x100000;
    int r = ChangeDisplaySettings(ref best, 0);
    return modes + "; set " + best.dmPelsWidth + "x" + best.dmPelsHeight + " -> " + r + "; now " + Current();
  }

  [StructLayout(LayoutKind.Sequential)] public struct LUID { public uint Low; public int High; }
  [StructLayout(LayoutKind.Sequential)] struct PATH_SOURCE { public LUID adapterId; public uint id; public uint modeInfoIdx; public uint statusFlags; }
  [StructLayout(LayoutKind.Sequential)] struct RATIONAL { public uint n, d; }
  [StructLayout(LayoutKind.Sequential)] struct PATH_TARGET { public LUID adapterId; public uint id; public uint modeInfoIdx; public int outputTechnology, rotation, scaling; public RATIONAL refresh; public int scanLineOrdering, targetAvailable; public uint statusFlags; }
  [StructLayout(LayoutKind.Sequential)] struct PATH_INFO { public PATH_SOURCE source; public PATH_TARGET target; public uint flags; }
  [StructLayout(LayoutKind.Sequential, Size = 64)] struct MODE_INFO { public int infoType; public uint id; public LUID adapterId; }
  [StructLayout(LayoutKind.Sequential)] struct HEADER { public int type, size; public LUID adapterId; public uint id; }
  [StructLayout(LayoutKind.Sequential)] struct DPI_GET { public HEADER header; public int minRel, curRel, maxRel; }
  [StructLayout(LayoutKind.Sequential)] struct DPI_SET { public HEADER header; public int rel; }
  [DllImport("user32.dll")] static extern int GetDisplayConfigBufferSizes(uint flags, out uint np, out uint nm);
  [DllImport("user32.dll")] static extern int QueryDisplayConfig(uint flags, ref uint np, [Out] PATH_INFO[] p, ref uint nm, [Out] MODE_INFO[] m, IntPtr topo);
  [DllImport("user32.dll")] static extern int DisplayConfigGetDeviceInfo(ref DPI_GET p);
  [DllImport("user32.dll")] static extern int DisplayConfigSetDeviceInfo(ref DPI_SET p);
  static readonly int[] Scales = { 100, 125, 150, 175, 200, 225, 250, 300, 350, 400, 450, 500 };

  // Ask for percent, or the highest scale the display allows below it
  // (undocumented DISPLAYCONFIG get/set DPI scale, as Settings uses).
  public static string Scale(int percent) {
    uint np, nm; GetDisplayConfigBufferSizes(2, out np, out nm);
    var p = new PATH_INFO[np]; var m = new MODE_INFO[nm];
    int r = QueryDisplayConfig(2, ref np, p, ref nm, m, IntPtr.Zero); if (r != 0) return "query " + r;
    var g = new DPI_GET(); g.header.type = -3; g.header.size = Marshal.SizeOf(typeof(DPI_GET));
    g.header.adapterId = p[0].source.adapterId; g.header.id = p[0].source.id;
    r = DisplayConfigGetDeviceInfo(ref g); if (r != 0) return "get " + r;
    int rec = Math.Abs(g.minRel);
    int want = Array.IndexOf(Scales, percent) - rec;
    if (want > g.maxRel) want = g.maxRel;
    string info = "recommended " + Scales[rec] + "%, allowed up to " + Scales[Math.Min(rec + g.maxRel, Scales.Length - 1)] + "%";
    var s = new DPI_SET(); s.header = g.header; s.header.type = -4; s.header.size = Marshal.SizeOf(typeof(DPI_SET)); s.rel = want;
    r = DisplayConfigSetDeviceInfo(ref s);
    return info + "; set " + Scales[rec + want] + "% -> " + r;
  }
}
