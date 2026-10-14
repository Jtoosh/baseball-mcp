package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jtoosh/baseball-mcp/internal/data"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type OpenAIModel struct {
	Client    openai.Client
	ModelName string
	Budget    *SpendingGuard
}

func NewOpenAIModel(model string) *OpenAIModel {
	return &OpenAIModel{Client: openai.NewClient(option.WithMaxRetries(0)), ModelName: model}
}
func (m *OpenAIModel) structured(ctx context.Context, name, instructions, input string, schema map[string]any, target any) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	params := responses.ResponseNewParams{Model: m.ModelName, Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(input)}, Instructions: openai.String(instructions), Store: openai.Bool(false), Text: responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{Name: name, Schema: schema, Strict: openai.Bool(true)}}}}
	var reservation spendingReservation
	if m.Budget != nil {
		params.MaxOutputTokens = openai.Int(hostedOutputTokens)
		params.ServiceTier = responses.ResponseNewParamsServiceTierDefault
		var err error
		reservation, err = m.Budget.reserve(ctx, m.ModelName)
		if err != nil {
			return err
		}
	}
	response, err := m.Client.Responses.New(ctx, params)
	if err != nil {
		return err
	}
	if m.Budget != nil {
		if err = m.Budget.settle(ctx, reservation, response); err != nil {
			return err
		}
	}
	if response.Status != "completed" {
		return errors.New("model response was incomplete")
	}
	for _, item := range response.Output {
		if item.Type == "message" {
			for _, content := range item.AsMessage().Content {
				if content.Type == "refusal" {
					return errors.New("model declined the request")
				}
			}
		}
	}
	raw := response.OutputText()
	if strings.TrimSpace(raw) == "" {
		return errors.New("empty model response")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
func objectSchema(properties map[string]any) map[string]any {
	required := []string{}
	for key := range properties {
		required = append(required, key)
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

var domainInstructions = `You answer MLB regular-season batting questions using fixed season snapshots. The league means all MLB. Rankings and league leaders must filter pa >= 502 BEFORE ranking. Use DENSE_RANK over the statistic alone; top ten means rank <= 10 with every tie. A top-ten overlap is the intersection of every requested ranking within the same season, using AND across the rank conditions, never a union. Rank each supplied season separately for multi-season leaders. Direct player lookups include nonqualifying records and include pa. Use exported rates without recalculation. Single (1B) means singles, double (2B) means doubles. A missing season defaults to the maximum year in the available imported seasons, not the current calendar year; select that season and disclose it without clarification solely for the default. Source names use Last, First and must be displayed First Last. Never disclose SQL, column identifiers, database schema, private player IDs, provider errors or secrets in natural-language text. Return only permitted baseball values; never select internal player IDs for output. Questions are independent. `

func (m *OpenAIModel) Plan(ctx context.Context, input PlanInput) (QueryPlan, error) {
	var plan QueryPlan
	labels := []string{}
	meanings := []string{"name", "year", "rank", "player_count"}
	for _, metric := range data.Metrics {
		labels = append(labels, metric.Name+" = "+metric.Label)
		meanings = append(meanings, metric.Name)
	}
	prompt := fmt.Sprintf("Question: %s\nResolved interpretation: %s\nAvailable seasons: %v\nSchema:\n%s\nMetric meanings:\n%s", input.Question, input.Interpretation, input.Seasons, input.Schema, strings.Join(labels, "\n"))
	if input.ResolvedIntent != nil {
		prompt += fmt.Sprintf("\nConfirmed player identity (private): %d\nUser revision: %s", input.ResolvedIntent.PlayerID, input.ResolvedIntent.Revision)
	}
	if input.Strategy == StrategyExamples {
		prompt += workedQueryExamples
	}
	ambiguityInstructions := `Determine scope and whether meaningful interpretation remains unresolved BEFORE generating SQL. For ambiguous meaning such as best hitter, return status clarification, a natural-language best-guess message (for example highest OPS among qualifying MLB hitters), and EMPTY sql. A missing year alone is not ambiguous: default to latest available season. If a resolved interpretation is supplied, use it; a user revision overrides the pending interpretation and may require a new clarification. `
	statuses := []string{"query", "clarification", "unsupported"}
	if input.SkipClarification {
		ambiguityInstructions = `Choose an interpretation independently from the original question. For ambiguous metrics or player names, make your best interpretation and generate SQL directly without asking for clarification or confirmation. A missing year defaults to the latest available season. Use the same interpretation and seasons for corrections. `
		statuses = []string{"query", "unsupported"}
	}
	instructions := domainInstructions + ambiguityInstructions + `If the request is outside available batting scope (pitching, absent seasons, teams, AL/NL splits), return status unsupported and a helpful explanation based on the question and schema, with EMPTY sql and no schema details. Otherwise return status query. Put the resolved natural-language meaning in interpretation. A confirmed private player identity must be used to filter the lookup; do not expose it. For a query, generate exactly one SQLite SELECT or read-only WITH query over players and stats. Select stored names, seasons and metrics directly so the server can identify their source columns. Select p.name unchanged: the server converts Last, First into First Last. Do not concatenate, substring, cast or otherwise format selected names. Do not wrap selected metrics in MAX, AVG, ROUND, CAST or other expressions, and do not select literal seasons. For leaders, use DENSE_RANK in a CTE and select the original name, year, metric and pa columns, optionally also the rank. Aggregates such as MAX may be used in filters without projecting them. The server handles public labels and numeric formatting. The columns array must contain one approved meaning per selected column in the same order: use name, year, the stored metric identifier, rank or player_count, never SQL aliases or friendly labels. Use rank for calculated dense ranks and player_count for calculated player counts. Those are the only supported calculated presentation measures; prefer stored columns for every exported metric. For status query return seasons, SQL, empty message. SQL may use IDs to filter and join but must not select IDs. Include season and plate appearances for direct lookups. Include player names where appropriate. Do not query SQLite metadata or administration facilities.`
	if input.PreviousSQL != "" || input.DatabaseError != "" {
		correction, err := json.Marshal(map[string]string{"previous_sql": input.PreviousSQL, "database_error": input.DatabaseError})
		if err != nil {
			return plan, err
		}
		prompt += "\nPrivate correction context:\n" + string(correction)
		instructions += ` Correct the failed query using the private diagnostic context. Keep the same resolved baseball question and seasons. Return one permitted read query. Never repeat SQL or diagnostic details in natural-language text.`
	}
	err := m.structured(ctx, "query_plan", instructions, prompt, objectSchema(map[string]any{"interpretation": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": statuses}, "sql": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}, "seasons": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, "columns": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": meanings}}}), &plan)

	return plan, err
}

// These training questions are distinct from the held-out reference questions.
const workedQueryExamples = `
Worked question/SQL examples:
Question: Show Shohei Ohtani's walks in 2024.
SQL: SELECT p.name,s.year,s.walk,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.name='Ohtani, Shohei' AND s.year=2024
Question: Show hitters with at least 25 doubles in 2025.
SQL: SELECT p.name,s.year,s.double,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE s.year=2025 AND s.double>=25 ORDER BY s.double DESC,p.name
End of worked examples. Answer the actual Question at the start of the input, applying its resolved interpretation and all domain rules.
`

func (m *OpenAIModel) Answer(ctx context.Context, input AnswerInput) (AnswerPresentation, error) {
	var answer AnswerPresentation
	table, err := presentTable(input.Result)
	if err != nil {
		return answer, err
	}
	payload, err := json.Marshal(map[string]any{"question": input.Question, "interpretation": input.Interpretation, "seasons": input.Seasons, "table": table, "partial": input.Result.Truncated})
	if err != nil {
		return answer, err
	}
	err = m.structured(ctx, "answer", domainInstructions+`Write a concise answer based only on the supplied result table, question and season context. Include plate appearances for nonranking player lookups. Empty results are a normal no-matches outcome. When partial is true, the table contains only the first 500 matching rows: explicitly limit every claim to that returned portion, identify the answer as partial, and ask the user to narrow the question. Do not infer totals or coverage beyond the supplied portion. Do not invent values. Mention that results reflect imported snapshots.`, string(payload), objectSchema(map[string]any{"text": map[string]any{"type": "string"}}), &answer)
	return answer, err
}
