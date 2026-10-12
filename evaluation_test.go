package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateEvaluationRecordsStayOutsideHTTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "eval.jsonl")
	service, _ := testApp(t, &scriptedModel{plan: QueryPlan{Status: "query", SQL: "SELECT year,home_run,pa FROM stats WHERE player_id=670541 AND year=2026", Seasons: []int{2026}}})
	recorder, err := OpenEvaluationRecorder(path, "dummy-private-key")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	service.Recorder = recorder
	handler := NewHandler(service)
	askRanking(t, handler, "What were Yordan Alvarez's home runs in 2026? dummy-private-key")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "dummy-private-key") {
		t.Fatal("secret reached evaluation file")
	}
	var record EvaluationRecord
	if err = json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &record); err != nil {
		t.Fatal(err)
	}
	if record.Question == "" || len(record.Attempts) != 3 || record.Attempts[0].SQL == "" || record.Attempts[1].Result == nil || record.Outcome.Kind != "answer" {
		t.Fatalf("incomplete private record: %s", raw)
	}
	for _, route := range []string{"/.private/eval.jsonl", "/evaluation/reference-answers.json", "/developer.log"} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest("GET", route, nil))
		if res.Code != 404 {
			t.Fatalf("private route %s served", route)
		}
	}
}

func TestPrivateRecordingPreservesFailureAndClarificationOutcomes(t *testing.T) {
	for _, tc := range []struct{ name, question, sql, kind string }{
		{"exhausted correction", "What was Yordan Alvarez's OPS in 2026?", "SELECT missing_metric FROM stats", "error"},
		{"identity clarification", "What was Max Muncy's OPS in 2026?", "SELECT missing_metric FROM stats", "clarification"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "record.jsonl")
			recorder, err := OpenEvaluationRecorder(path)
			if err != nil {
				t.Fatal(err)
			}
			defer recorder.Close()
			service, app := testApp(t, &scriptedModel{plan: QueryPlan{Status: "query", SQL: tc.sql, Seasons: []int{2026}}})
			service.Recorder = recorder
			raw, _ := json.Marshal(QuestionRequest{Question: tc.question})
			response := httptest.NewRecorder()
			app.ServeHTTP(response, httptest.NewRequest("POST", "/api/questions", strings.NewReader(string(raw))))
			if strings.Contains(response.Body.String(), "missing_metric") || strings.Contains(response.Body.String(), tc.sql) {
				t.Fatalf("private SQL exposed: %s", response.Body.String())
			}
			bytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var record EvaluationRecord
			if err = json.Unmarshal(bytes, &record); err != nil {
				t.Fatal(err)
			}
			if record.Outcome.Kind != tc.kind {
				t.Fatalf("recorded outcome %+v", record)
			}
			executions := 0
			for _, event := range record.Attempts {
				if event.Stage == "execution" {
					executions++
					if !event.Failed {
						t.Fatal("failed execution not marked")
					}
				}
			}
			if tc.kind == "error" && executions != 2 {
				t.Fatalf("expected bounded correction, got %d executions", executions)
			}
			if tc.kind == "clarification" && executions != 0 {
				t.Fatal("executed before confirmation")
			}
		})
	}
}
