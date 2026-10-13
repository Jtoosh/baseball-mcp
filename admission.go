package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *OperationalStore) admit(ctx context.Context, req QuestionRequest) (func(), error) {
	identity, ok := ctx.Value(hostedContextKey{}).(*hostedIdentity)
	if !ok || identity.User == "" {
		return nil, errors.New("missing identity")
	}
	now := s.Now().UTC()
	lease := randomToken()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO baseball_users(namespace,user_id,lease,expires) VALUES($1,$2,'',$3) ON CONFLICT DO NOTHING`, s.namespace, identity.User, now); err != nil {
		return nil, err
	}
	var existing string
	var expiry time.Time
	if err = tx.QueryRow(ctx, `SELECT lease,expires FROM baseball_users WHERE namespace=$1 AND user_id=$2 FOR UPDATE`, s.namespace, identity.User).Scan(&existing, &expiry); err != nil {
		return nil, err
	}
	if existing != "" && expiry.After(now) {
		return nil, &publicFailure{"You already have a question loading. Please wait for it to finish."}
	}
	admission := randomToken()
	if req.Confirmation != "" {
		var payload []byte
		err = tx.QueryRow(ctx, `SELECT admission,payload FROM baseball_confirmations WHERE namespace=$1 AND token=$2 AND user_id=$3 AND question=$4 AND expires>$5`, s.namespace, req.Confirmation, identity.User, strings.TrimSpace(req.Question), now).Scan(&admission, &payload)
		if err != nil {
			return nil, &publicFailure{"That confirmation has expired or does not match this question. Please ask the question again."}
		}
		var stored storedIntent
		if err = json.Unmarshal(payload, &stored); err != nil {
			return nil, err
		}
		pending := pendingIntent{intent: stored.Intent, players: stored.Players}
		if _, ok := pending.resolve(req); !ok {
			return nil, &publicFailure{"Please choose which player you mean."}
		}
	} else {
		if _, err = tx.Exec(ctx, `DELETE FROM baseball_admissions WHERE namespace=$1 AND user_id=$2 AND created<=$3`, s.namespace, identity.User, now.Add(-time.Hour)); err != nil {
			return nil, err
		}
		var count int
		var oldest time.Time
		if err = tx.QueryRow(ctx, `SELECT COUNT(*),COALESCE(MIN(created),$3) FROM baseball_admissions WHERE namespace=$1 AND user_id=$2`, s.namespace, identity.User, now).Scan(&count, &oldest); err != nil {
			return nil, err
		}
		if count >= 20 {
			return nil, &publicFailure{fmt.Sprintf("You have reached twenty questions this hour. Please try again after %s UTC.", oldest.Add(time.Hour).Format("15:04:05"))}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO baseball_admissions(namespace,id,user_id,created) VALUES($1,$2,$3,$4)`, s.namespace, admission, identity.User, now); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE baseball_users SET lease=$3,expires=$4 WHERE namespace=$1 AND user_id=$2`, s.namespace, identity.User, lease, now.Add(125*time.Second)); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	identity.Admission = admission
	return func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		s.pool.Exec(cleanup, `UPDATE baseball_users SET lease='',expires=$4 WHERE namespace=$1 AND user_id=$2 AND lease=$3`, s.namespace, identity.User, lease, s.Now())
	}, nil
}
