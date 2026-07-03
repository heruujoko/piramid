package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/heruujoko/piramid/internal/bootstrap"
	"github.com/heruujoko/piramid/internal/daemon"
	"github.com/heruujoko/piramid/internal/home"
	"github.com/spf13/cobra"
)

type runningEngine interface {
	ListenAddress() string
	Close(context.Context) error
}

var startEngine = func(ctx context.Context, options bootstrap.Options) (runningEngine, error) {
	return bootstrap.Start(ctx, options)
}

var launchDaemon = daemon.Launch
var executablePath = os.Executable

func newStartCommand() *cobra.Command {
	var (
		background      bool
		host            string
		port            int
		definitionsRoot string
		internalDaemon  bool
	)
	command := &cobra.Command{
		Use:   "start",
		Short: "Start the Pi-Ramid orchestration engine",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isLoopbackHost(host) {
				_, _ = fmt.Fprintf(
					cmd.ErrOrStderr(),
					"WARNING: Pi-Ramid v1 has no authentication; binding to %s exposes control access.\n",
					host,
				)
			}
			if background && !internalDaemon {
				return launchBackground(cmd, host, port, definitionsRoot)
			}
			portCopy := port
			running, err := startEngine(cmd.Context(), bootstrap.Options{
				Host: host, Port: &portCopy, DefinitionsRoot: definitionsRoot,
			})
			if err != nil {
				return err
			}
			paths, err := home.Resolve()
			if err != nil {
				_ = running.Close(context.Background())
				return err
			}
			if internalDaemon {
				if err := os.WriteFile(
					filepath.Join(paths.Runtime, "piramid.pid"),
					[]byte(strconv.Itoa(os.Getpid())+"\n"),
					0o600,
				); err != nil {
					_ = running.Close(context.Background())
					return err
				}
				defer os.Remove(filepath.Join(paths.Runtime, "piramid.pid"))
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), running.ListenAddress())
			<-cmd.Context().Done()
			closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return running.Close(closeCtx)
		},
	}
	command.Flags().BoolVar(&background, "d", false, "run in the background")
	command.Flags().StringVar(&host, "s", "127.0.0.1", "server host")
	command.Flags().IntVar(&port, "p", 7433, "server port")
	command.Flags().StringVar(&definitionsRoot, "definitions", "", "loop definitions root directory")
	command.Flags().BoolVar(&internalDaemon, "internal-daemon", false, "internal daemon child")
	_ = command.Flags().MarkHidden("internal-daemon")
	return command
}

func launchBackground(cmd *cobra.Command, host string, port int, definitionsRoot string) error {
	paths, err := home.Resolve()
	if err != nil {
		return err
	}
	if err := home.Init(paths); err != nil {
		return err
	}
	executable, err := executablePath()
	if err != nil {
		return err
	}
	dialHost := host
	if !isLoopbackHost(host) {
		dialHost = "127.0.0.1"
	}
	pid, err := launchDaemon(daemon.LaunchOptions{
		Executable: executable,
		Args: func() []string {
			args := []string{
				"start", "--s", host, "--p", strconv.Itoa(port), "--internal-daemon",
			}
			if definitionsRoot != "" {
				args = append(args, "--definitions", definitionsRoot)
			}
			return args
		}(),
		StdoutPath: filepath.Join(paths.Runtime, "daemon.stdout.log"),
		StderrPath: filepath.Join(paths.Runtime, "daemon.stderr.log"),
		Host:       dialHost,
		Port:       port,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Pi-Ramid daemon started with PID %d\n", pid)
	return err
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
