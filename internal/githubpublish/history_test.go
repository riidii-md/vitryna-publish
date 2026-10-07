package githubpublish

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

type historyTransport func(*http.Request) (*http.Response, error)

func (f historyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type pagedHistoryAPI struct {
	*fakeAPI
	client *githubapi.Client
}

func (f *pagedHistoryAPI) WalkPullRequests(ctx context.Context, repository, base, head string, visit func(githubapi.PullRequest) error) error {
	return f.client.WalkPullRequests(ctx, repository, base, head, visit)
}

func TestInvalidationScansCompletePagedHistory(t *testing.T) {
	for _, row := range []struct {
		count   int
		refusal string
	}{
		{499, ""}, {500, ""}, {501, ""}, {1501, ""},
		{501, "foreign closed"}, {501, "multiple open"}, {501, "duplicate"}, {501, "cancel"},
	} {
		t.Run(fmt.Sprintf("%d-%s", row.count, row.refusal), func(t *testing.T) {
			p, f := ownedFixture()
			template := f.pulls[0]
			f.pulls = nil
			for i := 0; i < row.count; i++ {
				pull := template
				pull.Number = i + 1
				pull.NodeID = fmt.Sprintf("PR_%d", i+1)
				if i < row.count-1 {
					pull.State = "closed"
					pull.AutoMerge = nil
				}
				f.pulls = append(f.pulls, pull)
			}
			switch row.refusal {
			case "foreign closed":
				f.pulls[row.count-2].User.ID = 8
			case "multiple open":
				f.pulls[row.count-2].State = "open"
			case "duplicate":
				f.pulls[row.count-2] = f.pulls[0]
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pages := 0
			client := githubapi.New("synthetic", &http.Client{Transport: historyTransport(func(r *http.Request) (*http.Response, error) {
				pages++
				if r.Method != "GET" || r.URL.Path != "/repos/example/public/pulls" || r.URL.Query().Get("state") != "all" || r.URL.Query().Get("base") != "main" || r.URL.Query().Get("head") != "example:vitryna/public" {
					t.Fatal("history query changed ownership scope")
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if row.refusal == "cancel" && page == 6 {
					cancel()
					return nil, ctx.Err()
				}
				start := min((page-1)*100, len(f.pulls))
				end := min(start+100, len(f.pulls))
				data, err := json.Marshal(f.pulls[start:end])
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
			})})
			p.API = &pagedHistoryAPI{fakeAPI: f, client: client}
			if row.refusal == "" {
				state, err := p.Inspect(ctx)
				if err != nil || state.Previous == nil || state.Previous.Number != row.count-1 || state.Open == nil || state.Open.Number != row.count {
					t.Fatalf("complete latest/open selection: %#v %v", state, err)
				}
			}
			result, err := p.Invalidate(ctx)
			if row.refusal != "" {
				if err == nil || len(mutations(f.calls)) != 0 || f.pulls[row.count-1].AutoMerge == nil {
					t.Fatalf("late refusal mutated candidate: %s %#v %v %v", row.refusal, result, err, f.calls)
				}
				return
			}
			if err != nil || result.Status != "closed" || result.PullNumber != row.count || !reflect.DeepEqual(mutations(f.calls), []string{"disable", "close"}) || f.pulls[row.count-1].AutoMerge != nil || f.pulls[row.count-1].State != "closed" {
				t.Fatalf("owned history blocked invalidation: %#v %v %v", result, err, f.calls)
			}
			if pages < 2*(row.count/100+1) {
				t.Fatal("history not read completely")
			}
		})
	}
}
