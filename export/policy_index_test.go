package export

import (
	"errors"
	"fmt"
	"testing"
)

func TestPolicyDiagnosticOrder(t *testing.T) {
	for _, row := range []struct {
		name             string
		public, private  []string
		code, path, rule string
	}{
		{"later ancestor", []string{"public"}, []string{"a/z", "a/b", "a"}, "policy_redundant_rule", "a", "a/z"},
		{"earlier ancestor", []string{"public"}, []string{"a", "a/b"}, "policy_redundant_rule", "a/b", "a"},
		{"duplicate", []string{"public"}, []string{"a", "a"}, "policy_redundant_rule", "a", "a"},
		{"conflict before unsafe", []string{"public"}, []string{"a", "a/b", "../bad"}, "policy_redundant_rule", "a/b", "a"},
		{"unsafe before conflict", []string{"public"}, []string{"../bad", "a", "a/b"}, "policy_unsafe_rule", "../bad", ""},
		{"list checks before portability", []string{"CON.txt"}, []string{"a", "a/b"}, "policy_redundant_rule", "a/b", "a"},
		{"cross first descendant", []string{"a"}, []string{"a/z", "a/b"}, "policy_visibility_overlap", "a", "a/z"},
		{"cross ancestor", []string{"a/b"}, []string{"a"}, "policy_visibility_overlap", "a/b", "a"},
		{"portability before cross", []string{"CON.txt"}, []string{"CON.txt"}, "public_path_invalid", "CON.txt", ""},
		{"cross before control", []string{".github"}, []string{".github/other"}, "policy_visibility_overlap", ".github", ".github/other"},
		{"control", []string{".github"}, []string{"private"}, "policy_public_control", ".github", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			policy := Policy{PublicPaths: row.public, PrivatePaths: row.private}
			var got *Diagnostic
			if err := policy.Validate(); !errors.As(err, &got) || got.Code != row.code || got.Path != row.path || got.Rule != row.rule {
				t.Fatalf("diagnostic: %#v, want %s/%s/%s", got, row.code, row.path, row.rule)
			}
		})
	}
}

func BenchmarkPolicyExactRules(b *testing.B) {
	for _, count := range []int{1000, 10000, 60000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			policy := Policy{PublicPaths: []string{"README.md"}}
			for i := 0; i < count; i++ {
				policy.PrivatePaths = append(policy.PrivatePaths, fmt.Sprintf("p%05d", i))
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := policy.Validate(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
