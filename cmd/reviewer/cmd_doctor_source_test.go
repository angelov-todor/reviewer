package main

import (
	"context"
	"strings"
	"testing"

	"github.com/angelov-todor/reviewer/internal/config"
	"github.com/angelov-todor/reviewer/internal/ghpr"
	"github.com/angelov-todor/reviewer/internal/runner"
)

// Two pull requests, one of them a bot's, in one repository.
const doctorSearchJSON = `{"total_count":2,"items":[
{"number":1,"repository_url":"https://api.github.com/repos/Example-Org/aex-a",
 "updated_at":"2026-09-07T08:30:00Z","user":{"login":"colleague","type":"User"},"pull_request":{}},
{"number":2,"repository_url":"https://api.github.com/repos/Example-Org/aex-a",
 "updated_at":"2026-09-07T08:31:00Z","user":{"login":"dependabot[bot]","type":"Bot"},"pull_request":{}}
]}`

func doctorSource() config.Source {
	return config.Source{Type: "github", Owner: "Example-Org", ExcludeBots: true}
}

// The counts are the whole point: a source returning nothing looks exactly
// like a quiet week, so doctor has to say what came back rather than only that
// the call succeeded.
func TestCheckSourceReportsWhatCameBack(t *testing.T) {
	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "search/issues", Result: runner.Result{Stdout: []byte(doctorSearchJSON)}},
	}}
	detail, err := checkSource(context.Background(), ghpr.New(f, "gh"), doctorSource(), "angelov-todor")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 scanned", "2 matched", "1 after bots"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the detail must contain %q: %s", want, detail)
		}
	}
}

// A full page is a failure and not a note. It is the one outcome the operator
// has to act on: pull requests exist that the service will never see until the
// noisiest authors are excluded, and reporting it as a pass would leave that
// invisible.
func TestCheckSourceFailsOnAFullPage(t *testing.T) {
	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "search/issues", Result: runner.Result{Stdout: []byte(doctorSearchJSON)}},
	}}
	src := doctorSource()
	src.Limit = 2 // two items against a limit of two is a full page

	detail, err := checkSource(context.Background(), ghpr.New(f, "gh"), src, "angelov-todor")
	if err == nil {
		t.Fatal("a full page must be reported as a failure")
	}
	if !strings.Contains(err.Error(), "exclude_authors") {
		t.Errorf("the error must say what to do about it: %v", err)
	}
	// The counts still come back, because they are what the operator needs in
	// order to act on the failure.
	if !strings.Contains(detail, "2 scanned") {
		t.Errorf("the detail must survive the failure: %q", detail)
	}
}

func TestCheckSourceReportsAFailedQuery(t *testing.T) {
	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "search/issues", Result: runner.Result{
			ExitCode: 1, Stderr: []byte("HTTP 422: Validation Failed"),
		}},
	}}
	if _, err := checkSource(context.Background(), ghpr.New(f, "gh"),
		doctorSource(), "angelov-todor"); err == nil {
		t.Fatal("a failed query must be a failed check")
	} else if !strings.Contains(err.Error(), "422") {
		t.Errorf("the error must carry what GitHub said: %v", err)
	}
}

// TestGhAuthNamesTheAccount is the detail an operator is really asking for:
// reviews are posted under that identity, so "authenticated" without a name
// answers half the question.
func TestGhAuthNamesTheAccount(t *testing.T) {
	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "api user", Result: runner.Result{Stdout: []byte("angelov-todor\n")}},
	}}
	login, err := ghAuth(context.Background(), f, "gh")
	if err != nil {
		t.Fatal(err)
	}
	if login != "angelov-todor" {
		t.Errorf("login = %q", login)
	}
	if d := loginDetail(login, "gh"); !strings.Contains(d, "angelov-todor") {
		t.Errorf("the detail must name the account: %q", d)
	}
}

// TestGhAuthIgnoresAnUnusedStaleAccount is the bug.
//
// `gh auth status` reports on every account gh has stored and exits non-zero
// if ANY of them fails to log in, including a stale credential nothing uses.
// On a real machine that produced a red "gh authenticated" beside a green "gh
// can submit reviews" in the same run, which tells an operator nothing except
// that the checks disagree. The question worth asking is whether a call made
// the way the service makes them comes back.
func TestGhAuthIgnoresAnUnusedStaleAccount(t *testing.T) {
	f := &runner.Fake{Replies: []runner.Reply{
		// What the machine that prompted this actually had: `gh auth status`
		// exits 1 over a dead second account, while the API call the service
		// depends on succeeds.
		{Match: "auth status", Result: runner.Result{
			ExitCode: 1, Stderr: []byte("X Failed to log in to github.com account angelov-todor (default)"),
		}},
		{Match: "api user", Result: runner.Result{Stdout: []byte("angelov-todor\n")}},
	}}
	if _, err := ghAuth(context.Background(), f, "gh"); err != nil {
		t.Errorf("a working token must pass, whatever gh says about its other accounts: %v", err)
	}
}

// A token that genuinely cannot call GitHub must still fail, and say what
// GitHub said rather than only that something went wrong.
func TestGhAuthFailsWhenTheCallFails(t *testing.T) {
	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "api user", Result: runner.Result{
			ExitCode: 1, Stderr: []byte("HTTP 401: Bad credentials"),
		}},
	}}
	if _, err := ghAuth(context.Background(), f, "gh"); err == nil {
		t.Fatal("a failed call must fail the check")
	} else if !strings.Contains(err.Error(), "401") {
		t.Errorf("the error must carry what GitHub said: %v", err)
	}
}

