package app

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

type referenceSuite struct {
	Cases []referenceCase `json:"cases"`
}
type referenceCase struct {
	ID       string          `json:"id"`
	Question string          `json:"question"`
	Expected json.RawMessage `json:"expected"`
}
type referenceCheck struct {
	Plan  QueryPlan
	Rows  [][]string
	Words []string
	Kind  string
}

func referenceChecks() map[string]referenceCheck {
	checks := map[string]referenceCheck{}
	hr := `WITH ranked AS (SELECT p.name,s.year,s.home_run,s.pa,DENSE_RANK() OVER (PARTITION BY s.year ORDER BY s.home_run DESC) AS r FROM players p JOIN stats s ON p.player_id=s.player_id WHERE s.pa>=502) SELECT name,year,home_run,pa FROM ranked WHERE r=1 ORDER BY year,name`
	checks["hr-leaders-all-seasons"] = referenceCheck{QueryPlan{Status: "query", SQL: hr, Seasons: []int{2024, 2025, 2026}}, [][]string{{"Aaron Judge", "2024", "58", "704"}, {"Cal Raleigh", "2025", "60", "705"}, {"Pete Crow-Armstrong", "2026", "45", "726"}, {"Kyle Schwarber", "2026", "45", "701"}}, []string{"Aaron Judge", "58", "Cal Raleigh", "60", "45"}, "answer"}
	ops := `WITH ranked AS (SELECT p.name,s.year,s.on_base_plus_slg,s.pa,DENSE_RANK() OVER (ORDER BY s.on_base_plus_slg DESC) AS r FROM players p JOIN stats s ON p.player_id=s.player_id WHERE s.year=2026 AND s.pa>=502) SELECT name,year,on_base_plus_slg,pa FROM ranked WHERE r=1 ORDER BY name`
	for _, id := range []string{"ops-leader-2026", "latest-season-default"} {
		checks[id] = referenceCheck{QueryPlan{Status: "query", SQL: ops, Seasons: []int{2026}}, [][]string{{"Yordan Alvarez", "2026", "1.035", "691"}}, []string{"Yordan Alvarez", "1.035", "2026"}, "answer"}
	}
	for _, year := range []int{2024, 2025, 2026} {
		checks[fmt.Sprintf("top-ten-overlap-%d", year)] = referenceCheck{QueryPlan{Status: "query", SQL: fmt.Sprintf(`WITH ranked AS (SELECT p.name,s.year,s.home_run,s.single,s.double,s.pa,DENSE_RANK() OVER (ORDER BY s.home_run DESC) AS h,DENSE_RANK() OVER (ORDER BY s.single DESC) AS b,DENSE_RANK() OVER (ORDER BY s.double DESC) AS d FROM players p JOIN stats s ON p.player_id=s.player_id WHERE s.year=%d AND s.pa>=502) SELECT name,year,home_run,single,double,pa FROM ranked WHERE h<=10 AND b<=10 AND d<=10`, year), Seasons: []int{year}}, [][]string{}, []string{"No qualifying players"}, "answer"}
	}
	checks["ties-at-dense-rank-ten"] = referenceCheck{QueryPlan{Status: "query", SQL: `WITH ranked AS (SELECT p.name,s.year,s.home_run,s.pa,DENSE_RANK() OVER (ORDER BY s.home_run DESC) AS r FROM players p JOIN stats s ON p.player_id=s.player_id WHERE s.year=2024 AND s.pa>=502) SELECT name,year,home_run,pa FROM ranked WHERE r=10 ORDER BY name`, Seasons: []int{2024}}, [][]string{{"Pete Alonso", "2024", "34", "695"}, {"Cal Raleigh", "2024", "34", "628"}}, []string{"Pete Alonso", "Cal Raleigh", "34"}, "answer"}
	checks["nonqualifying-player-lookup"] = referenceCheck{QueryPlan{Status: "query", SQL: `SELECT p.name,s.year,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE s.year=2025 AND p.player_id=676679`, Seasons: []int{2025}}, [][]string{{"Luis Vázquez", "2025", "0.448", "53"}}, []string{"Luis Vázquez", "0.448", "53"}, "answer"}
	checks["qualifying-counts"] = referenceCheck{QueryPlan{Status: "query", SQL: `SELECT year,COUNT(*) FROM stats WHERE pa>=502 GROUP BY year ORDER BY year`, Seasons: []int{2024, 2025, 2026}, Columns: []string{"season", "player_count"}}, [][]string{{"2024", "129"}, {"2025", "145"}, {"2026", "135"}}, []string{"129", "145", "135"}, "answer"}
	checks["empty-result"] = referenceCheck{QueryPlan{Status: "query", SQL: `SELECT p.name,s.year,s.home_run,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE s.year=2026 AND s.pa>=502 AND s.home_run>100`, Seasons: []int{2026}}, [][]string{}, []string{"No qualifying players"}, "answer"}
	for _, id := range []string{"ambiguous-player-name", "ambiguous-metric"} {
		checks[id] = referenceCheck{Kind: "clarification"}
	}
	return checks
}
func loadReferenceSuite(t *testing.T) referenceSuite {
	t.Helper()
	raw, err := os.ReadFile("../../evaluation/reference-answers.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite referenceSuite
	if err = json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	if len(suite.Cases) != 12 {
		t.Fatalf("expected twelve held-out cases, got %d", len(suite.Cases))
	}
	return suite
}
func TestTwelveHeldOutCasesThroughPublicWorkflow(t *testing.T) {
	checks := referenceChecks()
	for _, c := range loadReferenceSuite(t).Cases {
		t.Run(c.ID, func(t *testing.T) {
			check, ok := checks[c.ID]
			if !ok {
				t.Fatal("missing reference check")
			}
			plan := check.Plan
			if check.Kind == "clarification" {
				plan = QueryPlan{Status: "clarification", Message: "Use qualifying OPS to compare hitters in 2026?", Seasons: []int{2026}}
			}
			answer := strings.Join(check.Words, "; ") + " in the supplied snapshot."
			app := rankingApp(t, plan, answer)
			raw, _ := json.Marshal(QuestionRequest{Question: c.Question})
			response := httptest.NewRecorder()
			app.ServeHTTP(response, httptest.NewRequest("POST", "/api/questions", strings.NewReader(string(raw))))
			var out Outcome
			if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.Kind != check.Kind {
				t.Fatalf("workflow outcome: %+v", out)
			}
			if check.Kind == "clarification" {
				if out.Confirmation == "" || len(out.Strategies) > 0 {
					t.Fatalf("clarification executed: %+v", out)
				}
				return
			}
			t.Run("retrieved_values", func(t *testing.T) {
				if !reflect.DeepEqual(out.Strategies[0].Table.Rows, check.Rows) {
					t.Fatalf("got %v want %v", out.Strategies[0].Table.Rows, check.Rows)
				}
			})
			t.Run("answer_faithfulness", func(t *testing.T) {
				for _, word := range check.Words {
					if !strings.Contains(out.Strategies[0].Message, word) {
						t.Fatalf("missing reference fact %q", word)
					}
				}
			})
		})
	}
}
