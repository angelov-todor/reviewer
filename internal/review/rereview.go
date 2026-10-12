package review

import (
	"strconv"

	"github.com/angelov-todor/reviewer/internal/prref"
)

// reReviewClause tells a later pass to review the increment rather than the
// whole change again, and how to get it.
//
// It travels in the -p value, not the system prompt. That is not a style
// choice: this project measured an instruction in an appended system prompt
// being ignored over fourteen consecutive production reviews, while the same
// instruction in the task text was followed. Data belongs in the system
// prompt; demands belong here.
//
// The command is spelled out because the obvious way to get the increment does
// not work. The service force-updates the mirror ref it checks out, so the
// previously reviewed commit is often unreachable from the worktree -- which
// is why the system-prompt note used to say "work out what has changed from
// the pull request itself", and that vagueness is what made a later pass
// re-read the whole change. GitHub still has the commit: a compare against a
// reviewed head from a month earlier, on a branch that had been force-pushed
// since, answered with three commits and six files.
//
// The head is named as refs/pull/N/head, not as HEAD, which was this clause's
// first mistake. In a compare URL, HEAD is the repository's default branch: on
// a real pull request that turned `ahead=3, files=6` into `diverged, ahead=103,
// behind=2, files=300` -- the whole of master, which is worse than re-reading
// the change. The pull ref gives the identical answer to the explicit head
// SHA, needs no second call to resolve it, and works when the branch lives on
// a fork.
func reReviewClause(ref prref.PRRef, prev PreviousPass) string {
	short := ShortSHA(prev.HeadSHA)
	repo := ref.Owner + "/" + ref.Repo
	return "\n\nThis pull request was reviewed before, at commit " + short +
		". Review the INCREMENT since then, not the whole change again. Get it from GitHub " +
		"rather than from this checkout, which may no longer contain " + short + ":" +
		"\n\n    gh api repos/" + repo + "/compare/" + prev.HeadSHA + "...refs/pull/" +
		strconv.Itoa(ref.Number) + "/head" +
		"\n\nComment on what that shows, plus whether each point raised earlier is now fixed." +
		"\n\nIf that comparison reports `diverged`, the history was rewritten -- a rebase, an " +
		"amend or a force-push. The author's own changes then reappear in the comparison even " +
		"where they have not changed, so do not read them as new work: establish what actually " +
		"moved before you comment. If the change itself is unchanged and only the base moved, " +
		"say so plainly and review what moved underneath it."
}

// verificationClause is the discipline a later pass applies to everything
// already said on the pull request.
//
// Each rule here answers a way a re-review goes wrong in practice:
//
// A fix taken on trust is the commonest. "Fixed in abc123" is a claim about
// code, and the only thing that settles it is the code.
//
// The author's own reply is the one the team asked for. An author who explains
// why they are not making a change has answered the point; a reviewer that
// cannot tell an argument from silence raises it again on the next push, and
// the author has to make the same case twice. That is also why it affects the
// verdict: an answered point is not an outstanding change.
//
// Another reviewer's claim is evidence of a concern, not of a defect. Repeating
// it unverified lends it a second voice it has not earned, and acting on it can
// mean asking for a change the code does not need.
const verificationClause = "\n\nCheck every point already raised on this pull request -- by your " +
	"earlier passes and by people -- against the code as it now stands:" +
	"\n\n  - A fix is confirmed by reading the code, never by the author saying it is fixed. " +
	"Treat `fixed in <sha>` as a claim to check." +
	"\n  - Read the author's own replies. A reasoned push-back is an ANSWER, not an outstanding " +
	"change: do not raise that point again and do not let it hold back your verdict. If you " +
	"still think it matters after reading their reasoning, say so once, briefly, and move on." +
	"\n  - Another reviewer's claim is not evidence. Verify it in the code before you repeat it " +
	"or act on it." +
	"\n\nA formal `changes requested` review that is still outstanding is the exception: the " +
	"service withholds the approval on its own account while one stands, whatever the author has " +
	"said about it."
