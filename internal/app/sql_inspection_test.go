package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLocalQuestionShowsInitialSQLForEachStrategy(t *testing.T) {
	service, app := testApp(t, &inspectionComparisonModel{})
	service.Config.ComparisonMode = true
	path := t.TempDir() + "/config.json"
	writeInspectionConfig(t, path, `{"model":"test-model","comparison_mode":true,"developer_sql":true}`)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	service.Config = config
	response := postQuestion(app)
	var got struct {
		Kind       string            `json:"kind"`
		Strategies []StrategyOutcome `json:"strategies"`
		Inspection []struct {
			Strategy string `json:"strategy"`
			Queries  []struct {
				SQL string `json:"sql"`
			} `json:"queries"`
		} `json:"sql_inspection"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "answer" || len(got.Strategies) != 2 || len(got.Inspection) != 2 {
		t.Fatalf("missing answers or SQL: %s", response.Body.String())
	}
	want := "SELECT p.name,s.year,s.on_base_plus_slg,s.pa FROM players p JOIN stats s ON p.player_id=s.player_id WHERE p.player_id=676679 AND s.year=2025"
	for i, inspection := range got.Inspection {
		if inspection.Strategy != got.Strategies[i].Strategy || len(inspection.Queries) != 1 || inspection.Queries[0].SQL != want+" -- "+inspection.Strategy {
			t.Fatalf("wrong initial SQL: %s", response.Body.String())
		}
		if !strings.Contains(got.Strategies[i].Message, "0.448") {
			t.Fatal("answer lost")
		}
	}
}

func inspectionResponse(t *testing.T, app httpHandler) map[string]json.RawMessage {
	t.Helper()
	response := httptest.NewRecorder()
	app.ServeHTTP(response, httptest.NewRequest("POST", "/api/questions", strings.NewReader(`{"question":"What was Luis Vázquez's OPS in 2025?"}`)))
	var got map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func writeInspectionConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSQLInspectionRequiresExplicitLocalLoopbackConfiguration(t *testing.T) {
	service, app := testApp(t, &scriptedModel{plan: QueryPlan{Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}}})
	for _, tc := range []struct {
		name, config string
		visible      bool
	}{
		{"default off", `{"model":"test-model"}`, false},
		{"explicit off", `{"model":"test-model","developer_sql":false}`, false},
		{"default loopback", `{"model":"test-model","developer_sql":true}`, true},
		{"localhost", `{"model":"test-model","developer_sql":true,"listen":"localhost:8080"}`, true},
		{"ipv6 loopback", `{"model":"test-model","developer_sql":true,"listen":"[::1]:8080"}`, true},
		{"all interfaces", `{"model":"test-model","developer_sql":true,"listen":"0.0.0.0:8080"}`, false},
		{"empty host", `{"model":"test-model","developer_sql":true,"listen":":8080"}`, false},
		{"network address", `{"model":"test-model","developer_sql":true,"listen":"192.168.1.2:8080"}`, false},
		{"hostname", `{"model":"test-model","developer_sql":true,"listen":"example.com:8080"}`, false},
		{"malformed", `{"model":"test-model","developer_sql":true,"listen":"127.0.0.1"}`, false},
		{"preview", `{"model":"test-model","developer_sql":true,"environment":"preview"}`, false},
		{"production", `{"model":"test-model","developer_sql":true,"environment":"production"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir() + "/config.json"
			writeInspectionConfig(t, path, tc.config)
			config, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			service.Config = config
			got := inspectionResponse(t, app)
			_, visible := got["sql_inspection"]
			if visible != tc.visible {
				t.Fatalf("SQL visibility=%v, want %v: %s", visible, tc.visible, got)
			}
		})
	}
}

