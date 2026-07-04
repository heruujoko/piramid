package webhook

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/heruujoko/piramid/internal/domain"
)

func TestVerifySignature_HMACMatches(t *testing.T) {
	secret := "test-secret"
	payload := []byte(`{"action":"opened"}`)
	sig := hashHMAC(secret, payload)

	err := VerifySignature(secret, payload, sig)
	if err != nil {
		t.Fatalf("expected valid signature, got: %v", err)
	}
}

func TestVerifySignature_HMACMismatch(t *testing.T) {
	secret := "test-secret"
	payload := []byte(`{"action":"opened"}`)
	sig := hashHMAC("wrong-secret", payload)

	err := VerifySignature(secret, payload, sig)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestVerifySignature_EmptySecretSkips(t *testing.T) {
	err := VerifySignature("", []byte(`{}`), "anything")
	if err != nil {
		t.Fatalf("expected nil for empty secret, got: %v", err)
	}
}

func TestVerifySignature_MissingSignature(t *testing.T) {
	err := VerifySignature("secret", []byte(`{}`), "")
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestBuildEventKey_PR_Opened(t *testing.T) {
	key := BuildEventKey("pull_request", Payload{Action: "opened"})
	if key != "pull_request.opened" {
		t.Fatalf("expected pull_request.opened, got %q", key)
	}
}

func TestBuildEventKey_PR_Synchronize(t *testing.T) {
	key := BuildEventKey("pull_request", Payload{Action: "synchronize"})
	if key != "pull_request.synchronize" {
		t.Fatalf("expected pull_request.synchronize, got %q", key)
	}
}

func TestBuildEventKey_PR_Closed_Merged(t *testing.T) {
	key := BuildEventKey("pull_request", Payload{
		Action: "closed",
		PullRequest: &struct {
			Merged bool `json:"merged"`
		}{Merged: true},
	})
	if key != "pull_request.closed.merged" {
		t.Fatalf("expected pull_request.closed.merged, got %q", key)
	}
}

func TestBuildEventKey_PR_Closed_Unmerged(t *testing.T) {
	key := BuildEventKey("pull_request", Payload{
		Action: "closed",
		PullRequest: &struct {
			Merged bool `json:"merged"`
		}{Merged: false},
	})
	if key != "pull_request.closed.unmerged" {
		t.Fatalf("expected pull_request.closed.unmerged, got %q", key)
	}
}

func TestBuildEventKey_NonPR(t *testing.T) {
	key := BuildEventKey("push", Payload{})
	if key != "push" {
		t.Fatalf("expected push, got %q", key)
	}
}

func TestMatchLoops_Matches(t *testing.T) {
	loops := []domain.Loop{
		{
			ID:     "pr-review",
			Active: true,
			Triggers: domain.Triggers{
				GitHub: &domain.GitHubTrigger{
					Repos:  []string{"owner/repo"},
					Events: []string{"pull_request.opened", "pull_request.synchronize"},
				},
			},
		},
	}
	matched := MatchLoops("pull_request.opened", "owner/repo", loops)
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
}

func TestMatchLoops_NoMatch_Repo(t *testing.T) {
	loops := []domain.Loop{
		{
			ID:     "pr-review",
			Active: true,
			Triggers: domain.Triggers{
				GitHub: &domain.GitHubTrigger{
					Repos:  []string{"owner/repo"},
					Events: []string{"pull_request.opened"},
				},
			},
		},
	}
	matched := MatchLoops("pull_request.opened", "other/repo", loops)
	if len(matched) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matched))
	}
}

func TestMatchLoops_NoMatch_Event(t *testing.T) {
	loops := []domain.Loop{
		{
			ID:     "pr-review",
			Active: true,
			Triggers: domain.Triggers{
				GitHub: &domain.GitHubTrigger{
					Repos:  []string{"owner/repo"},
					Events: []string{"pull_request.opened"},
				},
			},
		},
	}
	matched := MatchLoops("pull_request.closed.merged", "owner/repo", loops)
	if len(matched) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matched))
	}
}

func TestMatchLoops_InactiveLoop(t *testing.T) {
	loops := []domain.Loop{
		{
			ID:     "disabled-loop",
			Active: false,
			Triggers: domain.Triggers{
				GitHub: &domain.GitHubTrigger{
					Repos:  []string{"owner/repo"},
					Events: []string{"pull_request.opened"},
				},
			},
		},
	}
	matched := MatchLoops("pull_request.opened", "owner/repo", loops)
	if len(matched) != 0 {
		t.Fatalf("expected 0 matches for inactive loop, got %d", len(matched))
	}
}

func TestMatchLoops_NoTrigger(t *testing.T) {
	loops := []domain.Loop{
		{ID: "no-trigger", Active: true},
	}
	matched := MatchLoops("pull_request.opened", "owner/repo", loops)
	if len(matched) != 0 {
		t.Fatalf("expected 0 matches for loop without trigger, got %d", len(matched))
	}
}

func TestParseEvent_Valid(t *testing.T) {
	payload := json.RawMessage(`{
		"action": "opened",
		"repository": {"full_name": "owner/repo"}
	}`)
	p, err := ParseEvent(payload)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if p.Repository.FullName != "owner/repo" {
		t.Fatalf("expected owner/repo, got %q", p.Repository.FullName)
	}
}

func TestParseEvent_MissingRepo(t *testing.T) {
	_, err := ParseEvent([]byte(`{"action":"opened"}`))
	if err == nil {
		t.Fatal("expected error for missing repository")
	}
	if !strings.Contains(err.Error(), "repository.full_name") {
		t.Fatalf("expected repository.full_name error, got: %v", err)
	}
}
