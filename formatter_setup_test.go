package compiler

import (
	"strings"
	"testing"
)

// setup is formatted right after the hardware block, with or without comments.
func TestFormatPlacesSetupAfterHardware(t *testing.T) {
	cases := map[string]string{
		"no comments": "hardware {\n    pin led = 2\n}\n\nvar n: int = 0\n\nevery 1s {\n    led.toggle()\n}\n\nsetup {\n    n = 1\n}\n",
		"comments":    "// blink\nhardware {\n    pin led = 2\n}\n\nvar n: int = 0\n\nevery 1s {\n    led.toggle()\n}\n\n// runs once at boot\nsetup {\n    n = 1 // start\n}\n",
	}
	for name, src := range cases {
		out, err := Format(src)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		hwEnd := strings.Index(out, "}\n")
		rest := strings.TrimLeft(out[hwEnd+2:], "\n")
		if name == "comments" {
			if !strings.HasPrefix(rest, "// runs once at boot\nsetup {") {
				t.Errorf("%s: setup (with its comment) not right after hardware:\n%s", name, out)
			}
			if !strings.Contains(out, "n = 1 // start") || !strings.HasPrefix(out, "// blink\n") {
				t.Errorf("%s: comments lost:\n%s", name, out)
			}
		} else if !strings.HasPrefix(rest, "setup {") {
			t.Errorf("%s: setup not right after hardware:\n%s", name, out)
		}
		if strings.Contains(out, "\n\n\n") {
			t.Errorf("%s: double blank line:\n%s", name, out)
		}
		if strings.Count(out, "setup {") != 1 {
			t.Errorf("%s: setup duplicated or lost:\n%s", name, out)
		}
		again, _ := Format(out)
		if again != out {
			t.Errorf("%s: not stable on a second format:\n%s\n---\n%s", name, out, again)
		}
	}
}

// Already in place, or no hardware block: the source is left as it is.
func TestFormatSetupAlreadyPlaced(t *testing.T) {
	src := "// x\nhardware {\n    pin led = 2\n}\n\nsetup {\n    led.on()\n}\n\nevery 1s {\n    led.toggle()\n}\n"
	if out, _ := Format(src); out != src {
		t.Errorf("changed a program already in order:\n%s", out)
	}
}
