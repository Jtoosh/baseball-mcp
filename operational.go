package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jtoosh/baseball-mcp/internal/data"
)

// OperationalStore owns durable hosted state. Batting data remains in SQLite.
type OperationalStore struct {
	pool      *pgxpool.Pool
	namespace string
	Now       func() time.Time
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random source unavailable")
	}
	return hex.EncodeToString(b[:])
}
func OpenOperationalStore(ctx context.Context, dsn, namespace string) (*OperationalStore, error) {
	if namespace == "" {
		return nil, errors.New("operational namespace required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid operational connection configuration")
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.ConnectTimeout = 10 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("operational connection unavailable")
	}
	store := &OperationalStore{pool: pool, namespace: namespace, Now: time.Now}
	if err = store.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}
func (s *OperationalStore) Close() { s.pool.Close() }
func (s *OperationalStore) migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(4522026)`); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS baseball_state_version (version integer PRIMARY KEY);
 INSERT INTO baseball_state_version(version) VALUES(1) ON CONFLICT DO NOTHING;
 CREATE TABLE IF NOT EXISTS baseball_confirmations(namespace text NOT NULL,token text NOT NULL,user_id text NOT NULL,question text NOT NULL,admission text NOT NULL,payload jsonb NOT NULL,expires timestamptz NOT NULL,PRIMARY KEY(namespace,token));
 CREATE INDEX IF NOT EXISTS baseball_confirmations_expiry ON baseball_confirmations(namespace,expires);
 CREATE TABLE IF NOT EXISTS baseball_users(namespace text NOT NULL,user_id text NOT NULL,lease text NOT NULL,expires timestamptz NOT NULL,PRIMARY KEY(namespace,user_id));
 CREATE TABLE IF NOT EXISTS baseball_admissions(namespace text NOT NULL,id text NOT NULL,user_id text NOT NULL,created timestamptz NOT NULL,PRIMARY KEY(namespace,id));
 CREATE INDEX IF NOT EXISTS baseball_admissions_user ON baseball_admissions(namespace,user_id,created);
 CREATE TABLE IF NOT EXISTS baseball_allowances(namespace text NOT NULL,month text NOT NULL,spent bigint NOT NULL CHECK(spent>=0 AND spent<=10000000000),PRIMARY KEY(namespace,month));
 CREATE TABLE IF NOT EXISTS baseball_spending(namespace text NOT NULL,id text NOT NULL,month text NOT NULL,amount bigint NOT NULL CHECK(amount>=0),settled boolean NOT NULL,created timestamptz NOT NULL,PRIMARY KEY(namespace,id));`)
	if err != nil {
		return err
	}
	var count, version int
	if err = tx.QueryRow(ctx, `SELECT COUNT(*),MAX(version) FROM baseball_state_version`).Scan(&count, &version); err != nil {
		return err
	}
	if count != 1 || version != 1 {
		return errors.New("unsupported operational migration version")
	}
	return tx.Commit(ctx)
}

type storedIntent struct {
	Intent  ResolvedIntent
	Players []data.PlayerCandidate
}

func (s *OperationalStore) remember(ctx context.Context, question string, intent ResolvedIntent, players []data.PlayerCandidate) (string, error) {
	identity, ok := ctx.Value(hostedContextKey{}).(*hostedIdentity)
	if !ok || identity.User == "" {
		return "", errors.New("missing hosted identity")
	}
	payload, err := json.Marshal(storedIntent{intent, players})
	if err != nil {
		return "", err
	}
	token := randomToken()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM baseball_confirmations WHERE namespace=$1 AND expires<=$2`, s.namespace, s.Now()); err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO baseball_confirmations(namespace,token,user_id,question,admission,payload,expires) VALUES($1,$2,$3,$4,$5,$6,$7)`, s.namespace, token, identity.User, question, identity.Admission, payload, s.Now().Add(15*time.Minute))
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return token, nil
}
func (s *OperationalStore) consume(ctx context.Context, req QuestionRequest) (pendingIntent, error) {
	identity, ok := ctx.Value(hostedContextKey{}).(*hostedIdentity)
	if !ok {
		return pendingIntent{}, errors.New("missing hosted identity")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return pendingIntent{}, err
	}
	defer tx.Rollback(ctx)
	var payload []byte
	var admission string
	var expiry time.Time
	err = tx.QueryRow(ctx, `SELECT payload,admission,expires FROM baseball_confirmations WHERE namespace=$1 AND token=$2 AND user_id=$3 AND question=$4 AND expires>$5 FOR UPDATE`, s.namespace, req.Confirmation, identity.User, req.Question, s.Now()).Scan(&payload, &admission, &expiry)
	if err != nil {
		return pendingIntent{}, err
	}
	var stored storedIntent
	if err = json.Unmarshal(payload, &stored); err != nil {
		return pendingIntent{}, err
	}
	pending := pendingIntent{question: req.Question, intent: stored.Intent, players: stored.Players, expires: expiry}
	if _, ok := pending.resolve(req); !ok {
		return pendingIntent{}, errors.New("player choice required")
	}
	if _, err = tx.Exec(ctx, `DELETE FROM baseball_confirmations WHERE namespace=$1 AND token=$2`, s.namespace, req.Confirmation); err != nil {
		return pendingIntent{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return pendingIntent{}, err
	}
	identity.Admission = admission
	return pending, nil
}
