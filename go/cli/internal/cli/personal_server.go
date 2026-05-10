package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/GehirnInc/crypt/sha512_crypt"
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

type personalServerPreviewCloudClient interface {
	personalServerCloudClient
	Locations(ctx context.Context) ([]personalServerLocation, error)
	ServerTypes(ctx context.Context) ([]personalServerType, error)
}

type personalServerCloudServer struct {
	ID   int
	IPv4 string
	IPv6 string
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
	Location          personalServerLocation
	ServerType        personalServerType
	User              string
	ServerName        string
	PasswordHash      string
	GitIdentity       personalServerGitIdentity
	RemoteProjectRoot string
	SSHIdentityFile   string
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
	currentUsername    func() string
	gitConfigValue     func(scope personalServerGitConfigScope, key string) (string, bool)
	passwordSaltReader io.Reader
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
	return gate.previewPersonalServerCreation(out, token, cfg, prompter)
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
		return gate.previewPersonalServerCreation(out, token, cfg, prompter)
	}

	fmt.Fprintf(out, "Personal Server already configured: server %d exists.\n", cfg.PersonalServer.ServerID)
	fmt.Fprintf(out, "Saved addresses: %s\n", formatPersonalServerAddresses(cfg.PersonalServer.IPv4, cfg.PersonalServer.IPv6))
	fmt.Fprintf(out, "Current addresses: %s\n", formatPersonalServerAddresses(server.IPv4, server.IPv6))
	return nil
}

func (gate personalServerProvisioningGate) previewPersonalServerCreation(out io.Writer, token string, cfg appConfig, prompter configurePrompter) error {
	client, ok := gate.cloudClient(token).(personalServerPreviewCloudClient)
	if !ok {
		return fmt.Errorf("Personal Server preview requires a Hetzner client that can list Locations and Server Types")
	}
	ctx := context.Background()

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
		writePersonalServerCreationPlan(out, plan)

		create, err := prompter.Confirm("Create Personal Server?", false)
		if err != nil {
			return err
		}
		if !create {
			fmt.Fprintln(out, "Personal Server creation declined. No cloud resources were created.")
			return nil
		}

		return fmt.Errorf("Personal Server creation is not implemented yet")
	}
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
	fmt.Fprintln(out, "Firewall: me-personal-server (inbound SSH over IPv4 and IPv6)")
	fmt.Fprintln(out, "Public network: IPv4 and IPv6 enabled")
	fmt.Fprintf(out, "Remote project root: ~/%s\n", plan.RemoteProjectRoot)
	fmt.Fprintln(out, "Install plan:")
	fmt.Fprintln(out, "System services:")
	fmt.Fprintln(out, "- security updates and unattended security upgrades")
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
	if price, ok := personalServerTypeMonthlyGrossText(plan.ServerType, plan.Location.Name); ok {
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
	price, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return price, true
}

func personalServerTypeMonthlyGrossText(serverType personalServerType, locationName string) (string, bool) {
	for _, pricing := range serverType.Pricings {
		if pricing.LocationName != locationName {
			continue
		}
		value := strings.TrimSpace(pricing.MonthlyGrossEUR)
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return "", false
		}
		return value, true
	}
	return "", false
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
