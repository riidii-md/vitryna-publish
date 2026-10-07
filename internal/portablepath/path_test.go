package portablepath

import (
	"strings"
	"testing"
)

func TestPathByteBoundary(t *testing.T) {
	for _, atLimit := range []string{strings.Repeat("a/", 511) + "aa", strings.Repeat("é/", 341) + "x"} {
		if len(atLimit) != 1024 {
			t.Fatal("invalid byte boundary fixture")
		}
		if err := Validate(atLimit); err != nil {
			t.Fatalf("valid 1024-byte path rejected: %v", err)
		}
		if err := Validate(atLimit + "a"); err == nil {
			t.Fatal("otherwise-valid 1025-byte path accepted")
		}
	}
}

func TestValidate(t *testing.T) {
	for _, p := range []string{"README.md", ".github/workflows/test.yml", "src/café.go", "a b/c"} {
		if err := Validate(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	for _, p := range []string{"", ".", "..", "../a", "/a", "a//b", "a/./b", "a/../b", "a\\b", "a\x00b", "a\nb", "a/.GIT/x", "CON.txt", "com1", "LPT³.txt", "a/NUL", "a.", "a ", "a:b", "a?", "cafe\u0301", string([]byte{0xff})} {
		if err := Validate(p); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
}

func TestCollisionPrefixes(t *testing.T) {
	for _, pair := range [][2]string{{"Foo/a", "foo/b"}, {"a", "a/b"}, {"a/b", "a"}, {"Straße/a", "STRASSE/b"}, {"a/b", "a/b"}} {
		set := NewSet()
		if err := set.Add(pair[0]); err != nil {
			t.Fatal(err)
		}
		if err := set.Add(pair[1]); err == nil {
			t.Errorf("missed collision %q / %q", pair[0], pair[1])
		}
	}
	set := NewSet()
	for _, p := range []string{"a/b", "a/c", "a/sub/d"} {
		if err := set.Add(p); err != nil {
			t.Fatal(err)
		}
	}
}