func TestLocalSQLInspectionPreservesQueriesOnValidationAndAnswerFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		plan      QueryPlan
		answerErr error
	}{
		{"invalid season", QueryPlan{Status: "query", SQL: correctionLookupSQL, Seasons: []int{1900}}, nil},
		{"answer failure", QueryPlan{Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}}, errInspectionProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, app := testApp(t, &scriptedModel{plan: tc.plan, answerErr: tc.answerErr})
			service.Config.DeveloperSQL = true
			service.Config.Listen = "127.0.0.1:8080"
			got := inspectionResponse(t, app)
			if string(got["kind"]) != `"error"` || !strings.Contains(string(got["sql_inspection"]), correctionLookupSQL) {
				t.Fatalf("query lost: %s", got)
			}
			if strings.Contains(string(got["message"]), "private provider") {
				t.Fatal("provider error leaked")
			}
		})
	}
}

func TestLocalSQLInspectionReportsNoQueryOnClarification(t *testing.T) {
	service, app := testApp(t, &scriptedModel{plan: QueryPlan{Status: "clarification", Message: "Use qualifying OPS in 2025?", Seasons: []int{2025}}})
	service.Config.DeveloperSQL = true
	service.Config.Listen = "127.0.0.1:8080"
	got := inspectionResponse(t, app)
	if string(got["kind"]) != `"clarification"` || string(got["sql_inspection"]) != `[{"strategy":"schema","queries":[]}]` {
		t.Fatalf("no-query state lost: %s", got)
	}
}

var errInspectionProvider = errors.New("private provider error")

func TestHostedQuestionsExcludeInspectionEvenWhenConfigured(t *testing.T) {
	auth, sign, _ := hostedAuthFixture(t)
	for _, mode := range []string{"preview", "production"} {
		t.Run(mode, func(t *testing.T) {
			service, _ := testApp(t, &scriptedModel{plan: QueryPlan{Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}}})
			service.Config.DeveloperSQL = true
			service.Config.Listen = "127.0.0.1:8080"
			app := NewHostedHandler(service, HostedOptions{Mode: mode, Auth: auth, State: operationalFixture(t)})
			token := sign("user_invited", "https://clerk.test.accounts.dev", time.Now().Add(time.Hour))
			response := hostedPost(app, token, `{"question":"What was Luis Vázquez's OPS in 2025?"}`)
			if response.Code != 200 || !strings.Contains(response.Body.String(), `"kind":"answer"`) {
				t.Fatalf("answer unavailable: %s", response.Body.String())
			}
			for _, private := range []string{"sql_inspection", correctionLookupSQL, "player_id"} {
				if strings.Contains(response.Body.String(), private) {
					t.Fatalf("hosted SQL leaked: %s", response.Body.String())
				}
			}
		})
	}
}

func TestLocalSQLInspectionShowsFailedAndCorrectedQueries(t *testing.T) {
	m := &correctionModel{plans: []QueryPlan{
		{Status: "query", SQL: "SELECT missing_private_column FROM stats", Seasons: []int{2025}},
		{Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}},
	}}
	service, app := testApp(t, m)
	service.Config.DeveloperSQL = true
	service.Config.Listen = "127.0.0.1:8080"
	got := inspectionResponse(t, app)
	want := `[{"strategy":"schema","queries":[{"sql":"SELECT missing_private_column FROM stats","failed":true},{"sql":"SELECT year, on_base_plus_slg, pa FROM stats WHERE player_id=676679 AND year=2025"}]}]`
	if string(got["kind"]) != `"answer"` || string(got["sql_inspection"]) != want {
		t.Fatalf("failed/corrected SQL missing: %s", got)
	}
}

type inspectionComparisonModel struct{ comparisonModel }

func (m *inspectionComparisonModel) Plan(ctx context.Context, in PlanInput) (QueryPlan, error) {
	plan, err := m.comparisonModel.Plan(ctx, in)
	if plan.SQL != "" {
		plan.SQL += " -- " + in.Strategy
	}
	return plan, err
}

