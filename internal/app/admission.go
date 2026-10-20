package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

type admissionFailure struct {
	message    string
	retryAfter int
}

func (e *admissionFailure) Error() string   { return e.message }
func retrySeconds(until, now time.Time) int { return max(1, int(math.Ceil(until.Sub(now).Seconds()))) }

func (s *OperationalStore) admit(ctx context.Context, limit int) (func(), error) {
	identity, ok := ctx.Value(hostedContextKey{}).(*hostedIdentity)
	if !ok || identity.LimiterKey == "" {
		return nil, errors.New("missing network identity")
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 {
		return nil, errors.New("invalid hourly allowance")
	}
	now := s.Now().UTC()
	lease := randomToken()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// Bounded global pruning also covers networks that never return. Skip locked
	// rows so cleanup never waits on another network's active transaction.
	if _, err = tx.Exec(ctx, `DELETE FROM baseball_ip_admissions WHERE ctid IN (SELECT ctid FROM baseball_ip_admissions WHERE namespace=$1 AND created<=$2 ORDER BY created LIMIT 256 FOR UPDATE SKIP LOCKED)`, s.namespace, now.Add(-time.Hour)); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM baseball_ip_leases WHERE ctid IN (SELECT ctid FROM baseball_ip_leases WHERE namespace=$1 AND expires<=$2 ORDER BY expires LIMIT 256 FOR UPDATE SKIP LOCKED)`, s.namespace, now.Add(-time.Hour)); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO baseball_ip_leases(namespace,limiter_key,lease,expires) VALUES($1,$2,'',$3) ON CONFLICT DO NOTHING`, s.namespace, identity.LimiterKey, now); err != nil {
		return nil, err
	}
	var existing string
	var expiry time.Time
	if err = tx.QueryRow(ctx, `SELECT lease,expires FROM baseball_ip_leases WHERE namespace=$1 AND limiter_key=$2 FOR UPDATE`, s.namespace, identity.LimiterKey).Scan(&existing, &expiry); err != nil {
		return nil, err
	}
	if existing != "" && expiry.After(now) {
		return nil, &admissionFailure{"Someone on your network already has a question loading. Please try again shortly.", retrySeconds(expiry, now)}
	}
	var count int
	var oldest time.Time
	if err = tx.QueryRow(ctx, `SELECT COUNT(*),COALESCE(MIN(created),$3) FROM baseball_ip_admissions WHERE namespace=$1 AND limiter_key=$2 AND created>$4`, s.namespace, identity.LimiterKey, now, now.Add(-time.Hour)).Scan(&count, &oldest); err != nil {
		return nil, err
	}
	if count >= limit {
		return nil, &admissionFailure{fmt.Sprintf("Your network has reached its allowance of %d questions per hour. Please try again after %s UTC.", limit, oldest.Add(time.Hour).UTC().Format("15:04:05")), retrySeconds(oldest.Add(time.Hour), now)}
	}
	admission := randomToken()
	if _, err = tx.Exec(ctx, `INSERT INTO baseball_ip_admissions(namespace,id,limiter_key,created) VALUES($1,$2,$3,$4)`, s.namespace, admission, identity.LimiterKey, now); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE baseball_ip_leases SET lease=$3,expires=$4 WHERE namespace=$1 AND limiter_key=$2`, s.namespace, identity.LimiterKey, lease, now.Add(125*time.Second)); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	identity.Admission = admission
	return func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		s.pool.Exec(cleanup, `UPDATE baseball_ip_leases SET lease='',expires=$4 WHERE namespace=$1 AND limiter_key=$2 AND lease=$3`, s.namespace, identity.LimiterKey, lease, s.Now())
	}, nil
}
