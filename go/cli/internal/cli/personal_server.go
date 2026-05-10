package cli

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
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
	return gate.previewPersonalServerCreation(out, token, prompter)
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
		return gate.previewPersonalServerCreation(out, token, prompter)
	}

	fmt.Fprintf(out, "Personal Server already configured: server %d exists.\n", cfg.PersonalServer.ServerID)
	fmt.Fprintf(out, "Saved addresses: %s\n", formatPersonalServerAddresses(cfg.PersonalServer.IPv4, cfg.PersonalServer.IPv6))
	fmt.Fprintf(out, "Current addresses: %s\n", formatPersonalServerAddresses(server.IPv4, server.IPv6))
	return nil
}

func (gate personalServerProvisioningGate) previewPersonalServerCreation(out io.Writer, token string, prompter configurePrompter) error {
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
	for _, pricing := range serverType.Pricings {
		if pricing.LocationName != locationName {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(pricing.MonthlyGrossEUR), 64)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
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