func TestLocalSQLInspectionKeepsIndependentCorrectionsForBothStrategies(t *testing.T) {
	model := &inspectionComparisonModel{comparisonModel: comparisonModel{needsCorrection: true}}
	service, app := testApp(t, model)
	service.Config.DeveloperSQL = true
	service.Config.Listen = "127.0.0.1:8080"
	service.Config.ComparisonMode = true
	got := inspectionResponse(t, app)
	var inspections []SQLInspection
	if err := json.Unmarshal(got["sql_inspection"], &inspections); err != nil {
		t.Fatal(err)
	}
	if string(got["kind"]) != `"answer"` || len(inspections) != 2 {
		t.Fatalf("comparison lost: %s", got)
	}
	for _, inspection := range inspections {
		if len(inspection.Queries) != 2 || inspection.Queries[0].SQL != "SELECT missing_metric FROM stats -- "+inspection.Strategy || !inspection.Queries[0].Failed || inspection.Queries[1].Failed || !strings.HasSuffix(inspection.Queries[1].SQL, " -- "+inspection.Strategy) {
			t.Fatalf("strategy correction lost: %+v", inspection)
		}
	}
}

func TestLocalSQLInspectionRetainsQueriesWhenCorrectionCannotAnswer(t *testing.T) {
	for _, tc := range []struct {
		name       string
		second     QueryPlan
		planErrors map[int]error
		answerErr  error
		count      int
	}{
		{"second execution fails", QueryPlan{Status: "query", SQL: "SELECT another_missing_column FROM stats", Seasons: []int{2025}}, nil, nil, 2},
		{"corrected season invalid", QueryPlan{Status: "query", SQL: correctionLookupSQL, Seasons: []int{1900}}, nil, nil, 2},
		{"correction provider fails", QueryPlan{}, map[int]error{2: errInspectionProvider}, nil, 1},
		{"answer provider fails", QueryPlan{Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}}, nil, errInspectionProvider, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &correctionModel{plans: []QueryPlan{{Status: "query", SQL: "SELECT missing_private_column FROM stats", Seasons: []int{2025}}, tc.second}, planErrors: tc.planErrors, answerErr: tc.answerErr}
			service, app := testApp(t, m)
			service.Config.DeveloperSQL = true
			service.Config.Listen = "127.0.0.1:8080"
			got := inspectionResponse(t, app)
			var inspections []SQLInspection
			if err := json.Unmarshal(got["sql_inspection"], &inspections); err != nil {
				t.Fatal(err)
			}
			if string(got["kind"]) != `"error"` || len(inspections) != 1 || len(inspections[0].Queries) != tc.count || inspections[0].Queries[0].SQL != "SELECT missing_private_column FROM stats" || !inspections[0].Queries[0].Failed {
				t.Fatalf("failure queries lost: %s", got)
			}
			if tc.count == 2 && inspections[0].Queries[1].SQL != tc.second.SQL {
				t.Fatal("corrected SQL replaced")
			}
			raw, _ := json.Marshal(got)
			if strings.Contains(string(raw), "private provider error") || strings.Contains(string(raw), "no such column") {
				t.Fatal("error details leaked")
			}
		})
	}
}

func TestLocalSQLInspectionMarksResultValidationFailure(t *testing.T) {
	initial := QueryPlan{Status: "query", SQL: "SELECT MAX(on_base_plus_slg) FROM stats WHERE year=2025", Seasons: []int{2025}, Columns: []string{"on_base_plus_slg"}}
	m := &correctionModel{plans: []QueryPlan{initial, {Status: "query", SQL: correctionLookupSQL, Seasons: []int{2025}}}}
	service, app := testApp(t, m)
	service.Config.DeveloperSQL = true
	service.Config.Listen = "127.0.0.1:8080"
	got := inspectionResponse(t, app)
	var inspections []SQLInspection
	if err := json.Unmarshal(got["sql_inspection"], &inspections); err != nil {
		t.Fatal(err)
	}
	if string(got["kind"]) != `"answer"` || len(inspections) != 1 || len(inspections[0].Queries) != 2 || inspections[0].Queries[0].SQL != initial.SQL || !inspections[0].Queries[0].Failed || inspections[0].Queries[1].Failed {
		t.Fatalf("result validation failure not marked: %s", got)
	}
}
