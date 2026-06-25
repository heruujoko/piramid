package doctor

import "context"

type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
)

type Result struct {
	Name        string
	Status      Status
	Message     string
	Remediation string
}

type Check interface {
	Run(context.Context) Result
}

type checkFunc struct {
	run func(context.Context) Result
}

func (c checkFunc) Run(ctx context.Context) Result {
	return c.run(ctx)
}
