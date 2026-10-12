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
}

func NewOpenAIModel(model string) *OpenAIModel {
	return &OpenAIModel{openai.NewClient(option.WithMaxRetries(0)), model}
}
func (m *OpenAIModel) structured(ctx context.Context, name, instructions, input string, schema map[string]any, target any) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	response, err := m.Client.Responses.New(ctx, responses.ResponseNewParams{Model: m.ModelName, Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(input)}, Instructions: openai.String(instructions), Store: openai.Bool(false), Text: responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{Name: name, Schema: schema, Strict: openai.Bool(true)}}}})
	if err != nil {
		return err
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

var domainInstructions = `You answer MLB regular-season batting questions using fixed season snapshots. The league means all MLB. Rankings and league leaders must filter pa >= 502 BEFORE ranking. Use DENSE_RANK over the statistic alone; top ten means rank <= 10 with every tie. Direct player lookups include nonqualifying records and include pa. Use exported rates without recalculation. Single (1B) means singles, double (2B) means doubles. A missing season defaults to the latest available season and must be disclosed. Source names use Last, First and must be displayed First Last. Never disclose SQL, column identifiers, database schema, private player IDs, provider errors or secrets in natural-language text. Return only permitted baseball values; never select internal player IDs for output. Questions are independent. `

func (m *OpenAIModel) Plan(ctx context.Context, input PlanInput) (QueryPlan, error) {
	var plan QueryPlan
	labels := []string{}
	for _, metric := range data.Metrics {
		labels = append(labels, metric.Name+" = "+metric.Label)
	}
	prompt := fmt.Sprintf("Question: %s\nResolved interpretation: %s\nAvailable seasons: %v\nSchema:\n%s\nMetric meanings:\n%s", input.Question, input.Interpretation, input.Seasons, input.Schema, strings.Join(labels, "\n"))
	if input.ResolvedIntent != nil {
		prompt += fmt.Sprintf("\nConfirmed player identity (private): %d\nUser revision: %s", input.ResolvedIntent.PlayerID, input.ResolvedIntent.Revision)
	}
	instructions := domainInstructions + `Determine scope and whether meaningful interpretation remains unresolved BEFORE generating SQL. For ambiguous meaning such as best hitter, return status clarification, a natural-language best-guess message (for example highest OPS among qualifying MLB hitters), and EMPTY sql. A missing year alone is not ambiguous: default to latest available season. If a resolved interpretation is supplied, use it; a user revision overrides the pending interpretation and may require a new clarification. If the request is outside available batting scope (pitching, absent seasons, teams, AL/NL splits), return status unsupported and a helpful explanation based on the question and schema, with EMPTY sql and no schema details. Otherwise return status query. Put the resolved natural-language meaning in interpretation. A confirmed private player identity must be used to filter the lookup; do not expose it. For a query, generate exactly one SQLite SELECT or read-only WITH query over players and stats. Prefer direct selected columns so a server-owned catalog can present results. The columns array contains semantic meanings in order (not arbitrary SQL labels). For status query return seasons, SQL, empty message. SQL may use IDs to filter and join but must not select IDs. Include season and plate appearances for direct lookups. Include player names where appropriate. Do not query SQLite metadata or administration facilities.`
	err := m.structured(ctx, "query_plan", instructions, prompt, objectSchema(map[string]any{"interpretation": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"query", "clarification", "unsupported"}}, "sql": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}, "seasons": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, "columns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}), &plan)
	return plan, err
}
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
	err = m.structured(ctx, "answer", domainInstructions+`Write a concise answer based only on the supplied result table, question and season context. Include plate appearances for nonranking player lookups. Empty results are a normal no-matches outcome. Do not invent values. Mention that results reflect imported snapshots.`, string(payload), objectSchema(map[string]any{"text": map[string]any{"type": "string"}}), &answer)
	return answer, err
}