// An empty answer is not a pass. gh exiting zero while naming nobody means
// something unexpected, and reporting "authenticated as nobody" would be
// worse than reporting a problem.
func TestGhAuthRefusesAnEmptyAnswer(t *testing.T) {
	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "api user", Result: runner.Result{Stdout: []byte("  \n")}},
	}}
	if _, err := ghAuth(context.Background(), f, "gh"); err == nil {
		t.Fatal("gh naming no account must not read as authenticated")
	}
}

// TestClaudeAnswersCatchesAnUnauthenticatedCLI is the false pass this check
// was rewritten to stop, in the words the real box produced.
//
// A freshly provisioned machine printed "Not logged in · Please run /login"
// and exited ZERO. The old check ran `claude --version`, which passed, so
// doctor reported "claude works" on a box that could not review a single pull
// request -- and the operator had run the login step, it simply had not taken.
func TestClaudeAnswersCatchesAnUnauthenticatedCLI(t *testing.T) {
	f := &runner.Fake{Replies: []runner.Reply{
		// Exit code zero, exactly as observed.
		{Match: "-p", Result: runner.Result{Stdout: []byte("Not logged in · Please run /login\n")}},
	}}
	if _, err := claudeAnswers(context.Background(), f, "claude"); err == nil {
		t.Fatal("an unauthenticated claude must fail the check despite exiting zero")
	} else if !strings.Contains(err.Error(), "setup-token") {
		t.Errorf("the error must say how to fix it: %v", err)
	}
}

// The other credential failures are worth the same treatment: each is a box
// that cannot review anything while reporting a version perfectly well.
func TestClaudeAnswersCatchesTheOtherCredentialFailures(t *testing.T) {
	for _, out := range []string{
		"Invalid API key · Please run /login",
		"API Error: authentication_error",
		"Your credit balance is too low to access the Anthropic API",
	} {
		f := &runner.Fake{Replies: []runner.Reply{
			{Match: "-p", Result: runner.Result{Stdout: []byte(out)}},
		}}
		if _, err := claudeAnswers(context.Background(), f, "claude"); err == nil {
			t.Errorf("must be caught: %q", out)
		}
	}
}

func TestClaudeAnswersPassesWhenItAnswers(t *testing.T) {
	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "-p", Result: runner.Result{Stdout: []byte("ok\n")}},
	}}
	detail, err := claudeAnswers(context.Background(), f, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if detail == "" {
		t.Error("a pass must say what it established, not just pass")
	}
}

// A model that editorialises over a one-word instruction is not a reason to
// refuse to start: the credentials plainly work, which is the only thing this
// check is asking about.
func TestClaudeAnswersToleratesAChattyReply(t *testing.T) {
	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "-p", Result: runner.Result{Stdout: []byte("Sure! Here you go.\n")}},
	}}
	detail, err := claudeAnswers(context.Background(), f, "claude")
	if err != nil {
		t.Errorf("a chatty but authenticated claude must pass: %v", err)
	}
	if !strings.Contains(detail, "not as asked") {
		t.Errorf("the detail should say it answered oddly: %q", detail)
	}
}

// TestCheckSourceProbesThePathTheServiceUses is the third instance of one
// mistake in this file: a check that answers a different question from the one
// that matters.
//
// `gh auth status` reported on accounts the service does not use. `claude
// --version` reported that a binary exists rather than that it can review.
// And this one probed the search API while the service walked the
// repositories: on the box it printed "12 scanned, 7 matched" beside a service
// that was seeing 286 and 15.
func TestCheckSourceProbesThePathTheServiceUses(t *testing.T) {
	const allOpen = `{"data":{"organization":{"repositories":{
	 "pageInfo":{"hasNextPage":false,"endCursor":""},
	 "nodes":[{"name":"aex-a","isArchived":false,"pullRequests":{"nodes":[
	   {"number":1,"isDraft":false,"updatedAt":"2026-10-12T09:00:00Z",
	    "author":{"__typename":"User","login":"colleague"}}]}}]}}}}`

	f := &runner.Fake{Replies: []runner.Reply{
		{Match: "graphql", Result: runner.Result{Stdout: []byte(allOpen)}},
		// Configured so a wrong route is visible as a wrong answer rather
		// than as an error: if the check still reaches for search, it gets a
		// usable reply and reports the wrong numbers.
		{Match: "search/issues", Result: runner.Result{
			Stdout: []byte(`{"total_count":0,"items":[]}`),
		}},
	}}
	src := config.Source{
		Type: "github", Owner: "Example-Org",
		RepoPrefixes: []string{"aex-"}, AllOpen: true,
	}
	detail, err := checkSource(context.Background(), ghpr.New(f, "gh"), src, "angelov-todor")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.Calls[0].String(), "search/issues") {
		t.Errorf("an all_open source must be probed the way the service reads it, not via "+
			"search: %q", f.Calls[0].String())
	}
	if !strings.Contains(detail, "1 scanned") {
		t.Errorf("the detail must come from the all-open walk: %q", detail)
	}
}
