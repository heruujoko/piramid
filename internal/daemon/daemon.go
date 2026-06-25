package daemon

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

type LaunchOptions struct {
	Executable string
	Args       []string
	StdoutPath string
	StderrPath string
	Host       string
	Port       int
	Timeout    time.Duration
}

func ChildArgs(original []string) []string {
	child := make([]string, 0, len(original)+1)
	for _, argument := range original {
		if argument == "--d" || argument == "-d" || argument == "--daemon" {
			continue
		}
		child = append(child, argument)
	}
	child = append(child, "--internal-daemon")
	return child
}

func Launch(options LaunchOptions) (int, error) {
	stdout, err := os.OpenFile(options.StdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(options.StderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer stderr.Close()
	process, err := os.StartProcess(options.Executable, append(
		[]string{options.Executable}, options.Args...,
	), &os.ProcAttr{
		Files: []*os.File{os.Stdin, stdout, stderr},
		Env:   os.Environ(),
	})
	if err != nil {
		return 0, err
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	address := net.JoinHostPort(options.Host, strconv.Itoa(options.Port))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connection, dialErr := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if dialErr == nil {
			connection.Close()
			return process.Pid, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = process.Kill()
	_, _ = process.Wait()
	return 0, fmt.Errorf("daemon did not become ready at %s", address)
}
