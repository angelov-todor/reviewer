package pipeline

import (
	"context"
	"time"

	"github.com/angelov-todor/reviewer/internal/config"
	"github.com/angelov-todor/reviewer/internal/ghpr"
	"github.com/angelov-todor/reviewer/internal/prref"
	"github.com/angelov-todor/reviewer/internal/store"
)

// sourceFound is one pull request a configured source offered.
type sourceFound struct {
	ref       prref.PRRef
	updatedAt time.Time
}

// discover asks every configured source what it has.
//
// Failures are logged and skipped rather than failing the sweep. A source is
// an addition to the chat space, never a replacement for it: a GitHub outage
// or a rate limit must not stop the service reviewing what the team posted, and
// the pull requests a source would have offered are offered again on the next
// sweep, five minutes later, having lost nothing but the delay.
func (p *Pipeline) discover(ctx context.Context, opts Options) []sourceFound {
	if len(p.Cfg.Sources) == 0 {
		return nil
	}
	var out []sourceFound
	for _, src := range p.Cfg.Sources {
		// The chat source is declared alongside the others so the config file
		// names every source, but it is not searched: Sweep has already
		// fetched its messages, with a watermark, reactions and the sibling
		// grouping that go with them.
		if src.Type != config.SourceGitHub {
			continue
		}
		q := ghpr.Query{
			Owner:              src.Owner,
			ReviewRequestedFor: src.Login(p.Cfg.GithubLogin),
			AllOpen:            src.AllOpen,
			RepoPrefixes:       src.RepoPrefixes,
			ExcludeAuthors:     src.ExcludeAuthors,
			Limit:              src.Limit,
		}
		// The cold start, and the reason it is checked before the search
		// rather than after: a source switched on for the first time has no
		// business reviewing what was already open. Those pull requests are
		// its history -- eight on the organisation this runs against, half of
		// them months old -- and the service would post on all of them within a
		// quarter of an hour of being started.
		//
		// Exactly the rule the chat side has always had for a first run
		// against a populated space, applied to a source. What comes after
		// the moment the service started watching is offered; what was already
		// there is not.
		id := src.ID(p.Cfg.GithubLogin)
		since, watched, serr := p.Store.SourceSince(id)
		if serr != nil {
			p.Log.Error("could not read when this source was first watched; skipping it "+
				"rather than risking its whole backlog", "owner", src.Owner, "err", serr)
			continue
		}
		if !watched && !src.ReviewBacklog {
			// print-only must not consume the cold start. It is documented as
			// writing no state, and this is the most consequential piece of
			// state there is: recording it turns every pull request currently
			// open into history, permanently, for an operator who was only
			// looking. It happened on the first run against a real box, and
			// the only sign was a timestamp in a log line.
			//
			// So it reports what the first real sweep will exclude, and
			// records nothing. That is also the more useful answer: the
			// backlog is exactly what somebody running print-only before
			// starting the service wants to see.
			// The cold start's cutoff. Without updated_within it is this
			// moment, so everything already open is history; with it the
			// window reaches back, and work touched inside it is reviewed on
			// the first sweep.
			cold := p.now()
			if w := src.UpdatedWithin.D(); w > 0 {
				cold = cold.Add(-w)
			}
			if opts.PrintOnly {
				page, perr := p.PRs.Discover(ctx, q)
				if perr != nil {
					p.Log.Warn("a source could not be read", "owner", src.Owner, "err", perr)
					continue
				}
				// print-only must not consume the cold start. It is documented
				// as writing no state, and this is the most consequential
				// piece of state there is: recording it turns every pull
				// request currently open into history, permanently, for an
				// operator who was only looking. It happened on the first run
				// against a real box, and the only sign was a timestamp in a
				// log line.
				all := p.keepable(src, page.Found, time.Time{})
				due := p.keepable(src, page.Found, cold)
				p.Log.Info("new source, nothing recorded: the first real sweep would review "+
					"what has been touched inside the window and treat the rest as history",
					"owner", src.Owner, "would_review", len(due),
					"would_exclude", len(all)-len(due),
					"window", src.UpdatedWithin.D().String())
				dueKey := map[string]bool{}
				for _, f := range due {
					dueKey[f.ref.Key()] = true
					p.Log.Info("  would review", "pr", f.ref.URL(),
						"updated", f.updatedAt.Format(time.RFC3339))
				}
				for _, f := range all {
					if !dueKey[f.ref.Key()] {
						p.Log.Info("  would be excluded as history", "pr", f.ref.URL(),
							"updated", f.updatedAt.Format(time.RFC3339))
					}
				}
				continue
			}
			// Recorded before anything is offered, so a crash between here and
			// the next sweep cannot turn the cold start into a backlog sweep.
			if werr := p.Store.SetSourceSince(id, cold); werr != nil {
				p.Log.Error("could not record when this source was first watched; skipping it "+
					"rather than risking its whole backlog", "owner", src.Owner, "err", werr)
				continue
			}
			p.Log.Info("new source: reviewing what has been touched inside the window and "+
				"treating everything older as history",
				"owner", src.Owner, "since", cold.Format(time.RFC3339),
				"window", src.UpdatedWithin.D().String())
			// Not `continue`: with a window configured there may be pull
			// requests inside it to review on this very sweep, which is the
			// whole point of having one. Without a window `since` is now and
			// the filter below excludes everything anyway.
			since = cold
			watched = true
		}
		if !watched {
			// review_backlog: the operator asked for the history on purpose.
			// Still recorded, so it happens once rather than every sweep.
			if werr := p.Store.SetSourceSince(id, p.now()); werr != nil {
				p.Log.Warn("could not record when this source was first watched",
					"owner", src.Owner, "err", werr)
			}
			p.Log.Warn("new source with review_backlog set: every open pull request it finds "+
				"will be reviewed, however old", "owner", src.Owner)
			since = time.Time{}
		}

		dctx, cancel := context.WithTimeout(ctx, p.Cfg.GHTimeout.D())
		page, err := p.PRs.Discover(dctx, q)
		cancel()
		if err != nil {
			p.Log.Warn("a source could not be read; the sweep continues without it",
				"owner", src.Owner, "err", err)
			continue
		}
		// Reported once per sweep, because the operator's fix -- adding the
		// noisy author to exclude_authors -- is not something they can guess
		// from a page of results they never see. Without the exclusions this
		// operator's own query returns 327 pull requests of which 314 are
		// dependabot, so a full page is the expected symptom.
		// Info, not Warn: nothing the operator can configure fixes a partial
		// search, and the next sweep re-runs the query, so a pull request this
		// one missed is offered five minutes later.
		if page.Partial {
			p.Log.Info("a source's search came back partial; anything it missed will be "+
				"offered again next sweep", "owner", src.Owner, "scanned", page.Scanned)
		}
		if page.Truncated {
			p.Log.Warn("a source returned a full page, so some pull requests were not seen; "+
				"add the noisiest authors to exclude_authors",
				"owner", src.Owner, "scanned", page.Scanned, "kept", len(page.Found))
		}

		kept := 0
		for _, f := range p.keepable(src, page.Found, since) {
			out = append(out, f)
			kept++
		}
		p.Log.Info("source offered pull requests", "owner", src.Owner,
			"scanned", page.Scanned, "matched", len(page.Found), "kept", kept)
	}
	return out
}

