package cli

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/heruujoko/piramid/internal/api"
	"github.com/spf13/cobra"
)

type clientFlags struct {
	host string
	port int
}

func (f *clientFlags) bind(command *cobra.Command) {
	command.Flags().StringVar(&f.host, "s", "127.0.0.1", "server host")
	command.Flags().IntVar(&f.port, "p", 7433, "server port")
}

func (f clientFlags) client() (*api.Client, error) {
	if f.host == "" {
		return nil, fmt.Errorf("server host is required")
	}
	if f.port < 1 || f.port > 65535 {
		return nil, fmt.Errorf("server port must be between 1 and 65535")
	}
	address := "http://" + net.JoinHostPort(f.host, strconv.Itoa(f.port))
	return api.NewClient(address, 30*time.Second), nil
}
