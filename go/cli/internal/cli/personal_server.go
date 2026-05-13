package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GehirnInc/crypt/sha512_crypt"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

type personalServerProvisioner interface {
	Configure(ctx context.Context, out io.Writer, appConfigPath string, cfg appConfig, prompter configurePrompter) error
}

type personalServerProvisionerFunc func(context.Context, io.Writer, string, appConfig, configurePrompter) error

func (fn personalServerProvisionerFunc) Configure(ctx context.Context, out io.Writer, appConfigPath string, cfg appConfig, prompter configurePrompter) error {
	return fn(ctx, out, appConfigPath, cfg, prompter)
}

type personalServerCloudClient interface {
	ServerByID(ctx context.Context, id int) (personalServerCloudServer, bool, error)
}

type personalServerPreviewCloudClient interface {
	personalServerCloudClient
	Locations(ctx context.Context) ([]personalServerLocation, error)
	ServerTypes(ctx context.Context) ([]personalServerType, error)
	Pricing(ctx context.Context) (personalServerPricing, error)
}

type personalServerCreateCloudClient interface {
	personalServerPreviewCloudClient
	ServerByName(ctx context.Context, name string) (personalServerCloudServer, bool, error)
	Images(ctx context.Context) ([]personalServerImage, error)
	FirewallByName(ctx context.Context, name string) (personalServerFirewall, bool, error)
	CreateFirewall(ctx context.Context, firewall personalServerFirewall) (personalServerFirewall, []personalServerAction, error)
	SSHKeyByFingerprint(ctx context.Context, fingerprint string) (personalServerSSHKey, bool, error)
	CreateSSHKey(ctx context.Context, sshKey personalServerSSHKey) (personalServerSSHKey, error)
	CreateServer(ctx context.Context, request personalServerCreateServerRequest) (personalServerCloudServer, []personalServerAction, error)
	WaitActions(ctx context.Context, actions []personalServerAction) error
}

type personalServerCloudServer struct {
	ID   int
	Name string
	IPv4 string
	IPv6 string
}

type personalServerImage struct {
	ID           int
	Name         string
	Type         string
	Status       string
	OSFlavor     string
	OSVersion    string
	Architecture string
	Deprecated   bool
}

type personalServerFirewall struct {
	ID     int
	Name   string
	Labels map[string]string
	Rules  []personalServerFirewallRule
}

type personalServerFirewallRule struct {
	Direction string
	Protocol  string
	Port      string
	SourceIPs []string
}

type personalServerSSHKey struct {
	ID          int
	Name        string
	Fingerprint string
	PublicKey   string
	Labels      map[string]string
}

type personalServerAction struct {
	ID     int
	Status string
}

type personalServerCreateServerRequest struct {
	Name           string
	LocationName   string
	ServerTypeName string
	ImageID        int
	ImageName      string
	SSHKeyID       int
	FirewallID     int
	UserData       string
	Labels         map[string]string
	EnableIPv4     bool
	EnableIPv6     bool
}

type personalServerLocation struct {
	Name        string
	Description string
	City        string
	Country     string
}

type personalServerLocationChoice struct {
	Label    string
	Location personalServerLocation
}

type personalServerType struct {
	Name         string
	CPUType      string
	Architecture string
	Deprecated   bool
	Cores        int
	MemoryGB     float64
	DiskGB       int
	StorageType  string
	Locations    []personalServerTypeLocation
	Pricings     []personalServerTypeLocationPricing
}

type personalServerTypeLocation struct {
	LocationName string
	Available    bool
	Deprecated   bool
}

type personalServerTypeLocationPricing struct {
	LocationName    string
	MonthlyGrossEUR string
}

type personalServerPricing struct {
	PrimaryIPs []personalServerPrimaryIPPricing
}

type personalServerPrimaryIPPricing struct {
	Type     string
	Pricings []personalServerPrimaryIPLocationPricing
}

type personalServerPrimaryIPLocationPricing struct {
	LocationName    string
	MonthlyGrossEUR string
}

type personalServerTypeChoice struct {
	Label      string
	ServerType personalServerType
}

type personalServerCreationInputs struct {
	User         string
	ServerName   string
	PasswordHash string
	GitIdentity  personalServerGitIdentity
}

type personalServerCreationPlan struct {
	Location                   personalServerLocation
	ServerType                 personalServerType
	User                       string
	ServerName                 string
	PasswordHash               string
	GitIdentity                personalServerGitIdentity
	RemoteProjectRoot          string
	SSHIdentityFile            string
	ExistingFirewall           bool
	PrimaryIPv4MonthlyGrossEUR string
}

type personalServerGitIdentity struct {
	Name  string
	Email string
}

type personalServerGitConfigScope string

const (
	personalServerGitConfigGlobal personalServerGitConfigScope = "global"
	personalServerGitConfigLocal  personalServerGitConfigScope = "local"
)

type personalServerProvisioningGate struct {
	newCloudClient     func(token string) personalServerCloudClient
	saveConfig         func(path string, cfg appConfig) error
	runSSH             personalServerSSHRunner
	sleep              func(context.Context, time.Duration) error
	bootstrapTimeout   time.Duration
	sshPollInterval    time.Duration
	userHomeDir        func() (string, error)
	stat               func(string) (os.FileInfo, error)
	readFile           func(string) ([]byte, error)
	writeFile          func(string, []byte, os.FileMode) error
	chmod              func(string, os.FileMode) error
	sshPublicKey       func(string) (string, error)
	currentUsername    func() string
	gitConfigValue     func(scope personalServerGitConfigScope, key string) (string, bool)
	passwordSaltReader io.Reader
}

func (gate personalServerProvisioningGate) Configure(ctx context.Context, out io.Writer, appConfigPath string, cfg appConfig, prompter configurePrompter) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(cfg.SSH.IdentityFile) == "" {
		fmt.Fprintln(out, "Personal Server creation skipped: SSH identity is not configured.")
		return nil
	}
	token := strings.TrimSpace(cfg.Auth.Hetzner.Token)
	if token == "" {
		fmt.Fprintln(out, "Personal Server creation skipped: Hetzner Credentials are not configured. Run `myn auth hetzner` first.")
		return nil
	}

	if cfg.PersonalServer.ServerID != 0 {
		return gate.verifyConfiguredPersonalServer(ctx, out, appConfigPath, cfg, token, prompter)
	}

	if !prompter.CanPrompt() {
		fmt.Fprintln(out, "Personal Server creation skipped: configure is running non-interactively.")
		return nil
	}

	fmt.Fprintln(out, "Personal Server provisioning prerequisites are ready.")
	return gate.previewPersonalServerCreation(ctx, out, appConfigPath, token, cfg, prompter)
}

