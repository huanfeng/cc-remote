#!/bin/sh
# cc-remote installer / updater for Linux and macOS.
#
# Re-running the script updates an existing installation in place: the new
# binary's "service install" stops the service, copies itself to the stable
# location and starts it again.
#
# Public releases download anonymously. For a private fork (CC_REMOTE_REPO),
# credentials are used when available:
#   1. an authenticated GitHub CLI ("gh auth login"), or
#   2. GITHUB_TOKEN / GH_TOKEN with "Contents: Read" on the repository.
#
# Usage:
#   sh install.sh [--version vX.Y.Z] [--listen ADDR]... [--no-service]
#                 [--dir DIR] [--no-alias] [--dry-run]
#   gh api repos/huanfeng/cc-remote/contents/scripts/install.sh \
#     -H "Accept: application/vnd.github.raw" | sh -s -- --listen lan:8787
#
# Environment: CC_REMOTE_REPO (default huanfeng/cc-remote), GITHUB_TOKEN/GH_TOKEN,
# CC_REMOTE_API_URL (GitHub API base, default https://api.github.com).

set -eu

REPO="${CC_REMOTE_REPO:-huanfeng/cc-remote}"
API="${CC_REMOTE_API_URL:-https://api.github.com}"
VERSION=""
INSTALL_DIR="${HOME}/.local/bin"
NO_SERVICE=0
ALIAS=1
DRY_RUN=0
LISTEN=""

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
# GitHub's "latest" release never includes prereleases.
missing_release() {
	if [ -n "$VERSION" ]; then
		printf 'no release %s found in %s' "$VERSION" "$REPO"
	else
		printf 'no stable release found in %s (prereleases are skipped; use --version <tag>)' "$REPO"
	fi
}

usage() {
	sed -n '2,20p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//' || true
	exit "${1:-0}"
}

add_listen() {
	# accept repeated flags and comma separated lists
	old_ifs=$IFS
	IFS=,
	for a in $1; do
		[ -n "$a" ] && LISTEN="${LISTEN}${LISTEN:+ }$a"
	done
	IFS=$old_ifs
}

while [ $# -gt 0 ]; do
	case "$1" in
		--version) [ $# -ge 2 ] || die "--version needs a value"; VERSION=$2; shift 2 ;;
		--version=*) VERSION=${1#*=}; shift ;;
		--listen) [ $# -ge 2 ] || die "--listen needs a value"; add_listen "$2"; shift 2 ;;
		--listen=*) add_listen "${1#*=}"; shift ;;
		--dir) [ $# -ge 2 ] || die "--dir needs a value"; INSTALL_DIR=$2; shift 2 ;;
		--dir=*) INSTALL_DIR=${1#*=}; shift ;;
		--no-service) NO_SERVICE=1; shift ;;
		--alias) ALIAS=1; shift ;;
		--no-alias) ALIAS=0; shift ;;
		--dry-run) DRY_RUN=1; shift ;;
		-h|--help) usage 0 ;;
		*) die "unknown argument: $1 (see --help)" ;;
	esac
done

case "$VERSION" in
	""|v*) ;;
	*) VERSION="v$VERSION" ;;
esac

# --- platform ---------------------------------------------------------------
case "$(uname -s)" in
	Linux) OS=linux ;;
	Darwin) OS=darwin ;;
	*) die "unsupported OS $(uname -s); use install.ps1 on Windows" ;;
esac
case "$(uname -m)" in
	x86_64|amd64) ARCH=amd64 ;;
	aarch64|arm64) ARCH=arm64 ;;
	*) die "unsupported architecture $(uname -m)" ;;
esac

# --- credentials --------------------------------------------------------------
TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
METHOD=""
if command -v gh >/dev/null 2>&1 && gh auth status --hostname github.com >/dev/null 2>&1; then
	METHOD=gh
elif [ -n "$TOKEN" ]; then
	METHOD=token
else
	METHOD=anonymous
fi

