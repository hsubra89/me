package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const defaultIdleLeaseDirectory = "/run/me/idle/leases"

type idleStatusOptions struct {
	json bool
}

type idleStatusDeps struct {
	env          func(string) string
	now          func() time.Time
	processAlive func(int) bool
}

func newIdleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "idle",
		Short: "Inspect Personal Server idle state",
	}
	cmd.AddCommand(newIdleStatusCommand())
	return cmd
}

func newIdleStatusCommand() *cobra.Command {
	var opts idleStatusOptions
	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Report Idle Lease state",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdleStatus(cmd.OutOrStdout(), opts, idleStatusDeps{})
		},
	}
	cmd.Flags().BoolVar(&opts.json, "json", false, "emit machine-readable JSON")
	return cmd
}

func runIdleStatus(w io.Writer, opts idleStatusOptions, deps idleStatusDeps) error {
	if !opts.json {
		return errors.New("human-readable idle status is not implemented yet; use --json")
	}
	if deps.env == nil {
		deps.env = os.Getenv
	}
	if deps.now == nil {
		deps.now = func() time.Time {
			return time.Now().UTC()
		}
	}
	if deps.processAlive == nil {
		deps.processAlive = idleProcessAlive
	}

	report, err := readIdleStatusReport(deps)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encode idle status: %w", err)
	}
	return nil
}

func readIdleStatusReport(deps idleStatusDeps) (idleStatusReport, error) {
	now := deps.now().UTC()
	leaseDir, err := resolveIdleLeaseDirectory(deps.env)
	if err != nil {
		return idleStatusReport{}, err
	}
	if err := ensureIdleLeaseDirectory(leaseDir); err != nil {
		return idleStatusReport{}, err
	}

	entries, err := os.ReadDir(leaseDir)
	if err != nil {
		return idleStatusReport{}, fmt.Errorf("read idle lease directory %s: %w", leaseDir, err)
	}

	report := idleStatusReport{
		LeaseDirectory: leaseDir,
		Now:            now,
		Leases:         []idleStatusLease{},
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		lease := readAndEvaluateIdleLease(filepath.Join(leaseDir, entry.Name()), entry.Name(), now, deps.processAlive)
		report.Leases = append(report.Leases, lease)
	}
	sort.Slice(report.Leases, func(i, j int) bool {
		return report.Leases[i].ID < report.Leases[j].ID
	})
	for _, lease := range report.Leases {
		switch lease.State {
		case idleLeaseStateActive:
			report.Counts.Active++
		case idleLeaseStateIdle:
			report.Counts.Idle++
		case idleLeaseStateStale:
			report.Counts.Stale++
		}
	}
	report.Counts.Total = len(report.Leases)
	return report, nil
}

func resolveIdleLeaseDirectory(env func(string) string) (string, error) {
	if dir := strings.TrimSpace(env("ME_LEASE_DIR")); dir != "" {
		return dir, nil
	}
	return defaultIdleLeaseDirectory, nil
}

func ensureIdleLeaseDirectory(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("create idle lease directory %s: %w; Personal Server Bootstrap or systemd must create the runtime lease directory", dir, err)
		}
		return fmt.Errorf("create idle lease directory %s: %w", dir, err)
	}
	return nil
}

func readAndEvaluateIdleLease(path, filename string, now time.Time, processAlive func(int) bool) idleStatusLease {
	data, err := os.ReadFile(path)
	if err != nil {
		return malformedIdleStatusLease(filename, fmt.Sprintf("read lease file: %v", err))
	}

	var lease idleLease
	if err := json.Unmarshal(data, &lease); err != nil {
		return malformedIdleStatusLease(filename, fmt.Sprintf("malformed lease JSON: %v", err))
	}
	if lease.ID == "" {
		lease.ID = strings.TrimSuffix(filename, filepath.Ext(filename))
	}

	return evaluateIdleLease(lease, now, processAlive)
}

func malformedIdleStatusLease(filename, reason string) idleStatusLease {
	return idleStatusLease{
		ID:     strings.TrimSuffix(filename, filepath.Ext(filename)),
		State:  idleLeaseStateStale,
		Reason: reason,
	}
}

