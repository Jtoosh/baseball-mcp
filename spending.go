package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openai/openai-go/v3/responses"
)

const monthlyAllowance int64 = 10_000_000_000 // nano-USD; never configurable
const hostedOutputTokens int64 = 2048

type PricingProfile struct {
	Model         string `json:"model"`
	ContextTokens int64  `json:"context_tokens"`
	InputNanoUSD  int64  `json:"input_nano_usd_per_token"`
	OutputNanoUSD int64  `json:"output_nano_usd_per_token"`
	ValidUntil    string `json:"valid_until"`
}

// Reviewed against the official model/pricing documentation on 2026-10-12.
// Long-context cache-write and output rates dominate every default/global category.
var validatedPricingProfiles = map[string]PricingProfile{
	"gpt-6.1-sol":  {"gpt-6.1-sol", 1_050_000, 5000, 15000, "2026-11-15"},
	"gpt-4.1-mini": {"gpt-4.1-mini", 1_047_576, 400, 1600, "2026-11-15"},
}

type publicFailure struct{ message string }

func (e *publicFailure) Error() string { return e.message }
func friendlyFailure(err error) string {
	var p *publicFailure
	if errors.As(err, &p) {
		return p.message
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "This question took too long. Please ask a narrower question."
	}
	return questionFailureMessage
}

type SpendingGuard struct {
	State   *OperationalStore
	Profile *PricingProfile
}
type spendingReservation struct {
	ID, Month string
	Amount    int64
	Profile   PricingProfile
}

func (g *SpendingGuard) reserve(ctx context.Context, model string) (spendingReservation, error) {
	if g.State == nil {
		return spendingReservation{}, errors.New("spending storage required")
	}
	minimum, known := validatedPricingProfiles[model]
	if !known {
		return spendingReservation{}, errors.New("model pricing profile requires validation")
	}
	profile := minimum
	if g.Profile != nil {
		profile = *g.Profile
	}
	until, err := time.Parse("2006-01-02", profile.ValidUntil)
	if err != nil || !g.State.Now().Before(until) || profile.Model != model || profile.ContextTokens < minimum.ContextTokens || profile.ContextTokens > 2_000_000 || profile.InputNanoUSD < minimum.InputNanoUSD || profile.InputNanoUSD > 1_000_000 || profile.OutputNanoUSD < minimum.OutputNanoUSD || profile.OutputNanoUSD > 1_000_000 {
		return spendingReservation{}, errors.New("model pricing profile requires validation")
	}
	// Reserve the entire model context, including provider framing and schemas;
	// no heuristic text estimate or potentially billed counting call is needed.
	amount := profile.ContextTokens*profile.InputNanoUSD + hostedOutputTokens*profile.OutputNanoUSD
	now := g.State.Now().UTC()
	month := now.Format("2006-01")
	next := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	tx, err := g.State.pool.Begin(ctx)
	if err != nil {
		return spendingReservation{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO baseball_allowances(namespace,month,spent) VALUES($1,$2,0) ON CONFLICT DO NOTHING`, g.State.namespace, month); err != nil {
		return spendingReservation{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE baseball_allowances SET spent=spent+$3 WHERE namespace=$1 AND month=$2 AND spent+$3<=$4`, g.State.namespace, month, amount, monthlyAllowance)
	if err != nil {
		return spendingReservation{}, err
	}
	if tag.RowsAffected() != 1 {
		return spendingReservation{}, &publicFailure{fmt.Sprintf("The monthly question allowance is paused. Questions resume at %s UTC.", next.Format("2006-01-02 15:04"))}
	}
	id := randomToken()
	if _, err = tx.Exec(ctx, `INSERT INTO baseball_spending(namespace,id,month,amount,settled,created) VALUES($1,$2,$3,$4,false,$5)`, g.State.namespace, id, month, amount, now); err != nil {
		return spendingReservation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return spendingReservation{}, err
	}
	return spendingReservation{id, month, amount, profile}, nil
}
func (g *SpendingGuard) settle(ctx context.Context, r spendingReservation, response *responses.Response) error {
	// Missing/partial usage, unexpected tier/model and any uncertain response retain
	// the full reservation. Reasoning tokens are already included in OutputTokens.
	u := response.Usage
	if !u.JSON.InputTokens.Valid() || !u.JSON.OutputTokens.Valid() || !u.JSON.TotalTokens.Valid() || !u.JSON.InputTokensDetails.Valid() || !u.JSON.OutputTokensDetails.Valid() || !u.InputTokensDetails.JSON.CachedTokens.Valid() || !u.OutputTokensDetails.JSON.ReasoningTokens.Valid() || u.InputTokensDetails.CacheWriteTokens < 0 || u.InputTokensDetails.CacheWriteTokens > u.InputTokens-u.InputTokensDetails.CachedTokens || response.Status != "completed" || response.Model != r.Profile.Model || response.ServiceTier != "default" || u.InputTokens <= 0 || u.OutputTokens < 0 || u.OutputTokens > hostedOutputTokens || u.InputTokens > r.Profile.ContextTokens || u.TotalTokens != u.InputTokens+u.OutputTokens || u.OutputTokensDetails.ReasoningTokens < 0 || u.OutputTokensDetails.ReasoningTokens > u.OutputTokens || u.InputTokensDetails.CachedTokens < 0 || u.InputTokensDetails.CachedTokens > u.InputTokens {
		return nil // Retain full reservation; the completed response remains usable.
	}
	actual := u.InputTokens*r.Profile.InputNanoUSD + u.OutputTokens*r.Profile.OutputNanoUSD
	tx, err := g.State.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var held int64
	var settled bool
	if err = tx.QueryRow(ctx, `SELECT amount,settled FROM baseball_spending WHERE namespace=$1 AND id=$2 AND month=$3 FOR UPDATE`, g.State.namespace, r.ID, r.Month).Scan(&held, &settled); err != nil {
		return err
	}
	if settled {
		return nil
	}
	if actual > held {
		return errors.New("usage exceeds validated reservation")
	}
	if _, err = tx.Exec(ctx, `UPDATE baseball_allowances SET spent=spent-$3 WHERE namespace=$1 AND month=$2`, g.State.namespace, r.Month, held-actual); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE baseball_spending SET amount=$3,settled=true WHERE namespace=$1 AND id=$2`, g.State.namespace, r.ID, actual); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
