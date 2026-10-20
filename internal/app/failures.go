package app

import (
	"context"
	"errors"
)

var errProviderUnavailable = errors.New("provider unavailable")

func friendlyFailure(err error) string {
	if errors.Is(err, errProviderUnavailable) {
		return "Questions are temporarily unavailable. Please try again later."
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "This question took too long. Please ask a narrower question."
	}
	return questionFailureMessage
}