// keepable applies the filters that do not depend on the store: the bot
// exclusion, the owner allowlist, and the activity cutoff.
//
// Shared with the print-only report of a new source, which has to apply the
// same rules to say truthfully what the first real sweep would exclude. A
// zero since means no cutoff.
func (p *Pipeline) keepable(src config.Source, found []ghpr.Found, since time.Time) []sourceFound {
	var out []sourceFound
	for _, f := range found {
		{
			if src.ExcludeBots && f.IsBot {
				continue
			}
			// The same allowlist every candidate passes, applied here as well
			// so a misconfigured source is visible as an empty result rather
			// than as a sweep full of refusals.
			if !p.Cfg.OwnerAllowed(f.Ref.Owner) || p.Cfg.RepoDenied(f.Ref.Owner, f.Ref.Repo) {
				continue
			}
			// Untouched since the service started watching this source, so it is
			// part of the history the cold start excluded. A pull request
			// somebody pushes to, comments on, or requests a review on lands
			// after that moment and is offered.
			//
			// A missing timestamp is treated as old rather than new. GitHub
			// always sends one, so an absent one means something unexpected,
			// and the safe reading of "unknown age" is not "review it".
			if !since.IsZero() && !f.UpdatedAt.After(since) {
				continue
			}
			out = append(out, sourceFound{ref: f.Ref, updatedAt: f.UpdatedAt})
		}
	}
	return out
}

// sourcePassDue is the second-pass prompt for a candidate no chat message
// asked for.
//
// It answers the same question secondPassDue answers for a re-post -- is this
// worth spending an Inspect on -- and it is not the rule. The rule is one
// review per commit, and the head SHA gate below Inspect enforces it for every
// candidate whatever found it. This only decides whether the service pays a `gh
// pr view` to ask.
//
// GitHub's updated_at moves for a comment as readily as for a push, so this
// is deliberately generous: a pull request touched since the last decision is
// worth asking about, and the SHA gate turns away the ones that were only
// talked about. The alternative -- inspecting every discovered pull request
// every sweep -- costs one GitHub call per pull request per five minutes for
// no new information, and the service shares its rate limit with the reviews.
func sourcePassDue(prev store.Review, activityAt time.Time) bool {
	// Same first condition as a re-post, and for the same reasons: only a
	// completed review can have a second pass, and a record that does not say
	// which commit it reviewed cannot support "one review per commit".
	if prev.Outcome != store.OutcomeReviewed || prev.HeadSHA == "" {
		return false
	}
	if prev.DecidedAt.IsZero() || activityAt.IsZero() {
		return false
	}
	return activityAt.After(prev.DecidedAt)
}

// secondPassPrompted reports whether this candidate is worth an Inspect even
// though its pull request already has a completed review, by whichever
// condition suits the source that offered it.
//
// The two conditions are prompts and not permissions; see sourcePassDue.
func (p *Pipeline) secondPassPrompted(prev store.Review, c candidate) bool {
	if c.fromChat {
		return secondPassDue(prev, c.trigger, c.triggerAt)
	}
	// A candidate with neither chat provenance nor an activity time is the
	// backlog's anonymous re-offer, and it must never be a second pass: it
	// carries no evidence that anybody asked for one, and pending is keyed by
	// pull request rather than by post, so it comes back every sweep. That
	// falls out of sourcePassDue refusing a zero time -- see
	// TestARefFromPendingIsNeverASecondPass and
	// TestAPendingRowWithNoRecordedProvenanceIsStillNeverASecondPass, both of
	// which fail if this stops being true.
	return sourcePassDue(prev, c.activityAt)
}
