package install_test

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallerInstallsPinnedLinuxAmd64Release(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	env.writeReleaseArchive(t, "0.1.0", "linux", "amd64", "#!/bin/sh\necho 'myn 0.1.0'\necho 'commit: test'\necho 'date: test'\n")
	env.writeFakeCurl(t)
	env.writeFakeUname(t, "Linux", "x86_64")

	result := env.runInstaller(t, "MYN_VERSION=0.1.0")
	if result.err != nil {
		t.Fatalf("install failed: %v\n%s", result.err, result.output)
	}

	installed := filepath.Join(env.home, ".local", "bin", "myn")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("installed myn missing: %v", err)
	}
	versionOutput, err := exec.Command(installed, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("run installed myn: %v\n%s", err, versionOutput)
	}
	if !strings.Contains(string(versionOutput), "myn 0.1.0") {
		t.Fatalf("installed version output mismatch:\n%s", versionOutput)
	}
	if !strings.Contains(result.output, "Installed myn to "+installed) {
		t.Fatalf("install output should report installed path:\n%s", result.output)
	}
}

func TestInstallerDefaultsToLatestStableRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	env.writeReleaseArchive(t, "0.1.0", "linux", "amd64", "#!/bin/sh\necho 'myn 0.1.0'\n")
	env.writeFakeCurl(t)
	env.writeFakeUname(t, "Linux", "x86_64")

	result := env.runInstaller(t)
	if result.err != nil {
		t.Fatalf("install failed: %v\n%s", result.err, result.output)
	}

	if !strings.Contains(result.output, "Installing myn 0.1.0") {
		t.Fatalf("install output should report latest resolved version:\n%s", result.output)
	}
}

func TestInstallerFallsBackToWgetWhenCurlIsUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	env.writeReleaseArchive(t, "0.1.0", "linux", "amd64", "#!/bin/sh\necho 'myn 0.1.0'\n")
	env.writeFakeWget(t)
	env.writeFakeUname(t, "Linux", "x86_64")
	env.linkRequiredTools(t, "mktemp", "tar", "gzip", "grep", "sed", "mkdir", "rm", "mv", "chmod", "sha256sum")

	result := env.runInstallerWithPath(t, env.fakeBin, "MYN_VERSION=0.1.0")
	if result.err != nil {
		t.Fatalf("install failed: %v\n%s", result.err, result.output)
	}
	if !strings.Contains(result.output, "Installed myn to ") {
		t.Fatalf("install output should report success through wget fallback:\n%s", result.output)
	}
}

func TestInstallerInstallsExplicitPrereleaseVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	env.writeReleaseArchive(t, "0.2.0-alpha.1", "linux", "amd64", "#!/bin/sh\necho 'myn 0.2.0-alpha.1'\n")
	env.writeFakeCurl(t)
	env.writeFakeUname(t, "Linux", "x86_64")

	result := env.runInstaller(t, "MYN_VERSION=v0.2.0-alpha.1")
	if result.err != nil {
		t.Fatalf("install failed: %v\n%s", result.err, result.output)
	}

	if !strings.Contains(result.output, "Downloading myn_0.2.0-alpha.1_linux_amd64.tar.gz") {
		t.Fatalf("install output should report prerelease asset without leading v:\n%s", result.output)
	}
}

func TestInstallerMapsDarwinArm64ReleaseAsset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	env.writeReleaseArchive(t, "0.1.0", "darwin", "arm64", "#!/bin/sh\necho 'myn 0.1.0'\n")
	env.writeFakeCurl(t)
	env.writeFakeUname(t, "Darwin", "arm64")

	result := env.runInstaller(t, "MYN_VERSION=0.1.0")
	if result.err != nil {
		t.Fatalf("install failed: %v\n%s", result.err, result.output)
	}

	if !strings.Contains(result.output, "Downloading myn_0.1.0_darwin_arm64.tar.gz") {
		t.Fatalf("install output should report darwin arm64 asset:\n%s", result.output)
	}
}

func TestInstallerFailsForUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	env.writeReleaseArchive(t, "0.1.0", "linux", "amd64", "#!/bin/sh\necho 'myn 0.1.0'\n")
	env.writeFakeCurl(t)
	env.writeFakeUname(t, "FreeBSD", "x86_64")

	result := env.runInstaller(t, "MYN_VERSION=0.1.0")
	if result.err == nil {
		t.Fatalf("install should fail for unsupported OS:\n%s", result.output)
	}
	if !strings.Contains(result.output, "unsupported OS: FreeBSD") {
		t.Fatalf("install output should explain unsupported OS:\n%s", result.output)
	}
}

