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

const defaultIdleLeaseDirectory = "/run/myn/idle/leases"

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

	if !opts.json {
		return renderHumanIdleStatus(w, report)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encode idle status: %w", err)
	}
	return nil
}

func renderHumanIdleStatus(w io.Writer, report idleStatusReport) error {
	var out strings.Builder
	fmt.Fprintf(&out, "Idle leases: %d active, %d idle, %d stale (%d total)\n",
		report.Counts.Active,
		report.Counts.Idle,
		report.Counts.Stale,
		report.Counts.Total,
	)
	fmt.Fprintf(&out, "Lease directory: %s\n", report.LeaseDirectory)
	if len(report.Leases) == 0 {
		out.WriteString("No idle lease files found.\n")
		_, err := io.WriteString(w, out.String())
		return err
	}

	for _, lease := range report.Leases {
		fmt.Fprintf(&out, "- %s [%s] %s: command=%s",
			lease.ID,
			humanField(lease.Kind),
			lease.State,
			humanField(lease.Command),
		)
		if lease.WorkingDirectory != "" {
			fmt.Fprintf(&out, " cwd=%s", lease.WorkingDirectory)
		}
		fmt.Fprintf(&out, " reason=%s\n", lease.Reason)
	}

	_, err := io.WriteString(w, out.String())
	return err
}

func humanField(value string) string {
	if value == "" {
		return "-"
	}
	return value
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
	if dir := strings.TrimSpace(env("MYN_LEASE_DIR")); dir != "" {
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

	return idleStatusLeaseFromEvaluation(lease, lease.evaluate(now, processAlive(lease.RootPID)))
}

func malformedIdleStatusLease(filename, reason string) idleStatusLease {
	return idleStatusLease{
		ID:     strings.TrimSuffix(filename, filepath.Ext(filename)),
		State:  idleLeaseStateStale,
		Reason: reason,
	}
}

func idleStatusLeaseFromEvaluation(lease idleLease, evaluation idleLeaseEvaluation) idleStatusLease {
	return idleStatusLease{
		ID:               lease.ID,
		Kind:             lease.Kind,
		State:            evaluation.State,
		Reason:           idleLeaseEvaluationReasonText(evaluation.Reason),
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
}

func idleLeaseEvaluationReasonText(reason idleLeaseEvaluationReason) string {
	switch reason {
	case idleLeaseEvaluationReasonInvalidIdleAfter:
		return "invalid idleAfter"
	case idleLeaseEvaluationReasonMissingExpiresAt:
		return "missing expiresAt"
	case idleLeaseEvaluationReasonMissingHeartbeat:
		return "missing heartbeat"
	case idleLeaseEvaluationReasonLeaseExpired:
		return "lease expired"
	case idleLeaseEvaluationReasonHeartbeatTooOld:
		return "heartbeat is too old"
	case idleLeaseEvaluationReasonRootProcessNotRunning:
		return "root process is not running"
	case idleLeaseEvaluationReasonNoTerminalActivity:
		return "no terminal activity recorded"
	case idleLeaseEvaluationReasonTerminalActivityOlderThanIdleWindow:
		return "terminal activity older than idle window"
	case idleLeaseEvaluationReasonTerminalActivityWithinIdleWindow:
		return "terminal activity within idle window"
	default:
		return "unknown"
	}
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
