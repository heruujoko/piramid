package doctor

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/heruujoko/piramid/internal/config"
	"github.com/heruujoko/piramid/internal/home"
	_ "modernc.org/sqlite"
)

const latestSchemaVersion = 3

type Options struct {
	Paths       home.Paths
	ProjectPath string
	SmokeTest   bool
	LookPath    func(string) (string, error)
	RunCommand  func(context.Context, string, ...string) ([]byte, error)
	Dial        func(context.Context, string, string) (net.Conn, error)
}

type Doctor struct {
	options Options
}

func New(options Options) *Doctor {
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	if options.RunCommand == nil {
		options.RunCommand = func(ctx context.Context, command string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, command, args...).CombinedOutput()
		}
	}
	if options.Dial == nil {
		dialer := net.Dialer{Timeout: 500 * time.Millisecond}
		options.Dial = dialer.DialContext
	}
	return &Doctor{options: options}
}

func (d *Doctor) Run(ctx context.Context) []Result {
	checks := []Check{
		checkFunc{run: d.checkPlatform},
		checkFunc{run: d.checkHome},
		checkFunc{run: d.checkConfig},
		checkFunc{run: d.checkSQLite},
		checkFunc{run: d.checkSchema},
		checkFunc{run: d.checkNode},
		checkFunc{run: d.checkPi},
		checkFunc{run: d.checkPolicies},
		checkFunc{run: d.checkServer},
		checkFunc{run: d.checkSecurity},
	}
	if d.options.ProjectPath != "" {
		checks = append(checks, checkFunc{run: d.checkProject})
	}
	if d.options.SmokeTest {
		checks = append(checks, checkFunc{run: d.checkPiSmoke})
	}
	results := make([]Result, 0, len(checks))
	for _, check := range checks {
		results = append(results, check.Run(ctx))
	}
	return results
}

func (d *Doctor) checkPlatform(context.Context) Result {
	supportedOS := runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	supportedArch := runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	if !supportedOS || !supportedArch {
		return Result{
			Name: "platform", Status: Fail,
			Message:     runtime.GOOS + "/" + runtime.GOARCH + " is unsupported",
			Remediation: "Use Linux, macOS, or Windows on amd64 or arm64.",
		}
	}
	return Result{Name: "platform", Status: Pass, Message: runtime.GOOS + "/" + runtime.GOARCH}
}

func (d *Doctor) checkHome(context.Context) Result {
	info, err := os.Stat(d.options.Paths.Root)
	if err != nil {
		return Result{
			Name: "home", Status: Fail, Message: err.Error(),
			Remediation: "Run `piramid init`.",
		}
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return Result{
			Name: "home", Status: Fail, Message: "home is not a private directory",
			Remediation: "Set the Pi-Ramid home directory permissions to 0700.",
		}
	}
	return Result{Name: "home", Status: Pass, Message: d.options.Paths.Root}
}

func (d *Doctor) loadConfig() (config.Config, error) {
	return config.Load(d.options.Paths.Config)
}

func (d *Doctor) checkConfig(context.Context) Result {
	if _, err := d.loadConfig(); err != nil {
		return Result{
			Name: "config", Status: Fail, Message: err.Error(),
			Remediation: "Correct config.yaml without removing required runtime roles.",
		}
	}
	return Result{Name: "config", Status: Pass, Message: "configuration is valid"}
}

func (d *Doctor) openReadOnlyDatabase() (*sql.DB, error) {
	uri := (&url.URL{Scheme: "file", Path: d.options.Paths.Database}).String() + "?mode=ro&immutable=1"
	return sql.Open("sqlite", uri)
}

func (d *Doctor) checkSQLite(ctx context.Context) Result {
	db, err := d.openReadOnlyDatabase()
	if err != nil {
		return Result{Name: "sqlite", Status: Fail, Message: err.Error()}
	}
	defer db.Close()
	var version string
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		return Result{
			Name: "sqlite", Status: Fail, Message: err.Error(),
			Remediation: "Run `piramid init`, then start the engine once.",
		}
	}
	return Result{Name: "sqlite", Status: Pass, Message: "embedded SQLite " + version}
}

func (d *Doctor) checkSchema(ctx context.Context) Result {
	db, err := d.openReadOnlyDatabase()
	if err != nil {
		return Result{Name: "schema", Status: Fail, Message: err.Error()}
	}
	defer db.Close()
	var version int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return Result{
			Name: "schema", Status: Fail, Message: err.Error(),
			Remediation: "Start Pi-Ramid to apply forward-only migrations.",
		}
	}
	if version != latestSchemaVersion {
		return Result{
			Name: "schema", Status: Fail,
			Message:     fmt.Sprintf("schema version %d; binary expects %d", version, latestSchemaVersion),
			Remediation: "Start the current Pi-Ramid binary to apply migrations.",
		}
	}
	return Result{Name: "schema", Status: Pass, Message: fmt.Sprintf("version %d", version)}
}