func TestInstallerFailsWhenChecksumDoesNotMatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	env.writeReleaseArchive(t, "0.1.0", "linux", "amd64", "#!/bin/sh\necho 'myn 0.1.0'\n")
	env.writeCorruptChecksum(t, "myn_0.1.0_linux_amd64.tar.gz")
	env.writeFakeCurl(t)
	env.writeFakeUname(t, "Linux", "x86_64")

	result := env.runInstaller(t, "MYN_VERSION=0.1.0")
	if result.err == nil {
		t.Fatalf("install should fail when checksum does not match:\n%s", result.output)
	}
	if !strings.Contains(result.output, "checksum verification failed for myn_0.1.0_linux_amd64.tar.gz") {
		t.Fatalf("install output should explain checksum failure:\n%s", result.output)
	}
	if _, err := os.Stat(filepath.Join(env.home, ".local", "bin", "myn")); !os.IsNotExist(err) {
		t.Fatalf("myn should not be installed after checksum failure, stat err: %v", err)
	}
}

func TestInstallerOverwritesExistingInstalledBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	installDir := filepath.Join(env.home, ".local", "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("create install dir: %v", err)
	}
	oldBinary := filepath.Join(installDir, "myn")
	if err := os.WriteFile(oldBinary, []byte("#!/bin/sh\necho 'myn old'\n"), 0o755); err != nil {
		t.Fatalf("write old binary: %v", err)
	}

	env.writeReleaseArchive(t, "0.1.0", "linux", "amd64", "#!/bin/sh\necho 'myn new'\n")
	env.writeFakeCurl(t)
	env.writeFakeUname(t, "Linux", "x86_64")

	result := env.runInstaller(t, "MYN_VERSION=0.1.0")
	if result.err != nil {
		t.Fatalf("install failed: %v\n%s", result.err, result.output)
	}

	versionOutput, err := exec.Command(oldBinary, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("run installed myn: %v\n%s", err, versionOutput)
	}
	if !strings.Contains(string(versionOutput), "myn new") {
		t.Fatalf("installer should overwrite existing binary, got:\n%s", versionOutput)
	}
}

func TestInstallerWarnsWhenInstallDirectoryIsNotOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	env.writeReleaseArchive(t, "0.1.0", "linux", "amd64", "#!/bin/sh\necho 'myn 0.1.0'\n")
	env.writeFakeCurl(t)
	env.writeFakeUname(t, "Linux", "x86_64")

	result := env.runInstaller(t, "MYN_VERSION=0.1.0")
	if result.err != nil {
		t.Fatalf("install failed: %v\n%s", result.err, result.output)
	}

	want := "Add " + filepath.Join(env.home, ".local", "bin") + " to PATH"
	if !strings.Contains(result.output, want) {
		t.Fatalf("install output should warn when install dir is not on PATH:\n%s", result.output)
	}
}

func TestInstallerRejectsInstallDirectoryOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	env.writeReleaseArchive(t, "0.1.0", "linux", "amd64", "#!/bin/sh\necho 'myn 0.1.0'\n")
	env.writeFakeCurl(t)
	env.writeFakeUname(t, "Linux", "x86_64")

	result := env.runInstaller(t, "MYN_VERSION=0.1.0", "MYN_INSTALL_DIR="+filepath.Join(env.root, "custom-bin"))
	if result.err == nil {
		t.Fatalf("installer should reject install directory override:\n%s", result.output)
	}
	if !strings.Contains(result.output, "MYN_INSTALL_DIR is not supported") {
		t.Fatalf("install output should explain unsupported install dir override:\n%s", result.output)
	}
}

func TestInstallerRejectsInvalidPinnedVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer is a shell script")
	}

	env := newInstallerTestEnv(t)
	env.writeReleaseArchive(t, "0.1.0", "linux", "amd64", "#!/bin/sh\necho 'myn 0.1.0'\n")
	env.writeFakeCurl(t)
	env.writeFakeUname(t, "Linux", "x86_64")

	result := env.runInstaller(t, "MYN_VERSION=latest")
	if result.err == nil {
		t.Fatalf("installer should reject invalid pinned version:\n%s", result.output)
	}
	if !strings.Contains(result.output, "release version must be vMAJOR.MINOR.PATCH") {
		t.Fatalf("install output should explain invalid version:\n%s", result.output)
	}
}

type installerTestEnv struct {
	root    string
	home    string
	fakeBin string
	release string
}

type installerResult struct {
	output string
	err    error
}

