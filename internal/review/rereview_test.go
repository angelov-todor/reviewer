package review

import (
	"strings"
	"testing"
)

// TestTheIncrementDemandNamesTheCommand is the team's first recommendation.
//
// The reviewer cannot get the increment from its own checkout: the service
// force-updates the mirror ref, so the previously reviewed commit is often
// unreachable. The old system-prompt note said so and then asked the reviewer
// to "work out what has changed from the pull request itself", which is vague
// enough that a later pass re-read the whole change.
//
// GitHub still has the commit. Verified against a real repository: a compare
// between a head reviewed a month earlier and the current one, on a branch
// force-pushed since, answered with three commits and six files.
func TestTheIncrementDemandNamesTheCommand(t *testing.T) {
	got := reReviewClause(ref, PreviousPass{HeadSHA: prevSHA, Posted: true})

	// The exact call, with the repository and the old SHA in it. A reviewer
	// that has to compose this itself is a reviewer that might not.
	want := "gh api repos/" + ref.Owner + "/" + ref.Repo + "/compare/" + prevSHA +
		"...refs/pull/12/head"
	if !strings.Contains(got, want) {
		t.Errorf("the clause must name the command:\nwant %q\nin   %q", want, got)
	}
	for _, frag := range []string{"INCREMENT", "not the whole change again", "now fixed"} {
		if !strings.Contains(got, frag) {
			t.Errorf("missing %q from:\n%s", frag, got)
		}
	}
}

// A rebase or an amend makes the comparison report `diverged`, and then the
// author's own work reappears in it even where nothing changed. A reviewer
// that reads that as new work re-reviews the whole change and says the rebase
// introduced it.
func TestTheIncrementDemandHandlesARewrittenHistory(t *testing.T) {
	got := reReviewClause(ref, PreviousPass{HeadSHA: prevSHA, Posted: true})
	for _, frag := range []string{"diverged", "rebase", "do not read them as new work",
		"only the base moved"} {
		if !strings.Contains(got, frag) {
			t.Errorf("missing %q from:\n%s", frag, got)
		}
	}
}

// TestTheVerificationRulesAreInTheTaskText covers the rest of the team's
// recommendation, and where it has to live.
//
// This package measured an instruction in an appended system prompt being
// ignored over fourteen consecutive production reviews while the same words
// in the task text were followed. These are instructions, so they go in the
// -p value with the other demands.
func TestTheVerificationRulesAreInTheTaskText(t *testing.T) {
	clause := priorClause(&PriorFeedback{Items: []PriorItem{{Surface: "comment", Author: "a"}}})

	cases := map[string]string{
		"a fix is read in the code, not taken on trust": "never by the author saying it is fixed",
		"the author's push-back is an answer":           "reasoned push-back is an ANSWER",
		"it does not hold back the verdict":             "do not let it hold back your verdict",
		"another reviewer's claim is verified first":    "not evidence",
	}
	for why, frag := range cases {
		if !strings.Contains(clause, frag) {
			t.Errorf("%s: missing %q", why, frag)
		}
	}

	// The one thing a push-back does not override, because the service
	// enforces it independently of anything the reviewer decides.
	if !strings.Contains(clause, "changes requested") {
		t.Error("the clause must say that an outstanding `changes requested` review still stands")
	}
}

// With no prior feedback there is nothing to verify and nothing to push back
// against, so none of it is said. A prompt that carries rules about material
// that does not exist spends the reviewer's attention on nothing.
func TestTheVerificationRulesAreSilentWithoutFeedback(t *testing.T) {
	if clause := priorClause(nil); clause != "" {
		t.Errorf("no feedback must mean no clause, got %q", clause)
	}
	if clause := priorClause(&PriorFeedback{}); clause != "" {
		t.Errorf("an empty list must mean no clause, got %q", clause)
	}
}

