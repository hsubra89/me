package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

type personalServerProvisioner interface {
	Configure(out io.Writer, appConfigPath string, cfg appConfig, prompter configurePrompter) error
}

type personalServerProvisionerFunc func(io.Writer, string, appConfig, configurePrompter) error

func (fn personalServerProvisionerFunc) Configure(out io.Writer, appConfigPath string, cfg appConfig, prompter configurePrompter) error {
	return fn(out, appConfigPath, cfg, prompter)
}

type personalServerCloudClient interface {
	ServerByID(ctx context.Context, id int) (personalServerCloudServer, bool, error)
}

type personalServerCloudServer struct {
	ID   int
	IPv4 string
	IPv6 string
}

type personalServerProvisioningGate struct {
	newCloudClient func(token string) personalServerCloudClient
	saveConfig     func(path string, cfg appConfig) error
}

func (gate personalServerProvisioningGate) Configure(out io.Writer, appConfigPath string, cfg appConfig, prompter configurePrompter) error {
	if strings.TrimSpace(cfg.SSH.IdentityFile) == "" {
		fmt.Fprintln(out, "Personal Server creation skipped: SSH identity is not configured.")
		return nil
	}
	token := strings.TrimSpace(cfg.Auth.Hetzner.Token)
	if token == "" {
		fmt.Fprintln(out, "Personal Server creation skipped: Hetzner Credentials are not configured. Run `me auth hetzner` first.")
		return nil
	}

	if cfg.PersonalServer.ServerID != 0 {
		return gate.verifyConfiguredPersonalServer(out, appConfigPath, cfg, token, prompter)
	}

	if !prompter.CanPrompt() {
		fmt.Fprintln(out, "Personal Server creation skipped: configure is running non-interactively.")
		return nil
	}

	fmt.Fprintln(out, "Personal Server provisioning prerequisites are ready.")
	return nil
}

func (gate personalServerProvisioningGate) verifyConfiguredPersonalServer(out io.Writer, appConfigPath string, cfg appConfig, token string, prompter configurePrompter) error {
	client := gate.cloudClient(token)
	server, found, err := client.ServerByID(context.Background(), cfg.PersonalServer.ServerID)
	if err != nil {
		return fmt.Errorf("verify Personal Server %d in Hetzner: %w", cfg.PersonalServer.ServerID, err)
	}
	if !found {
		fmt.Fprintf(out, "Personal Server Configuration references missing server %d.\n", cfg.PersonalServer.ServerID)
		if !prompter.CanPrompt() {
			return fmt.Errorf("Personal Server Configuration references missing server %d; rerun `me configure` interactively to clear it", cfg.PersonalServer.ServerID)
		}

		clear, err := prompter.Confirm(fmt.Sprintf("Clear stale Personal Server Configuration for missing server %d?", cfg.PersonalServer.ServerID), true)
		if err != nil {
			return err
		}
		if !clear {
			return fmt.Errorf("Personal Server Configuration still references missing server %d", cfg.PersonalServer.ServerID)
		}

		cfg.PersonalServer = personalServerConfig{}
		if err := gate.writeConfig(appConfigPath, cfg); err != nil {
			return err
		}
		fmt.Fprintln(out, "Cleared stale Personal Server Configuration.")
		fmt.Fprintln(out, "Personal Server provisioning prerequisites are ready.")
		return nil
	}

	fmt.Fprintf(out, "Personal Server already configured: server %d exists.\n", cfg.PersonalServer.ServerID)
	fmt.Fprintf(out, "Saved addresses: %s\n", formatPersonalServerAddresses(cfg.PersonalServer.IPv4, cfg.PersonalServer.IPv6))
	fmt.Fprintf(out, "Current addresses: %s\n", formatPersonalServerAddresses(server.IPv4, server.IPv6))
	return nil
}

func (gate personalServerProvisioningGate) writeConfig(path string, cfg appConfig) error {
	if gate.saveConfig != nil {
		return gate.saveConfig(path, cfg)
	}
	return saveAppConfig(path, cfg)
}

func (gate personalServerProvisioningGate) cloudClient(token string) personalServerCloudClient {
	if gate.newCloudClient != nil {
		return gate.newCloudClient(token)
	}
	return newHcloudPersonalServerCloudClient(token, os.Getenv("HCLOUD_ENDPOINT"))
}

func formatPersonalServerAddresses(ipv4 string, ipv6 string) string {
	return fmt.Sprintf("IPv4 %s, IPv6 %s", displayPersonalServerAddress(ipv4), displayPersonalServerAddress(ipv6))
}

func displayPersonalServerAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unavailable"
	}
	return value
}

type hcloudPersonalServerCloudClient struct {
	client *hcloud.Client
}

func newHcloudPersonalServerCloudClient(token string, endpoint string) hcloudPersonalServerCloudClient {
	options := []hcloud.ClientOption{hcloud.WithToken(token)}
	if strings.TrimSpace(endpoint) != "" {
		options = append(options, hcloud.WithEndpoint(endpoint))
	}
	return hcloudPersonalServerCloudClient{
		client: hcloud.NewClient(options...),
	}
}

func (client hcloudPersonalServerCloudClient) ServerByID(ctx context.Context, id int) (personalServerCloudServer, bool, error) {
	server, _, err := client.client.Server.GetByID(ctx, int64(id))
	if err != nil {
		return personalServerCloudServer{}, false, err
	}
	if server == nil {
		return personalServerCloudServer{}, false, nil
	}

	result := personalServerCloudServer{
		ID: int(server.ID),
	}
	if !server.PublicNet.IPv4.IsUnspecified() {
		result.IPv4 = server.PublicNet.IPv4.IP.String()
	}
	if !server.PublicNet.IPv6.IsUnspecified() {
		result.IPv6 = server.PublicNet.IPv6.IP.String()
	}
	return result, true, nil
}
