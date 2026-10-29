#!/bin/sh
# Install unlog from its latest GitHub release:
#
#   curl -fsSL https://raw.githubusercontent.com/oxforge/unlog/main/install.sh | sh
#
# UNLOG_VERSION      the release to install, such as v0.2.2 (default: the latest)
# UNLOG_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
#
# macOS and Linux on amd64 or arm64. On Windows, Git Bash, MSYS2 and Cygwin
# get unlog.exe and WSL gets the Linux build; from PowerShell, use
# go install. The archive is checked against the release's checksums.txt.
# curl sets no quarantine flag, so macOS runs the binary as is.
#
# Everything runs from main, called on the last line, so a download cut short
# runs nothing.

set -eu

repo=https://github.com/oxforge/unlog
docs=https://github.com/oxforge/unlog#installation

say() { printf 'unlog: %s\n' "$*" >&2; }
die() { say "$*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

# platform sets os, arch, ext and bin from uname, as the release names them.
platform() {
  case $(uname -s) in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    MINGW* | MSYS* | CYGWIN*) os=windows ;;
    *) die "no release for $(uname -s); see $docs" ;;
  esac
  case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "no release for $(uname -m); see $docs" ;;
  esac
  # A shell under Rosetta says x86_64 on an Apple silicon Mac.
  if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
    [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
    arch=arm64
  fi
  ext=tar.gz bin=unlog
  if [ "$os" = windows ]; then ext=zip bin=unlog.exe; fi
}

# latest prints the latest release's tag, read from where
# github.com/<repo>/releases/latest redirects (the API has a rate limit).
latest() {
  url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$repo/releases/latest") ||
    die "cannot reach $repo/releases/latest"
  case ${url##*/} in
    v[0-9]*) printf '%s\n' "${url##*/}" ;;
    *) die "no release found at $repo/releases" ;;
  esac
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d ' ' -f 1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d ' ' -f 1
  else
    die "sha256sum or shasum is required to check the download"
  fi
}

# unpack extracts the binary from archive $1 into directory $2.
unpack() {
  if [ "$ext" = tar.gz ]; then
    tar -xzf "$1" -C "$2" "$bin"
  elif command -v unzip >/dev/null 2>&1; then
    unzip -q -o "$1" "$bin" -d "$2"
  else
    powershell.exe -NoProfile -Command \
      "Expand-Archive -Force -LiteralPath '$(cygpath -w "$1")' -DestinationPath '$(cygpath -w "$2")'"
  fi
}

main() {
  need curl
  need uname
  platform

  version=${UNLOG_VERSION:-}
  if [ -n "$version" ]; then version=v${version#v}; else version=$(latest); fi
  archive=unlog_${version#v}_${os}_${arch}.$ext
  dir=${UNLOG_INSTALL_DIR:-$HOME/.local/bin}

  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT

  say "downloading $archive ($version)"
  curl -fsSL -o "$tmp/$archive" "$repo/releases/download/$version/$archive" ||
    die "no $archive in release $version: see $repo/releases"
  curl -fsSL -o "$tmp/checksums.txt" "$repo/releases/download/$version/checksums.txt" ||
    die "no checksums.txt in release $version"
  want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
  [ -n "$want" ] || die "checksums.txt names no $archive"
  [ "$(sha256 "$tmp/$archive")" = "$want" ] || die "$archive does not match its checksum"

  unpack "$tmp/$archive" "$tmp"
  chmod 755 "$tmp/$bin"

  # sudo only when the directory cannot be written otherwise.
  sudo=
  if ! mkdir -p "$dir" 2>/dev/null || [ ! -w "$dir" ]; then
    command -v sudo >/dev/null 2>&1 ||
      die "cannot write to $dir; set UNLOG_INSTALL_DIR to a directory you can write"
    say "$dir is not writable; using sudo"
    sudo=sudo
    $sudo mkdir -p "$dir"
  fi
  # A new file renamed over the old one: overwriting a binary in place gets it
  # killed on macOS, whose signature check caches the old file.
  $sudo cp "$tmp/$bin" "$dir/.$bin.new"
  $sudo mv -f "$dir/.$bin.new" "$dir/$bin"

  say "installed $("$dir/$bin" version) in $dir"
  case ":$PATH:" in
    *":$dir:"*)
      found=$(command -v "$bin" || true)
      if [ -n "$found" ] && [ "$found" != "$dir/$bin" ]; then
        say "warning: $found comes first on PATH"
      fi
      ;;
    *)
      say "$dir is not on PATH; add it in your shell's profile:"
      say "  export PATH=\"$dir:\$PATH\""
      ;;
  esac
}

main "$@"
