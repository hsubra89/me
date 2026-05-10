package cli

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
)

const (
	sshKeyTypeEd25519             = "ssh-ed25519"
	primaryGeneratedSSHIdentity   = ".ssh/id_ed25519"
	fallbackGeneratedSSHIdentity  = ".ssh/id_me_25519"
	generateSSHIdentityChoiceText = "Generate a new Ed25519 keypair"
)

type sshPublicKey struct {
	KeyType string
	Body    string
	Comment string
}

type sshIdentityCandidate struct {
	IdentityFile string
	IdentityPath string
	PublicPath   string
	PublicKey    sshPublicKey
	Fingerprint  string
	Warning      string
}

type sshIdentityPromptChoice struct {
	Label    string
	Identity sshIdentityCandidate
	Generate bool
}

type sshGenerationTarget struct {
	IdentityFile string
	IdentityPath string
	Recovered    *sshIdentityCandidate
}

type sshIdentityNotConfiguredError struct {
	reason string
}

func (err sshIdentityNotConfiguredError) Error() string {
	if err.reason != "" {
		return err.reason
	}
	return "SSH identity is not configured"
}

func discoverSSHIdentities(home string, deps configureDeps) ([]sshIdentityCandidate, error) {
	sshDir := filepath.Join(home, ".ssh")
	entries, err := deps.readDir(sshDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read ~/.ssh: %w", err)
	}

	var identities []sshIdentityCandidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pub") {
			continue
		}

		publicPath := filepath.Join(sshDir, entry.Name())
		publicKey, err := readSSHPublicKeyFile(publicPath, deps)
		if err != nil || publicKey.KeyType != sshKeyTypeEd25519 {
			continue
		}

		identityPath := strings.TrimSuffix(publicPath, ".pub")
		info, err := deps.stat(identityPath)
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			continue
		}

		identityFile, _, err := normalizeSSHIdentityFile(identityPath, home, deps.stat)
		if err != nil {
			continue
		}

		fingerprint, err := sshPublicKeyFingerprint(publicKey)
		if err != nil {
			continue
		}

		identities = append(identities, sshIdentityCandidate{
			IdentityFile: identityFile,
			IdentityPath: identityPath,
			PublicPath:   publicPath,
			PublicKey:    publicKey,
			Fingerprint:  fingerprint,
			Warning:      sshIdentityPermissionWarning(identityFile, info),
		})
	}

	sort.SliceStable(identities, func(i, j int) bool {
		return identities[i].IdentityFile < identities[j].IdentityFile
	})
	return identities, nil
}

func loadSSHIdentity(input string, home string, deps configureDeps) (sshIdentityCandidate, error) {
	identityFile, identityPath, err := normalizeSSHIdentityFile(input, home, deps.stat)
	if err != nil {
		return sshIdentityCandidate{}, err
	}

	info, err := deps.stat(identityPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sshIdentityCandidate{}, fmt.Errorf("SSH identity file does not exist")
		}
		return sshIdentityCandidate{}, fmt.Errorf("check SSH identity file: %w", err)
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return sshIdentityCandidate{}, fmt.Errorf("SSH identity file must be a regular file")
	}

	publicPath := identityPath + ".pub"
	publicKey, err := readSSHPublicKeyFile(publicPath, deps)
	if errors.Is(err, os.ErrNotExist) {
		publicKey, err = regenerateSSHPublicKey(identityPath, publicPath, deps)
	}
	if err != nil {
		return sshIdentityCandidate{}, err
	}
	if publicKey.KeyType != sshKeyTypeEd25519 {
		return sshIdentityCandidate{}, fmt.Errorf("SSH identity must be an Ed25519 keypair")
	}

	if generated, err := deps.sshPublicKey(identityPath); err == nil {
		generatedPublicKey, err := parseSSHPublicKey(generated)
		if err != nil {
			return sshIdentityCandidate{}, fmt.Errorf("parse generated public key: %w", err)
		}
		if generatedPublicKey.KeyType != publicKey.KeyType || generatedPublicKey.Body != publicKey.Body {
			return sshIdentityCandidate{}, fmt.Errorf("SSH public key does not match private key")
		}
	}

	fingerprint, err := sshPublicKeyFingerprint(publicKey)
	if err != nil {
		return sshIdentityCandidate{}, err
	}

	return sshIdentityCandidate{
		IdentityFile: identityFile,
		IdentityPath: identityPath,
		PublicPath:   publicPath,
		PublicKey:    publicKey,
		Fingerprint:  fingerprint,
		Warning:      sshIdentityPermissionWarning(identityFile, info),
	}, nil
}

func normalizeSSHIdentityFile(input string, home string, stat func(string) (os.FileInfo, error)) (string, string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", "", fmt.Errorf("SSH identity file is required")
	}
	if strings.HasSuffix(value, ".pub") {
		return "", "", fmt.Errorf("SSH identity file must be the private key path, not the .pub file")
	}
	if stat == nil {
		stat = os.Stat
	}

	candidate, err := localProjectRootPath(value, home)
	if err != nil {
		message := strings.NewReplacer("local project root", "SSH identity file").Replace(err.Error())
		return "", "", fmt.Errorf("%s", message)
	}

	relative, err := relativeSubdirectory(candidate, home, "SSH identity file")
	if err != nil {
		return "", "", err
	}

	return filepath.ToSlash(relative), candidate, nil
}

func readSSHPublicKeyFile(path string, deps configureDeps) (sshPublicKey, error) {
	data, err := deps.readFile(path)
	if err != nil {
		return sshPublicKey{}, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return parseSSHPublicKey(line)
	}
	return sshPublicKey{}, fmt.Errorf("SSH public key is empty")
}