func (gate personalServerProvisioningGate) verifyConfiguredPersonalServer(ctx context.Context, out io.Writer, appConfigPath string, cfg appConfig, token string, prompter configurePrompter) error {
	client := gate.cloudClient(token)
	server, found, err := client.ServerByID(ctx, cfg.PersonalServer.ServerID)
	if err != nil {
		return fmt.Errorf("verify Personal Server %d in Hetzner: %w", cfg.PersonalServer.ServerID, err)
	}
	if !found {
		fmt.Fprintf(out, "Personal Server Configuration references missing server %d.\n", cfg.PersonalServer.ServerID)
		if !prompter.CanPrompt() {
			return fmt.Errorf("Personal Server Configuration references missing server %d; rerun `myn configure` interactively to clear it", cfg.PersonalServer.ServerID)
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
		return gate.previewPersonalServerCreation(ctx, out, appConfigPath, token, cfg, prompter)
	}

	fmt.Fprintf(out, "Personal Server already configured: server %d exists.\n", cfg.PersonalServer.ServerID)
	fmt.Fprintf(out, "Saved addresses: %s\n", formatPersonalServerAddresses(cfg.PersonalServer.IPv4, cfg.PersonalServer.IPv6))
	fmt.Fprintf(out, "Current addresses: %s\n", formatPersonalServerAddresses(server.IPv4, server.IPv6))
	return nil
}

func (gate personalServerProvisioningGate) previewPersonalServerCreation(ctx context.Context, out io.Writer, appConfigPath string, token string, cfg appConfig, prompter configurePrompter) error {
	client, ok := gate.cloudClient(token).(personalServerPreviewCloudClient)
	if !ok {
		return fmt.Errorf("Personal Server preview requires a Hetzner client that can list Locations, Server Types, and Pricing")
	}

	locations, err := client.Locations(ctx)
	if err != nil {
		return fmt.Errorf("list Personal Server Locations: %w", err)
	}
	locationChoices := personalServerLocationChoices(locations)
	if len(locationChoices) == 0 {
		return fmt.Errorf("no Hetzner Locations are available")
	}

	serverTypes, err := client.ServerTypes(ctx)
	if err != nil {
		return fmt.Errorf("list Personal Server Types: %w", err)
	}
	if !hasAnyEligiblePersonalServerType(serverTypes, locationChoices) {
		return fmt.Errorf("no eligible Server Types are available in any Location")
	}

	pricing, err := client.Pricing(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		pricing = personalServerPricing{}
	}

	defaultLocation := defaultPersonalServerLocationChoice(locationChoices)
	for {
		locationChoice, err := prompter.SelectPersonalServerLocation(locationChoices, defaultLocation)
		if err != nil {
			return err
		}

		serverTypeChoices := eligiblePersonalServerTypeChoices(serverTypes, locationChoice.Location.Name)
		if len(serverTypeChoices) == 0 {
			fmt.Fprintf(out, "No eligible Server Types are available in Location %s.\n", locationChoice.Location.Name)
			if nextDefault := firstLocationWithEligiblePersonalServerType(serverTypes, locationChoices); nextDefault >= 0 {
				defaultLocation = nextDefault
			}
			continue
		}

		serverTypeChoice, err := prompter.SelectPersonalServerType(serverTypeChoices, defaultPersonalServerTypeChoice(serverTypeChoices, locationChoice.Location.Name))
		if err != nil {
			return err
		}

		fmt.Fprintf(out, "Selected Personal Server Location: %s\n", locationChoice.Location.Name)
		fmt.Fprintf(out, "Selected Server Type: %s\n", serverTypeChoice.ServerType.Name)

		inputs, err := gate.collectPersonalServerCreationInputs(prompter)
		if err != nil {
			return err
		}
		plan := personalServerCreationPlan{
			Location:          locationChoice.Location,
			ServerType:        serverTypeChoice.ServerType,
			User:              inputs.User,
			ServerName:        inputs.ServerName,
			PasswordHash:      inputs.PasswordHash,
			GitIdentity:       inputs.GitIdentity,
			RemoteProjectRoot: cfg.Projects.RemoteRoot,
			SSHIdentityFile:   cfg.SSH.IdentityFile,
		}
		if createClient, ok := client.(personalServerCreateCloudClient); ok {
			existingFirewall, err := personalServerFirewallExists(ctx, createClient)
			if err != nil {
				return err
			}
			plan.ExistingFirewall = existingFirewall
		}
		plan.PrimaryIPv4MonthlyGrossEUR, _ = personalServerPrimaryIPMonthlyGrossText(
			pricing,
			string(hcloud.PrimaryIPTypeIPv4),
			locationChoice.Location.Name,
		)
		writePersonalServerCreationPlan(out, plan)

		create, err := prompter.Confirm("Create Personal Server?", false)
		if err != nil {
			return err
		}
		if !create {
			fmt.Fprintln(out, "Personal Server creation declined. No cloud resources were created.")
			return nil
		}

		createClient, ok := client.(personalServerCreateCloudClient)
		if !ok {
			return fmt.Errorf("Personal Server creation requires a Hetzner client that can create resources")
		}
		return gate.createPersonalServer(ctx, out, appConfigPath, cfg, createClient, plan)
	}
}

func (gate personalServerProvisioningGate) createPersonalServer(ctx context.Context, out io.Writer, appConfigPath string, cfg appConfig, client personalServerCreateCloudClient, plan personalServerCreationPlan) error {
	if _, found, err := client.ServerByName(ctx, plan.ServerName); err != nil {
		return fmt.Errorf("check for existing Personal Server name %q: %w", plan.ServerName, err)
	} else if found {
		return fmt.Errorf("Personal Server name %q already exists in Hetzner", plan.ServerName)
	}

	identity, err := gate.loadPersonalServerSSHIdentity(plan.SSHIdentityFile)
	if err != nil {
		return err
	}

	userData, err := renderPersonalServerBootstrapCloudInit(personalServerBootstrapInput{
		User:              plan.User,
		PasswordHash:      plan.PasswordHash,
		SSHPublicKey:      identity.PublicKey.Line(),
		RemoteProjectRoot: plan.RemoteProjectRoot,
		GitIdentity:       plan.GitIdentity,
		ToolPlan:          defaultPersonalServerBootstrapToolPlan(),
	})
	if err != nil {
		return err
	}

	image, err := latestPersonalServerUbuntuImage(ctx, client)
	if err != nil {
		return err
	}

	firewall, err := ensurePersonalServerFirewall(ctx, client)
	if err != nil {
		return err
	}

	sshKey, err := ensurePersonalServerSSHKey(ctx, client, identity)
	if err != nil {
		return err
	}

	server, actions, err := client.CreateServer(ctx, personalServerCreateServerRequest{
		Name:           plan.ServerName,
		LocationName:   plan.Location.Name,
		ServerTypeName: plan.ServerType.Name,
		ImageID:        image.ID,
		ImageName:      image.Name,
		SSHKeyID:       sshKey.ID,
		FirewallID:     firewall.ID,
		UserData:       userData,
		Labels:         personalServerResourceLabels(),
		EnableIPv4:     true,
		EnableIPv6:     true,
	})
	if err != nil {
		return fmt.Errorf("create Personal Server: %w", err)
	}
	if err := client.WaitActions(ctx, actions); err != nil {
		if saveErr := gate.savePersonalServerConfig(appConfigPath, cfg, server); saveErr != nil {
			return saveErr
		}
		return fmt.Errorf("wait for Personal Server create actions: %w", err)
	}

	if err := gate.savePersonalServerConfig(appConfigPath, cfg, server); err != nil {
		return err
	}

	fmt.Fprintf(out, "Personal Server created: server %d.\n", server.ID)
	fmt.Fprintf(out, "Personal Server addresses: %s\n", formatPersonalServerAddresses(server.IPv4, server.IPv6))

	marker, err := gate.waitForPersonalServerBootstrap(ctx, identity, server)
	if err != nil {
		fmt.Fprintf(out, "Personal Server bootstrap failed: %v\n", err)
		writePersonalServerSSHCommands(out, plan, server)
		return err
	}
	return writePersonalServerBootstrapReport(out, marker, plan, server)
}

func (gate personalServerProvisioningGate) savePersonalServerConfig(appConfigPath string, cfg appConfig, server personalServerCloudServer) error {
	cfg.PersonalServer = personalServerConfig{
		ServerID: server.ID,
		IPv4:     server.IPv4,
		IPv6:     server.IPv6,
	}
	return gate.writeConfig(appConfigPath, cfg)
}

const (
	personalServerFirewallName         = "myn-personal-server"
	personalServerMoshUDPPortRange     = "60000-61000"
	personalServerSSHKeyName           = "myn-personal-server"
	personalServerBootstrapMarkerPath  = "/var/lib/myn/personal-server-bootstrap.json"
	defaultPersonalServerBootstrapWait = 5 * time.Minute
	defaultPersonalServerSSHPollWait   = 5 * time.Second
)

type personalServerSSHRunner func(ctx context.Context, identityFile string, user string, host string, command string) (string, error)

type personalServerBootstrapMarker struct {
	Status             string            `json:"status"`
	Timestamp          string            `json:"timestamp"`
	Failure            string            `json:"failure"`
	RebootRequired     bool              `json:"rebootRequired"`
	ToolVersions       map[string]string `json:"toolVersions"`
	PartialFailures    []string          `json:"partialFailures"`
	SkippedGitIdentity []string          `json:"skippedGitIdentity"`
}

func (gate personalServerProvisioningGate) waitForPersonalServerBootstrap(ctx context.Context, identity sshIdentityCandidate, server personalServerCloudServer) (personalServerBootstrapMarker, error) {
	host := personalServerBootstrapHost(server)
	if host == "" {
		return personalServerBootstrapMarker{}, fmt.Errorf("Personal Server has no reachable public address")
	}

	runner := gate.personalServerSSHRunner()
	if err := gate.waitForPersonalServerRootSSH(ctx, runner, identity.IdentityPath, host); err != nil {
		return personalServerBootstrapMarker{}, err
	}

	timeout := gate.personalServerBootstrapTimeout()
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		output, err := runner(pollCtx, identity.IdentityPath, "root", host, "cat "+personalServerBootstrapMarkerPath)
		if err == nil {
			marker, parseErr := parsePersonalServerBootstrapMarker(output)
			if parseErr == nil {
				return marker, nil
			}
		}

		if err := ctx.Err(); err != nil {
			return personalServerBootstrapMarker{}, err
		}
		if pollCtx.Err() != nil {
			return personalServerBootstrapMarker{}, fmt.Errorf("timed out waiting for Personal Server Bootstrap marker after %s", timeout)
		}
		if err := gate.personalServerSleep(pollCtx, gate.personalServerSSHPollInterval()); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return personalServerBootstrapMarker{}, ctxErr
			}
			return personalServerBootstrapMarker{}, fmt.Errorf("timed out waiting for Personal Server Bootstrap marker after %s", timeout)
		}
	}
}

