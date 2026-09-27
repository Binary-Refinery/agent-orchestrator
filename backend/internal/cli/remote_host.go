package cli

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/spf13/cobra"
)

// remoteHostStatus mirrors the small part of the loopback mobile status API
// needed to pair another client with a headless daemon.
type remoteHostStatus struct {
	Enabled   bool   `json:"enabled"`
	HostID    string `json:"hostId"`
	Password  string `json:"password"`
	Endpoints []struct {
		Host   string `json:"host"`
		Port   int    `json:"port"`
		Secure bool   `json:"secure"`
	} `json:"endpoints"`
}

func newRemoteHostCommand(ctx *commandContext) *cobra.Command {
	root := &cobra.Command{Use: "remote-host", Short: "Manage this machine's remote listener"}
	for _, action := range []struct {
		name, method, path string
	}{
		{"status", "GET", "mobile/status"},
		{"enable", "POST", "mobile/enable-lan-only"},
		{"disable", "POST", "mobile/disable"},
	} {
		root.AddCommand(&cobra.Command{
			Use: action.name, Args: noArgs,
			Short: action.name + " this machine's authenticated remote listener",
			RunE: func(cmd *cobra.Command, _ []string) error {
				var status remoteHostStatus
				err := ctx.doJSON(cmd.Context(), action.method, action.path, nil, &status)
				if err != nil {
					if errors.Is(err, errDaemonNotRunning) {
						return daemonUnavailableError{message: "AO daemon is not running — run `ao daemon` on this machine", cause: errDaemonNotRunning}
					}
					return err
				}
				if !status.Enabled {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), "Remote host disabled")
					return err
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Remote host enabled\nHost ID: %s\n", status.HostID); err != nil {
					return err
				}
				for _, endpoint := range status.Endpoints {
					scheme := "http"
					if endpoint.Secure {
						scheme = "https"
					}
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Address: %s://%s\n", scheme, net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))); err != nil {
						return err
					}
				}
				if status.Password != "" {
					_, err := fmt.Fprintf(cmd.OutOrStdout(), "Password: %s\n", status.Password)
					return err
				}
				return nil
			},
		})
	}
	return root
}
