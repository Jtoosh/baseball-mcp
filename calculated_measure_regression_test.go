package main

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

type presentationCorrectionModel struct {
	first, corrected QueryPlan
	plans, answers   int
	correction       PlanInput
}

func (m *presentationCorrectionModel) Plan(_ context.Context, in PlanInput) (QueryPlan, error) {
	m.plans++
	if m.plans == 1 {
		return m.first, nil
	}
	m.correction = in
	return m.corrected, nil
}
func (m *presentationCorrectionModel) Answer(_ context.Context, in AnswerInput) (AnswerPresentation, error) {
	m.answers++
	return AnswerPresentation{Text: "The requested qualifying leader is shown for the selected imported season."}, nil
}

func TestBrowserCorrectsUnsupportedLeaderPresentation(t *testing.T) {
	for _, c := range []struct{ question, metric, year, name, value, pa string }{
		{"Who led the league in AVG in 2025?", "batting_avg", "2025", "Aaron Judge", "0.331", "679"},
		{"Who led MLB in OPS?", "on_base_plus_slg", "2026", "Yordan Alvarez", "1.035", "691"},
	} {
		t.Run(c.metric, func(t *testing.T) {
			year := 2025
			if c.year == "2026" {
				year = 2026
			}
			prefix := "SELECT p.name,s.year,"
			suffix := ",s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE s.year=" + c.year + " AND s.pa>=502"
			model := &presentationCorrectionModel{
				first:     QueryPlan{Status: "query", SQL: prefix + "MAX(s." + c.metric + ")" + suffix, Seasons: []int{year}, Columns: []string{"name", "year", c.metric, "pa"}},
				corrected: QueryPlan{Status: "query", SQL: prefix + "s." + c.metric + suffix + " AND s." + c.metric + "=(SELECT MAX(" + c.metric + ") FROM stats WHERE year=" + c.year + " AND pa>=502)", Seasons: []int{year}},
			}
			svc, app := testApp(t, model)
			var diagnostic bytes.Buffer
			svc.Logger = log.New(&diagnostic, "", 0)
			got := askPublic(t, app, QuestionRequest{Question: c.question})
			if got.Kind != "answer" {
				t.Fatalf("leader lookup failed: kind=%s; developer log=%s", got.Kind, strings.TrimSpace(diagnostic.String()))
			}
			table := got.Strategies[0].Table
			if table == nil || len(table.Rows) != 1 || table.Rows[0][0] != c.name || table.Rows[0][1] != c.year || table.Rows[0][2] != c.value || table.Rows[0][3] != c.pa {
				t.Fatalf("incorrect exported leader: %+v", table)
			}
			if model.plans != 2 || model.answers != 1 || model.correction.PreviousSQL == "" || model.correction.DatabaseError == "" {
				t.Fatalf("missing bounded correction: plans=%d answers=%d", model.plans, model.answers)
			}
		})
	}
}

func TestBrowserCorrectsFormattedNamesAndRankMetadata(t *testing.T) {
	sourceSQL := "SELECT p.name,s.year,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.player_id=670541 AND s.year=2026"
	for _, example := range []struct {
		name, sql string
		columns   []string
	}{
		{"formatted player name", "SELECT substr(p.name,instr(p.name,', ')+2)||' '||substr(p.name,1,instr(p.name,', ')-1),s.year,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.player_id=670541 AND s.year=2026", []string{"name", "year", "on_base_plus_slg", "pa"}},
		{"unsupported rank label", "SELECT DENSE_RANK() OVER (ORDER BY on_base_plus_slg DESC) FROM stats WHERE year=2026 AND pa>=502 LIMIT 1", []string{"dense_rank"}},
		{"missing calculation meanings", "SELECT DENSE_RANK() OVER (ORDER BY on_base_plus_slg DESC) FROM stats WHERE year=2026 AND pa>=502 LIMIT 1", nil},
	} {
		t.Run(example.name, func(t *testing.T) {
			model := &presentationCorrectionModel{first: QueryPlan{Status: "query", SQL: example.sql, Seasons: []int{2026}, Columns: example.columns}, corrected: QueryPlan{Status: "query", SQL: sourceSQL, Seasons: []int{2026}}}
			_, app := testApp(t, model)
			got := askPublic(t, app, QuestionRequest{Question: "Who led MLB in OPS?"})
			if got.Kind != "answer" || got.Strategies[0].Table.Rows[0][0] != "Yordan Alvarez" || got.Strategies[0].Table.Rows[0][2] != "1.035" || model.plans != 2 || model.answers != 1 {
				t.Fatalf("lookup correction failed: %+v, plans=%d answers=%d", got, model.plans, model.answers)
			}
		})
	}
}

func TestBrowserPresentationCorrectionUsesExistingAttemptBudget(t *testing.T) {
	for _, firstSQL := range []string{"SELECT MAX(on_base_plus_slg) FROM stats WHERE year=2026 AND pa>=502", "SELECT absent_metric FROM stats"} {
		t.Run(firstSQL, func(t *testing.T) {
			badPresentation := QueryPlan{Status: "query", SQL: "SELECT MAX(on_base_plus_slg) FROM stats WHERE year=2026 AND pa>=502", Seasons: []int{2026}, Columns: []string{"on_base_plus_slg"}}
			first := badPresentation
			first.SQL = firstSQL
			model := &presentationCorrectionModel{first: first, corrected: badPresentation}
			_, app := testApp(t, model)
			got := askPublic(t, app, QuestionRequest{Question: "Who led MLB in OPS?"})
			if got.Kind != "error" || got.Strategies[0].Table != nil || model.plans != 2 || model.answers != 0 {
				t.Fatalf("correction budget exceeded: %+v, plans=%d answers=%d", got, model.plans, model.answers)
			}
			if strings.Contains(got.Strategies[0].Message, "MAX") || strings.Contains(got.Strategies[0].Message, "on_base_plus_slg") {
				t.Fatal("private diagnostic leaked")
			}
		})
	}
}
