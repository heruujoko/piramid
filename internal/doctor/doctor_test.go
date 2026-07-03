package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/home"
	sqlitestore "github.com/heruujoko/piramid/internal/store/sqlite"
)

func initializedDoctorHome(t *testing.T) home.Paths {
	t.Helper()
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	if err := home.Init(paths); err != nil {
		t.Fatal(err)
	}
	st, err := sqlitestore.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return paths
}

func resultByName(results []Result, name string) Result {
	for _, result := range results {
		if result.Name == name {
			return result
		}
	}
	return Result{}
}

func TestDoctorAcceptsCurrentSchemaVersion(t *testing.T) {
	paths := initializedDoctorHome(t)
	d := New(Options{Paths: paths})
	result := d.checkSchema(context.Background())
	if result.Status != Pass {
		t.Fatalf("schema result = %#v, want pass", result)
	}
}

func TestDoctorReportsMissingNodeAndPiWithRemediation(t *testing.T) {
	paths := initializedDoctorHome(t)
	d := New(Options{
		Paths: paths,
		LookPath: func(string) (string, error) {
			return "", errors.New("missing")
		},
	})
	results := d.Run(context.Background())
	for _, name := range []string{"nodejs", "pi"} {
		result := resultByName(results, name)
		if result.Status != Fail || result.Remediation == "" {
			t.Fatalf("%s result = %#v", name, result)
		}
	}
}

func TestDoctorWarnsForEmptyPoliciesAndNonLoopbackAddress(t *testing.T) {
	paths := initializedDoctorHome(t)
	content, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "127.0.0.1", "0.0.0.0", 1))
	if err := os.WriteFile(paths.Config, content, 0o600); err != nil {
		t.Fatal(err)
	}
	d := New(Options{
		Paths:    paths,
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		RunCommand: func(context.Context, string, ...string) ([]byte, error) {
			return []byte("v22.0.0"), nil
		},
	})
	results := d.Run(context.Background())
	if resultByName(results, "prompt policies").Status != Warn {
		t.Fatalf("prompt result = %#v", resultByName(results, "prompt policies"))
	}
	if resultByName(results, "server security").Status != Warn {
		t.Fatalf("security result = %#v", resultByName(results, "server security"))
	}
}

func TestDoctorDefaultDoesNotInvokePi(t *testing.T) {
	paths := initializedDoctorHome(t)
	var piInvocations int
	d := New(Options{
		Paths:    paths,
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		RunCommand: func(_ context.Context, command string, _ ...string) ([]byte, error) {
			if strings.Contains(command, "pi") {
				piInvocations++
			}
			return []byte("v22.0.0"), nil
		},
	})
	_ = d.Run(context.Background())
	if piInvocations != 0 {
		t.Fatalf("Pi invocations = %d", piInvocations)
	}
}

func TestDoctorSmokeTestInvokesPi(t *testing.T) {
	paths := initializedDoctorHome(t)
	var piInvocations int
	d := New(Options{
		Paths: paths, SmokeTest: true,
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		RunCommand: func(_ context.Context, command string, _ ...string) ([]byte, error) {
			if strings.HasSuffix(command, "/pi") {
				piInvocations++
			}
			return []byte("ok"), nil
		},
	})
	results := d.Run(context.Background())
	if piInvocations != 1 || resultByName(results, "pi smoke test").Status != Pass {
		t.Fatalf("invocations=%d result=%#v", piInvocations, resultByName(results, "pi smoke test"))
	}
}

func TestDoctorDoesNotCreateMissingHome(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	paths := home.NewPaths(root)
	d := New(Options{Paths: paths})
	results := d.Run(context.Background())
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("doctor created home: %v", err)
	}
	if resultByName(results, "home").Status != Fail {
		t.Fatalf("home result = %#v", resultByName(results, "home"))
	}
}

func TestDoctorDoesNotModifyCheckedFiles(t *testing.T) {
	paths := initializedDoctorHome(t)
	before := snapshotFiles(t, paths.Root)
	time.Sleep(10 * time.Millisecond)
	d := New(Options{
		Paths:    paths,
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		RunCommand: func(context.Context, string, ...string) ([]byte, error) {
			return []byte("v22"), nil
		},
	})
	_ = d.Run(context.Background())
	after := snapshotFiles(t, paths.Root)
	if len(before) != len(after) {
		t.Fatalf("file count changed: before=%d after=%d", len(before), len(after))
	}
	for path, state := range before {
		if got := after[path]; got != state {
			t.Fatalf("%s changed: before=%q after=%q", path, state, got)
		}
	}
}

func snapshotFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[path] = info.ModTime().UTC().Format(time.RFC3339Nano) + ":" + string(content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
