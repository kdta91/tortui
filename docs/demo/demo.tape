# Regenerate with `make demo-gif` (dev-only: needs vhs, ttyd and ffmpeg).
# Records `tortui --demo` only: synthetic data, no network.
Output docs/demo/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 560
Set Padding 10
Set Theme "Dracula"
Set TypingSpeed 70ms
Set Framerate 12

Hide
Type "./bin/tortui --demo"
Enter
Sleep 1500ms
Show

Type "/"
Type "archive"
Sleep 600ms
Enter
Sleep 2s

Down
Sleep 400ms
Down
Sleep 800ms

Type "d"
Sleep 3s

Enter
Sleep 3s
Enter
Sleep 1500ms

Type "4"
Sleep 6s
