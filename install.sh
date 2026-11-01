#!/bin/sh
# Install or upgrade epub-tools from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/gkwang/epub-tools/main/install.sh | sh
#
# Environment:
#   EPUB_TOOLS_VERSION      release tag to install, e.g. v0.1.0 (default: latest)
#   EPUB_TOOLS_INSTALL_DIR  where to put the binary (default: $HOME/.local/bin)
#   EPUB_TOOLS_BASE_URL     download base URL (testing; default: GitHub Releases)
#
# The archive is verified against the release's checksums.txt before install.

set -eu

REPO="gkwang/epub-tools"
BIN="epub-tools"

say() { printf '%s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not installed"; }

detect_platform() {
	case "$(uname -s)" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) die "unsupported OS '$(uname -s)'; download manually from https://github.com/$REPO/releases" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) die "unsupported architecture '$(uname -m)'" ;;
	esac
	# Rosetta: an arm64 Mac running this shell under x86_64 should still get arm64.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
		[ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
		arch=arm64
	fi
}

latest_tag() {
	# github.com/<repo>/releases/latest redirects to .../releases/tag/<tag>;
	# following the redirect avoids the rate-limited GitHub API.
	url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
		die "could not reach GitHub to find the latest release"
	tag=${url##*/}
	case "$tag" in
	v[0-9]*) printf '%s\n' "$tag" ;;
	*) die "no published release found for $REPO" ;;
	esac
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "need sha256sum or shasum to verify the download"
	fi
}

main() {
	need curl
	need tar
	need uname

	detect_platform
	tag=${EPUB_TOOLS_VERSION:-}
	[ -n "$tag" ] || tag=$(latest_tag)
	case "$tag" in v*) ;; *) tag="v$tag" ;; esac
	version=${tag#v}

	base=${EPUB_TOOLS_BASE_URL:-https://github.com/$REPO/releases/download}
	archive="${BIN}_${version}_${os}_${arch}.tar.gz"
	dir=${EPUB_TOOLS_INSTALL_DIR:-$HOME/.local/bin}

	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t epub-tools)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	say "Downloading $BIN $tag for $os/$arch..."
	curl -fsSL -o "$tmp/$archive" "$base/$tag/$archive" ||
		die "download failed: $base/$tag/$archive"
	curl -fsSL -o "$tmp/checksums.txt" "$base/$tag/checksums.txt" ||
		die "download failed: $base/$tag/checksums.txt"

	want=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || die "$archive is not listed in checksums.txt"
	got=$(sha256 "$tmp/$archive")
	[ "$want" = "$got" ] || die "checksum mismatch for $archive (expected $want, got $got); not installing"

	tar -xzf "$tmp/$archive" -C "$tmp" "$BIN" || die "could not extract $BIN from $archive"
	mkdir -p "$dir" || die "cannot create $dir"
	# Install via a temp name in the target dir, then rename: atomic, and safe
	# even if the old binary is currently running.
	cp "$tmp/$BIN" "$dir/.$BIN.new" && chmod 755 "$dir/.$BIN.new" &&
		mv -f "$dir/.$BIN.new" "$dir/$BIN" || die "cannot write to $dir (try EPUB_TOOLS_INSTALL_DIR)"

	say "Installed $("$dir/$BIN" version 2>/dev/null || echo "$BIN $tag") to $dir/$BIN"
	case ":$PATH:" in
	*":$dir:"*) ;;
	*)
		say ""
		say "$dir is not on your PATH. Add it by appending this line to your"
		say "shell profile (~/.zshrc, ~/.bashrc, ...), then open a new terminal:"
		say ""
		say "  export PATH=\"$dir:\$PATH\""
		;;
	esac
}

main "$@"