func (gate personalServerProvisioningGate) waitForPersonalServerRootSSH(ctx context.Context, runner personalServerSSHRunner, identityFile string, host string) error {
	for {
		if _, err := runner(ctx, identityFile, "root", host, "true"); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := gate.personalServerSleep(ctx, gate.personalServerSSHPollInterval()); err != nil {
			return err
		}
	}
}

func parsePersonalServerBootstrapMarker(output string) (personalServerBootstrapMarker, error) {
	var marker personalServerBootstrapMarker
	if err := json.Unmarshal([]byte(output), &marker); err != nil {
		return personalServerBootstrapMarker{}, fmt.Errorf("parse Personal Server Bootstrap marker: %w", err)
	}
	if strings.TrimSpace(marker.Status) == "" {
		return personalServerBootstrapMarker{}, fmt.Errorf("Personal Server Bootstrap marker is missing status")
	}
	return marker, nil
}

func writePersonalServerBootstrapReport(out io.Writer, marker personalServerBootstrapMarker, plan personalServerCreationPlan, server personalServerCloudServer) error {
	switch strings.ToLower(strings.TrimSpace(marker.Status)) {
	case "success":
		fmt.Fprintln(out, "Personal Server bootstrap completed.")
		if strings.TrimSpace(marker.Timestamp) != "" {
			fmt.Fprintf(out, "Bootstrap timestamp: %s\n", marker.Timestamp)
		}
		fmt.Fprintf(out, "Reboot required: %t\n", marker.RebootRequired)
		writePersonalServerToolVersions(out, marker.ToolVersions)
		writePersonalServerPartialFailures(out, marker.PartialFailures)
		writePersonalServerSSHCommands(out, plan, server)
		writePersonalServerMoshCommands(out, plan, server)
		return nil
	case "failed":
		fmt.Fprintln(out, "Personal Server bootstrap failed.")
		if strings.TrimSpace(marker.Failure) != "" {
			fmt.Fprintf(out, "Bootstrap failure: %s\n", marker.Failure)
		}
		writePersonalServerPartialFailures(out, marker.PartialFailures)
		writePersonalServerSSHCommands(out, plan, server)
		if strings.TrimSpace(marker.Failure) != "" {
			return fmt.Errorf("Personal Server Bootstrap failed: %s", marker.Failure)
		}
		return fmt.Errorf("Personal Server Bootstrap failed")
	default:
		writePersonalServerSSHCommands(out, plan, server)
		return fmt.Errorf("Personal Server Bootstrap marker has unknown status %q", marker.Status)
	}
}

func writePersonalServerToolVersions(out io.Writer, versions map[string]string) {
	if len(versions) == 0 {
		fmt.Fprintln(out, "Installed tool versions: unavailable")
		return
	}
	fmt.Fprintln(out, "Installed tool versions:")
	names := make([]string, 0, len(versions))
	for name, version := range versions {
		if strings.TrimSpace(version) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(out, "- %s: %s\n", name, versions[name])
	}
}

func writePersonalServerPartialFailures(out io.Writer, failures []string) {
	if len(failures) == 0 {
		return
	}
	fmt.Fprintln(out, "Partial bootstrap failures:")
	for _, failure := range failures {
		if strings.TrimSpace(failure) == "" {
			continue
		}
		fmt.Fprintf(out, "- %s\n", failure)
	}
}

func writePersonalServerSSHCommands(out io.Writer, plan personalServerCreationPlan, server personalServerCloudServer) {
	fmt.Fprintln(out, "SSH commands:")
	writePersonalServerSSHCommand(out, "user IPv4", plan.SSHIdentityFile, plan.User, server.IPv4)
	writePersonalServerSSHCommand(out, "root IPv4", plan.SSHIdentityFile, "root", server.IPv4)
	writePersonalServerSSHCommand(out, "user IPv6", plan.SSHIdentityFile, plan.User, server.IPv6)
	writePersonalServerSSHCommand(out, "root IPv6", plan.SSHIdentityFile, "root", server.IPv6)
}

func writePersonalServerSSHCommand(out io.Writer, label string, identityFile string, user string, host string) {
	host = strings.TrimSpace(host)
	if host == "" {
		fmt.Fprintf(out, "- %s: unavailable\n", label)
		return
	}
	fmt.Fprintf(out, "- %s: ssh -i ~/%s %s@%s\n", label, identityFile, user, personalServerSSHCommandHost(host))
}

func writePersonalServerMoshCommands(out io.Writer, plan personalServerCreationPlan, server personalServerCloudServer) {
	fmt.Fprintln(out, "Mosh commands:")
	writePersonalServerMoshCommand(out, "user IPv4", plan.SSHIdentityFile, plan.User, server.IPv4)
	writePersonalServerMoshCommand(out, "user IPv6", plan.SSHIdentityFile, plan.User, server.IPv6)
}

