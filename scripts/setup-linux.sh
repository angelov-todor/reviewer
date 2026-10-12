#!/usr/bin/env bash
# Provision a Debian or Ubuntu box to run the reviewer as a service.
#
# Safe to re-run: every step checks before it acts. It installs packages,
# creates the service user and its directories, and writes the systemd unit.
# It deliberately does NOT authenticate anything -- gh and claude both need a
# human once, and those two steps are the end of INSTALL.md.
#
# Run it with sudo, from a checkout or just this file on its own.
set -euo pipefail

SERVICE_USER=${SERVICE_USER:-reviewer}
STATE_DIR=${STATE_DIR:-/var/lib/reviewer}
CONFIG_DIR=${CONFIG_DIR:-/etc/reviewer}
BIN=${BIN:-/usr/local/bin/reviewer}

# The Claude Code release signing key, and the fingerprint to check it
# against. Both are published by Anthropic; the fingerprint is here so a
# substituted key fails loudly rather than being installed quietly.
CLAUDE_KEY_URL=https://downloads.claude.ai/keys/claude-code.asc
CLAUDE_KEY_FPR=31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE
# The stable channel, about a week behind latest, skipping releases with known
# major regressions. The right trade for a service: nothing about this box
# benefits from being first onto a new version, and an apt install does not
# auto-update, so a version cannot change underneath a running review.
CLAUDE_APT="deb [signed-by=/etc/apt/keyrings/claude-code.asc] https://downloads.claude.ai/claude-code/apt/stable stable main"

GH_KEY_URL=https://cli.github.com/packages/githubcli-archive-keyring.gpg

say() { printf '\n==> %s\n' "$*"; }
have() { command -v "$1" >/dev/null 2>&1; }

if [ "$(id -u)" -ne 0 ]; then
	echo "run this with sudo: the packages, the user and the unit all need root" >&2
	exit 1
fi
if ! have apt-get; then
	echo "this script is for Debian or Ubuntu; on anything else follow INSTALL.md by hand" >&2
	exit 1
fi

say "base packages"
apt-get update -qq
# git does the cloning; the rest are what the two apt repositories below need
# in order to be added and verified at all.
apt-get install -y --no-install-recommends git curl gnupg ca-certificates

say "gh (GitHub CLI)"
if have gh; then
	echo "already installed: $(gh --version | head -1)"
else
	install -d -m 0755 /etc/apt/keyrings
	curl -fsSL "$GH_KEY_URL" -o /etc/apt/keyrings/githubcli-archive-keyring.gpg
	chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg
	echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
		>/etc/apt/sources.list.d/github-cli.list
	apt-get update -qq
	apt-get install -y --no-install-recommends gh
fi

say "claude (Claude Code)"
if have claude; then
	echo "already installed: $(claude --version)"
else
	install -d -m 0755 /etc/apt/keyrings
	curl -fsSL "$CLAUDE_KEY_URL" -o /etc/apt/keyrings/claude-code.asc
	# Verified before the repository is trusted, not after. A wrong or
	# truncated key otherwise surfaces much later as an opaque NO_PUBKEY
	# failure from apt.
	got=$(gpg --show-keys --with-colons /etc/apt/keyrings/claude-code.asc |
		awk -F: '$1=="fpr" {print $10; exit}')
	if [ "$got" != "$CLAUDE_KEY_FPR" ]; then
		echo "signing key fingerprint mismatch" >&2
		echo "  expected $CLAUDE_KEY_FPR" >&2
		echo "  got      ${got:-<none: the download failed or returned HTML>}" >&2
		rm -f /etc/apt/keyrings/claude-code.asc
		exit 1
	fi
	echo "$CLAUDE_APT" >/etc/apt/sources.list.d/claude-code.list
	apt-get update -qq
	apt-get install -y claude-code
fi

say "service user and directories"
if id "$SERVICE_USER" >/dev/null 2>&1; then
	echo "user $SERVICE_USER exists"
else
	# A system user with a real home: claude keeps ~/.claude and gh keeps
	# ~/.config/gh, so a nologin user with no home cannot authenticate. No
	# login shell all the same -- reach it with `sudo -u reviewer -H`.
	useradd --system --create-home --home-dir "$STATE_DIR" \
		--shell /usr/sbin/nologin "$SERVICE_USER"
fi
install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0750 \
	"$STATE_DIR" "$STATE_DIR/workspace" "$STATE_DIR/repos" "$STATE_DIR/work" "$STATE_DIR/reports"
install -d -m 0755 "$CONFIG_DIR"

if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
	say "config template at $CONFIG_DIR/config.yaml"
	cat >"$CONFIG_DIR/config.yaml" <<EOF
# Written by setup-linux.sh. Edit before starting the service.
# dry_run stays true until you have read a sweep's output and meant it.
dry_run: true

github_login: "CHANGE-ME"
allow_owners:
  - AstraBit-CPT

sources:
  - type: github
    owner: AstraBit-CPT
    repo_prefixes: [aex-, astraex-]
    exclude_authors: [app/dependabot, app/astraex-pipeline-bot]
    exclude_bots: true

state_dir: $STATE_DIR
interval: 5m
review_concurrency: 3
max_reviews_per_sweep: 5

# docs_root: $STATE_DIR/workspace/astraex-docs
EOF
	chown root:"$SERVICE_USER" "$CONFIG_DIR/config.yaml"
	# Readable by the service, writable only by root: it is the file that says
	# whether anything is posted to GitHub at all.
	chmod 0640 "$CONFIG_DIR/config.yaml"
else
	echo "config already present, left alone"
fi

say "systemd unit"
UNIT=/etc/systemd/system/reviewer.service
SRC=$(dirname "$(readlink -f "$0")")/reviewer.service
if [ -f "$SRC" ]; then
	install -m 0644 "$SRC" "$UNIT"
	systemctl daemon-reload
	echo "installed $UNIT"
else
	echo "reviewer.service not found next to this script; copy it to $UNIT by hand" >&2
fi

say "what is left, and it needs a human"
cat <<EOF
  1. install the binary:   sudo install -m 0755 /tmp/reviewer $BIN
  2. authenticate gh:      sudo -u $SERVICE_USER -H gh auth login --with-token < token.txt
  3. authenticate claude:  sudo -u $SERVICE_USER -H claude setup-token
  4. edit $CONFIG_DIR/config.yaml (github_login, and docs_root if wanted)
  5. check it:             sudo -u $SERVICE_USER -H $BIN doctor -config $CONFIG_DIR/config.yaml
  6. one sweep, no posts:  sudo -u $SERVICE_USER -H $BIN scan -print-only -config $CONFIG_DIR/config.yaml
  7. start it:             sudo systemctl enable --now reviewer

See INSTALL.md for what each of those does and why it cannot be scripted.
EOF
