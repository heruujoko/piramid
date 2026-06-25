package engine

import (
	"fmt"
	"io"
	"strings"

	"github.com/heruujoko/piramid/internal/domain"
	"gopkg.in/yaml.v3"
)

func ParseVerification(reader io.Reader, retryAvailable bool) (domain.Verification, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var verification domain.Verification
	if err := decoder.Decode(&verification); err != nil {
		return domain.Verification{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return domain.Verification{}, fmt.Errorf("multiple YAML documents are not allowed")
		}
		return domain.Verification{}, err
	}
	if verification.Status != domain.VerificationPass &&
		verification.Status != domain.VerificationFail {
		return domain.Verification{}, fmt.Errorf("verification status must be PASS or FAIL")
	}
	if len(verification.Reasons) == 0 {
		return domain.Verification{}, fmt.Errorf("verification reasons are required")
	}
	for _, reason := range verification.Reasons {
		if strings.TrimSpace(reason) == "" {
			return domain.Verification{}, fmt.Errorf("verification reasons cannot be empty")
		}
	}
	if verification.Status == domain.VerificationFail && retryAvailable &&
		strings.TrimSpace(verification.RetryPrompt) == "" {
		return domain.Verification{}, fmt.Errorf("retry_prompt is required for FAIL")
	}
	if verification.Status == domain.VerificationPass &&
		strings.TrimSpace(verification.RetryPrompt) != "" {
		return domain.Verification{}, fmt.Errorf("retry_prompt must be empty for PASS")
	}
	return verification, nil
}
