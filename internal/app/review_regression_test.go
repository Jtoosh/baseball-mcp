package app

import (
	"context"
	"testing"
)

type publicTextModel struct {
	scriptedModel
	text, status string
}

func (m *publicTextModel) Plan(context.Context, PlanInput) (QueryPlan, error) {
	if m.status != "answer" {
		return QueryPlan{Status: m.status, Message: m.text, Seasons: []int{2025}}, nil
	}
	return QueryPlan{Status: "query", SQL: "SELECT year,pa FROM stats WHERE year=2025 AND player_id=676679", Seasons: []int{2025}}, nil
}
func (m *publicTextModel) Answer(context.Context, AnswerInput) (AnswerPresentation, error) {
	return AnswerPresentation{Text: m.text}, nil
}
func TestPublicModelTextRejectsSQLAndSchemaAcrossResponsePaths(t *testing.T) {
	texts := []string{"SELECT\nname FROM players", "SELECT\tname FROM players", "SELECT/*separator*/name FROM players", "SELECT--separator\nname FROM players", "/* SELECT name FROM players */", "/* player_id */", "SELECT 42", "SELECT name", "```sql\nSELECT arbitrary_alias\n```", "The stats.year field stores seasons.", "The players.name field stores names.", "CREATE VIRTUAL TABLE extra USING fts5(text)", "SELECT count(*)", "SELECT name FROM `players`", "DROP TABLE players", "UPDATE stats SET pa=1", "INSERT INTO stats VALUES (1)", "The players table contains name and player identity columns.", "The stats table has year and pa columns.", "The database has two tables: players and stats.", "The column name stores player names.", "Tables players and stats are joined by identity.", "The table `players` stores names."}
	for _, status := range []string{"answer", "clarification", "unsupported"} {
		model := &publicTextModel{status: status}
		_, app := testApp(t, model)
		for _, text := range texts {
			t.Run(status+"/"+text, func(t *testing.T) {
				model.text = text
				got := askPublic(t, app, QuestionRequest{Question: "What was Luis Vázquez's OPS in 2025?"})
				if got.Kind != "error" {
					t.Fatalf("private text accepted: %+v", got)
				}
				for _, result := range got.Strategies {
					if result.Table != nil {
						t.Fatal("private answer retained table")
					}
				}
			})
		}
	}
}
func TestPublicModelTextAllowsOrdinaryBaseballDescriptions(t *testing.T) {
	for _, text := range []string{"Select a season to compare home runs.", "Player names appear beside their plate appearances.", "Update your question to ask about batting.", "This table shows MLB batting results.", "These snapshots contain batting statistics; pitching ERA is outside the available scope."} {
		_, app := testApp(t, &publicTextModel{text: text, status: "unsupported"})
		if got := askPublic(t, app, QuestionRequest{Question: "Who led MLB in ERA?"}); got.Kind != "unsupported" {
			t.Fatalf("ordinary explanation rejected: %q %+v", text, got)
		}
	}
}

func TestBrowserCanReviseSameNamePlayerClarification(t *testing.T) {
	for _, example := range []struct {
		revision, ops string
		model         Model
	}{
		{"What was Max Muncy's OPS in 2026, age 23?", "0.683", &playerChoiceModel{}},
		{"I meant the player age 23.", "0.683", &playerChoiceModel{}},
		{"I meant Luis Vázquez in 2025.", "0.448", &clarificationModel{scriptedModel: scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT year,on_base_plus_slg,pa FROM stats WHERE player_id=676679 AND year=2025", Seasons: []int{2025}}}}},
	} {
		t.Run(example.revision, func(t *testing.T) {
			_, app := testApp(t, example.model)
			pending := askPublic(t, app, QuestionRequest{Question: "What was Max Muncy's OPS in 2026?"})
			got := askPublic(t, app, QuestionRequest{Question: "What was Max Muncy's OPS in 2026?", Confirmation: pending.Confirmation, Revision: example.revision})
			if got.Kind != "answer" || got.Strategies[0].Table.Rows[0][1] != example.ops {
				t.Fatalf("revision ignored: %+v", got)
			}
		})
	}
}

type seasonDriftModel struct {
	scriptedModel
	calls    map[string]int
	answered []string
}

func (m *seasonDriftModel) Plan(_ context.Context, in PlanInput) (QueryPlan, error) {
	if m.calls == nil {
		m.calls = map[string]int{}
	}
	m.calls[in.Strategy]++
	if in.Strategy == StrategySchema {
		if m.calls[in.Strategy] == 1 {
			return QueryPlan{Status: "query", SQL: "SELECT absent_metric FROM stats", Seasons: []int{2026}}, nil
		}
		return QueryPlan{Status: "query", SQL: "SELECT year,pa FROM stats WHERE year=2025", Seasons: []int{2025}}, nil
	}
	return QueryPlan{Status: "query", SQL: "SELECT year,pa FROM stats WHERE year=2026 AND player_id=676679", Seasons: []int{2026}}, nil
}
func (m *seasonDriftModel) Answer(_ context.Context, in AnswerInput) (AnswerPresentation, error) {
	m.answered = append(m.answered, in.Strategy)
	return AnswerPresentation{Text: "The 2026 snapshot has the requested plate appearances."}, nil
}
func TestBrowserRejectsCorrectionThatChangesSharedSeason(t *testing.T) {
	m := &seasonDriftModel{}
	svc, app := testApp(t, m)
	svc.Config.ComparisonMode = true
	got := askPublic(t, app, QuestionRequest{Question: "What were Luis Vázquez's plate appearances in 2026?"})
	if got.Kind != "answer" || len(got.Strategies) != 2 || got.Strategies[0].Kind != "error" || got.Strategies[0].Table != nil || got.Strategies[1].Kind != "answer" || len(m.answered) != 1 || m.answered[0] != StrategyExamples {
		t.Fatalf("correction changed season: %+v; summarized %v", got, m.answered)
	}
}
