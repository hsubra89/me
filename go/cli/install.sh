#!/bin/sh
set -eu

repo="hsubra89/myn"
install_dir="$HOME/.local/bin"
install_path="$install_dir/myn"

say() {
  printf '%s\n' "$*"
}

fail() {
  printf 'myn installer: %s\n' "$*" >&2
  exit 1
}

have() {
  command -v "$1" >/dev/null 2>&1
}

require() {
  have "$1" || fail "missing required tool: $1"
}

if [ "${MYN_INSTALL_DIR:-}" != "" ]; then
  fail "MYN_INSTALL_DIR is not supported; myn installs to $install_path"
fi

for tool in uname mktemp tar gzip grep sed mkdir rm mv chmod; do
  require "$tool"
done

if have curl; then
  fetch_to_file() {
    curl -fsSL -o "$1" "$2"
  }
  fetch_to_stdout() {
    curl -fsSL "$1"
  }
elif have wget; then
  fetch_to_file() {
    wget -qO "$1" "$2"
  }
  fetch_to_stdout() {
    wget -qO- "$1"
  }
else
  fail "missing required tool: curl or wget"
fi

if have sha256sum; then
  verify_checksum() {
    sha256sum -c "$1"
  }
elif have shasum; then
  verify_checksum() {
    shasum -a 256 -c "$1"
  }
else
  fail "missing required tool: sha256sum or shasum"
fi

detect_os() {
  case "$(uname -s)" in
    Linux | linux)
      printf 'linux'
      ;;
    Darwin | darwin)
      printf 'darwin'
      ;;
    *)
      fail "unsupported OS: $(uname -s)"
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64)
      printf 'amd64'
      ;;
    arm64 | aarch64)
      printf 'arm64'
      ;;
    *)
      fail "unsupported architecture: $(uname -m)"
      ;;
  esac
}

resolve_tag() {
  if [ "${MYN_VERSION:-}" != "" ]; then
    case "$MYN_VERSION" in
      v*)
        printf '%s' "$MYN_VERSION"
        ;;
      *)
        printf 'v%s' "$MYN_VERSION"
        ;;
    esac
    return
  fi

  latest_url="https://api.github.com/repos/$repo/releases/latest"
  tag="$(fetch_to_stdout "$latest_url" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | sed -n '1p')"
  if [ "$tag" = "" ]; then
    fail "could not resolve latest stable release from GitHub; set MYN_VERSION to install a specific version"
  fi
  printf '%s' "$tag"
}

validate_tag() {
  semver_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?$'
  printf '%s\n' "$1" | grep -Eq "$semver_re" || fail "release version must be vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-prerelease: $1"
}

tag="$(resolve_tag)"
validate_tag "$tag"
version="${tag#v}"
os="$(detect_os)"
arch="$(detect_arch)"
archive="myn_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/$repo/releases/download/$tag"

say "Installing myn $version"
say "Detected platform: $os/$arch"
say "Downloading $archive"

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

fetch_to_file "$tmpdir/$archive" "$base_url/$archive" || fail "download failed: $archive"
fetch_to_file "$tmpdir/checksums.txt" "$base_url/checksums.txt" || fail "download failed: checksums.txt"

(
  cd "$tmpdir"
  grep "[[:space:]]$archive$" checksums.txt > checksums.selected || fail "checksum entry missing for $archive"
  verify_checksum checksums.selected >/dev/null || fail "checksum verification failed for $archive"
)
say "Verified checksum"

mkdir -p "$tmpdir/extract"
tar -xzf "$tmpdir/$archive" -C "$tmpdir/extract"
if [ ! -f "$tmpdir/extract/myn" ]; then
  fail "release archive did not contain myn"
fi
chmod 0755 "$tmpdir/extract/myn"

mkdir -p "$install_dir" || fail "could not create install directory: $install_dir"
tmp_install="$install_dir/.myn.tmp.$$"
mv "$tmpdir/extract/myn" "$tmp_install" || fail "could not stage myn in $install_dir"
mv "$tmp_install" "$install_path" || fail "could not install myn to $install_path"

say "Installed myn to $install_path"

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) say "Add $install_dir to PATH to run myn from any directory." ;;
esac

"$install_path" version
