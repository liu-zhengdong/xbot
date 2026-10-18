# dmgbuild settings for the drag-to-Applications disk image.
# dmgbuild -s packaging/macos/dmg-settings.py -D app=<path/XBot.app> \
#   -D background=<path/dmg-background.png> XBot <out.dmg>
# Runs on macOS (it needs hdiutil); scripts/build-macos-dmg.sh passes both paths.
app = defines["app"]  # noqa: F821 (provided by dmgbuild; __file__ is not)

format = "UDZO"
filesystem = "HFS+"
files = [app]
symlinks = {"Applications": "/Applications"}
hide_extension = ["XBot.app"]

# dmgbuild combines dmg-background.png with dmg-background@2x.png for Retina.
background = defines["background"]  # noqa: F821
window_rect = ((200, 120), (660, 400))
default_view = "icon-view"
show_status_bar = False
show_tab_view = False
show_toolbar = False
show_pathbar = False
show_sidebar = False
icon_size = 112
text_size = 13
icon_locations = {
    "XBot.app": (170, 180),
    "Applications": (490, 180),
}
