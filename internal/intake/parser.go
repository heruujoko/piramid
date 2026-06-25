package intake

import (
	"fmt"
	"io"

	"github.com/heruujoko/piramid/internal/domain"
	"gopkg.in/yaml.v3"
)

func ParsePlan(reader io.Reader) (domain.Plan, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var plan domain.Plan
	if err := decoder.Decode(&plan); err != nil {
		return domain.Plan{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return domain.Plan{}, fmt.Errorf("multiple YAML documents are not allowed")
		}
		return domain.Plan{}, err
	}
	if err := domain.ValidatePlan(&plan); err != nil {
		return domain.Plan{}, err
	}
	return plan, nil
}