func newInstallerTestEnv(t *testing.T) installerTestEnv {
	t.Helper()
	root := t.TempDir()
	env := installerTestEnv{
		root:    root,
		home:    filepath.Join(root, "home"),
		fakeBin: filepath.Join(root, "bin"),
		release: filepath.Join(root, "release"),
	}
	for _, dir := range []string{env.home, env.fakeBin, env.release} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create test dir %s: %v", dir, err)
		}
	}
	return env
}

func (env installerTestEnv) runInstaller(t *testing.T, extraEnv ...string) installerResult {
	t.Helper()
	return env.runInstallerWithPath(t, env.fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"), extraEnv...)
}

func (env installerTestEnv) runInstallerWithPath(t *testing.T, path string, extraEnv ...string) installerResult {
	t.Helper()
	script := filepath.Clean(filepath.Join("..", "..", "install.sh"))
	cmd := exec.Command("sh", script)
	cmd.Env = append(os.Environ(),
		"HOME="+env.home,
		"PATH="+path,
		"MYN_TEST_RELEASE_DIR="+env.release,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	output, err := cmd.CombinedOutput()
	return installerResult{output: string(output), err: err}
}

func (env installerTestEnv) writeReleaseArchive(t *testing.T, version string, goos string, goarch string, binary string) {
	t.Helper()
	archiveName := fmt.Sprintf("myn_%s_%s_%s.tar.gz", version, goos, goarch)
	archivePath := filepath.Join(env.release, archiveName)

	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	data := []byte(binary)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "myn",
		Mode: 0o755,
		Size: int64(len(data)),
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarWriter.Write(data); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	contents, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	sum := sha256.Sum256(contents)
	checksumLine := fmt.Sprintf("%x  %s\n", sum, archiveName)
	if err := os.WriteFile(filepath.Join(env.release, "checksums.txt"), []byte(checksumLine), 0o644); err != nil {
		t.Fatalf("write checksums: %v", err)
	}
}

func (env installerTestEnv) writeCorruptChecksum(t *testing.T, archiveName string) {
	t.Helper()
	line := fmt.Sprintf("%064x  %s\n", 0, archiveName)
	if err := os.WriteFile(filepath.Join(env.release, "checksums.txt"), []byte(line), 0o644); err != nil {
		t.Fatalf("write corrupt checksums: %v", err)
	}
}

func (env installerTestEnv) writeFakeCurl(t *testing.T) {
	t.Helper()
	script := `#!/bin/sh
set -eu
out=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      out="$2"
      shift 2
      ;;
    -f|-s|-S|-L|-I)
      shift
      ;;
    -*)
      shift
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done

name="${url##*/}"
case "$url" in
  */releases/latest)
    printf '{"tag_name":"v0.1.0"}'
    ;;
  */checksums.txt)
    cp "$MYN_TEST_RELEASE_DIR/checksums.txt" "$out"
    ;;
  */myn_*.tar.gz)
    cp "$MYN_TEST_RELEASE_DIR/$name" "$out"
    ;;
  *)
    echo "unexpected curl URL: $url" >&2
    exit 1
    ;;
esac
`
	env.writeExecutable(t, "curl", script)
}

func (env installerTestEnv) writeFakeWget(t *testing.T) {
	t.Helper()
	cpPath, err := exec.LookPath("cp")
	if err != nil {
		t.Fatalf("find cp for fake wget: %v", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
set -eu
out=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -qO)
      out="$2"
      shift 2
      ;;
    -qO-)
      out="-"
      shift
      ;;
    -*)
      shift
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done

name="${url##*/}"
case "$url" in
  */releases/latest)
    printf '{"tag_name":"v0.1.0"}'
    ;;
  */checksums.txt)
    %q "$MYN_TEST_RELEASE_DIR/checksums.txt" "$out"
    ;;
  */myn_*.tar.gz)
    %q "$MYN_TEST_RELEASE_DIR/$name" "$out"
    ;;
  *)
    echo "unexpected wget URL: $url" >&2
    exit 1
    ;;
esac
`, cpPath, cpPath)
	env.writeExecutable(t, "wget", script)
}

func (env installerTestEnv) writeFakeUname(t *testing.T, osName string, archName string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  -s) echo %q ;;
  -m) echo %q ;;
  *) exit 1 ;;
esac
`, osName, archName)
	env.writeExecutable(t, "uname", script)
}

func (env installerTestEnv) writeExecutable(t *testing.T, name string, contents string) {
	t.Helper()
	path := filepath.Join(env.fakeBin, name)
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

func (env installerTestEnv) linkRequiredTools(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		target, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		link := filepath.Join(env.fakeBin, name)
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("link %s: %v", name, err)
		}
	}
}
