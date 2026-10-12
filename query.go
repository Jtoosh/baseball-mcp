package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/jtoosh/baseball-mcp/internal/data"
)

const StrategySchema = "schema"
const StrategyExamples = "examples"

type QuestionRequest struct {
	Question       string `json:"question"`
	Confirmation   string `json:"confirmation,omitempty"`
	Interpretation string `json:"interpretation,omitempty"`
}
type Outcome struct {
	Kind         string            `json:"kind"`
	Message      string            `json:"message,omitempty"`
	Seasons      []int             `json:"seasons,omitempty"`
	Strategies   []StrategyOutcome `json:"strategies,omitempty"`
	Confirmation string            `json:"confirmation,omitempty"`
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
	Status  string   `json:"status"`
	SQL     string   `json:"sql"`
	Message string   `json:"message"`
	Seasons []int    `json:"seasons"`
	Columns []string `json:"columns"`
}
type PlanInput struct {
	Question, Interpretation, Strategy, Schema string
	Seasons                                    []int
	PreviousSQL, DatabaseError                 string
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
	DB     *Database
	Model  Model
	Config Config
	Logger *log.Logger
}

func NewQueryService(db *Database, model Model, config Config, logger *log.Logger) *QueryService {
	return &QueryService{db, model, config, logger}
}
func (q *QueryService) Ask(ctx context.Context, req QuestionRequest) Outcome {
	if strings.TrimSpace(req.Question) == "" || len(req.Question) > 4000 {
		return Outcome{Kind: "error", Message: "Please enter a baseball statistics question of at most 4,000 characters."}
	}
	plan, err := q.Model.Plan(ctx, PlanInput{Question: req.Question, Interpretation: req.Interpretation, Strategy: q.Config.DefaultStrategy, Schema: data.Schema(), Seasons: q.DB.Seasons})
	if err != nil {
		return q.failure("interpret question", err)
	}
	strategy, seasons := q.executeStrategy(ctx, req, PlanInput{Question: req.Question, Interpretation: req.Interpretation, Strategy: q.Config.DefaultStrategy, Schema: data.Schema(), Seasons: q.DB.Seasons}, plan)
	return Outcome{Kind: strategy.Kind, Seasons: seasons, Strategies: []StrategyOutcome{strategy}}
}
func (q *QueryService) failure(stage string, err error) Outcome {
	if q.Logger != nil {
		q.Logger.Printf("%s: %v", stage, err)
	}
	return Outcome{Kind: "error", Message: "I couldn't answer that question. Please try again or ask a more specific baseball question."}
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
func presentTable(result QueryResult) (*PresentationTable, error) {
	table := &PresentationTable{Rows: [][]string{}}
	formats := make([]int, len(result.Columns))
	for i, origin := range result.Origins {
		switch origin {
		case "name":
			table.Columns = append(table.Columns, "Player")
			formats[i] = -2
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
