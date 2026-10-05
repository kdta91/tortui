# Regenerate with `make demo-gif` (dev-only: needs vhs, ttyd and ffmpeg).
# Records `tortui --demo` only: synthetic data, no network.
Output docs/demo/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 440
Set Padding 10
Set Theme "Dracula"
Set TypingSpeed 70ms
Set Framerate 12

# A short sandbox path instead of the long per-user temp directory.
Env TMPDIR "/tmp"

# Off camera: launch, and clear the demo's four scripted active rows (a stall and
# two failures among them) from Downloads with Remove, keep data, so the take
# shows one healthy download.
Hide
Type "./bin/tortui --demo"
Enter
Sleep 1500ms
Type "4"
Sleep 500ms
Type "x"
Sleep 300ms
Up
Up
Enter
Sleep 300ms
Type "x"
Sleep 300ms
Up
Up
Enter
Sleep 300ms
Type "x"
Sleep 300ms
Up
Up
Enter
Sleep 300ms
Type "x"
Sleep 300ms
Up
Up
Enter
Sleep 300ms
Type "1"
Sleep 12s

# Still off camera: choose the sources on the Search screen (the mirror only; the
# demo's other fixture rows carry emoji and CJK titles that render off-width, so
# this keeps the take to plain ASCII titles without touching the fixtures, and
# the failing source is switched off too) and run one search, so the status bar
# no longer shows the removals above (their status messages queue, hence the wait).
Down 2
Sleep 300ms
Space
Down 2
Sleep 300ms
Space
Up 4
Sleep 300ms
Type "/"
Type "weath"
Enter
Sleep 2s
Type "1"
Sleep 800ms
Show

# On camera: finish the query and search.
Type "/"
Type "er"
Sleep 800ms
Enter
Sleep 3s

Type "d"
Sleep 3s

Enter
Sleep 3s
Enter
Sleep 1500ms

Type "4"
Sleep 5s
