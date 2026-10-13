package main

import (
	"context"
	"fmt"
	"strings"
)

// PreviewModel cannot reach a provider: it contains only snapshot-grounded examples.
type PreviewModel struct{}

func (PreviewModel) Plan(_ context.Context, in PlanInput) (QueryPlan, error) {
	q := strings.ToLower(strings.TrimSpace(in.Question))
	type example struct {
		year   int
		metric string
	}
	examples := map[string]example{
		"who led mlb in ops?":                {2026, "on_base_plus_slg"},
		"who led mlb in ops in 2026?":        {2026, "on_base_plus_slg"},
		"who led the league in avg in 2025?": {2025, "avg"},
		"who led mlb in home runs in 2024?":  {2024, "hr"},
		"who led mlb in home runs in 2025?":  {2025, "hr"},
		"who led mlb in home runs in 2026?":  {2026, "hr"},
	}
	selected, ok := examples[q]
	if !ok {
		return QueryPlan{Status: "unsupported", Message: "This controlled preview uses test responses. Try: Who led MLB in OPS? or Who led the league in AVG in 2025?"}, nil
	}
	year, metric := selected.year, selected.metric
	return QueryPlan{Status: "query", Interpretation: fmt.Sprintf("Qualifying MLB leaders for %d", year), Seasons: []int{year}, SQL: fmt.Sprintf(`WITH ranked AS (SELECT player_id,year,%s,pa,DENSE_RANK() OVER(ORDER BY %s DESC) AS place FROM stats WHERE year=%d AND pa>=502) SELECT p.name,r.year,r.%s,r.pa,r.place FROM ranked r JOIN players p ON p.player_id=r.player_id WHERE r.place=1 ORDER BY p.name`, metric, metric, year, metric), Columns: []string{"name", "year", metric, "pa", "rank"}}, nil
}
func (PreviewModel) Answer(_ context.Context, in AnswerInput) (AnswerPresentation, error) {
	table, err := presentTable(in.Result)
	if err != nil {
		return AnswerPresentation{}, err
	}
	if len(table.Rows) == 0 {
		return AnswerPresentation{Text: "No players match this controlled example."}, nil
	}
	var lines []string
	for _, row := range table.Rows {
		lines = append(lines, fmt.Sprintf("**%s** led qualifying MLB players in %s with %s, over %s plate appearances.", row[0], row[1], row[2], row[3]))
	}
	return AnswerPresentation{Text: strings.Join(lines, "\n\n") + "\n\nControlled preview response from the fixed season snapshot."}, nil
}
