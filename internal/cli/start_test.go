package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/heruujoko/piramid/internal/bootstrap"
)

type fakeRunningEngine struct {
	closed bool
}

func (f *fakeRunningEngine) ListenAddress() string { return "127.0.0.1:7433" }
func (f *fakeRunningEngine) Close(context.Context) error {
	f.closed = true
	return nil
}

func TestStartCommandUsesDefaultAddressInForeground(t *testing.T) {
	original := startEngine
	t.Cleanup(func() { startEngine = original })
	var captured bootstrap.Options
	engine := &fakeRunningEngine{}
	startEngine = func(_ context.Context, options bootstrap.Options) (runningEngine, error) {
		captured = options
		return engine, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := newStartCommand()
	command.SetContext(ctx)
	var output bytes.Buffer
	command.SetOut(&output)

	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if captured.Host != "127.0.0.1" || captured.Port == nil || *captured.Port != 7433 {
		t.Fatalf("options = %#v", captured)
	}
	if !engine.closed || !strings.Contains(output.String(), "127.0.0.1:7433") {
		t.Fatalf("closed=%v output=%q", engine.closed, output.String())
	}
}

func TestStartCommandWarnsForNonLoopbackBinding(t *testing.T) {
	original := startEngine
	t.Cleanup(func() { startEngine = original })
	startEngine = func(_ context.Context, _ bootstrap.Options) (runningEngine, error) {
		return &fakeRunningEngine{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := newStartCommand()
	command.SetContext(ctx)
	command.SetArgs([]string{"--s", "0.0.0.0"})
	var stderr bytes.Buffer
	command.SetErr(&stderr)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "no authentication") {
		t.Fatalf("warning = %q", stderr.String())
	}
}
