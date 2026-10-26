# Leasekeeper demo — two agents, one resource pair, one refused cycle.
#
# Renders assets/demo.gif, the recording embedded in README.md.
#
# Render it by hand:
#
#   go build -o "$HOME/.local/bin/leasekeeper" ./cmd/leasekeeper
#   vhs docs/demo.tape
#
# `.github/workflows/demo.yml` re-renders it on demand; the committed GIF is
# the source of truth, so a flaky render can never block a release.
#
# The tape drives the real binary: `leasekeeper demo` starts a broker on a
# temporary socket and plays the same two-agent cycle the tests cover.

Output assets/demo.gif

Set Shell bash
Set FontSize 14
Set Width 880
Set Height 764
Set Padding 20
Set Margin 0
Set MarginFill "#050c0d"
Set BorderRadius 8
Set Framerate 24
Set TypingSpeed 55ms
Set Theme '{"name":"leasekeeper","black":"#0a1713","red":"#e05c5c","green":"#56e656","yellow":"#d9c46a","blue":"#6ae7ac","purple":"#c7f6ac","cyan":"#6ae7ac","white":"#f4fdee","brightBlack":"#557b6b","brightRed":"#ff7b72","brightGreen":"#7bf07b","brightYellow":"#f0e08a","brightBlue":"#8bf0c4","brightPurple":"#dcffd0","brightCyan":"#8bf0c4","brightWhite":"#ffffff","background":"#050c0d","foreground":"#f4fdee","cursor":"#56e656","selection":"#173c1d"}'

Type "leasekeeper demo --pause 3s"
Enter
Sleep 30s