func evaluateIdleLease(lease idleLease, now time.Time, processAlive func(int) bool) idleStatusLease {
	report := idleStatusLease{
		ID:               lease.ID,
		Kind:             lease.Kind,
		RootPID:          lease.RootPID,
		ProcessGroup:     lease.ProcessGroup,
		User:             lease.User,
		WorkingDirectory: lease.WorkingDirectory,
		Command:          lease.Command,
		Interactive:      lease.Interactive,
		StartedAt:        lease.StartedAt,
		UpdatedAt:        lease.UpdatedAt,
		LastInputAt:      lease.LastInputAt,
		LastOutputAt:     lease.LastOutputAt,
		IdleAfter:        lease.IdleAfter,
		ExpiresAt:        lease.ExpiresAt,
	}

	idleAfter := time.Duration(lease.IdleAfter)
	if idleAfter <= 0 {
		report.State = idleLeaseStateStale
		report.Reason = "invalid idleAfter"
		return report
	}
	if lease.ExpiresAt.IsZero() {
		report.State = idleLeaseStateStale
		report.Reason = "missing expiresAt"
		return report
	}
	if lease.UpdatedAt.IsZero() {
		report.State = idleLeaseStateStale
		report.Reason = "missing heartbeat"
		return report
	}
	if now.After(lease.ExpiresAt) {
		report.State = idleLeaseStateStale
		report.Reason = "lease expired"
		return report
	}
	if now.Sub(lease.UpdatedAt) > idleHeartbeatStaleAfter(idleAfter) {
		report.State = idleLeaseStateStale
		report.Reason = "heartbeat is too old"
		return report
	}
	if !processAlive(lease.RootPID) {
		report.State = idleLeaseStateStale
		report.Reason = "root process is not running"
		return report
	}

	lastActivity, ok := latestTerminalActivity(lease)
	if !ok {
		report.State = idleLeaseStateIdle
		report.Reason = "no terminal activity recorded"
		return report
	}
	if !lastActivity.Before(now.Add(-idleAfter)) {
		report.State = idleLeaseStateActive
		report.Reason = "terminal activity within idle window"
		return report
	}

	report.State = idleLeaseStateIdle
	report.Reason = "terminal activity older than idle window"
	return report
}

func latestTerminalActivity(lease idleLease) (time.Time, bool) {
	var latest time.Time
	if lease.LastInputAt != nil {
		latest = *lease.LastInputAt
	}
	if lease.LastOutputAt != nil && lease.LastOutputAt.After(latest) {
		latest = *lease.LastOutputAt
	}
	if latest.IsZero() {
		return time.Time{}, false
	}
	return latest, true
}

func idleHeartbeatStaleAfter(idleAfter time.Duration) time.Duration {
	staleAfter := idleAfter * 2
	if staleAfter < 5*time.Minute {
		return 5 * time.Minute
	}
	return staleAfter
}

type idleLeaseState string

const (
	idleLeaseStateActive idleLeaseState = "active"
	idleLeaseStateIdle   idleLeaseState = "idle"
	idleLeaseStateStale  idleLeaseState = "stale"
)

type idleLease struct {
	Kind             string            `json:"kind"`
	ID               string            `json:"id"`
	RootPID          int               `json:"rootPid"`
	ProcessGroup     int               `json:"processGroup"`
	User             string            `json:"user"`
	WorkingDirectory string            `json:"workingDirectory"`
	Command          string            `json:"command"`
	Interactive      bool              `json:"interactive"`
	StartedAt        time.Time         `json:"startedAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
	LastInputAt      *time.Time        `json:"lastInputAt,omitempty"`
	LastOutputAt     *time.Time        `json:"lastOutputAt,omitempty"`
	IdleAfter        idleLeaseDuration `json:"idleAfter"`
	ExpiresAt        time.Time         `json:"expiresAt"`
}

type idleStatusReport struct {
	LeaseDirectory string            `json:"leaseDirectory"`
	Now            time.Time         `json:"now"`
	Counts         idleStatusCounts  `json:"counts"`
	Leases         []idleStatusLease `json:"leases"`
}

type idleStatusCounts struct {
	Active int `json:"active"`
	Idle   int `json:"idle"`
	Stale  int `json:"stale"`
	Total  int `json:"total"`
}

type idleStatusLease struct {
	ID               string            `json:"id"`
	Kind             string            `json:"kind,omitempty"`
	State            idleLeaseState    `json:"state"`
	Reason           string            `json:"reason"`
	Command          string            `json:"command,omitempty"`
	WorkingDirectory string            `json:"workingDirectory,omitempty"`
	RootPID          int               `json:"rootPid,omitempty"`
	ProcessGroup     int               `json:"processGroup,omitempty"`
	User             string            `json:"user,omitempty"`
	Interactive      bool              `json:"interactive,omitempty"`
	StartedAt        time.Time         `json:"startedAt,omitempty"`
	UpdatedAt        time.Time         `json:"updatedAt,omitempty"`
	LastInputAt      *time.Time        `json:"lastInputAt,omitempty"`
	LastOutputAt     *time.Time        `json:"lastOutputAt,omitempty"`
	IdleAfter        idleLeaseDuration `json:"idleAfter,omitempty"`
	ExpiresAt        time.Time         `json:"expiresAt,omitempty"`
}

type idleLeaseDuration time.Duration

func (d *idleLeaseDuration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("idleAfter must be a duration string: %w", err)
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("parse idleAfter: %w", err)
	}
	*d = idleLeaseDuration(duration)
	return nil
}

func (d idleLeaseDuration) MarshalJSON() ([]byte, error) {
	if d == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(time.Duration(d).String())
}
