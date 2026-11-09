package core

import "testing"

func TestLaunchCommands(t *testing.T) {
	c, p, w := LaunchCommands(`M:\work\my proj`, "home2")
	if c != `cd /d "M:\work\my proj" && acsw codex --expect-home home2` || p != `Set-Location -LiteralPath 'M:\work\my proj'; acsw codex --expect-home home2` || w != "" {
		t.Fatal(c, p, w)
	}
	c, p, _ = LaunchCommands(`D:\`, "h.2")
	if c != `cd /d "D:\" && acsw codex --expect-home h.2` || p != `Set-Location -LiteralPath 'D:\'; acsw codex --expect-home h.2` {
		t.Fatal(c, p)
	}
	c, p, w = LaunchCommands(`C:\it's $x [1]`, "h")
	if c != `cd /d "C:\it's $x [1]" && acsw codex --expect-home h` || p != `Set-Location -LiteralPath 'C:\it''s $x [1]'; acsw codex --expect-home h` || w != "" {
		t.Fatal(c, p, w)
	}
	if c, p, w = LaunchCommands(`C:\100%`, "h"); c == "" || p == "" || w == "" {
		t.Fatal(c, p, w)
	}
	if c, p, w = LaunchCommands("C:\\a\"b", "h"); c != "" || p != "" || w == "" {
		t.Fatal(c, p, w)
	}
	if c, p, _ = LaunchCommands("", "h"); c != "" || p != "" {
		t.Fatal(c, p)
	}
}
