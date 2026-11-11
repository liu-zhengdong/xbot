// Helpers for macos.sh: `find <pid>` prints "<window id> <x> <y> <w> <h>" (points)
// for the pid's main window; `move <x> <y>` moves the mouse (hover states).
import CoreGraphics
import Foundation

let args = CommandLine.arguments
if args.count == 3, args[1] == "find", let pid = Int(args[2]) {
  let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as? [[String: Any]] ?? []
  for w in list where (w[kCGWindowOwnerPID as String] as? Int) == pid && (w[kCGWindowLayer as String] as? Int) == 0 {
    let b = w[kCGWindowBounds as String] as? [String: Double] ?? [:]
    print(w[kCGWindowNumber as String] as? Int ?? 0, Int(b["X"] ?? 0), Int(b["Y"] ?? 0), Int(b["Width"] ?? 0), Int(b["Height"] ?? 0))
    exit(0)
  }
  exit(1)
} else if args.count == 4, args[1] == "move", let x = Double(args[2]), let y = Double(args[3]) {
  let p = CGPoint(x: x, y: y)
  CGWarpMouseCursorPosition(p)
  CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: p, mouseButton: .left)?.post(tap: .cghidEventTap)
  exit(0)
}
FileHandle.standardError.write("usage: macwin find <pid> | move <x> <y>\n".data(using: .utf8)!)
exit(2)
