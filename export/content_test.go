package export

import (
	"bytes"
	"errors"
	"testing"
)

func TestContentGuardSplitMarker(t *testing.T) {
	for _, forbidden := range []bool{false, true} {
		var destination bytes.Buffer
		guard := contentGuard{destination: &destination, markers: []string{"FORBIDDEN"}, path: "public.txt"}
		if _, err := guard.Write([]byte("safe FOR")); err != nil {
			t.Fatal(err)
		}
		second := "BIDDEX tail"
		if forbidden {
			second = "BIDDEN tail"
		}
		_, err := guard.Write([]byte(second))
		if forbidden {
			var d *Diagnostic
			if !errors.As(err, &d) || d.Code != "public_forbidden_content" {
				t.Fatalf("split marker not rejected: %v", err)
			}
			if destination.String() != "safe FOR" {
				t.Fatal("rejected write reached destination")
			}
		} else if err != nil || destination.String() != "safe FOR"+second {
			t.Fatalf("safe split rejected: %v %q", err, destination.String())
		}
	}
}
