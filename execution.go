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
		if plan.Status != "query" {
			return q.strategyFailure(input.Strategy, "validate query plan", errors.New("expected query plan")), nil
		}
		if err := q.validateSeasons(plan.Seasons); err != nil {
			return q.strategyFailure(input.Strategy, "validate season", err), nil
		}
		executionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		result, err := q.DB.Query(executionCtx, plan.SQL)
		cancel()
		if err != nil {
			q.logFailure("execute query ("+input.Strategy+")", err)
			if attempt == 1 {
				return StrategyOutcome{Strategy: input.Strategy, Kind: "error", Message: questionFailureMessage}, plan.Seasons
			}
			input.PreviousSQL, input.DatabaseError = plan.SQL, err.Error()
			plan, err = q.Model.Plan(ctx, input)
			if err != nil {
				return q.strategyFailure(input.Strategy, "correct query", err), nil
			}
			continue
		}
		if result.Truncated {
			return StrategyOutcome{Strategy: input.Strategy, Kind: "error", Message: "This question returned more than 500 rows. Please narrow your question."}, plan.Seasons
		}
		if err := applyComputedMeasures(&result, plan.Columns); err != nil {
			return q.strategyFailure(input.Strategy, "validate result measures", err), plan.Seasons
		}
		table, err := presentTable(result)
		if err != nil {
			return q.strategyFailure(input.Strategy, "format result", err), plan.Seasons
		}
		answer, err := q.Model.Answer(ctx, AnswerInput{Question: req.Question, Interpretation: req.Interpretation, Strategy: input.Strategy, Seasons: plan.Seasons, Result: result})
		if err != nil {
			return q.strategyFailure(input.Strategy, "summarize result", err), plan.Seasons
		}
		if !q.safePublicText(answer.Text) {
			return q.strategyFailure(input.Strategy, "validate answer", errors.New("answer contained private details")), plan.Seasons
		}
		return StrategyOutcome{Strategy: input.Strategy, Kind: "answer", Message: answer.Text, Table: table}, plan.Seasons
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
		q.Logger.Printf("%s: %v", stage, err)
	}
}

func (q *QueryService) strategyFailure(strategy, stage string, err error) StrategyOutcome {
	q.logFailure(stage+" ("+strategy+")", err)
	return StrategyOutcome{Strategy: strategy, Kind: "error", Message: questionFailureMessage}
}
