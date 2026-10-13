package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jtoosh/baseball-mcp/internal/data"
)

// ResolvedIntent supplies one server-owned interpretation to strategy attempts.
// Private player identity never appears in the public response.
type ResolvedIntent struct {
	Interpretation string
	Revision       string
	Seasons        []int
	PlayerID       int64
}
type pendingIntent struct {
	question string
	intent   ResolvedIntent
	players  []data.PlayerCandidate
	expires  time.Time
}
type pendingClarifications struct {
	sync.Mutex
	items map[string]pendingIntent
}

func (q *QueryService) prepareQuestion(ctx context.Context, req QuestionRequest) (QuestionRequest, ResolvedIntent, *Outcome) {
	req.Question = strings.TrimSpace(req.Question)
	var intent ResolvedIntent
	var previousPlayers []data.PlayerCandidate
	bad := func(message string) (QuestionRequest, ResolvedIntent, *Outcome) {
		o := Outcome{Kind: "error", Message: message}
		return req, intent, &o
	}
	if req.Interpretation != "" || len(req.Revision) > 4000 || (req.Confirmation == "" && (req.Revision != "" || req.Choice != "")) {
		return bad("Please confirm the proposed interpretation or revise it using its confirmation.")
	}
	if req.Confirmation != "" {
		q.pending.Lock()
		p, ok := q.pending.items[req.Confirmation]
		if !ok || time.Now().After(p.expires) || p.question != req.Question {
			q.pending.Unlock()
			return bad("That confirmation has expired or does not match this question. Please ask the question again.")
		}
		if len(p.players) > 0 && req.Revision == "" {
			for _, player := range p.players {
				if req.Choice == player.Description {
					intent = p.intent
					intent.PlayerID = player.ID
					intent.Interpretation = player.Description
					break
				}
			}
			if intent.PlayerID == 0 {
				q.pending.Unlock()
				return bad("Please choose which player you mean.")
			}
		} else {
			intent = p.intent
		}
		previousPlayers = p.players
		delete(q.pending.items, req.Confirmation)
		q.pending.Unlock()
		if strings.TrimSpace(req.Revision) != "" {
			// A revision is interpreted anew; it cannot smuggle a generated query or
			// inherit a previously selected person without that person's distinction.
			intent = ResolvedIntent{Revision: strings.TrimSpace(req.Revision)}
		} else {
			req.Interpretation = intent.Interpretation
			return req, intent, nil
		}
	}
	identityQuestion := req.Question
	if intent.Revision != "" {
		identityQuestion = intent.Revision
	}
	candidates, err := q.DB.AmbiguousPlayers(ctx, identityQuestion)
	if err != nil {
		o := q.failure("identify player", err)
		return req, intent, &o
	}
	if len(candidates) == 0 && len(previousPlayers) > 1 {
		namedPlayer, err := q.DB.MentionsPlayer(ctx, identityQuestion)
		if err != nil {
			o := q.failure("identify revised player", err)
			return req, intent, &o
		}
		if !namedPlayer {
			candidates = previousPlayers
		}
	}
	if len(candidates) > 1 {
		matches := map[int64]data.PlayerCandidate{}
		for _, candidate := range candidates {
			for _, season := range candidate.Seasons {
				agePattern := regexp.MustCompile(fmt.Sprintf(`(?i)\bage\s+%d\b|\b%d[- ]year[- ]old\b`, season.Age, season.Age))
				if agePattern.MatchString(identityQuestion) {
					matches[candidate.ID] = candidate
				}
			}
		}
		if len(matches) == 1 {
			for _, selected := range matches {
				intent.PlayerID = selected.ID
			}
			return req, intent, nil
		}

		o := q.remember(req.Question, intent, candidates)
		return req, intent, &o
	}
	return req, intent, nil
}
func (q *QueryService) remember(question string, intent ResolvedIntent, players []data.PlayerCandidate) Outcome {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return q.failure("create confirmation", err)
	}
	token := hex.EncodeToString(tokenBytes)
	q.pending.Lock()
	defer q.pending.Unlock()
	if q.pending.items == nil {
		q.pending.items = make(map[string]pendingIntent)
	}
	now := time.Now()
	for k, p := range q.pending.items {
		if now.After(p.expires) {
			delete(q.pending.items, k)
		}
	}
	if len(q.pending.items) >= 256 {
		var oldest string
		var expiry time.Time
		for k, p := range q.pending.items {
			if oldest == "" || p.expires.Before(expiry) {
				oldest = k
				expiry = p.expires
			}
		}
		delete(q.pending.items, oldest)
	}
	q.pending.items[token] = pendingIntent{question, intent, players, now.Add(15 * time.Minute)}
	out := Outcome{Kind: "clarification", Message: intent.Interpretation, Seasons: intent.Seasons, Confirmation: token}
	if len(players) > 0 {
		out.Message = "There is more than one player named " + players[0].Name + ". Which one do you mean?"
		for _, p := range players {
			out.Choices = append(out.Choices, p.Description)
		}
	}
	return out
}
