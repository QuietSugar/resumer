# resumer demo — unified list + native TUI picker, grouped by workspace.
#
# Records against SYNTHETIC fixtures (tests/demo/gen-fixtures.sh), never real
# session data — the GIF is committed and public. Timestamps are generated
# relative to "now", so re-recording always looks fresh.
# Output is the committed README GIF at docs/demo.gif.

Output "docs/demo.gif"

Set Shell "bash"
Set FontSize 13
Set Width 1720
Set Height 780
Set TypingSpeed 50ms
Set Theme "Dracula"

Hide
Type "./tests/demo/gen-fixtures.sh >/dev/null && source tests/output/demo-fixtures/env.sh && mkdir -p /Users/demo/code/api-server /Users/demo/code/webapp /Users/demo/code/data-pipeline /Users/demo/dotfiles && clear"
Enter
Sleep 1s
Show

# 1. Merged list — full agent names, sessions grouped by workspace.
Type "resumer list"
Enter
Sleep 3s

# 2. Native TUI picker with live preview pane.
Type "resumer"
Enter
Sleep 2.5s

# Browse — preview pane follows the selection.
Down
Sleep 700ms
Down
Sleep 700ms
Up
Sleep 900ms

# Filter ('/' opens the fuzzy filter).
Type "/"
Sleep 400ms
Type "auth"
Sleep 1500ms
Enter
Sleep 1200ms

# Clear the filter, then cancel (mocks guard against a real resume anyway).
Escape
Sleep 600ms
Escape
Sleep 800ms