func regenerateSSHPublicKey(identityPath string, publicPath string, deps configureDeps) (sshPublicKey, error) {
	output, err := deps.sshPublicKey(identityPath)
	if err != nil {
		return sshPublicKey{}, fmt.Errorf("regenerate SSH public key: %w", err)
	}

	publicKey, err := parseSSHPublicKey(output)
	if err != nil {
		return sshPublicKey{}, fmt.Errorf("parse regenerated SSH public key: %w", err)
	}
	if publicKey.KeyType != sshKeyTypeEd25519 {
		return sshPublicKey{}, fmt.Errorf("SSH identity must be an Ed25519 keypair")
	}

	line := strings.TrimSpace(output)
	if err := deps.writeFile(publicPath, []byte(line+"\n"), 0o644); err != nil {
		return sshPublicKey{}, fmt.Errorf("write regenerated SSH public key: %w", err)
	}
	if err := deps.chmod(publicPath, 0o644); err != nil {
		return sshPublicKey{}, fmt.Errorf("secure regenerated SSH public key: %w", err)
	}

	return publicKey, nil
}

func parseSSHPublicKey(line string) (sshPublicKey, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 {
		return sshPublicKey{}, fmt.Errorf("SSH public key must include key type and key body")
	}
	if fields[0] != sshKeyTypeEd25519 {
		return sshPublicKey{}, fmt.Errorf("SSH public key must be Ed25519")
	}
	if _, err := decodeOpenSSHBase64(fields[1]); err != nil {
		return sshPublicKey{}, fmt.Errorf("SSH public key body is invalid: %w", err)
	}

	return sshPublicKey{
		KeyType: fields[0],
		Body:    fields[1],
		Comment: strings.Join(fields[2:], " "),
	}, nil
}

func sshPublicKeyFingerprint(publicKey sshPublicKey) (string, error) {
	decoded, err := decodeOpenSSHBase64(publicKey.Body)
	if err != nil {
		return "", fmt.Errorf("SSH public key body is invalid: %w", err)
	}
	sum := sha256.Sum256(decoded)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

func decodeOpenSSHBase64(value string) ([]byte, error) {
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}

	padded := value
	if remainder := len(padded) % 4; remainder != 0 {
		padded += strings.Repeat("=", 4-remainder)
	}
	return base64.StdEncoding.DecodeString(padded)
}

func sshIdentityPermissionWarning(identityFile string, info os.FileInfo) string {
	if info.Mode().Perm()&0o077 == 0 {
		return ""
	}
	return fmt.Sprintf("Warning: SSH identity ~/%s permissions are broader than recommended.", filepath.ToSlash(identityFile))
}

func sshIdentityLabel(candidate sshIdentityCandidate) string {
	label := fmt.Sprintf("~/%s", filepath.ToSlash(candidate.IdentityFile))
	if candidate.PublicKey.Comment != "" {
		return fmt.Sprintf("%s (%s)", label, candidate.PublicKey.Comment)
	}
	if candidate.Fingerprint != "" {
		return fmt.Sprintf("%s (%s)", label, candidate.Fingerprint)
	}
	return label
}

func dedupeSSHIdentityChoices(candidates []sshIdentityCandidate) []sshIdentityPromptChoice {
	seen := make(map[string]bool, len(candidates))
	choices := make([]sshIdentityPromptChoice, 0, len(candidates)+1)
	for _, candidate := range candidates {
		if seen[candidate.IdentityFile] {
			continue
		}
		seen[candidate.IdentityFile] = true
		choices = append(choices, sshIdentityPromptChoice{
			Label:    sshIdentityLabel(candidate),
			Identity: candidate,
		})
	}
	return choices
}

func generatedSSHIdentityTarget(home string, deps configureDeps) (sshGenerationTarget, error) {
	for _, identityFile := range []string{primaryGeneratedSSHIdentity, fallbackGeneratedSSHIdentity} {
		identityPath := filepath.Join(home, filepath.FromSlash(identityFile))
		publicPath := identityPath + ".pub"

		privateExists, err := pathExists(identityPath, deps.stat)
		if err != nil {
			return sshGenerationTarget{}, err
		}
		publicExists, err := pathExists(publicPath, deps.stat)
		if err != nil {
			return sshGenerationTarget{}, err
		}

		if !privateExists && !publicExists {
			return sshGenerationTarget{
				IdentityFile: identityFile,
				IdentityPath: identityPath,
			}, nil
		}

		if privateExists && !publicExists {
			candidate, err := loadSSHIdentity(identityFile, home, deps)
			if err == nil {
				return sshGenerationTarget{Recovered: &candidate}, nil
			}
		}
	}

	return sshGenerationTarget{}, fmt.Errorf("SSH key generation targets ~/.ssh/id_ed25519 and ~/.ssh/id_me_25519 are already occupied")
}

func pathExists(path string, stat func(string) (os.FileInfo, error)) (bool, error) {
	_, err := stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("check %s: %w", path, err)
}

func generatedSSHKeyComment(deps configureDeps) string {
	username := strings.TrimSpace(deps.currentUsername())
	if username == "" {
		username = "me"
	}
	username = strings.TrimPrefix(username, `.\`)
	if index := strings.LastIndexAny(username, `\/`); index >= 0 && index < len(username)-1 {
		username = username[index+1:]
	}

	hostname, err := deps.hostname()
	hostname = strings.TrimSpace(hostname)
	if err != nil || hostname == "" {
		hostname = "localhost"
	}

	return fmt.Sprintf("%s@%s", username, hostname)
}

func currentOSUsername() string {
	current, err := user.Current()
	if err == nil && strings.TrimSpace(current.Username) != "" {
		return current.Username
	}
	if value := strings.TrimSpace(os.Getenv("USER")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("USERNAME")); value != "" {
		return value
	}
	return "me"
}