auth_help() {
	cat >&2 <<EOF
error: cannot access releases of $REPO.
The repository may be private or the request was rate-limited. Either:
  1. install the GitHub CLI and run: gh auth login
  2. or export GITHUB_TOKEN=<fine-grained token with "Contents: Read" on $REPO>
EOF
	exit 1
}

if command -v curl >/dev/null 2>&1; then
	HTTP=curl
elif [ "$METHOD" = token ]; then
	# wget would forward the Authorization header across the asset redirect
	# to the storage host; only curl lets us follow it without the token.
	die "curl is required when downloading with GITHUB_TOKEN/GH_TOKEN (or use gh auth login)"
elif command -v wget >/dev/null 2>&1; then
	HTTP=wget
else
	[ "$METHOD" = gh ] || die "curl or wget is required"
	HTTP=none
fi

# api_get PATH OUTFILE ACCEPT -> prints the HTTP status code
api_get() {
	url="$API$1"
	if [ "$HTTP" = curl ]; then
		if [ -n "$TOKEN" ]; then
			# no -L: the asset redirect goes to a storage host that must not
			# receive the token, so the redirect is followed without it
			res=$(curl -sS -o "$2" -w '%{http_code} %{redirect_url}' -H "Accept: $3" \
				-H "Authorization: Bearer $TOKEN" -H "X-GitHub-Api-Version: 2022-11-28" "$url") || res=000
		else
			res=$(curl -sSL -o "$2" -w '%{http_code} ' -H "Accept: $3" "$url") || res=000
		fi
		code=${res%% *}
		loc=${res#* }
		case "$code" in
			301|302|303|307|308) code=$(curl -sSL -o "$2" -w '%{http_code}' "$loc") || code=000 ;;
		esac
		printf '%s' "$code"
	else
		# anonymous only: a token requires curl (see above)
		wget -q -O "$2" --header="Accept: $3" "$url" && printf 200 || printf 404
	fi
}

TMP=$(mktemp -d 2>/dev/null || mktemp -d -t cc-remote)
trap 'rm -rf "$TMP"' EXIT INT TERM

# --- resolve release ------------------------------------------------------------
# Produces $TAG and $TMP/assets ("<name> <id>" per line; id empty for gh).
if [ "$METHOD" = gh ]; then
	# shellcheck disable=SC2086 # empty $VERSION means "latest"
	if ! out=$(gh release view $VERSION -R "$REPO" --json tagName,assets \
		-q '.tagName, (.assets[] | .name)' 2>&1); then
		case "$out" in
			*"not found"*|*"Not Found"*)
				die "$(missing_release)" ;;
			*) die "gh release view failed: $out" ;;
		esac
	fi
	TAG=$(printf '%s\n' "$out" | sed -n 1p)
	printf '%s\n' "$out" | sed 1d | sed 's/$/ /' > "$TMP/assets"