func (d *Doctor) checkNode(ctx context.Context) Result {
	path, err := d.options.LookPath("node")
	if err != nil {
		return Result{
			Name: "nodejs", Status: Fail, Message: "Node.js was not found",
			Remediation: "Install a supported Node.js release and ensure `node` is on PATH.",
		}
	}
	output, err := d.options.RunCommand(ctx, path, "--version")
	if err != nil {
		return Result{
			Name: "nodejs", Status: Fail, Message: err.Error(),
			Remediation: "Repair the Node.js installation.",
		}
	}
	return Result{Name: "nodejs", Status: Pass, Message: strings.TrimSpace(string(output))}
}

func (d *Doctor) checkPi(context.Context) Result {
	path, err := d.options.LookPath("pi")
	if err != nil {
		return Result{
			Name: "pi", Status: Fail, Message: "Pi executable was not found",
			Remediation: "Install Pi and ensure `pi` is on PATH.",
		}
	}
	return Result{Name: "pi", Status: Pass, Message: path}
}

func (d *Doctor) checkPolicies(context.Context) Result {
	var empty []string
	for _, name := range []string{"orchestrator.md", "planner.md", "executor.md", "verifier.md"} {
		content, err := os.ReadFile(filepath.Join(d.options.Paths.Prompts, name))
		if err != nil {
			return Result{
				Name: "prompt policies", Status: Fail, Message: err.Error(),
				Remediation: "Run `piramid init` to restore missing prompt files.",
			}
		}
		if strings.TrimSpace(string(content)) == "" {
			empty = append(empty, name)
		}
	}
	if len(empty) > 0 {
		return Result{
			Name: "prompt policies", Status: Warn,
			Message:     "empty: " + strings.Join(empty, ", "),
			Remediation: "Optional: add machine-wide orchestration policies.",
		}
	}
	return Result{Name: "prompt policies", Status: Pass, Message: "all policy files contain instructions"}
}

func (d *Doctor) checkServer(ctx context.Context) Result {
	cfg, err := d.loadConfig()
	if err != nil {
		return Result{Name: "server", Status: Fail, Message: "configuration unavailable"}
	}
	address := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
	connection, err := d.options.Dial(ctx, "tcp", address)
	if err != nil {
		return Result{
			Name: "server", Status: Warn, Message: "engine is not reachable at " + address,
			Remediation: "Start it with `piramid start` if it should be running.",
		}
	}
	connection.Close()
	return Result{Name: "server", Status: Pass, Message: "reachable at " + address}
}

func (d *Doctor) checkSecurity(context.Context) Result {
	cfg, err := d.loadConfig()
	if err != nil {
		return Result{Name: "server security", Status: Fail, Message: "configuration unavailable"}
	}
	ip := net.ParseIP(cfg.Server.Host)
	if cfg.Server.Host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return Result{
			Name: "server security", Status: Warn,
			Message:     "non-loopback binding has no authentication in v1",
			Remediation: "Bind to 127.0.0.1 or restrict access with an external firewall/tunnel.",
		}
	}
	return Result{Name: "server security", Status: Pass, Message: "loopback binding"}
}

func (d *Doctor) checkProject(ctx context.Context) Result {
	info, err := os.Stat(d.options.ProjectPath)
	if err != nil || !info.IsDir() {
		return Result{
			Name: "project", Status: Fail, Message: "project directory is unavailable",
			Remediation: "Provide an existing clone directory with `--project`.",
		}
	}
	gitPath, gitErr := d.options.LookPath("git")
	ghPath, ghErr := d.options.LookPath("gh")
	if gitErr != nil || ghErr != nil {
		return Result{
			Name: "project", Status: Fail, Message: "git or gh is missing",
			Remediation: "Install Git and GitHub CLI for pull-request maintenance.",
		}
	}
	if _, err := d.options.RunCommand(ctx, gitPath, "-C", d.options.ProjectPath, "rev-parse", "--git-dir"); err != nil {
		return Result{
			Name: "project", Status: Fail, Message: "directory is not a Git repository",
			Remediation: "Use the project clone root.",
		}
	}
	return Result{Name: "project", Status: Pass, Message: "git=" + gitPath + " gh=" + ghPath}
}

func (d *Doctor) checkPiSmoke(ctx context.Context) Result {
	path, err := d.options.LookPath("pi")
	if err != nil {
		return Result{Name: "pi smoke test", Status: Fail, Message: "Pi executable was not found"}
	}
	output, err := d.options.RunCommand(
		ctx, path, "-p",
		"Read-only readiness check. Do not use tools or modify files. Respond with exactly OK.",
	)
	if err != nil {
		return Result{
			Name: "pi smoke test", Status: Fail, Message: err.Error(),
			Remediation: "Authenticate or repair Pi, then rerun with --smoke-test.",
		}
	}
	return Result{Name: "pi smoke test", Status: Pass, Message: strings.TrimSpace(string(output))}
}
