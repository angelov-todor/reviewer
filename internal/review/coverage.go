package review

// coverageClause requires every review to say what it examined, and makes an
// approval a claim the review has actually earned.
//
// Both halves answer the same criticism, from a colleague reading the pull
// requests rather than the code: "the reviewer never really reviewed the PR.
// Its tool picked a cheap job and then approved, by design." The reviews were
// better than that -- on a 38-file change it found five defects with file and
// line, and on the next pass verified each claimed fix in the code -- but the
// criticism was right about the process in two specific ways, and both were
// visible in what the team sees.
//
// The first is an overclaim. That same review said "I did not read the
// PDF/sample layout code or the test files in depth" and then submitted
// APPROVED. An approval is a claim about the whole pull request, and this
// service may submit it as an approving review that lets somebody merge. A
// review cannot both admit it skipped part of the change and approve it.
//
// The second is silence. The approving review body is one lean sentence -- "No
// findings needing a change." -- and the findings, when there are any, live in
// a separate comment. On a two-file documentation change the reviewer posted no
// comment at all, so the whole visible artefact was that sentence. From the
// Reviews tab, "I looked and it is fine" and "I did not look" are the same
// thing, and a reader is entitled to the worse reading. So a comment is now
// required even when nothing was found, and it has to open by naming what was
// examined.
//
// The coverage demand is scoped to what the pass was asked to cover, and that
// wording is load-bearing rather than fussy. An earlier version of this file
// said "read everything in this change", which contradicted the increment
// instruction added hours before it: a later pass is told to review the
// increment and not to restate the earlier pass's findings, so a demand to
// read the whole change again would have undone that and re-read every file on
// every push. The approving review body already states what a later pass
// covered, so the two now agree.
//
// Both demands live in the -p value with the other instructions, for the
// reason this package has already paid to learn: an instruction in an appended
// system prompt is not reliably followed over a long agentic run.
const coverageClause = "\n\nBefore the verdict line, post ONE comment on the pull request even " +
	"if you found nothing at all, and open it with a sentence naming what you examined and " +
	"anything you deliberately did not. An approval with no comment is indistinguishable from " +
	"not having looked, and a reader is entitled to the worse reading." +
	"\n\nAnd read all of what you are reviewing before you print `approve` -- the whole change " +
	"on a first pass, the whole increment on a later one. If you deliberately skipped part of " +
	"it, anything beyond generated files, lock files and fixtures, print `findings` instead and " +
	"name what you skipped. The service may " +
	"turn `approve` into an approving GitHub review that satisfies somebody's merge " +
	"requirement, so it has to be a claim about everything this pass was asked to cover. Saying " +
	"you did not read part of it and approving it anyway are not both available."

// dryRunCoverageClause is the same requirement for a run that posts nothing.
//
// The comment half cannot apply -- a dry run is forbidden to post -- but the
// coverage statement and the honesty of the verdict still do, and they have to
// be worded without asking for a comment. Otherwise the dry run contradicts
// the posting prohibition it is given two paragraphs earlier, and a
// contradiction is how a dry run ends up posting.
const dryRunCoverageClause = "\n\nOpen your output with a sentence naming what you examined and " +
	"anything you deliberately did not examine." +
	"\n\nAnd read all of what you are reviewing before you print `approve` -- the whole change " +
	"on a first pass, the whole increment on a later one. If you deliberately skipped part of " +
	"it, anything beyond generated files, lock files and fixtures, print `findings` instead and " +
	"name what you skipped. In a live run the " +
	"service may turn `approve` into an approving GitHub review that satisfies somebody's merge " +
	"requirement, and this run is asked the same question so that its answer means the same " +
	"thing."
