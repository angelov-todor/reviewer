package ghpr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/angelov-todor/reviewer/internal/prref"
)

// allOpenQuery walks the organisation's repositories and their open pull
// requests.
//
// GraphQL over repositories rather than the search API, for two measured
// reasons. Search is **incomplete** on this organisation -- three identical
// calls returned 6, 8 and 9 of a reported 9, with incomplete_results set every
// time -- and it is **stale**: it reported three merged pull requests as open.
// Neither is acceptable when the question is "what is open right now", and this
// query answers it exactly: it is the one that produced the counts the
// coverage gap was diagnosed from.
const allOpenQuery = `
query($owner:String!, $endCursor:String) {
  organization(login:$owner) {
    repositories(first:50, after:$endCursor, orderBy:{field:NAME,direction:ASC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        name
        isArchived
        pullRequests(states:OPEN, first:100) {
          nodes {
            number
            isDraft
            updatedAt
            author { __typename login }
          }
        }
      }
    }
  }
}`

type allOpenPage struct {
	Data struct {
		Organization struct {
			Repositories struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []struct {
					Name         string `json:"name"`
					IsArchived   bool   `json:"isArchived"`
					PullRequests struct {
						Nodes []struct {
							Number    int    `json:"number"`
							IsDraft   bool   `json:"isDraft"`
							UpdatedAt string `json:"updatedAt"`
							Author    struct {
								TypeName string `json:"__typename"`
								Login    string `json:"login"`
							} `json:"author"`
						} `json:"nodes"`
					} `json:"pullRequests"`
				} `json:"nodes"`
			} `json:"repositories"`
		} `json:"organization"`
	} `json:"data"`
}

// DiscoverAllOpen lists every open pull request in the owner's repositories
// that match the query's prefixes.
//
// This is the mode the operator's original request asked for -- review the
// team's pull requests -- and it was rejected at first on arithmetic that was
// wrong. The measurement said 350 open pull requests in scope, which sounded
// unaffordable; 312 of them were dependabot, which ExcludeAuthors and
// ExcludeBots already remove. The real number is under thirty, and requiring a
// review request instead left the service seeing four of twenty-eight.
//
// Paginated properly rather than truncated, because here a missed page is a
// pull request nobody reviews: the search path can afford to truncate since it
// re-runs every sweep against a list ordered by recency, and this one is the
// whole population.
func (c *Client) DiscoverAllOpen(ctx context.Context, q Query) (Page, error) {
	if q.Owner == "" {
		return Page{}, fmt.Errorf("an all-open query needs an owner")
	}
	if len(q.RepoPrefixes) == 0 {
		// Refused rather than defaulted. Without a prefix this walks every
		// repository the organisation has -- 321 of them here, 900 open pull
		// requests -- and the reviewer would comment on work from teams that
		// never asked for it. A prefix is how the operator says which part of
		// the organisation is theirs.
		return Page{}, fmt.Errorf("an all-open query needs repo_prefixes: without one it would " +
			"review every repository in the organisation")
	}

	var out []Found
	scanned, cursor := 0, ""
	for page := 0; page < 20; page++ {
		args := []string{"api", "graphql", "-f", "query=" + allOpenQuery, "-F", "owner=" + q.Owner}
		if cursor != "" {
			args = append(args, "-F", "endCursor="+cursor)
		}
		res, err := c.r.Run(ctx, "", c.gh, args...)
		if err != nil {
			return Page{}, fmt.Errorf("gh api graphql (all open): %w", err)
		}
		if res.ExitCode != 0 {
			return Page{}, fmt.Errorf("gh api graphql (all open) exit %d: %s",
				res.ExitCode, strings.TrimSpace(string(res.Stderr)))
		}
		var p allOpenPage
		if err := json.Unmarshal(res.Stdout, &p); err != nil {
			return Page{}, fmt.Errorf("decode all-open results: %w", err)
		}

		repos := p.Data.Organization.Repositories
		for _, repo := range repos.Nodes {
			// An archived repository cannot take a comment, so a review of one
			// would fail at the last step having spent a clone and a claude
			// run.
			if repo.IsArchived || !matchesPrefix(repo.Name, q.RepoPrefixes) {
				continue
			}
			for _, pr := range repo.PullRequests.Nodes {
				scanned++
				if pr.IsDraft {
					continue
				}
				f := Found{
					Ref:    prref.New(q.Owner, repo.Name, pr.Number),
					Author: pr.Author.Login,
					// GitHub names the type, so a bot is identified by what it
					// is rather than by a login this list happens to know.
					IsBot: pr.Author.TypeName == "Bot",
				}
				if t, terr := time.Parse(time.RFC3339, pr.UpdatedAt); terr == nil {
					f.UpdatedAt = t
				}
				out = append(out, f)
			}
		}
		if !repos.PageInfo.HasNextPage {
			return Page{Found: out, Scanned: scanned}, nil
		}
		cursor = repos.PageInfo.EndCursor
	}
	// Twenty pages is a thousand repositories. Reported rather than silently
	// truncated: the caller's whole premise is that this list is complete.
	return Page{Found: out, Scanned: scanned, Truncated: true}, nil
}
