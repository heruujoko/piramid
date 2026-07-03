package bootstrap

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/heruujoko/piramid/internal/api"
	"github.com/heruujoko/piramid/internal/app"
	"github.com/heruujoko/piramid/internal/config"
	"github.com/heruujoko/piramid/internal/definitions"
	"github.com/heruujoko/piramid/internal/engine"
	"github.com/heruujoko/piramid/internal/home"
	"github.com/heruujoko/piramid/internal/intake"
	"github.com/heruujoko/piramid/internal/looprunner"
	"github.com/heruujoko/piramid/internal/records"
	runtimepkg "github.com/heruujoko/piramid/internal/runtime"
	sqlitestore "github.com/heruujoko/piramid/internal/store/sqlite"
)

type Options struct {
	Paths           home.Paths
	Host            string
	Port            *int
	DefinitionsRoot string
}

type Running struct {
	Address string
	cancel  context.CancelFunc
	server  *http.Server
	store   *sqlitestore.Store
	wg      sync.WaitGroup
}

var goalSequence atomic.Uint64

func Start(ctx context.Context, options Options) (*Running, error) {
	paths := options.Paths
	var err error
	if paths.Root == "" {
		paths, err = home.Resolve()
		if err != nil {
			return nil, err
		}
	}
	if err := home.Init(paths); err != nil {
		return nil, err
	}
	cfg, err := config.Load(paths.Config)
	if err != nil {
		return nil, err
	}
	host := cfg.Server.Host
	if options.Host != "" {
		host = options.Host
	}
	port := cfg.Server.Port
	if options.Port != nil {
		port = *options.Port
	}
	st, err := sqlitestore.Open(paths.Database)
	if err != nil {
		return nil, err
	}
	if err := engine.Recover(ctx, st, engine.OSProcessInspector{}, time.Now().UTC()); err != nil {
		st.Close()
		return nil, err
	}

	recordStore := records.New(paths)
	commandAdapter := runtimepkg.NewCommandAdapter()
	intakeService := intake.NewService(intake.ServiceConfig{
		Repository: st,
		Records:    recordStore,
		Planner:    commandAdapter,
		Runtime:    intakeRuntime(cfg.Runtime.Planner),
		IDGenerator: func() string {
			return fmt.Sprintf(
				"GOAL-%s-%04d",
				time.Now().UTC().Format("20060102-150405"),
				goalSequence.Add(1),
			)
		},
	})
	runner := engine.NewRunner(engine.RunnerConfig{
		Store:           st,
		Records:         recordStore,
		Executor:        commandAdapter,
		Verifier:        commandAdapter,
		ExecutorRuntime: roleRuntime(cfg.Runtime.Executor),
		VerifierRuntime: roleRuntime(cfg.Runtime.Verifier),
		RetryDelay: func(attempt int) time.Duration {
			delay := time.Duration(cfg.Retry.InitialDelay)
			if cfg.Retry.Backoff == "exponential" {
				delay = delay << max(attempt-1, 0)
			}
			maxDelay := time.Duration(cfg.Retry.MaxDelay)
			if delay > maxDelay {
				return maxDelay
			}
			return delay
		},
	})
	supervisor := engine.NewSupervisor(runner)
	application := app.NewService(intakeService, st, recordStore, supervisor)
	dispatches := make(chan engine.Dispatch, cfg.Workers.Count)
	scheduler := engine.NewScheduler(engine.SchedulerConfig{
		Store: st, WorkerCount: cfg.Workers.Count, Dispatch: dispatches,
	})

	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		st.Close()
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	running := &Running{
		Address: listener.Addr().String(),
		cancel:  cancel,
		server:  &http.Server{Handler: api.NewServer(application), ReadHeaderTimeout: 10 * time.Second},
		store:   st,
	}
	running.wg.Add(3)
	go func() {
		defer running.wg.Done()
		supervisor.Run(runCtx, dispatches)
	}()
	go func() {
		defer running.wg.Done()
		_ = scheduler.Run(runCtx)
		close(dispatches)
	}()
	go func() {
		defer running.wg.Done()
		_ = running.server.Serve(listener)
	}()

	definitionsRoot := cfg.Loops.DefinitionRoot
	if options.DefinitionsRoot != "" {
		definitionsRoot = options.DefinitionsRoot
	}
	if definitionsRoot != "" {
		loopScheduler := looprunner.NewScheduler(looprunner.Config{
			Source:  &definitionsSource{root: definitionsRoot},
			Store:   st,
			Records: recordStore,
			OnError: func(err error) {
				log.Printf("loop scheduler error: %v", err)
			},
			Logger: &stdLogger{},
		})
		running.wg.Add(1)
		go func() {
			defer running.wg.Done()
			_ = loopScheduler.Run(runCtx)
		}()
	}

	return running, nil
}

func (r *Running) Close(ctx context.Context) error {
	r.cancel()
	serverErr := r.server.Shutdown(ctx)
	r.wg.Wait()
	storeErr := r.store.Close()
	if serverErr != nil {
		return serverErr
	}
	return storeErr
}

func (r *Running) ListenAddress() string {
	return r.Address
}

func intakeRuntime(runtime config.RuntimeConfig) intake.RuntimeConfig {
	return intake.RuntimeConfig{
		Command: runtime.Command, Args: runtime.Args,
		Environment: os.Environ(), Timeout: time.Duration(runtime.Timeout),
	}
}

func roleRuntime(runtime config.RuntimeConfig) engine.RoleRuntime {
	return engine.RoleRuntime{
		Command: runtime.Command, Args: runtime.Args,
		Environment: os.Environ(), Timeout: time.Duration(runtime.Timeout),
	}
}

type definitionsSource struct {
	root string
}

func (s *definitionsSource) Load(_ context.Context) (definitions.Snapshot, error) {
	return definitions.LoadRoot(s.root)
}

type stdLogger struct{}

func (l *stdLogger) Printf(format string, args ...any) {
	log.Printf(format, args...)
}