func writePersonalServerMoshCommand(out io.Writer, label string, identityFile string, user string, host string) {
	host = strings.TrimSpace(host)
	if host == "" {
		fmt.Fprintf(out, "- %s: unavailable\n", label)
		return
	}
	fmt.Fprintf(out, "- %s: mosh --ssh=\"ssh -i ~/%s\" %s@%s\n", label, identityFile, user, host)
}

func personalServerBootstrapHost(server personalServerCloudServer) string {
	if strings.TrimSpace(server.IPv4) != "" {
		return strings.TrimSpace(server.IPv4)
	}
	return strings.TrimSpace(server.IPv6)
}

func personalServerSSHCommandHost(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

func (gate personalServerProvisioningGate) personalServerSSHRunner() personalServerSSHRunner {
	if gate.runSSH != nil {
		return gate.runSSH
	}
	return defaultPersonalServerSSHRunner
}

func defaultPersonalServerSSHRunner(ctx context.Context, identityFile string, user string, host string, command string) (string, error) {
	sshHost := user + "@" + personalServerSSHCommandHost(host)
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-i", identityFile,
		sshHost,
		command,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", commandOutputError("ssh", output, err)
	}
	return string(output), nil
}

func (gate personalServerProvisioningGate) personalServerBootstrapTimeout() time.Duration {
	if gate.bootstrapTimeout > 0 {
		return gate.bootstrapTimeout
	}
	return defaultPersonalServerBootstrapWait
}

func (gate personalServerProvisioningGate) personalServerSSHPollInterval() time.Duration {
	if gate.sshPollInterval > 0 {
		return gate.sshPollInterval
	}
	return defaultPersonalServerSSHPollWait
}

func (gate personalServerProvisioningGate) personalServerSleep(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	if gate.sleep != nil {
		return gate.sleep(ctx, duration)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var personalServerUbuntuVersionPattern = regexp.MustCompile(`\d+(?:\.\d+)*`)

func latestPersonalServerUbuntuImage(ctx context.Context, client personalServerCreateCloudClient) (personalServerImage, error) {
	images, err := client.Images(ctx)
	if err != nil {
		return personalServerImage{}, fmt.Errorf("list Ubuntu images: %w", err)
	}

	image, ok := selectLatestPersonalServerUbuntuImage(images)
	if !ok {
		return personalServerImage{}, fmt.Errorf("no non-deprecated Ubuntu system image is available")
	}
	return image, nil
}

func selectLatestPersonalServerUbuntuImage(images []personalServerImage) (personalServerImage, bool) {
	var selected personalServerImage
	var selectedVersion []int
	for _, image := range images {
		version, ok := personalServerUbuntuImageVersion(image)
		if !ok {
			continue
		}
		comparison := comparePersonalServerVersions(version, selectedVersion)
		if selectedVersion == nil || comparison > 0 || (comparison == 0 && image.Name > selected.Name) {
			selected = image
			selectedVersion = version
		}
	}
	return selected, selected.Name != "" || selected.ID != 0
}

func personalServerUbuntuImageVersion(image personalServerImage) ([]int, bool) {
	if image.Deprecated {
		return nil, false
	}
	if !strings.EqualFold(image.Type, string(hcloud.ImageTypeSystem)) {
		return nil, false
	}
	if !strings.EqualFold(image.Status, string(hcloud.ImageStatusAvailable)) {
		return nil, false
	}
	if !strings.EqualFold(image.OSFlavor, "ubuntu") {
		return nil, false
	}
	if !strings.EqualFold(image.Architecture, string(hcloud.ArchitectureX86)) {
		return nil, false
	}

	for _, candidate := range []string{image.OSVersion, image.Name} {
		if version, ok := parsePersonalServerVersion(candidate); ok {
			return version, true
		}
	}
	return nil, false
}

func parsePersonalServerVersion(value string) ([]int, bool) {
	match := personalServerUbuntuVersionPattern.FindString(value)
	if match == "" {
		return nil, false
	}
	parts := strings.Split(match, ".")
	version := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		version = append(version, number)
	}
	return version, true
}

func comparePersonalServerVersions(left []int, right []int) int {
	limit := len(left)
	if len(right) > limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		var l, r int
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l > r {
			return 1
		}
		if l < r {
			return -1
		}
	}
	return 0
}

func ensurePersonalServerFirewall(ctx context.Context, client personalServerCreateCloudClient) (personalServerFirewall, error) {
	firewall, found, err := client.FirewallByName(ctx, personalServerFirewallName)
	if err != nil {
		return personalServerFirewall{}, fmt.Errorf("find Personal Server Firewall: %w", err)
	}
	if found {
		return firewall, nil
	}

	firewall, actions, err := client.CreateFirewall(ctx, personalServerFirewall{
		Name:   personalServerFirewallName,
		Labels: personalServerResourceLabels(),
		Rules: []personalServerFirewallRule{
			{Direction: "in", Protocol: "tcp", Port: "22", SourceIPs: []string{"0.0.0.0/0", "::/0"}},
			{Direction: "in", Protocol: "udp", Port: personalServerMoshUDPPortRange, SourceIPs: []string{"0.0.0.0/0", "::/0"}},
		},
	})
	if err != nil {
		return personalServerFirewall{}, fmt.Errorf("create Personal Server Firewall: %w", err)
	}
	if err := client.WaitActions(ctx, actions); err != nil {
		return personalServerFirewall{}, fmt.Errorf("wait for Personal Server Firewall create actions: %w", err)
	}
	return firewall, nil
}

func personalServerFirewallExists(ctx context.Context, client personalServerCreateCloudClient) (bool, error) {
	_, found, err := client.FirewallByName(ctx, personalServerFirewallName)
	if err != nil {
		return false, fmt.Errorf("find Personal Server Firewall: %w", err)
	}
	return found, nil
}

func ensurePersonalServerSSHKey(ctx context.Context, client personalServerCreateCloudClient, identity sshIdentityCandidate) (personalServerSSHKey, error) {
	fingerprint, err := sshPublicKeyHetznerFingerprint(identity.PublicKey)
	if err != nil {
		return personalServerSSHKey{}, err
	}

	sshKey, found, err := client.SSHKeyByFingerprint(ctx, fingerprint)
	if err != nil {
		return personalServerSSHKey{}, fmt.Errorf("find Personal Server SSH Key: %w", err)
	}
	if found {
		return sshKey, nil
	}

	sshKey, err = client.CreateSSHKey(ctx, personalServerSSHKey{
		Name:        personalServerSSHKeyNameForFingerprint(fingerprint),
		Fingerprint: fingerprint,
		PublicKey:   identity.PublicKey.Line(),
		Labels:      personalServerResourceLabels(),
	})
	if err != nil {
		return personalServerSSHKey{}, fmt.Errorf("create Personal Server SSH Key: %w", err)
	}
	return sshKey, nil
}

func personalServerSSHKeyNameForFingerprint(fingerprint string) string {
	fingerprint = strings.ReplaceAll(strings.TrimSpace(fingerprint), ":", "")
	if fingerprint == "" {
		return personalServerSSHKeyName
	}
	return personalServerSSHKeyName + "-" + fingerprint
}

func personalServerResourceLabels() map[string]string {
	return map[string]string{
		"managed_by": "myn",
		"role":       "personal_server",
	}
}

func (gate personalServerProvisioningGate) loadPersonalServerSSHIdentity(identityFile string) (sshIdentityCandidate, error) {
	home, err := gate.personalServerUserHomeDir()
	if err != nil {
		return sshIdentityCandidate{}, fmt.Errorf("find user home directory: %w", err)
	}

	candidate, err := loadSSHIdentity(identityFile, home, configureDeps{
		stat:         gate.personalServerStat(),
		readFile:     gate.personalServerReadFile(),
		writeFile:    gate.personalServerWriteFile(),
		chmod:        gate.personalServerChmod(),
		sshPublicKey: gate.personalServerSSHPublicKey(),
	})
	if err != nil {
		return sshIdentityCandidate{}, fmt.Errorf("load configured SSH identity for Personal Server: %w", err)
	}
	return candidate, nil
}

