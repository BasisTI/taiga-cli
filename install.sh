#!/bin/sh
# Install the taiga CLI from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/BasisTI/taiga-cli/main/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- --version v0.4.0 --dir "$HOME/bin"
#
# Options (the environment variable is used when the option is absent):
#   -v, --version VERSION   release to install, with or without "v"   (TAIGA_VERSION; default: latest)
#   -d, --dir DIR           destination directory                     (TAIGA_INSTALL_DIR; default: ~/.local/bin)
#   -h, --help              show this help
#
# TAIGA_INSTALL_BASE_URL replaces https://github.com/BasisTI/taiga-cli/releases and exists
# only so the installer can be tested against a local fixture; do not set it otherwise.
#
# The whole script lives in main(), called on the last line: a truncated download
# defines nothing and runs nothing. It never uses sudo.

main() {
  set -eu

  repo_url="https://github.com/BasisTI/taiga-cli/releases"
  base_url="${TAIGA_INSTALL_BASE_URL:-$repo_url}"
  base_url="${base_url%/}"
  version="${TAIGA_VERSION:-}"
  dir="${TAIGA_INSTALL_DIR:-${HOME:-}/.local/bin}"

  while [ $# -gt 0 ]; do
    case "$1" in
      -v | --version)
        [ $# -ge 2 ] || die "$1 needs a value"
        version="$2"
        shift 2
        ;;
      --version=*)
        version="${1#--version=}"
        shift
        ;;
      -d | --dir)
        [ $# -ge 2 ] || die "$1 needs a value"
        dir="$2"
        shift 2
        ;;
      --dir=*)
        dir="${1#--dir=}"
        shift
        ;;
      -h | --help)
        usage
        return 0
        ;;
      *)
        die "unknown option: $1 (try --help)"
        ;;
    esac
  done

  if [ -z "$dir" ] || [ "$dir" = "/.local/bin" ]; then
    die "cannot work out the destination: set HOME, TAIGA_INSTALL_DIR or --dir"
  fi
  while [ "${#dir}" -gt 1 ] && [ "${dir%/}" != "$dir" ]; do dir="${dir%/}"; done

  if command -v curl >/dev/null 2>&1; then
    fetcher=curl
  elif command -v wget >/dev/null 2>&1; then
    fetcher=wget
  else
    die "curl or wget is required"
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    hasher="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then
    hasher="shasum -a 256"
  else
    die "sha256sum or shasum is required to verify the download"
  fi
  command -v tar >/dev/null 2>&1 || die "tar is required"

  case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) die "unsupported operating system: $(uname -s) (supported: Linux, Darwin)" ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) die "unsupported architecture: $(uname -m) (supported: x86_64, arm64)" ;;
  esac

  if [ -z "$version" ]; then
    say "Looking up the latest release"
    version=$(latest_version)
    [ -n "$version" ] || die "could not work out the latest release from $base_url/latest"
  fi
  version="${version#v}"
  case "$version" in
    '' | *[!0-9A-Za-z._+-]*) die "invalid version: $version" ;;
  esac

  file="taiga_${version}_${os}_${arch}.tar.gz"
  url="$base_url/download/v$version"

  tmp=$(mktemp -d "${TMPDIR:-/tmp}/taiga-install.XXXXXX") || die "cannot create a temporary directory"
  staged=""
  trap 'cleanup' EXIT
  trap 'exit 1' INT TERM HUP

  say "Downloading taiga v$version for $os/$arch"
  download "$url/$file" "$tmp/$file" || die "could not download $url/$file"
  download "$url/SHA256SUMS" "$tmp/SHA256SUMS" || die "could not download $url/SHA256SUMS"

  expected=$(awk -v f="$file" '$2 == f { print $1; exit }' "$tmp/SHA256SUMS")
  [ -n "$expected" ] || die "$file is not listed in SHA256SUMS; nothing installed"
  actual=$($hasher "$tmp/$file" | awk '{ print $1 }')
  if [ "$expected" != "$actual" ]; then
    die "checksum mismatch for $file (expected $expected, got $actual); nothing installed"
  fi
  say "Checksum OK"

  tar -xzf "$tmp/$file" -C "$tmp" taiga || die "could not extract taiga from $file"
  [ -f "$tmp/taiga" ] || die "$file does not contain a taiga binary"

  mkdir -p "$dir" || die "cannot create $dir"
  staged="$dir/.taiga.$$"
  cp "$tmp/taiga" "$staged" || die "cannot write to $dir"
  chmod 0755 "$staged"
  mv -f "$staged" "$dir/taiga" || die "cannot install into $dir"
  staged=""

  say "Installed $dir/taiga"
  "$dir/taiga" version </dev/null || die "the installed binary does not run"

  case ":${PATH:-}:" in
    *":$dir:"*) ;;
    *) say "warning: $dir is not on your PATH; add it to your shell profile to run taiga" ;;
  esac
}

usage() {
  cat <<'EOT'
Usage: install.sh [--version VERSION] [--dir DIR]

  -v, --version VERSION   release to install, with or without "v" (TAIGA_VERSION; default: latest)
  -d, --dir DIR           destination directory (TAIGA_INSTALL_DIR; default: ~/.local/bin)
  -h, --help              show this help

Downloads the release tarball and SHA256SUMS, verifies the checksum and installs
the binary atomically. It never uses sudo.
EOT
}

say() {
  printf '%s\n' "$*" >&2
}

die() {
  printf 'install.sh: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  [ -z "${staged:-}" ] || rm -f "$staged"
  [ -z "${tmp:-}" ] || rm -rf "$tmp"
}

download() {
  if [ "$fetcher" = curl ]; then
    curl -fsSL -o "$2" "$1"
  else
    wget -q -O "$2" "$1"
  fi
}

# Prints the latest version from the redirect of <base>/latest (…/tag/vX.Y.Z), without the GitHub API.
latest_version() {
  if [ "$fetcher" = curl ]; then
    location=$(curl -fsS -o /dev/null -w '%{redirect_url}' "$base_url/latest") || return 0
  else
    location=$(wget -q -S --max-redirect=0 -O /dev/null "$base_url/latest" 2>&1 | awk 'tolower($1) == "location:" { print $2; exit }' | tr -d '\r') || return 0
  fi
  tag="${location##*/}"
  case "$tag" in
    v[0-9]*) printf '%s\n' "${tag#v}" ;;
  esac
}

main "$@"
