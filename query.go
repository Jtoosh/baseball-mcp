package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jtoosh/baseball-mcp/internal/data"
)

const StrategySchema = "schema"
const StrategyExamples = "examples"

type QuestionRequest struct {
	Question       string `json:"question"`
	Confirmation   string `json:"confirmation,omitempty"`
	Interpretation string `json:"interpretation,omitempty"`
	Revision       string `json:"revision,omitempty"`
	Choice         string `json:"choice,omitempty"`
}
type Outcome struct {
	Kind         string            `json:"kind"`
	Message      string            `json:"message,omitempty"`
	Seasons      []int             `json:"seasons,omitempty"`
	Strategies   []StrategyOutcome `json:"strategies,omitempty"`
	Confirmation string            `json:"confirmation,omitempty"`
	Choices      []string          `json:"choices,omitempty"`
}
type StrategyOutcome struct {
	Strategy string             `json:"strategy"`
	Kind     string             `json:"kind"`
	Message  string             `json:"message"`
	Table    *PresentationTable `json:"table,omitempty"`
	Partial  bool               `json:"partial,omitempty"`
}
type PresentationTable struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}
type QueryPlan struct {
	Interpretation string   `json:"interpretation"`
	Status         string   `json:"status"`
	SQL            string   `json:"sql"`
	Message        string   `json:"message"`
	Seasons        []int    `json:"seasons"`
	Columns        []string `json:"columns"`
}
type PlanInput struct {
	Question, Interpretation, Strategy, Schema string
	Seasons                                    []int
	PreviousSQL, DatabaseError                 string
	ResolvedIntent                             *ResolvedIntent
	PlayerCandidates                           []data.PlayerCandidate
}
type AnswerInput struct {
	Question, Interpretation, Strategy string
	Seasons                            []int
	Result                             QueryResult
}
type AnswerPresentation struct {
	Text string `json:"text"`
}
type Model interface {
	Plan(context.Context, PlanInput) (QueryPlan, error)
	Answer(context.Context, AnswerInput) (AnswerPresentation, error)
}
type QueryService struct {
	DB       *Database
	Model    Model
	Config   Config
	Logger   *log.Logger
	Recorder *EvaluationRecorder
	pending  pendingClarifications
}

func NewQueryService(db *Database, model Model, config Config, logger *log.Logger) *QueryService {
	return &QueryService{DB: db, Model: model, Config: config, Logger: logger}
}
func (q *QueryService) Ask(ctx context.Context, req QuestionRequest) (outcome Outcome) {
	if q.Recorder != nil {
		record := &EvaluationRecord{Time: time.Now().UTC(), Question: req.Question}
		ctx = context.WithValue(ctx, evaluationContextKey{}, record)
		defer func() {
			record.Outcome = outcome
			if err := q.Recorder.write(record); err != nil {
				q.logFailure("record evaluation", err)
			}
		}()
	}
	if strings.TrimSpace(req.Question) == "" || len(req.Question) > 4000 {
		return Outcome{Kind: "error", Message: "Please enter a baseball statistics question of at most 4,000 characters."}
	}
	req, intent, pending := q.prepareQuestion(ctx, req)
	if pending != nil {
		return *pending
	}
	strategies := []string{q.Config.DefaultStrategy}
	if q.Config.ComparisonMode {
		other := StrategyExamples
		if strategies[0] == StrategyExamples {
			other = StrategySchema
		}
		strategies = append(strategies, other)
	}
	type candidate struct {
		strategy string
		plan     QueryPlan
		failure  *StrategyOutcome
	}
	candidates := make([]candidate, 0, len(strategies))
	inputFor := func(strategy string) PlanInput {
		return PlanInput{Question: req.Question, Interpretation: intent.Interpretation, Strategy: strategy, Schema: data.Schema(), Seasons: q.DB.Seasons, ResolvedIntent: &intent}
	}
	// Resolve all strategy plans before executing any: either can identify an
	// ambiguity, and an initial planning failure must not suppress the other.
	for _, strategy := range strategies {
		item := candidate{strategy: strategy}
		plan, err := q.plan(ctx, inputFor(strategy))
		if err != nil {
			failure := q.strategyFailure(strategy, "interpret question", err)
			item.failure = &failure
			candidates = append(candidates, item)
			continue
		}
		switch plan.Status {
		case "clarification":
			if !q.safePublicText(plan.Message) {
				return q.failure("validate clarification", errors.New("private clarification"))
			}
			intent.Interpretation = plan.Message
			intent.Seasons = plan.Seasons
			return q.remember(req.Question, intent, nil)
		case "unsupported":
			if !q.safePublicText(plan.Message) {
				return q.failure("validate unsupported explanation", errors.New("private explanation"))
			}
			if !q.Config.ComparisonMode {
				return Outcome{Kind: "unsupported", Message: plan.Message}
			}
			failure := StrategyOutcome{Strategy: strategy, Kind: "unsupported", Message: plan.Message}
			item.failure = &failure
		case "query":
			err = q.validateSeasons(plan.Seasons)
			if err == nil && len(intent.Seasons) > 0 && !sameSeasons(intent.Seasons, plan.Seasons) {
				err = errors.New("strategy changed the resolved seasons")
			}
			if err != nil {
				failure := q.strategyFailure(strategy, "validate season", err)
				item.failure = &failure
				break
			}
			if intent.Interpretation == "" {
				intent.Interpretation = plan.Interpretation
				if intent.Interpretation == "" {
					intent.Interpretation = req.Question
				}
			}
			intent.Seasons = plan.Seasons
			item.plan = plan
		default:
			failure := q.strategyFailure(strategy, "validate query plan", errors.New("unknown plan status"))
			item.failure = &failure
		}
		candidates = append(candidates, item)
	}
	req.Interpretation = intent.Interpretation
	out := Outcome{Kind: "unsupported", Seasons: intent.Seasons}
	for _, item := range candidates {
		var result StrategyOutcome
		if item.failure != nil {
			result = *item.failure
		} else {
			result, _ = q.executeStrategy(ctx, req, inputFor(item.strategy), item.plan)
		}
		out.Strategies = append(out.Strategies, result)
		if result.Kind == "answer" {
			out.Kind = "answer"
		} else if result.Kind == "error" && out.Kind != "answer" {
			out.Kind = "error"
		}
	}
	return out
}