func (gate personalServerProvisioningGate) collectPersonalServerCreationInputs(prompter configurePrompter) (personalServerCreationInputs, error) {
	defaultUser := normalizePersonalServerUser(gate.personalServerCurrentUsername())
	user, err := prompter.Input("Personal Server User", defaultUser, validatePersonalServerUser)
	if err != nil {
		return personalServerCreationInputs{}, err
	}

	serverName, err := prompter.Input("Personal Server name", user+"-personal-server", validatePersonalServerName)
	if err != nil {
		return personalServerCreationInputs{}, err
	}

	passwordHash, err := collectPersonalServerPasswordHashWithReader(prompter, gate.personalServerPasswordSaltReader())
	if err != nil {
		return personalServerCreationInputs{}, err
	}

	return personalServerCreationInputs{
		User:         user,
		ServerName:   serverName,
		PasswordHash: passwordHash,
		GitIdentity:  gate.personalServerGitIdentity(),
	}, nil
}

func (gate personalServerProvisioningGate) personalServerCurrentUsername() string {
	if gate.currentUsername != nil {
		return gate.currentUsername()
	}
	return currentOSUsername()
}

func normalizePersonalServerUser(input string) string {
	value := strings.TrimSpace(input)
	value = strings.TrimPrefix(value, `.\`)
	if index := strings.LastIndexAny(value, `\/`); index >= 0 && index < len(value)-1 {
		value = value[index+1:]
	}
	value = strings.ToLower(value)

	var b strings.Builder
	lastHyphen := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if b.Len() > 0 && !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}

	normalized := strings.Trim(b.String(), "-")
	if normalized == "" {
		return ""
	}
	if normalized[0] >= '0' && normalized[0] <= '9' {
		normalized = "user-" + normalized
	}
	if len(normalized) > 32 {
		normalized = strings.TrimRight(normalized[:32], "-")
	}
	return normalized
}

func validatePersonalServerUser(input string) error {
	value := strings.TrimSpace(input)
	if value == "" {
		return fmt.Errorf("Personal Server User is required")
	}
	if value != input {
		return fmt.Errorf("Personal Server User must not have leading or trailing spaces")
	}
	if len(value) > 32 {
		return fmt.Errorf("Personal Server User must be 32 characters or fewer")
	}
	if value[0] < 'a' || value[0] > 'z' {
		return fmt.Errorf("Personal Server User must start with a lowercase letter")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("Personal Server User must use only lowercase letters, digits, and hyphens")
	}
	return nil
}

func validatePersonalServerName(input string) error {
	value := strings.TrimSpace(input)
	if value == "" {
		return fmt.Errorf("Personal Server name is required")
	}
	if value != input {
		return fmt.Errorf("Personal Server name must not have leading or trailing spaces")
	}
	if len(value) > 63 {
		return fmt.Errorf("Personal Server name must be 63 characters or fewer")
	}
	if value[0] == '-' {
		return fmt.Errorf("Personal Server name must start with a lowercase letter or digit")
	}
	if value[len(value)-1] == '-' {
		return fmt.Errorf("Personal Server name must end with a lowercase letter or digit")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("Personal Server name must use only lowercase letters, digits, and hyphens")
	}
	return nil
}

func collectPersonalServerPasswordHash(prompter configurePrompter) (string, error) {
	return collectPersonalServerPasswordHashWithReader(prompter, rand.Reader)
}

func collectPersonalServerPasswordHashWithReader(prompter configurePrompter, saltReader io.Reader) (string, error) {
	password, err := prompter.Password("Personal Server User password")
	if err != nil {
		return "", err
	}
	if password == "" {
		return "", fmt.Errorf("Personal Server User password is required")
	}

	confirmation, err := prompter.Password("Confirm Personal Server User password")
	if err != nil {
		return "", err
	}
	if confirmation != password {
		return "", fmt.Errorf("Personal Server User password confirmation does not match")
	}

	return hashPersonalServerPassword(password, saltReader)
}

func (gate personalServerProvisioningGate) personalServerPasswordSaltReader() io.Reader {
	if gate.passwordSaltReader != nil {
		return gate.passwordSaltReader
	}
	return rand.Reader
}

func (gate personalServerProvisioningGate) personalServerUserHomeDir() (string, error) {
	if gate.userHomeDir != nil {
		return gate.userHomeDir()
	}
	return os.UserHomeDir()
}

func (gate personalServerProvisioningGate) personalServerStat() func(string) (os.FileInfo, error) {
	if gate.stat != nil {
		return gate.stat
	}
	return os.Stat
}

func (gate personalServerProvisioningGate) personalServerReadFile() func(string) ([]byte, error) {
	if gate.readFile != nil {
		return gate.readFile
	}
	return os.ReadFile
}

func (gate personalServerProvisioningGate) personalServerWriteFile() func(string, []byte, os.FileMode) error {
	if gate.writeFile != nil {
		return gate.writeFile
	}
	return os.WriteFile
}

func (gate personalServerProvisioningGate) personalServerChmod() func(string, os.FileMode) error {
	if gate.chmod != nil {
		return gate.chmod
	}
	return os.Chmod
}

func (gate personalServerProvisioningGate) personalServerSSHPublicKey() func(string) (string, error) {
	if gate.sshPublicKey != nil {
		return gate.sshPublicKey
	}
	return func(identityPath string) (string, error) {
		output, err := exec.Command("ssh-keygen", "-y", "-f", identityPath).CombinedOutput()
		if err != nil {
			return "", commandOutputError("ssh-keygen -y", output, err)
		}
		return string(output), nil
	}
}

func hashPersonalServerPassword(password string, saltReader io.Reader) (string, error) {
	if password == "" {
		return "", fmt.Errorf("Personal Server User password is required")
	}
	salt, err := randomPersonalServerPasswordSalt(saltReader, 16)
	if err != nil {
		return "", err
	}
	return sha512_crypt.New().Generate([]byte(password), []byte("$6$"+salt))
}

func randomPersonalServerPasswordSalt(reader io.Reader, length int) (string, error) {
	if reader == nil {
		reader = rand.Reader
	}
	const alphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	const maxByte = 256 - (256 % len(alphabet))

	out := make([]byte, length)
	var one [1]byte
	for i := range out {
		for {
			if _, err := io.ReadFull(reader, one[:]); err != nil {
				return "", fmt.Errorf("generate password salt: %w", err)
			}
			if int(one[0]) >= maxByte {
				continue
			}
			out[i] = alphabet[int(one[0])%len(alphabet)]
			break
		}
	}
	return string(out), nil
}

func (gate personalServerProvisioningGate) personalServerGitIdentity() personalServerGitIdentity {
	name, _ := gate.firstPersonalServerGitConfigValue("user.name")
	email, _ := gate.firstPersonalServerGitConfigValue("user.email")
	return personalServerGitIdentity{
		Name:  name,
		Email: email,
	}
}

func (gate personalServerProvisioningGate) firstPersonalServerGitConfigValue(key string) (string, bool) {
	for _, scope := range []personalServerGitConfigScope{personalServerGitConfigGlobal, personalServerGitConfigLocal} {
		value, ok := gate.personalServerGitConfigValue(scope, key)
		if ok {
			return value, true
		}
	}
	return "", false
}

func (gate personalServerProvisioningGate) personalServerGitConfigValue(scope personalServerGitConfigScope, key string) (string, bool) {
	if gate.gitConfigValue != nil {
		return gate.gitConfigValue(scope, key)
	}
	return defaultPersonalServerGitConfigValue(scope, key)
}

func defaultPersonalServerGitConfigValue(scope personalServerGitConfigScope, key string) (string, bool) {
	args := []string{"config"}
	if scope == personalServerGitConfigGlobal {
		args = append(args, "--global")
	} else if scope == personalServerGitConfigLocal {
		args = append(args, "--local")
	}
	args = append(args, "--get", key)

	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", false
	}
	value := strings.TrimSpace(string(output))
	return value, value != ""
}

func writePersonalServerCreationPlan(out io.Writer, plan personalServerCreationPlan) {
	fmt.Fprintln(out, "Personal Server plan:")
	fmt.Fprintf(out, "Location: %s\n", plan.Location.Name)
	fmt.Fprintf(out, "Server Type: %s\n", plan.ServerType.Name)
	fmt.Fprintf(out, "Server name: %s\n", plan.ServerName)
	fmt.Fprintf(out, "Personal Server User: %s\n", plan.User)
	fmt.Fprintln(out, "SSH and network:")
	fmt.Fprintf(out, "SSH key: ~/%s\n", plan.SSHIdentityFile)
	if plan.ExistingFirewall {
		fmt.Fprintf(out, "Firewall: %s (existing rules reused unchanged; Mosh may require inbound UDP %s)\n", personalServerFirewallName, personalServerMoshUDPPortRange)
	} else {
		fmt.Fprintf(out, "Firewall: %s (inbound SSH and Mosh UDP %s over IPv4 and IPv6)\n", personalServerFirewallName, personalServerMoshUDPPortRange)
	}
	fmt.Fprintln(out, "Public network: IPv4 and IPv6 enabled")
	fmt.Fprintf(out, "Remote project root: ~/%s\n", plan.RemoteProjectRoot)
	fmt.Fprintln(out, "Install plan:")
	fmt.Fprintln(out, "System services:")
	fmt.Fprintln(out, "- security updates and unattended security upgrades")
	fmt.Fprintln(out, "- Mosh Access")
	fmt.Fprintln(out, "- Docker Engine and Docker Compose")
	fmt.Fprintln(out, "- Personal Server User in docker group (root-equivalent access)")
	fmt.Fprintln(out, "- Homebrew")
	fmt.Fprintln(out, "Homebrew tools:")
	fmt.Fprintln(out, "- tmux, jq, git, gh, rustup, go, nvm")
	fmt.Fprintln(out, "Coding agents:")
	fmt.Fprintln(out, "- Codex")
	fmt.Fprintln(out, "- Claude Code")
	fmt.Fprintln(out, "Git identity:")
	writePersonalServerGitIdentityLine(out, "user.name", plan.GitIdentity.Name)
	writePersonalServerGitIdentityLine(out, "user.email", plan.GitIdentity.Email)
	if price, ok := personalServerCreationPlanMonthlyGrossText(plan); ok {
		fmt.Fprintf(out, "Maximum monthly price: %s EUR gross\n", price)
	} else {
		fmt.Fprintln(out, "Maximum monthly price: unavailable")
	}
}

func writePersonalServerGitIdentityLine(out io.Writer, key string, value string) {
	if strings.TrimSpace(value) == "" {
		fmt.Fprintf(out, "- %s: skipped (not configured)\n", key)
		return
	}
	fmt.Fprintf(out, "- %s: %s\n", key, value)
}

func personalServerLocationChoices(locations []personalServerLocation) []personalServerLocationChoice {
	locations = append([]personalServerLocation(nil), locations...)
	sort.SliceStable(locations, func(i, j int) bool {
		return locations[i].Name < locations[j].Name
	})

	choices := make([]personalServerLocationChoice, 0, len(locations))
	for _, location := range locations {
		location.Name = strings.TrimSpace(location.Name)
		if location.Name == "" {
			continue
		}
		choices = append(choices, personalServerLocationChoice{
			Label:    personalServerLocationLabel(location),
			Location: location,
		})
	}
	return choices
}

func personalServerLocationLabel(location personalServerLocation) string {
	geography := strings.TrimSpace(location.Description)
	if geography == "" {
		parts := make([]string, 0, 2)
		if city := strings.TrimSpace(location.City); city != "" {
			parts = append(parts, city)
		}
		if country := strings.TrimSpace(location.Country); country != "" {
			parts = append(parts, country)
		}
		geography = strings.Join(parts, ", ")
	}
	if geography == "" {
		return location.Name
	}
	return fmt.Sprintf("%s - %s", location.Name, geography)
}

func defaultPersonalServerLocationChoice(choices []personalServerLocationChoice) int {
	for index, choice := range choices {
		if choice.Location.Name == "ash" {
			return index
		}
	}
	return 0
}

func eligiblePersonalServerTypeChoices(serverTypes []personalServerType, locationName string) []personalServerTypeChoice {
	eligible := make([]personalServerType, 0, len(serverTypes))
	for _, serverType := range serverTypes {
		if !isEligiblePersonalServerType(serverType, locationName) {
			continue
		}
		eligible = append(eligible, serverType)
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		return eligible[i].Name < eligible[j].Name
	})

	choices := make([]personalServerTypeChoice, 0, len(eligible))
	for _, serverType := range eligible {
		choices = append(choices, personalServerTypeChoice{
			Label:      personalServerTypeLabel(serverType),
			ServerType: serverType,
		})
	}
	return choices
}

func isEligiblePersonalServerType(serverType personalServerType, locationName string) bool {
	if strings.TrimSpace(serverType.Name) == "" {
		return false
	}
	if serverType.Deprecated {
		return false
	}
	if serverType.Architecture != string(hcloud.ArchitectureX86) {
		return false
	}
	for _, location := range serverType.Locations {
		if location.LocationName == locationName && location.Available && !location.Deprecated {
			return true
		}
	}
	return false
}

func hasAnyEligiblePersonalServerType(serverTypes []personalServerType, locationChoices []personalServerLocationChoice) bool {
	return firstLocationWithEligiblePersonalServerType(serverTypes, locationChoices) >= 0
}

func firstLocationWithEligiblePersonalServerType(serverTypes []personalServerType, locationChoices []personalServerLocationChoice) int {
	for index, locationChoice := range locationChoices {
		if len(eligiblePersonalServerTypeChoices(serverTypes, locationChoice.Location.Name)) > 0 {
			return index
		}
	}
	return -1
}

func personalServerTypeLabel(serverType personalServerType) string {
	return fmt.Sprintf("%s - %s, %d vCPU, %s GB RAM, %d GB %s disk",
		serverType.Name,
		serverType.CPUType,
		serverType.Cores,
		formatPersonalServerMemory(serverType.MemoryGB),
		serverType.DiskGB,
		serverType.StorageType,
	)
}

func formatPersonalServerMemory(memoryGB float64) string {
	return strconv.FormatFloat(memoryGB, 'f', -1, 64)
}

func defaultPersonalServerTypeChoice(choices []personalServerTypeChoice, locationName string) int {
	selected := 0
	for index := 1; index < len(choices); index++ {
		if betterPersonalServerTypeDefault(choices[index].ServerType, choices[selected].ServerType, locationName) {
			selected = index
		}
	}
	return selected
}

func betterPersonalServerTypeDefault(candidate personalServerType, current personalServerType, locationName string) bool {
	candidatePrice, candidatePriced := personalServerTypeMonthlyGross(candidate, locationName)
	currentPrice, currentPriced := personalServerTypeMonthlyGross(current, locationName)
	switch {
	case candidatePriced && !currentPriced:
		return true
	case !candidatePriced && currentPriced:
		return false
	case candidatePriced && currentPriced:
		candidateDistance := math.Abs(candidatePrice - 21)
		currentDistance := math.Abs(currentPrice - 21)
		if candidateDistance != currentDistance {
			return candidateDistance < currentDistance
		}
	}

	if personalServerTypeDedicated(candidate) != personalServerTypeDedicated(current) {
		return personalServerTypeDedicated(candidate)
	}
	if candidate.MemoryGB != current.MemoryGB {
		return candidate.MemoryGB > current.MemoryGB
	}
	if candidate.Cores != current.Cores {
		return candidate.Cores > current.Cores
	}
	return candidate.Name < current.Name
}

func personalServerTypeMonthlyGross(serverType personalServerType, locationName string) (float64, bool) {
	value, ok := personalServerTypeMonthlyGrossText(serverType, locationName)
	if !ok {
		return 0, false
	}
	return parsePersonalServerMonthlyGrossEUR(value)
}

func personalServerTypeMonthlyGrossText(serverType personalServerType, locationName string) (string, bool) {
	for _, pricing := range serverType.Pricings {
		if pricing.LocationName != locationName {
			continue
		}
		value := strings.TrimSpace(pricing.MonthlyGrossEUR)
		if _, ok := parsePersonalServerMonthlyGrossEUR(value); !ok {
			return "", false
		}
		return value, true
	}
	return "", false
}

func personalServerCreationPlanMonthlyGrossText(plan personalServerCreationPlan) (string, bool) {
	serverPrice, ok := personalServerTypeMonthlyGross(plan.ServerType, plan.Location.Name)
	if !ok {
		return "", false
	}

	primaryIPv4Price, ok := parsePersonalServerMonthlyGrossEUR(plan.PrimaryIPv4MonthlyGrossEUR)
	if !ok {
		return "", false
	}

	return formatPersonalServerMonthlyGrossEUR(serverPrice + primaryIPv4Price), true
}

func personalServerPrimaryIPMonthlyGrossText(pricing personalServerPricing, ipType string, locationName string) (string, bool) {
	ipType = strings.TrimSpace(ipType)
	for _, primaryIP := range pricing.PrimaryIPs {
		if strings.TrimSpace(primaryIP.Type) != ipType {
			continue
		}
		for _, locationPricing := range primaryIP.Pricings {
			if locationPricing.LocationName != locationName {
				continue
			}
			value := strings.TrimSpace(locationPricing.MonthlyGrossEUR)
			if _, ok := parsePersonalServerMonthlyGrossEUR(value); !ok {
				return "", false
			}
			return value, true
		}
	}
	return "", false
}

func parsePersonalServerMonthlyGrossEUR(value string) (float64, bool) {
	price, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, false
	}
	return price, true
}

func formatPersonalServerMonthlyGrossEUR(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func personalServerTypeDedicated(serverType personalServerType) bool {
	return serverType.CPUType == string(hcloud.CPUTypeDedicated)
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

	return personalServerCloudServerFromHcloud(server), true, nil
}

func (client hcloudPersonalServerCloudClient) ServerByName(ctx context.Context, name string) (personalServerCloudServer, bool, error) {
	server, _, err := client.client.Server.GetByName(ctx, name)
	if err != nil {
		return personalServerCloudServer{}, false, err
	}
	if server == nil {
		return personalServerCloudServer{}, false, nil
	}

	return personalServerCloudServerFromHcloud(server), true, nil
}

func (client hcloudPersonalServerCloudClient) Locations(ctx context.Context) ([]personalServerLocation, error) {
	locations, err := client.client.Location.All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]personalServerLocation, 0, len(locations))
	for _, location := range locations {
		if location == nil {
			continue
		}
		result = append(result, personalServerLocation{
			Name:        location.Name,
			Description: location.Description,
			City:        location.City,
			Country:     location.Country,
		})
	}
	return result, nil
}

func (client hcloudPersonalServerCloudClient) ServerTypes(ctx context.Context) ([]personalServerType, error) {
	serverTypes, err := client.client.ServerType.All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]personalServerType, 0, len(serverTypes))
	for _, serverType := range serverTypes {
		if serverType == nil {
			continue
		}

		locations := make([]personalServerTypeLocation, 0, len(serverType.Locations))
		for _, location := range serverType.Locations {
			typeLocation := personalServerTypeLocation{
				Available:  location.Available,
				Deprecated: location.IsDeprecated(),
			}
			if location.Location != nil {
				typeLocation.LocationName = location.Location.Name
			}
			locations = append(locations, typeLocation)
		}

		pricings := make([]personalServerTypeLocationPricing, 0, len(serverType.Pricings))
		for _, pricing := range serverType.Pricings {
			typePricing := personalServerTypeLocationPricing{
				MonthlyGrossEUR: pricing.Monthly.Gross,
			}
			if pricing.Location != nil {
				typePricing.LocationName = pricing.Location.Name
			}
			pricings = append(pricings, typePricing)
		}

		result = append(result, personalServerType{
			Name:         serverType.Name,
			CPUType:      string(serverType.CPUType),
			Architecture: string(serverType.Architecture),
			Deprecated:   serverType.IsDeprecated(),
			Cores:        serverType.Cores,
			MemoryGB:     float64(serverType.Memory),
			DiskGB:       serverType.Disk,
			StorageType:  string(serverType.StorageType),
			Locations:    locations,
			Pricings:     pricings,
		})
	}
	return result, nil
}

func (client hcloudPersonalServerCloudClient) Pricing(ctx context.Context) (personalServerPricing, error) {
	pricing, _, err := client.client.Pricing.Get(ctx)
	if err != nil {
		return personalServerPricing{}, err
	}
	return personalServerPricingFromHcloud(pricing), nil
}

func personalServerPricingFromHcloud(pricing hcloud.Pricing) personalServerPricing {
	primaryIPs := make([]personalServerPrimaryIPPricing, 0, len(pricing.PrimaryIPs))
	for _, primaryIP := range pricing.PrimaryIPs {
		primaryIPPricing := personalServerPrimaryIPPricing{
			Type:     primaryIP.Type,
			Pricings: make([]personalServerPrimaryIPLocationPricing, 0, len(primaryIP.Pricings)),
		}
		for _, price := range primaryIP.Pricings {
			primaryIPPricing.Pricings = append(primaryIPPricing.Pricings, personalServerPrimaryIPLocationPricing{
				LocationName:    price.Location,
				MonthlyGrossEUR: price.Monthly.Gross,
			})
		}
		primaryIPs = append(primaryIPs, primaryIPPricing)
	}
	return personalServerPricing{PrimaryIPs: primaryIPs}
}

func (client hcloudPersonalServerCloudClient) Images(ctx context.Context) ([]personalServerImage, error) {
	images, err := client.client.Image.AllWithOpts(ctx, hcloud.ImageListOpts{
		Type:              []hcloud.ImageType{hcloud.ImageTypeSystem},
		Status:            []hcloud.ImageStatus{hcloud.ImageStatusAvailable},
		Architecture:      []hcloud.Architecture{hcloud.ArchitectureX86},
		IncludeDeprecated: true,
	})
	if err != nil {
		return nil, err
	}

	result := make([]personalServerImage, 0, len(images))
	for _, image := range images {
		if image == nil {
			continue
		}
		result = append(result, personalServerImage{
			ID:           int(image.ID),
			Name:         image.Name,
			Type:         string(image.Type),
			Status:       string(image.Status),
			OSFlavor:     image.OSFlavor,
			OSVersion:    image.OSVersion,
			Architecture: string(image.Architecture),
			Deprecated:   image.IsDeprecated(),
		})
	}
	return result, nil
}

func (client hcloudPersonalServerCloudClient) FirewallByName(ctx context.Context, name string) (personalServerFirewall, bool, error) {
	firewall, _, err := client.client.Firewall.GetByName(ctx, name)
	if err != nil {
		return personalServerFirewall{}, false, err
	}
	if firewall == nil {
		return personalServerFirewall{}, false, nil
	}
	return personalServerFirewallFromHcloud(firewall), true, nil
}

func (client hcloudPersonalServerCloudClient) CreateFirewall(ctx context.Context, firewall personalServerFirewall) (personalServerFirewall, []personalServerAction, error) {
	rules, err := hcloudFirewallRules(firewall.Rules)
	if err != nil {
		return personalServerFirewall{}, nil, err
	}

	result, _, err := client.client.Firewall.Create(ctx, hcloud.FirewallCreateOpts{
		Name:   firewall.Name,
		Labels: firewall.Labels,
		Rules:  rules,
	})
	if err != nil {
		return personalServerFirewall{}, nil, err
	}
	return personalServerFirewallFromHcloud(result.Firewall), personalServerActionsFromHcloud(result.Actions), nil
}

func (client hcloudPersonalServerCloudClient) SSHKeyByFingerprint(ctx context.Context, fingerprint string) (personalServerSSHKey, bool, error) {
	sshKey, _, err := client.client.SSHKey.GetByFingerprint(ctx, fingerprint)
	if err != nil {
		return personalServerSSHKey{}, false, err
	}
	if sshKey == nil {
		return personalServerSSHKey{}, false, nil
	}
	return personalServerSSHKeyFromHcloud(sshKey), true, nil
}

func (client hcloudPersonalServerCloudClient) CreateSSHKey(ctx context.Context, sshKey personalServerSSHKey) (personalServerSSHKey, error) {
	created, _, err := client.client.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{
		Name:      sshKey.Name,
		PublicKey: sshKey.PublicKey,
		Labels:    sshKey.Labels,
	})
	if err != nil {
		return personalServerSSHKey{}, err
	}
	return personalServerSSHKeyFromHcloud(created), nil
}

func (client hcloudPersonalServerCloudClient) CreateServer(ctx context.Context, request personalServerCreateServerRequest) (personalServerCloudServer, []personalServerAction, error) {
	result, _, err := client.client.Server.Create(ctx, hcloud.ServerCreateOpts{
		Name:       request.Name,
		ServerType: &hcloud.ServerType{Name: request.ServerTypeName},
		Image:      &hcloud.Image{ID: int64(request.ImageID), Name: request.ImageName},
		SSHKeys:    []*hcloud.SSHKey{{ID: int64(request.SSHKeyID)}},
		Location:   &hcloud.Location{Name: request.LocationName},
		UserData:   request.UserData,
		Labels:     request.Labels,
		Firewalls: []*hcloud.ServerCreateFirewall{
			{Firewall: hcloud.Firewall{ID: int64(request.FirewallID)}},
		},
		PublicNet: &hcloud.ServerCreatePublicNet{
			EnableIPv4: request.EnableIPv4,
			EnableIPv6: request.EnableIPv6,
		},
	})
	if err != nil {
		return personalServerCloudServer{}, nil, err
	}

	actions := personalServerActionsFromHcloud(append([]*hcloud.Action{result.Action}, result.NextActions...))
	return personalServerCloudServerFromHcloud(result.Server), actions, nil
}

func (client hcloudPersonalServerCloudClient) WaitActions(ctx context.Context, actions []personalServerAction) error {
	if len(actions) == 0 {
		return nil
	}

	hcloudActions := make([]*hcloud.Action, 0, len(actions))
	for _, action := range actions {
		if action.ID == 0 {
			continue
		}
		status := hcloud.ActionStatus(action.Status)
		if status == "" {
			status = hcloud.ActionStatusRunning
		}
		hcloudActions = append(hcloudActions, &hcloud.Action{
			ID:     int64(action.ID),
			Status: status,
		})
	}
	if len(hcloudActions) == 0 {
		return nil
	}
	return client.client.Action.WaitFor(ctx, hcloudActions...)
}

func personalServerCloudServerFromHcloud(server *hcloud.Server) personalServerCloudServer {
	if server == nil {
		return personalServerCloudServer{}
	}
	result := personalServerCloudServer{
		ID:   int(server.ID),
		Name: server.Name,
	}
	if !server.PublicNet.IPv4.IsUnspecified() {
		result.IPv4 = server.PublicNet.IPv4.IP.String()
	}
	if !server.PublicNet.IPv6.IsUnspecified() {
		result.IPv6 = server.PublicNet.IPv6.IP.String()
	}
	return result
}

func personalServerFirewallFromHcloud(firewall *hcloud.Firewall) personalServerFirewall {
	if firewall == nil {
		return personalServerFirewall{}
	}
	result := personalServerFirewall{
		ID:     int(firewall.ID),
		Name:   firewall.Name,
		Labels: copyPersonalServerLabels(firewall.Labels),
		Rules:  make([]personalServerFirewallRule, 0, len(firewall.Rules)),
	}
	for _, rule := range firewall.Rules {
		result.Rules = append(result.Rules, personalServerFirewallRule{
			Direction: string(rule.Direction),
			Protocol:  string(rule.Protocol),
			Port:      stringValue(rule.Port),
			SourceIPs: ipNetStrings(rule.SourceIPs),
		})
	}
	return result
}

func personalServerSSHKeyFromHcloud(sshKey *hcloud.SSHKey) personalServerSSHKey {
	if sshKey == nil {
		return personalServerSSHKey{}
	}
	return personalServerSSHKey{
		ID:          int(sshKey.ID),
		Name:        sshKey.Name,
		Fingerprint: sshKey.Fingerprint,
		PublicKey:   sshKey.PublicKey,
		Labels:      copyPersonalServerLabels(sshKey.Labels),
	}
}

func personalServerActionsFromHcloud(actions []*hcloud.Action) []personalServerAction {
	result := make([]personalServerAction, 0, len(actions))
	for _, action := range actions {
		if action == nil {
			continue
		}
		result = append(result, personalServerAction{
			ID:     int(action.ID),
			Status: string(action.Status),
		})
	}
	return result
}

func hcloudFirewallRules(rules []personalServerFirewallRule) ([]hcloud.FirewallRule, error) {
	result := make([]hcloud.FirewallRule, 0, len(rules))
	for _, rule := range rules {
		sourceIPs, err := parsePersonalServerCIDRs(rule.SourceIPs)
		if err != nil {
			return nil, err
		}
		port := rule.Port
		result = append(result, hcloud.FirewallRule{
			Direction: hcloud.FirewallRuleDirection(rule.Direction),
			Protocol:  hcloud.FirewallRuleProtocol(rule.Protocol),
			Port:      &port,
			SourceIPs: sourceIPs,
		})
	}
	return result, nil
}

func parsePersonalServerCIDRs(values []string) ([]net.IPNet, error) {
	result := make([]net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("parse firewall source CIDR %q: %w", value, err)
		}
		result = append(result, *network)
	}
	return result, nil
}

func ipNetStrings(values []net.IPNet) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.String())
	}
	return result
}

func copyPersonalServerLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		out[key] = value
	}
	return out
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
