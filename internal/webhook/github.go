package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/heruujoko/piramid/internal/domain"
)

// Common errors.
var (
	ErrInvalidSignature = errors.New("invalid webhook signature")
	ErrInvalidPayload   = errors.New("invalid webhook payload")
)

// Payload is the minimal GitHub webhook payload needed for event matching.
type Payload struct {
	Action      string `json:"action"`
	PullRequest *struct {
		Merged bool `json:"merged"`
	} `json:"pull_request,omitempty"`
	Repository *struct {
		FullName string `json:"full_name"`
	} `json:"repository,omitempty"`
}

// VerifySignature checks the HMAC-SHA256 signature.
func VerifySignature(secret string, payload []byte, signature string) error {
	if secret == "" {
		return nil // no secret configured, skip verification
	}
	if signature == "" {
		return fmt.Errorf("%w: missing signature header", ErrInvalidSignature)
	}
	expected := hashHMAC(secret, payload)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return fmt.Errorf("%w: signature mismatch", ErrInvalidSignature)
	}
	return nil
}

const signaturePrefix = "sha256="

func hashHMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// BuildEventKey constructs a canonical event key from the GitHub event type
// and the parsed payload. For pull_request events the action is appended,
// and for closed PRs the merged status is distinguished.
func BuildEventKey(eventType string, payload Payload) string {
	if eventType != "pull_request" {
		return eventType
	}
	if payload.Action == "closed" {
		if payload.PullRequest != nil && payload.PullRequest.Merged {
			return "pull_request.closed.merged"
		}
		return "pull_request.closed.unmerged"
	}
	return "pull_request." + payload.Action
}

// MatchResult holds the loops that matched a webhook event.
type MatchResult struct {
	EventKey string         `json:"event_key"`
	Repo     string         `json:"repo"`
	Loops    []domain.Loop  `json:"loops"`
}

// MatchLoops finds loops whose triggers.github whitelist matches the given
// event key and repository. Returns loops that should fire.
func MatchLoops(eventKey string, repo string, loops []domain.Loop) []domain.Loop {
	var matched []domain.Loop
	for _, loop := range loops {
		if !loop.Active {
			continue
		}
		if loop.Triggers.GitHub == nil {
			continue
		}
		trigger := loop.Triggers.GitHub
		if !repoMatches(trigger.Repos, repo) {
			continue
		}
		if !eventMatches(trigger.Events, eventKey) {
			continue
		}
		matched = append(matched, loop)
	}
	return matched
}

// ParseEvent parses the minimal GitHub webhook JSON payload.
func ParseEvent(payload []byte) (Payload, error) {
	var p Payload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Payload{}, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	if p.Repository == nil || p.Repository.FullName == "" {
		return Payload{}, fmt.Errorf("%w: repository.full_name is required", ErrInvalidPayload)
	}
	return p, nil
}

func repoMatches(repos []string, repo string) bool {
	for _, r := range repos {
		if strings.EqualFold(r, repo) {
			return true
		}
	}
	return false
}

func eventMatches(events []string, eventKey string) bool {
	for _, e := range events {
		if e == eventKey {
			return true
		}
	}
	return false
}
