package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
		out  string
		err  string
	}{
		{"default help", nil, 0, "Usage: vitryna", ""},
		{"help", []string{"help"}, 0, "check", ""},
		{"help flag", []string{"--help"}, 0, "export", ""},
		{"short help", []string{"-h"}, 0, "version", ""},
		{"version", []string{"version"}, 0, "vitryna test-version\n", ""},
		{"version flag", []string{"--version"}, 0, "vitryna test-version\n", ""},
		{"unknown command", []string{"oops"}, 2, "", "unknown command"},
		{"unknown flag", []string{"--oops"}, 2, "", "unknown command"},
		{"version extra", []string{"version", "extra"}, 2, "", "unexpected arguments"},
		{"help extra", []string{"help", "extra"}, 2, "", "unexpected arguments"},
		{"init missing provider", []string{"init"}, 2, "", "init requires github"},
		{"reconcile missing identity", []string{"workflow", "reconcile"}, 2, "", "requires exact seal identity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errs bytes.Buffer
			if code := Run(context.Background(), tt.args, &out, &errs, "test-version"); code != tt.code {
				t.Fatalf("exit = %d; want %d; stderr=%q", code, tt.code, errs.String())
			}
			if !strings.Contains(out.String(), tt.out) || !strings.Contains(errs.String(), tt.err) {
				t.Fatalf("stdout=%q stderr=%q; want %q / %q", out.String(), errs.String(), tt.out, tt.err)
			}
			if tt.out == "" && out.Len() != 0 {
				t.Fatalf("unexpected stdout: %q", out.String())
			}
			if tt.err == "" && errs.Len() != 0 {
				t.Fatalf("unexpected stderr: %q", errs.String())
			}
		})
	}
}

func TestCancelledContextFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errs bytes.Buffer
	if code := Run(ctx, []string{"version"}, &out, &errs, "test-version"); code != 1 || out.Len() != 0 {
		t.Fatalf("cancelled invocation: exit=%d stdout=%q", code, out.String())
	}
}
