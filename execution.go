package main

import (
	"context"
	"errors"
	"time"
)

// Each strategy owns its query attempts and presentation, so a failed strategy
// cannot discard another strategy's answer when comparison is enabled.
func (q *QueryService) executeStrategy(ctx context.Context, req QuestionRequest, input PlanInput, plan QueryPlan) (StrategyOutcome, []int) {
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
		executionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		result, err := q.DB.Query(executionCtx, plan.SQL)
		cancel()
		stage := "execute query"
		var table *PresentationTable
		if err == nil {
			stage = "validate result measures"
			err = applyComputedMeasures(&result, plan.Columns)
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
		answer, err := q.Model.Answer(ctx, AnswerInput{Question: req.Question, Interpretation: req.Interpretation, Strategy: input.Strategy, Seasons: plan.Seasons, Result: result})
		if err != nil {
			return q.strategyFailure(input.Strategy, "summarize result", err), plan.Seasons
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