// A first pass is asked none of it: there is no increment and nothing earlier
// to check. The demand appears only once a pass has actually posted, which is
// the same gate the system-prompt note is behind -- an earlier pass whose
// findings went to a dry-run report left nothing on the pull request to
// compare against.
func TestAFirstPassIsNotAskedForAnIncrement(t *testing.T) {
	rr := New(nil, "claude", nil, false, t.TempDir())

	if p := rr.Prompt(ref, nil); strings.Contains(p, "INCREMENT") {
		t.Error("a first pass has no increment to review")
	}
	notPosted := &PreviousPass{HeadSHA: prevSHA, Posted: false}
	if p := rr.Prompt(ref, notPosted); strings.Contains(p, "INCREMENT") {
		t.Error("a pass that posted nothing left nothing to compare against")
	}
	unchanged := &PreviousPass{HeadSHA: prevSHA, Posted: true, HeadUnchanged: true}
	if p := rr.Prompt(ref, unchanged); strings.Contains(p, "INCREMENT") {
		t.Error("a replay at the same commit has no increment: there is nothing between " +
			"the reviewed commit and the current one")
	}
	posted := &PreviousPass{HeadSHA: prevSHA, Posted: true}
	if p := rr.Prompt(ref, posted); !strings.Contains(p, "INCREMENT") {
		t.Error("a later pass over new commits must be asked for the increment")
	}
}

// TestTheCompareNamesThePullRequestNotTheDefaultBranch is the bug this clause
// shipped with, caught in self-review against a real repository.
//
// The first version ended the compare URL with `...HEAD`. In a compare URL
// HEAD is the REPOSITORY's default branch, not the pull request's head: on a
// real pull request that turned `ahead=3, files=6` into `diverged, ahead=103,
// behind=2, files=300` -- the whole of master. A reviewer following it would
// have reviewed every unrelated change on the default branch, which is worse
// than re-reading the pull request it was meant to avoid.
func TestTheCompareNamesThePullRequestNotTheDefaultBranch(t *testing.T) {
	got := reReviewClause(ref, PreviousPass{HeadSHA: prevSHA, Posted: true})

	// refs/pull/N/head returned the identical answer to the explicit head SHA
	// when measured, needs no second call to resolve, and works when the
	// branch lives on a fork.
	want := "/compare/" + prevSHA + "...refs/pull/12/head"
	if !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}
	if strings.Contains(got, "...HEAD") {
		t.Error("`...HEAD` compares against the default branch, not the pull request")
	}
}

// TestAnApprovalCannotOutrunWhatWasRead is the colleague's criticism, made
// into a requirement.
//
// A real review on a 38-file change said "I did not read the PDF/sample
// layout code or the test files in depth" and then submitted APPROVED. The
// service may turn that into an approving GitHub review that satisfies
// somebody's merge requirement, so it is a claim about the whole change --
// and a review cannot both admit it skipped part of the change and approve
// it.
func TestAnApprovalCannotOutrunWhatWasRead(t *testing.T) {
	live := New(nil, "claude", nil, false, t.TempDir()).Prompt(ref, nil)
	dry := New(nil, "claude", nil, true, t.TempDir()).Prompt(ref, nil)

	for mode, p := range map[string]string{"live": live, "dry run": dry} {
		for _, frag := range []string{
			"could alter behaviour",    // what must be read
			"print `findings` instead", // what to do when it was not
			"name what you skipped",    // and say so
			"merge requirement",        // why it matters
		} {
			if !strings.Contains(p, frag) {
				t.Errorf("%s prompt must carry %q", mode, frag)
			}
		}
	}
}

// TestEveryReviewSaysWhatItExamined is the other half: silence read as
// thoroughness.
//
// The approving review body is one lean sentence, and the findings live in a
// separate comment. On a two-file documentation change the reviewer posted no
// comment at all, so the entire visible artefact was "No findings needing a
// change." From the Reviews tab that is indistinguishable from not having
// looked, and a reader is entitled to the worse reading.
func TestEveryReviewSaysWhatItExamined(t *testing.T) {
	live := New(nil, "claude", nil, false, t.TempDir()).Prompt(ref, nil)

	// A comment is required even on a clean review, and it has to open with
	// the coverage statement.
	for _, frag := range []string{"even if you found nothing", "naming what you examined",
		"indistinguishable from"} {
		if !strings.Contains(live, frag) {
			t.Errorf("the live prompt must carry %q", frag)
		}
	}

	// A dry run must NOT be asked to post, or it contradicts the prohibition
	// it is given in the same prompt -- and a contradiction is how a dry run
	// ends up posting on a colleague's pull request.
	dry := New(nil, "claude", nil, true, t.TempDir()).Prompt(ref, nil)
	if strings.Contains(dry, "post ONE comment") {
		t.Error("a dry run must not be told to post a comment")
	}
	if !strings.Contains(dry, "naming what you examined") {
		t.Error("a dry run must still state its coverage, so its verdict means the same thing")
	}
}
