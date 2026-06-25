package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandShowsProductName(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Pi-Ramid") {
		t.Fatalf("help output did not identify Pi-Ramid: %q", out.String())
	}
}
