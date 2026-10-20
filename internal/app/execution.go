package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/jtoosh/baseball-mcp/internal/data"
)

// Each strategy owns its query attempts and presentation, so a failed strategy
// cannot discard another strategy's answer when comparison is enabled.
func (q *QueryService) executeStrategy(ctx context.Context, req QuestionRequest, input PlanInput, plan QueryPlan) (StrategyOutcome, []int) {
	lookupIDs := slices.Clone(plan.LookupPlayerIDs)
	slices.Sort(lookupIDs)
	for attempt := 0; attempt < 2; attempt++ {
		if plan.Status == "clarification" && input.SkipClarification {
			return StrategyOutcome{Strategy: input.Strategy, Kind: "error", Message: localClarificationError}, nil
		}
		if plan.Status != "query" {
			return q.strategyFailure(input.Strategy, "validate query plan", errors.New("expected query plan")), nil
		}
		if err := q.validateSeasons(plan.Seasons); err != nil {
			return q.strategyFailure(input.Strategy, "validate season", err), nil
		}
		if input.ResolvedIntent != nil && !sameSeasons(input.ResolvedIntent.Seasons, plan.Seasons) {
			return q.strategyFailure(input.Strategy, "validate corrected season", errors.New("strategy changed the resolved seasons")), nil
		}
		correctedIDs := slices.Clone(plan.LookupPlayerIDs)
		slices.Sort(correctedIDs)
		// Corrections may add an omitted requested identity, but cannot replace
		// one already selected. Catalog validation below rejects unrelated IDs.
		for _, id := range lookupIDs {
			if !slices.Contains(correctedIDs, id) {
				return q.strategyFailure(input.Strategy, "validate corrected players", errors.New("strategy changed the resolved player identities")), nil
			}
		}
		lookupIDs = correctedIDs
		executionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		var result QueryResult
		var err error
		if input.RequireComparisonCoverage && len(lookupIDs) == 0 {
			err = errors.New("player-season coverage: direct comparison must identify every requested catalog player in lookup_player_ids")
		} else if len(lookupIDs) > 0 {
			result, err = q.DB.QueryPlayerLookup(executionCtx, plan.SQL)
		} else {
			result, err = q.DB.Query(executionCtx, plan.SQL)
		}
		cancel()
		stage := "execute query"
		var table *PresentationTable
		var missing []data.MissingPlayerSeason
		if err == nil {
			stage = "validate result measures"
			err = applyComputedMeasures(&result, plan.Columns)
		}
		if err == nil {
			stage = "validate player-season coverage"
			if len(lookupIDs) > 0 && input.ResolvedIntent != nil && input.ResolvedIntent.PlayerID != 0 && !slices.Contains(lookupIDs, input.ResolvedIntent.PlayerID) {
				err = errors.New("player-season coverage: lookup changed the confirmed player")
			} else {
				missing, err = data.VerifyPlayerCoverage(input.PlayerCandidates, lookupIDs, plan.Seasons, result)
			}
		}
		if err == nil {
			stage = "format result"
			table, err = presentTable(result)
		}
		inspectExecution(ctx, input.Strategy, err != nil)
		if err != nil {
			q.logFailure(stage+" ("+input.Strategy+")", err)
			if attempt == 1 {
				return StrategyOutcome{Strategy: input.Strategy, Kind: "error", Message: friendlyFailure(err)}, plan.Seasons
			}
			input.PreviousSQL, input.DatabaseError = plan.SQL, stage+": "+err.Error()
			plan, err = q.Model.Plan(ctx, input)
			if err == nil {
				inspectPlan(ctx, input.Strategy, plan.SQL)
			}
			if err != nil {
				return q.strategyFailure(input.Strategy, "correct query", err), nil
			}
			continue
		}
		answer, err := q.Model.Answer(ctx, AnswerInput{Question: req.Question, Interpretation: req.Interpretation, Strategy: input.Strategy, Seasons: plan.Seasons, Result: result, MissingPlayerSeasons: missing})
		if err != nil {
			return q.strategyFailure(input.Strategy, "summarize result", err), plan.Seasons
		}
		for _, gap := range missing {
			answer.Text += fmt.Sprintf("\n\nThe imported snapshot has no player-season record for %s in %d.", gap.Player, gap.Year)
		}
		if !q.safePublicText(answer.Text) {
			return q.strategyFailure(input.Strategy, "validate answer", errors.New("answer contained private details")), plan.Seasons
		}
		if result.Truncated {
			answer.Text = "This is a partial answer based only on the first 500 matching rows. " + answer.Text + " Please narrow your question for a complete result."
		}
		return StrategyOutcome{Strategy: input.Strategy, Kind: "answer", Message: answer.Text, Table: table, Partial: result.Truncated}, plan.Seasons
	}
	return StrategyOutcome{Strategy: input.Strategy, Kind: "error", Message: questionFailureMessage}, nil
}

// Explicit comparisons cannot opt out of coverage by omitting model metadata.
// Questions asking for a ranking or filtered portion retain their normal flow.
var directComparison = regexp.MustCompile(`(?i)\b(compare|comparison|versus|vs)\b`)
var filteredComparison = regexp.MustCompile(`(?i)\b(rank|ranked|ranking|leaders?)\b|\b(only\s+)?(show|return|list|include|select)\s+(only\s+)?(the\s+)?(players?|hitters?|those)\s+(who\b|with\s+(more|fewer|higher|lower|most|least|highest|lowest|at least|at most|above|below|over|under)\b)|\b(show|return|list|include|select)\s+(only\s+)?(the\s+)?top\s+\d+\b|\b(players?|hitters?)\s+(with|having)\s+(at least|at most|more than|fewer than|greater than|less than|above|below|over|under)\s+\d+\b`)

func (q *QueryService) validateSeasons(seasons []int) error {
	if len(seasons) == 0 {
		return errors.New("model omitted season")
	}
	for _, year := range seasons {
		found := false
		for _, available := range q.DB.Seasons {
			if year == available {
				found = true
			}
		}
		if !found {
			return errors.New("model selected absent season")
		}
	}
	return nil
}

const questionFailureMessage = "I couldn't answer that question. Please try again or ask a more specific baseball question."

func (q *QueryService) logFailure(stage string, err error) {
	if q.Logger != nil {
		if q.Config.Environment == "production" || q.Config.Environment == "preview" {
			classification := "dependency_or_validation_failure"
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				classification = "deadline_or_cancellation"
			}
			var denied *publicFailure
			if errors.As(err, &denied) {
				classification = "admission_denied"
			}
			q.Logger.Printf("stage=%q class=%s", stage, classification)
		} else {
			q.Logger.Printf("%s: %v", stage, err)
		}
	}
}

func (q *QueryService) strategyFailure(strategy, stage string, err error) StrategyOutcome {
	q.logFailure(stage+" ("+strategy+")", err)
	return StrategyOutcome{Strategy: strategy, Kind: "error", Message: friendlyFailure(err)}
}
