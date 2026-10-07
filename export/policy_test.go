package export

import (
	"errors"
	"strings"
	"testing"
)

func TestPolicyRejectsInvalidMarkerEncoding(t *testing.T) {
	for _, marker := range []string{"\xff", "\xfe", "safe\xffsuffix"} {
		p := Policy{PublicPaths: []string{"a"}, PrivatePaths: []string{"private"}, ForbiddenContent: []string{marker}}
		var diagnostic *Diagnostic
		if err := p.Validate(); !errors.As(err, &diagnostic) || diagnostic.Code != "policy_invalid_marker" {
			t.Fatalf("invalid marker encoding not rejected with redacted diagnostic: %v", err)
		}
	}
	a := Policy{PublicPaths: []string{"a"}, PrivatePaths: []string{"private"}, ForbiddenContent: []string{"приватне"}}
	b := a
	b.ForbiddenContent = []string{"інше"}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	if a.Digest() == b.Digest() {
		t.Fatal("distinct valid markers not bound")
	}
}

func TestPolicyValidation(t *testing.T) {
	valid := Policy{PublicPaths: []string{"README.md", "cmd/tool"}, PrivatePaths: []string{"docs/private", ".vitryna.yml"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []Policy{
		{PrivatePaths: []string{"secret"}},
		{PublicPaths: []string{"README.md"}},
		{PublicPaths: []string{"a"}, PrivatePaths: []string{"a/b"}},
		{PublicPaths: []string{"a/b"}, PrivatePaths: []string{"a"}},
		{PublicPaths: []string{"a", "a/b"}, PrivatePaths: []string{"secret"}},
		{PublicPaths: []string{"a"}, PrivatePaths: []string{"secret", "secret"}},
		{PublicPaths: []string{".github"}, PrivatePaths: []string{"secret"}},
		{PublicPaths: []string{".vitryna.yml"}, PrivatePaths: []string{"secret"}},
		{PublicPaths: []string{"../a"}, PrivatePaths: []string{"secret"}},
		{PublicPaths: []string{"a"}, PrivatePaths: []string{"../secret"}},
		{PublicPaths: []string{"CON.txt"}, PrivatePaths: []string{"secret"}},
		{PublicPaths: []string{"a"}, PrivatePaths: []string{"secret"}, ForbiddenContent: []string{""}},
		{PublicPaths: []string{"a"}, PrivatePaths: []string{"secret"}, ForbiddenContent: []string{"hidden-token", "hidden-token"}},
	}
	for i, p := range tests {
		if err := p.Validate(); err == nil {
			t.Errorf("invalid policy %d accepted", i)
		} else if strings.Contains(err.Error(), "hidden-token") {
			t.Fatal("marker value leaked")
		}
	}
}

func TestPolicyDigestCanonicalAndDoesNotMutate(t *testing.T) {
	a := Policy{PublicPaths: []string{"z", "a"}, PrivatePaths: []string{"private"}, ForbiddenContent: []string{"b", "a"}}
	if got := a.Digest(); got != "f77f7af77f7e35c5fdf71fd66eba5372c11a5acf8194948891a4213ee7c530a8" {
		t.Fatalf("canonical digest changed: %s", got)
	}
	b := Policy{PublicPaths: []string{"a", "z"}, PrivatePaths: []string{"private"}, ForbiddenContent: []string{"a", "b"}}
	if a.Digest() != b.Digest() {
		t.Fatal("rule order changed digest")
	}
	if a.PublicPaths[0] != "z" || a.ForbiddenContent[0] != "b" {
		t.Fatal("digest mutated input")
	}
	if a.Digest() == (Policy{PublicPaths: []string{"z", "a"}, PrivatePaths: []string{"private"}}).Digest() {
		t.Fatal("markers not bound")
	}
}