else
	if [ -n "$VERSION" ]; then path="/repos/$REPO/releases/tags/$VERSION"; else path="/repos/$REPO/releases/latest"; fi
	code=$(api_get "$path" "$TMP/release.json" "application/vnd.github+json")
	case "$code" in
		200) ;;
		404) [ "$METHOD" = anonymous ] && auth_help
		     die "$(missing_release)" ;;
		401|403) say "GitHub API returned $code; check the token's permissions." >&2; auth_help ;;
		*) [ "$METHOD" = anonymous ] && auth_help
		   die "GitHub API request failed (HTTP $code)" ;;
	esac
	TAG=$(tr ',' '\n' < "$TMP/release.json" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
	# Split the JSON into one field per line; an asset object starts with its
	# "url" (…/releases/assets/<id>) and its "name" follows.
	# shellcheck disable=SC2020 # three chars to three newlines, portable to BSD tr
	tr ',{}' '\n\n\n' < "$TMP/release.json" | awk '
		/"url": *"[^"]*\/releases\/assets\/[0-9]+"/ { s=$0; sub(/.*\/releases\/assets\//, "", s); sub(/".*/, "", s); id=s; next }
		/"name": *"/ && id != "" { s=$0; sub(/.*"name": *"/, "", s); sub(/".*/, "", s); print s " " id; id="" }
	' > "$TMP/assets"
fi
[ -n "$TAG" ] || die "could not determine the release tag"

EXT=tar.gz
ASSET=$(awk -v suf="_${OS}_${ARCH}.${EXT}" '{ n=$1; if (index(n, "cc-remote_") == 1 && substr(n, length(n)-length(suf)+1) == suf) { print n; exit } }' "$TMP/assets")
[ -n "$ASSET" ] || die "release $TAG has no archive for ${OS}/${ARCH}"
grep -q '^checksums.txt ' "$TMP/assets" || die "release $TAG has no checksums.txt; refusing to install unverified files"

asset_id() { awk -v n="$1" '$1 == n { print $2; exit }' "$TMP/assets"; }

set --
for a in $LISTEN; do set -- "$@" --listen "$a"; done
if [ "$NO_SERVICE" = 1 ]; then
	PLAN="copy cc-remote to $INSTALL_DIR"
	[ "$ALIAS" = 1 ] && PLAN="$PLAN; run: $INSTALL_DIR/cc-remote alias install"
else
	PLAN="run: cc-remote service install $*"
	[ "$ALIAS" = 1 ] && PLAN="$PLAN --alias"
fi

say "repository : $REPO (via $METHOD)"
say "release    : $TAG"
say "asset      : $ASSET"
say "plan       : $PLAN"
if [ "$DRY_RUN" = 1 ]; then
	say "dry run: nothing downloaded or installed"
	exit 0
fi

# --- download + verify --------------------------------------------------------------
download() {
	if [ "$METHOD" = gh ]; then
		gh release download "$TAG" -R "$REPO" -p "$1" -D "$TMP" --clobber >/dev/null
	else
		code=$(api_get "/repos/$REPO/releases/assets/$(asset_id "$1")" "$TMP/$1" "application/octet-stream")
		[ "$code" = 200 ] || die "download of $1 failed (HTTP $code)"
	fi
}
say "downloading $ASSET"
download "$ASSET"
download checksums.txt

want=$(awk -v n="$ASSET" '{ f=$2; sub(/^\*/, "", f); if (f == n) { print tolower($1); exit } }' "$TMP/checksums.txt")
[ -n "$want" ] || die "checksums.txt has no entry for $ASSET"
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$TMP/$ASSET" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$TMP/$ASSET" | awk '{print $1}')
else
	die "sha256sum or shasum is required to verify the download"
fi
[ "$got" = "$want" ] || die "sha256 mismatch for $ASSET (got $got, want $want)"
say "sha256 verified"

mkdir -p "$TMP/x"
tar -xzf "$TMP/$ASSET" -C "$TMP/x"
BIN="$TMP/x/cc-remote"
[ -f "$BIN" ] || die "archive does not contain cc-remote"
chmod +x "$BIN"

# --- install ----------------------------------------------------------------------
if [ "$NO_SERVICE" = 1 ]; then
	mkdir -p "$INSTALL_DIR"
	cp "$BIN" "$INSTALL_DIR/cc-remote.new"
	chmod +x "$INSTALL_DIR/cc-remote.new"
	mv -f "$INSTALL_DIR/cc-remote.new" "$INSTALL_DIR/cc-remote"
	say "installed $INSTALL_DIR/cc-remote ($TAG)"
	if [ "$ALIAS" = 1 ]; then
		"$INSTALL_DIR/cc-remote" alias install
	fi
	case ":$PATH:" in
		*":$INSTALL_DIR:"*) ;;
		*) say "note: $INSTALL_DIR is not on PATH" ;;
	esac
else
	if [ "$ALIAS" = 1 ]; then
		"$BIN" service install "$@" --alias
	else
		"$BIN" service install "$@"
	fi
	"$BIN" service status || true
fi
say "done: cc-remote $TAG"
