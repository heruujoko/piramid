package bootstrap

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/home"
)

func TestStartListensOnlyAfterStorageIsReady(t *testing.T) {
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	port := 0
	running, err := Start(context.Background(), Options{
		Paths: paths, Host: "127.0.0.1", Port: &port,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close(context.Background())

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + running.ListenAddress() + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", response.StatusCode)
	}
	if _, err := filepath.Glob(filepath.Join(paths.Root, "state.db*")); err != nil {
		t.Fatal(err)
	}
}

func TestStartRejectsUnavailableAddress(t *testing.T) {
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	port := 0
	first, err := Start(context.Background(), Options{
		Paths: paths, Host: "127.0.0.1", Port: &port,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(context.Background())
	_, portText, err := net.SplitHostPort(first.ListenAddress())
	if err != nil {
		t.Fatal(err)
	}
	fixedPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	secondPaths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	if _, err := Start(context.Background(), Options{
		Paths: secondPaths, Host: "127.0.0.1", Port: &fixedPort,
	}); err == nil {
		t.Fatal("second Start() unexpectedly succeeded")
	}
}