func sameSeasons(a, b []int) bool {
	left, right := map[int]bool{}, map[int]bool{}
	for _, year := range a {
		left[year] = true
	}
	for _, year := range b {
		right[year] = true
	}
	if len(left) != len(right) {
		return false
	}
	for year := range left {
		if !right[year] {
			return false
		}
	}
	return true
}
func (q *QueryService) failure(stage string, err error) Outcome {
	if q.Logger != nil {
		q.Logger.Printf("%s: %v", stage, err)
	}
	return Outcome{Kind: "error", Message: "I couldn't answer that question. Please try again or ask a more specific baseball question."}
}

func (q *QueryService) safePublicText(text string) bool {
	return safePublicText(text) && !q.DB.ContainsPlayerIdentity(text)
}

func safePublicText(text string) bool {
	lower := strings.ToLower(text)
	if strings.TrimSpace(text) == "" {
		return false
	}
	for _, term := range []string{"select ", "create table", "sqlite", "player_id", "on_base_plus_slg", "database schema", "pragma ", "sql query"} {
		if strings.Contains(lower, term) {
			return false
		}
	}
	for _, m := range data.Metrics {
		if strings.Contains(m.Name, "_") && strings.Contains(lower, m.Name) {
			return false
		}
	}
	return true
}

// Native column origins identify stored metrics even when SQL chooses aliases.
// Calculated values require explicit meanings from this small server catalog;
// arbitrary SQL aliases never become public labels.
func applyComputedMeasures(result *QueryResult, meanings []string) error {
	for i, origin := range result.Origins {
		if origin != "" {
			continue
		}
		if len(meanings) != len(result.Columns) {
			return errors.New("unknown calculated measure")
		}
		minimum := int64(0)
		switch meanings[i] {
		case "rank":
			minimum = 1
		case "player_count":
		default:
			return errors.New("unknown calculated measure")
		}
		for _, row := range result.Rows {
			value, ok := row[i].(int64)
			if !ok || value < minimum {
				return errors.New("invalid calculated measure")
			}
		}
		result.Origins[i] = meanings[i]
	}
	return nil
}

func presentTable(result QueryResult) (*PresentationTable, error) {
	table := &PresentationTable{Rows: [][]string{}}
	formats := make([]int, len(result.Columns))
	for i, origin := range result.Origins {
		switch origin {
		case "name":
			table.Columns = append(table.Columns, "Player")
			formats[i] = -2
		case "player_count":
			table.Columns = append(table.Columns, "Players")
			formats[i] = 0
		case "rank":
			table.Columns = append(table.Columns, "Rank")
			formats[i] = 0
		case "year":
			table.Columns = append(table.Columns, "Season")
			formats[i] = 0
		default:
			found := false
			for _, m := range data.Metrics {
				if origin == m.Name {
					table.Columns = append(table.Columns, m.Label)
					formats[i] = m.Decimals
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("unrecognized presentation column %d", i)
			}
		}
	}
	for _, row := range result.Rows {
		display := make([]string, len(row))
		for i, value := range row {
			if value == nil {
				display[i] = "—"
				continue
			}
			switch v := value.(type) {
			case string:
				if formats[i] != -2 {
					return nil, errors.New("unexpected text result")
				}
				parts := strings.SplitN(v, ", ", 2)
				if len(parts) == 2 {
					v = parts[1] + " " + parts[0]
				}
				if !safePublicText(v) {
					return nil, errors.New("invalid player display")
				}
				display[i] = v
			case int64:
				display[i] = strconv.FormatInt(v, 10)
			case float64:
				display[i] = strconv.FormatFloat(v, 'f', formats[i], 64)
			default:
				return nil, errors.New("unsupported display value")
			}
		}
		table.Rows = append(table.Rows, display)
	}
	return table, nil
}
