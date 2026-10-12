package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Evaluation files are explicit private artifacts, never part of the HTTP DTO.
type EvaluationRecord struct {
	Time     time.Time           `json:"time"`
	Question string              `json:"question"`
	Attempts []EvaluationAttempt `json:"attempts"`
	Outcome  Outcome             `json:"outcome"`
}
type EvaluationAttempt struct {
	Strategy string       `json:"strategy"`
	SQL      string       `json:"sql,omitempty"`
	Result   *QueryResult `json:"results,omitempty"`
	Answer   string       `json:"answer,omitempty"`
	Stage    string       `json:"stage"`
	Failed   bool         `json:"failed,omitempty"`
}
type EvaluationRecorder struct {
	mu      sync.Mutex
	file    *os.File
	secrets []string
}

func OpenEvaluationRecorder(path string, secrets ...string) (*EvaluationRecorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	return &EvaluationRecorder{file: file, secrets: secrets}, nil
}
func (r *EvaluationRecorder) Close() error { return r.file.Close() }
func (r *EvaluationRecorder) write(record any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	text := string(raw)
	for _, secret := range r.secrets {
		if secret != "" {
			encoded, _ := json.Marshal(secret)
			text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "[REDACTED]")
		}
	}
	_, err = r.file.WriteString(text + "\n")
	return err
}

type evaluationContextKey struct{}

func evaluationFrom(ctx context.Context) *EvaluationRecord {
	record, _ := ctx.Value(evaluationContextKey{}).(*EvaluationRecord)
	return record
}
func (q *QueryService) plan(ctx context.Context, in PlanInput) (QueryPlan, error) {
	plan, err := q.Model.Plan(ctx, in)
	if record := evaluationFrom(ctx); record != nil {
		record.Attempts = append(record.Attempts, EvaluationAttempt{Strategy: in.Strategy, SQL: plan.SQL, Stage: "plan", Failed: err != nil})
	}
	return plan, err
}
func (q *QueryService) recordExecution(ctx context.Context, strategy, sql string, result QueryResult, failed bool) {
	if record := evaluationFrom(ctx); record != nil {
		attempt := EvaluationAttempt{Strategy: strategy, SQL: sql, Stage: "execution", Failed: failed}
		if !failed {
			attempt.Result = &result
		}
		record.Attempts = append(record.Attempts, attempt)
	}
}
func (q *QueryService) answer(ctx context.Context, in AnswerInput) (AnswerPresentation, error) {
	answer, err := q.Model.Answer(ctx, in)
	if record := evaluationFrom(ctx); record != nil {
		record.Attempts = append(record.Attempts, EvaluationAttempt{Strategy: in.Strategy, Result: &in.Result, Answer: answer.Text, Stage: "answer", Failed: err != nil})
	}
	return answer, err
}
