package ghpr

import (
	"context"
	"strings"
	"testing"

	"github.com/angelov-todor/reviewer/internal/runner"
)

// Two repositories in scope, one out of scope, one archived; a draft, a bot
// and a human in the first.
const allOpenJSON = `{"data":{"organization":{"repositories":{
 "pageInfo":{"hasNextPage":false,"endCursor":""},
 "nodes":[
  {"name":"aex-balances","isArchived":false,"pullRequests":{"nodes":[
    {"number":12,"isDraft":false,"updatedAt":"2026-10-12T09:00:00Z","author":{"__typename":"User","login":"colleague"}},
    {"number":13,"isDraft":true,"updatedAt":"2026-10-12T09:10:00Z","author":{"__typename":"User","login":"colleague"}},
    {"number":14,"isDraft":false,"updatedAt":"2026-10-12T09:20:00Z","author":{"__typename":"Bot","login":"dependabot"}}]}},
  {"name":"astraex-docs","isArchived":false,"pullRequests":{"nodes":[
    {"number":495,"isDraft":false,"updatedAt":"2026-10-11T09:00:00Z","author":{"__typename":"User","login":"gmanolov-prime"}}]}},
  {"name":"bot-service","isArchived":false,"pullRequests":{"nodes":[
    {"number":9,"isDraft":false,"updatedAt":"2026-10-12T09:00:00Z","author":{"__typename":"User","login":"someone"}}]}},
  {"name":"aex-retired","isArchived":true,"pullRequests":{"nodes":[
    {"number":1,"isDraft":false,"updatedAt":"2026-10-12T09:00:00Z","author":{"__typename":"User","login":"someone"}}]}}
 ]}}}}`

func allOpenFake(out string) (*Client, *runner.Fake) {
	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "graphql", Result: runner.Result{Stdout: []byte(out)}},
	}}
	return New(f, "gh"), f
}

// TestDiscoverAllOpenIsTheModeTheSearchPathCouldNotServe is the coverage gap
// this exists to close.
//
// Requiring a review request left the service seeing four of twenty-eight open
// pull requests: most here have no reviewer set, and several request a team,
// which GitHub searches under a different qualifier than a user.
func TestDiscoverAllOpenIsTheModeTheSearchPathCouldNotServe(t *testing.T) {
	c, f := allOpenFake(allOpenJSON)
	page, err := c.DiscoverAllOpen(context.Background(), Query{
		Owner: "AstraBit-CPT", RepoPrefixes: []string{"aex-", "astraex-docs"}, AllOpen: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Not the search endpoint: it is incomplete and stale on this
	// organisation, which is the whole reason this method exists.
	line := f.Calls[0].String()
	if strings.Contains(line, "search/issues") {
		t.Error("all-open must not go through the search API")
	}
	if !strings.Contains(line, "graphql") {
		t.Errorf("expected a graphql call, got %q", line)
	}
	// No review-request qualifier anywhere: that requirement is exactly what
	// this mode drops.
	if strings.Contains(line, "review-requested") {
		t.Error("all-open must not filter on a review request")
	}

	var got []string
	for _, p := range page.Found {
		got = append(got, p.Ref.Key())
	}
	want := []string{
		"astrabit-cpt/aex-balances#12",  // human, not draft, in scope
		"astrabit-cpt/aex-balances#14",  // the bot, reported as one for the caller to drop
		"astrabit-cpt/astraex-docs#495", // the second prefix
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("found %v, want %v", got, want)
	}
	for _, p := range page.Found {
		if p.Ref.Number == 14 && !p.IsBot {
			t.Error("a Bot author must be reported as one, or exclude_bots cannot work")
		}
	}
}

// Drafts are not reviewable work, and an archived repository cannot take a
// comment -- a review of one would fail at the last step having spent a clone
// and a claude run.
func TestDiscoverAllOpenSkipsDraftsAndArchivedRepos(t *testing.T) {
	c, _ := allOpenFake(allOpenJSON)
	page, err := c.DiscoverAllOpen(context.Background(), Query{
		Owner: "AstraBit-CPT", RepoPrefixes: []string{"aex-"}, AllOpen: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range page.Found {
		if p.Ref.Repo == "aex-retired" {
			t.Error("an archived repository must be skipped")
		}
		if p.Ref.Number == 13 {
			t.Error("a draft must be skipped")
		}
	}
}

// Without a prefix this walks every repository the organisation has -- 321
// here, 900 open pull requests -- and comments on work from teams that never
// asked. It is refused rather than defaulted.
func TestDiscoverAllOpenRefusesToSweepTheWholeOrganisation(t *testing.T) {
	c, f := allOpenFake(allOpenJSON)
	if _, err := c.DiscoverAllOpen(context.Background(), Query{
		Owner: "AstraBit-CPT", AllOpen: true,
	}); err == nil {
		t.Fatal("all-open with no repo_prefixes must be refused")
	} else if !strings.Contains(err.Error(), "repo_prefixes") {
		t.Errorf("the error must name what is missing: %v", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("nothing should have been asked of GitHub, got %v", f.Calls)
	}
}

// Discover dispatches on the mode, so the pipeline calls one method and the
// right API is chosen for the question being asked.
func TestDiscoverDispatchesToAllOpen(t *testing.T) {
	c, f := allOpenFake(allOpenJSON)
	if _, err := c.Discover(context.Background(), Query{
		Owner: "AstraBit-CPT", RepoPrefixes: []string{"aex-"}, AllOpen: true,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Calls[0].String(), "graphql") {
		t.Errorf("AllOpen must route to the graphql path, got %q", f.Calls[0].String())
	}
}

// A paginated organisation is followed to the end. Here a missed page is a
// pull request nobody reviews: unlike the search path, this list is the whole
// population rather than a recency-ordered sample that re-runs every sweep.
func TestDiscoverAllOpenFollowsPagination(t *testing.T) {
	first := `{"data":{"organization":{"repositories":{
	 "pageInfo":{"hasNextPage":true,"endCursor":"CURSOR1"},
	 "nodes":[{"name":"aex-a","isArchived":false,"pullRequests":{"nodes":[
	   {"number":1,"isDraft":false,"updatedAt":"2026-10-12T09:00:00Z","author":{"__typename":"User","login":"a"}}]}}]}}}}`
	second := `{"data":{"organization":{"repositories":{
	 "pageInfo":{"hasNextPage":false,"endCursor":""},
	 "nodes":[{"name":"aex-b","isArchived":false,"pullRequests":{"nodes":[
	   {"number":2,"isDraft":false,"updatedAt":"2026-10-12T09:00:00Z","author":{"__typename":"User","login":"b"}}]}}]}}}}`
	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "graphql", Result: runner.Result{Stdout: []byte(first)}, Times: 1},
		{Match: "graphql", Result: runner.Result{Stdout: []byte(second)}},
	}}
	page, err := New(f, "gh").DiscoverAllOpen(context.Background(), Query{
		Owner: "o", RepoPrefixes: []string{"aex-"}, AllOpen: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Found) != 2 {
		t.Fatalf("both pages must be read, got %d", len(page.Found))
	}
	if len(f.Calls) != 2 {
		t.Errorf("expected two requests, got %d", len(f.Calls))
	}
	// The cursor has to travel, or the second request repeats the first page
	// forever.
	if !strings.Contains(f.Calls[1].String(), "CURSOR1") {
		t.Errorf("the second call must carry the cursor: %q", f.Calls[1].String())
	}
}
