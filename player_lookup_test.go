package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func playerLookupApp(t *testing.T, respond func(string, string) any, source ...string) (*QueryService, httpHandler) {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Input string `json:"input"`
			Text  struct {
				Format struct {
					Name string `json:"name"`
				} `json:"format"`
			} `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		value := respond(payload.Text.Format.Name, payload.Input)
		raw, err := json.Marshal(value)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "resp_lookup", "object": "response", "status": "completed", "output": []any{map[string]any{"type": "message", "id": "msg_lookup", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": string(raw), "annotations": []any{}}}}}})
	}))
	t.Cleanup(provider.Close)
	model := &OpenAIModel{Client: openai.NewClient(option.WithAPIKey("test-only"), option.WithBaseURL(provider.URL+"/"), option.WithMaxRetries(0)), ModelName: "test-model"}
	return testApp(t, model, source...)
}

const playerComparisonSQL = `SELECT p.name,s.year,s.home_run,s.on_base_plus_slg,s.pa,p.player_id FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.player_id IN (608070,691406) AND s.year=2026 ORDER BY p.name`

func TestQuestionRecognizesUnaccentedPlayerComparison(t *testing.T) {
	for _, mode := range []string{"local", "production"} {
		for _, question := range []string{
			"Compare Jose Ramirez and junior caminero",
			"Compare José Ramírez and Junior Caminero",
			"Compare Jose\u0301 Rami\u0301rez and Junior Caminero",
			"COMPARE JOSE RAMIREZ AND JUNIOR CAMINERO",
			"Compare Jose\t  Ramirez and junior\n caminero",
		} {
			t.Run(mode+"/"+question, func(t *testing.T) {
				answerCalled := false
				service, app := playerLookupApp(t, func(kind, input string) any {
					if kind == "query_plan" {
						sql := `SELECT p.name,s.year,s.home_run,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.name IN ('Ramirez, Jose','Caminero, Junior') AND s.year=2026`
						// The external provider uses catalog identities only when the app supplies them.
						if strings.Contains(input, "Ramírez, José") && strings.Contains(input, "608070") && strings.Contains(input, "691406") {
							sql = playerComparisonSQL
						}
						return QueryPlan{Status: "query", SQL: sql, Seasons: []int{2026}, Interpretation: "Compare both players in 2026.", LookupPlayerIDs: []int64{608070, 691406}}
					}
					answerCalled = true
					var answer struct {
						Table PresentationTable `json:"table"`
					}
					if err := json.Unmarshal([]byte(input), &answer); err != nil {
						t.Error(err)
					}
					want := [][]string{{"Junior Caminero", "2026", "42", "0.884", "702"}, {"José Ramírez", "2026", "12", "0.688", "544"}}
					if !reflect.DeepEqual(answer.Table.Rows, want) {
						t.Errorf("answer provider received %v, want both players %v", answer.Table.Rows, want)
					}
					return AnswerPresentation{Text: "In the 2026 snapshot, José Ramírez had 12 HR, .688 OPS and 544 PA; Junior Caminero had 42 HR, .884 OPS and 702 PA."}
				})
				service.Config.Environment = mode
				got := askPublic(t, app, QuestionRequest{Question: question})
				if got.Kind != "answer" || !answerCalled || !strings.Contains(got.Strategies[0].Message, "José Ramírez") {
					t.Fatalf("comparison failed: %+v", got)
				}
				public, _ := json.Marshal(got)
				for _, private := range []string{"608070", "691406", "SELECT", "player_id"} {
					if strings.Contains(string(public), private) {
						t.Errorf("private detail leaked: %s", private)
					}
				}
			})
		}
	}
}

func TestQuestionMatchesOnlyCompletePlayerNames(t *testing.T) {
	for _, question := range []string{"Compare Jose Ramirezson and Junior Caminero", "Compare SuperJose Ramirez and Junior Caminero", "Compare Ramirez and Junior Caminero"} {
		t.Run(question, func(t *testing.T) {
			_, app := playerLookupApp(t, func(kind, input string) any {
				if kind == "query_plan" {
					if strings.Contains(input, "608070") {
						t.Error("unrequested José Ramírez identity matched")
					}
					if !strings.Contains(input, "691406") {
						t.Error("Junior Caminero was not resolved")
					}
					return QueryPlan{Status: "query", SQL: `SELECT p.name,s.year,s.pa FROM players p JOIN stats s ON s.player_id=p.player_id WHERE p.player_id=691406 AND s.year=2026`, Seasons: []int{2026}}
				}
				return AnswerPresentation{Text: "Junior Caminero had 702 PA in 2026."}
			})
			if got := askPublic(t, app, QuestionRequest{Question: question}); got.Kind != "answer" {
				t.Fatalf("lookup failed: %+v", got)
			}
		})
	}
}

func TestQuestionKeepsPlayersWithCollidingNormalizedNamesDistinct(t *testing.T) {
	source, err := os.ReadFile("advanced-mlb-24-26.csv")
	if err != nil {
		t.Fatal(err)
	}
	// Two separate IDs now share a search key but have different source spellings.
	fixture := strings.ReplaceAll(string(source), `"Muncy, Max",571970`, `"Múncy, Máx",571970`)
	path := filepath.Join(t.TempDir(), "collision.csv")
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	app := rankingApp(t, QueryPlan{Status: "query", SQL: `SELECT p.name,s.year,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON s.player_id=p.player_id WHERE p.player_id=691777 AND s.year=2026`, Seasons: []int{2026}}, "Max Muncy, age 23, had .683 OPS over 284 PA in 2026.", path)
	question := "What was MAX MUNCY's OPS in 2026?"
	pending := askPublic(t, app, QuestionRequest{Question: question})
	if pending.Kind != "clarification" || len(pending.Choices) != 2 {
		t.Fatalf("colliding players silently merged: %+v", pending)
	}
	choice := ""
	for _, candidate := range pending.Choices {
		if strings.Contains(candidate, "age 23") {
			choice = candidate
		}
	}
	got := askPublic(t, app, QuestionRequest{Question: question, Confirmation: pending.Confirmation, Choice: choice})
	if got.Kind != "answer" || len(got.Strategies[0].Table.Rows) != 1 || got.Strategies[0].Table.Rows[0][2] != "0.683" {
		t.Fatalf("wrong identity: %+v", got)
	}
}

func TestQuestionCorrectsIncompletePlayerComparisonBeforeAnswering(t *testing.T) {
	plans, answers := 0, 0
	service, app := playerLookupApp(t, func(kind, input string) any {
		if kind == "query_plan" {
			plans++
			sql := strings.Replace(playerComparisonSQL, "IN (608070,691406)", "IN (691406)", 1)
			if plans == 2 {
				if !strings.Contains(input, "coverage") || !strings.Contains(input, "608070") || !strings.Contains(input, "691406") {
					t.Error("correction lost coverage diagnostic or catalog")
				}
				sql = playerComparisonSQL
			}
			return QueryPlan{Status: "query", SQL: sql, Seasons: []int{2026}, LookupPlayerIDs: []int64{608070, 691406}}
		}
		answers++
		var answer struct {
			Table PresentationTable `json:"table"`
		}
		json.Unmarshal([]byte(input), &answer)
		if len(answer.Table.Rows) != 2 {
			t.Error("incomplete comparison reached answer generation")
		}
		return AnswerPresentation{Text: "José Ramírez and Junior Caminero's 2026 snapshot statistics are available."}
	})
	service.Config.Environment = "production"
	got := askPublic(t, app, QuestionRequest{Question: "Compare Jose Ramirez and junior caminero"})
	if got.Kind != "answer" || plans != 2 || answers != 1 {
		t.Fatalf("coverage correction failed: outcome=%+v plans=%d answers=%d", got, plans, answers)
	}
}

func TestQuestionStopsAfterOneCoverageCorrectionAndPreservesOtherStrategy(t *testing.T) {
	for _, comparison := range []bool{false, true} {
		t.Run(map[bool]string{false: "single strategy", true: "comparison mode"}[comparison], func(t *testing.T) {
			plans, answers := 0, 0
			service, app := playerLookupApp(t, func(kind, input string) any {
				if kind == "query_plan" {
					plans++
					sql := strings.Replace(playerComparisonSQL, "IN (608070,691406)", "IN (691406)", 1)
					if strings.Contains(input, "Worked question/SQL examples:") {
						sql = playerComparisonSQL
					}
					return QueryPlan{Status: "query", SQL: sql, Seasons: []int{2026}, LookupPlayerIDs: []int64{608070, 691406}}
				}
				answers++
				return AnswerPresentation{Text: "José Ramírez and Junior Caminero's 2026 statistics are available."}
			})
			service.Config.Environment = "production"
			service.Config.ComparisonMode = comparison
			got := askPublic(t, app, QuestionRequest{Question: "Compare Jose Ramirez and junior caminero"})
			if got.Strategies[0].Kind != "error" || got.Strategies[0].Table != nil {
				t.Fatalf("incomplete answer accepted: %+v", got)
			}
			if comparison {
				if got.Kind != "answer" || plans != 3 || answers != 1 || got.Strategies[1].Kind != "answer" {
					t.Fatalf("successful strategy lost: %+v plans=%d answers=%d", got, plans, answers)
				}
			} else if got.Kind != "error" || plans != 2 || answers != 0 {
				t.Fatalf("correction limit or answer guard failed: %+v plans=%d answers=%d", got, plans, answers)
			}
			public, _ := json.Marshal(got)
			for _, private := range []string{"608070", "691406", "player_id", "coverage:", "SELECT"} {
				if strings.Contains(string(public), private) {
					t.Errorf("private diagnostic leaked: %s", private)
				}
			}
		})
	}
}

func TestQuestionCoverageCorrectionCannotDropRequestedPlayer(t *testing.T) {
	plans, answers := 0, 0
	_, app := playerLookupApp(t, func(kind, input string) any {
		if kind == "query_plan" {
			plans++
			ids := []int64{608070, 691406}
			if plans == 2 {
				ids = []int64{691406}
			}
			return QueryPlan{Status: "query", SQL: strings.Replace(playerComparisonSQL, "IN (608070,691406)", "IN (691406)", 1), Seasons: []int{2026}, LookupPlayerIDs: ids}
		}
		answers++
		return AnswerPresentation{Text: "Only Junior Caminero is available."}
	})
	got := askPublic(t, app, QuestionRequest{Question: "Compare Jose Ramirez and junior caminero"})
	if got.Kind != "error" || plans != 2 || answers != 0 {
		t.Fatalf("correction dropped player: %+v plans=%d answers=%d", got, plans, answers)
	}
}

func TestQuestionDescribesGenuinelyMissingPlayerSeason(t *testing.T) {
	source, err := os.ReadFile("advanced-mlb-24-26.csv")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(source), "\n") {
		if !strings.HasPrefix(line, `"Ramírez, José",608070,2026,`) {
			lines = append(lines, line)
		}
	}
	path := filepath.Join(t.TempDir(), "missing-season.csv")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	plans, answers := 0, 0
	_, app := playerLookupApp(t, func(kind, input string) any {
		if kind == "query_plan" {
			plans++
			return QueryPlan{Status: "query", SQL: playerComparisonSQL, Seasons: []int{2026}, LookupPlayerIDs: []int64{608070, 691406}}
		}
		answers++
		var answer struct {
			Missing []struct {
				Player string `json:"player"`
				Year   int    `json:"year"`
			} `json:"missing_player_seasons"`
		}
		if err := json.Unmarshal([]byte(input), &answer); err != nil {
			t.Error(err)
		}
		if len(answer.Missing) != 1 || answer.Missing[0].Player != "José Ramírez" || answer.Missing[0].Year != 2026 {
			t.Errorf("missing season context: %+v", answer)
		}
		return AnswerPresentation{Text: "Junior Caminero had 42 HR over 702 PA in the 2026 snapshot."}
	}, path)
	got := askPublic(t, app, QuestionRequest{Question: "Compare Jose Ramirez and Junior Caminero in 2026"})
	if got.Kind != "answer" || plans != 1 || answers != 1 || !strings.Contains(got.Strategies[0].Message, "no player-season record for José Ramírez in 2026") {
		t.Fatalf("missing season misreported: %+v plans=%d answers=%d", got, plans, answers)
	}
}

func TestQuestionDoesNotRequireEveryPlayerInIntentionalFilteredSubset(t *testing.T) {
	for _, question := range []string{"Among Jose Ramirez and Junior Caminero, show only the player with more home runs in 2026", "Compare Jose Ramirez and Junior Caminero, but only show the player with more home runs in 2026"} {
		t.Run(question, func(t *testing.T) {
			plans := 0
			_, app := playerLookupApp(t, func(kind, input string) any {
				if kind == "query_plan" {
					plans++
					return QueryPlan{Status: "query", SQL: strings.Replace(playerComparisonSQL, ",p.player_id FROM", " FROM", 1) + " LIMIT 1", Seasons: []int{2026}, LookupPlayerIDs: []int64{}}
				}
				return AnswerPresentation{Text: "Junior Caminero had more home runs in the 2026 snapshot."}
			})
			got := askPublic(t, app, QuestionRequest{Question: question})
			if got.Kind != "answer" || plans != 1 {
				t.Fatalf("intentional subset rejected: %+v plans=%d", got, plans)
			}
		})
	}
}

func TestQuestionChecksEveryRequestedPlayerSeason(t *testing.T) {
	plans := 0
	_, app := playerLookupApp(t, func(kind, input string) any {
		if kind == "query_plan" {
			plans++
			sql := playerComparisonSQL
			if plans == 2 {
				sql = strings.Replace(sql, "s.year=2026", "s.year IN (2024,2026)", 1)
			}
			return QueryPlan{Status: "query", SQL: sql, Seasons: []int{2024, 2026}, LookupPlayerIDs: []int64{608070, 691406}}
		}
		var answer struct {
			Table PresentationTable `json:"table"`
		}
		json.Unmarshal([]byte(input), &answer)
		if len(answer.Table.Rows) != 4 {
			t.Errorf("not every requested player-season was retrieved: %v", answer.Table.Rows)
		}
		return AnswerPresentation{Text: "Both players have statistics in the 2024 and 2026 snapshots."}
	})
	got := askPublic(t, app, QuestionRequest{Question: "Compare Jose Ramirez and Junior Caminero in 2024 and 2026"})
	if got.Kind != "answer" || plans != 2 {
		t.Fatalf("season coverage failed: %+v plans=%d", got, plans)
	}
}

func TestHostedQuestionResolvesUnaccentedComparison(t *testing.T) {
	store := operationalFixture(t)
	auth, sign, _ := hostedAuthFixture(t)
	service, _ := playerLookupApp(t, func(kind, input string) any {
		if kind == "query_plan" {
			if !strings.Contains(input, "608070") || !strings.Contains(input, "691406") {
				t.Error("hosted planning omitted resolved catalog identities")
			}
			return QueryPlan{Status: "query", SQL: playerComparisonSQL, Seasons: []int{2026}, LookupPlayerIDs: []int64{608070, 691406}}
		}
		var answer struct {
			Table PresentationTable `json:"table"`
		}
		json.Unmarshal([]byte(input), &answer)
		if len(answer.Table.Rows) != 2 {
			t.Errorf("hosted answer omitted a player: %v", answer.Table.Rows)
		}
		return AnswerPresentation{Text: "In the 2026 snapshot, José Ramírez had 12 HR over 544 PA, and Junior Caminero had 42 HR over 702 PA."}
	})
	app := NewHostedHandler(service, HostedOptions{Mode: "production", Auth: auth, State: store})
	token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
	response := hostedPost(app, token, `{"question":"Compare Jose Ramirez and junior caminero"}`)
	var got Outcome
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || got.Kind != "answer" || !strings.Contains(got.Strategies[0].Message, "José Ramírez") {
		t.Fatalf("hosted comparison failed: %s", response.Body.String())
	}
}

func TestQuestionCorrectsWrongSameNameIdentityBeforeAnswering(t *testing.T) {
	for _, combine := range []bool{false, true} {
		t.Run(fmt.Sprint("combined identities: ", combine), func(t *testing.T) {
			plans, answers := 0, 0
			_, app := playerLookupApp(t, func(kind, input string) any {
				if kind == "query_plan" {
					plans++
					id := 571970
					if plans == 2 {
						id = 691777
					}
					sql := fmt.Sprintf("SELECT p.name,s.year,s.on_base_plus_slg,s.pa,p.player_id FROM players p JOIN stats s ON s.player_id=p.player_id WHERE p.player_id=%d AND s.year=2026", id)
					if plans == 1 && combine {
						sql = strings.Replace(sql, "p.player_id=571970", "p.player_id IN (571970,691777)", 1)
					}
					return QueryPlan{Status: "query", SQL: sql, Seasons: []int{2026}, LookupPlayerIDs: []int64{691777}}
				}
				answers++
				if strings.Contains(input, "691777") || strings.Contains(input, "571970") {
					t.Error("private row identities reached the answer provider")
				}
				var answer struct {
					Table PresentationTable `json:"table"`
				}
				json.Unmarshal([]byte(input), &answer)
				if len(answer.Table.Rows) != 1 || answer.Table.Rows[0][2] != "0.683" {
					t.Errorf("wrong same-name identity reached answer: %v", answer.Table.Rows)
				}
				return AnswerPresentation{Text: "The younger Max Muncy had .683 OPS in 2026 over 284 PA."}
			})
			got := askPublic(t, app, QuestionRequest{Question: "What was Max Muncy's OPS in 2026, the player age 23?"})
			if got.Kind != "answer" || plans != 2 || answers != 1 {
				t.Fatalf("same-name correction failed: %+v plans=%d answers=%d", got, plans, answers)
			}
		})
	}
}

func TestQuestionRetainsResolvedIdentityInNameFreeRevision(t *testing.T) {
	_, app := playerLookupApp(t, func(kind, input string) any {
		if kind == "query_plan" {
			if !strings.Contains(input, "Muncy, Max") || !strings.Contains(input, "691777") {
				t.Error("name-free revision lost the resolved catalog identity")
			}
			return QueryPlan{Status: "query", SQL: `SELECT p.name,s.year,s.on_base_plus_slg,s.pa,p.player_id FROM players p JOIN stats s ON s.player_id=p.player_id WHERE p.player_id=691777 AND s.year=2026`, Seasons: []int{2026}, LookupPlayerIDs: []int64{691777}}
		}
		return AnswerPresentation{Text: "Max Muncy, age 23, had .683 OPS over 284 PA in 2026."}
	})
	question := "What was Max Muncy's OPS in 2026?"
	pending := askPublic(t, app, QuestionRequest{Question: question})
	if pending.Kind != "clarification" {
		t.Fatalf("expected player clarification: %+v", pending)
	}
	got := askPublic(t, app, QuestionRequest{Question: question, Confirmation: pending.Confirmation, Revision: "I meant the player age 23"})
	if got.Kind != "answer" || got.Strategies[0].Table.Rows[0][2] != "0.683" {
		t.Fatalf("resolved identity lost in revision: %+v", got)
	}
}

func TestQuestionCannotBypassComparisonCoverageWithEmptyModelIdentities(t *testing.T) {
	for _, initialIDs := range [][]int64{{}, {691406}} {
		t.Run(fmt.Sprint("initial identities: ", initialIDs), func(t *testing.T) {
			plans, answers := 0, 0
			_, app := playerLookupApp(t, func(kind, input string) any {
				if kind == "query_plan" {
					plans++
					if plans == 1 {
						sql := strings.Replace(playerComparisonSQL, "IN (608070,691406)", "IN (691406)", 1)
						if len(initialIDs) == 0 {
							sql = strings.Replace(sql, ",p.player_id FROM", " FROM", 1)
						}
						return QueryPlan{Status: "query", SQL: sql, Seasons: []int{2026}, LookupPlayerIDs: initialIDs}
					}
					return QueryPlan{Status: "query", SQL: playerComparisonSQL, Seasons: []int{2026}, LookupPlayerIDs: []int64{608070, 691406}}
				}
				answers++
				var answer struct {
					Table PresentationTable `json:"table"`
				}
				json.Unmarshal([]byte(input), &answer)
				if len(answer.Table.Rows) != 2 {
					t.Error("empty metadata bypassed comparison coverage")
				}
				return AnswerPresentation{Text: "Both players' 2026 snapshot statistics are available."}
			})
			got := askPublic(t, app, QuestionRequest{Question: "Compare Jose Ramirez and junior caminero"})
			if got.Kind != "answer" || plans != 2 || answers != 1 {
				t.Fatalf("empty metadata bypass: %+v plans=%d answers=%d", got, plans, answers)
			}
		})
	}
}

func TestQuestionRequiresBothPlayersWhenOnlyRestrictsMetricsOrSeasons(t *testing.T) {
	for _, question := range []string{
		"Compare Jose Ramirez and Junior Caminero, only for 2026",
		"Compare only the home runs of Jose Ramirez and Junior Caminero in 2026",
		"Compare only Jose Ramirez and Junior Caminero in 2026",
	} {
		t.Run(question, func(t *testing.T) {
			plans, answers := 0, 0
			_, app := playerLookupApp(t, func(kind, input string) any {
				if kind == "query_plan" {
					plans++
					return QueryPlan{Status: "query", SQL: strings.Replace(strings.Replace(playerComparisonSQL, "IN (608070,691406)", "IN (691406)", 1), ",p.player_id FROM", " FROM", 1), Seasons: []int{2026}, LookupPlayerIDs: []int64{}}
				}
				answers++
				return AnswerPresentation{Text: "Only Junior Caminero's statistics are available."}
			})
			got := askPublic(t, app, QuestionRequest{Question: question})
			if got.Kind != "error" || plans != 2 || answers != 0 {
				t.Fatalf("metric/season restriction bypassed player coverage: %+v plans=%d answers=%d", got, plans, answers)
			}
		})
	}
}

func TestQuestionRetainsNamedPlayersInMetricRevision(t *testing.T) {
	plans, answers := 0, 0
	_, app := playerLookupApp(t, func(kind, input string) any {
		if kind == "query_plan" {
			plans++
			if plans == 1 {
				return QueryPlan{Status: "clarification", Message: "Compare both players by OPS in 2026?", Seasons: []int{2026}}
			}
			if !strings.Contains(input, "608070") || !strings.Contains(input, "691406") {
				t.Error("metric revision lost the original named players")
			}
			return QueryPlan{Status: "query", SQL: playerComparisonSQL, Seasons: []int{2026}, LookupPlayerIDs: []int64{608070, 691406}}
		}
		answers++
		return AnswerPresentation{Text: "In 2026, José Ramírez hit 12 HR and Junior Caminero hit 42 HR in the imported snapshot."}
	})
	question := "Compare Jose Ramirez and Junior Caminero in 2026"
	pending := askPublic(t, app, QuestionRequest{Question: question})
	if pending.Kind != "clarification" {
		t.Fatalf("missing metric clarification: %+v", pending)
	}
	got := askPublic(t, app, QuestionRequest{Question: question, Confirmation: pending.Confirmation, Revision: "Use home runs in 2026"})
	if got.Kind != "answer" || plans != 2 || answers != 1 {
		t.Fatalf("metric revision lost players: %+v plans=%d answers=%d", got, plans, answers)
	}
}
