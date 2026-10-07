package githubpublish

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestReconcileFinalSourceFreshnessPreservesRefs(t *testing.T) {
	for _, scenario := range []string{"absent promotion update", "owned promotion deletion"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			remote, base, tree, receipt := targetFixture(t)
			p, original := ownedFixture()
			original.pulls, original.head = nil, ""
			api := &localAPI{fakeAPI: original, remote: remote, nextNumber: 10}
			p.API = api
			request := ReconcileRequest{Directory: tree, Receipt: receipt, Remote: remote, BotLogin: "fixture[bot]"}
			expectedPromotion := ""
			if scenario == "owned promotion deletion" {
				if _, err := p.Reconcile(ctx, request); err != nil {
					t.Fatal(err)
				}
				expectedPromotion = runGit(t, remote, "rev-parse", "refs/heads/vitryna/public")
				runGit(t, remote, "update-ref", "refs/heads/main", expectedPromotion, base)
				base = expectedPromotion
			}
			baseReads := 0
			stale := false
			staleErr := errors.New("synthetic source advanced before ref mutation")
			api.onRef = func(branch string) {
				if branch == "main" {
					baseReads++
					if baseReads == 2 {
						stale = true
					}
				}
			}
			p.Fresh = func(context.Context) error {
				if stale {
					return staleErr
				}
				return nil
			}
			api.calls = nil
			result, err := p.Reconcile(ctx, request)
			// Independently read the durable remote ref, not the API's call log.
			data, readErr := exec.Command("git", "-C", remote, "rev-parse", "--verify", "refs/heads/vitryna/public").Output()
			observedPromotion := strings.TrimSpace(string(data))
			if expectedPromotion == "" && readErr == nil {
				t.Fatalf("stale run created promotion ref %s; result=%#v err=%v", observedPromotion, result, err)
			}
			if expectedPromotion != "" && (readErr != nil || observedPromotion != expectedPromotion) {
				t.Fatalf("stale run deleted/changed owned promotion ref; want=%s got=%s readErr=%v result=%#v err=%v", expectedPromotion, observedPromotion, readErr, result, err)
			}
			if observedBase := runGit(t, remote, "rev-parse", "refs/heads/main"); observedBase != base {
				t.Fatalf("public base changed: want=%s got=%s", base, observedBase)
			}
			if !errors.Is(err, staleErr) || baseReads != 2 {
				t.Fatalf("wrong stale boundary: reads=%d result=%#v err=%v", baseReads, result, err)
			}
			for _, call := range api.calls {
				if call == "create" || call == "reopen" || call == "enable" {
					t.Fatalf("stale source caused new publication: %s", call)
				}
			}
			t.Logf("last-minute stale prevented ref mutation (%s); public base preserved", scenario)
		})
	}
}
