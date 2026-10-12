# Installing on a Linux service box

Written for a fresh Debian or Ubuntu machine with nothing on it. Everything
below has been read off the current upstream documentation, but none of it has
been executed against a real box from the machine it was written on — run it
with your eyes open the first time, and re-run anything that fails. The setup
script is safe to re-run.

## What you need first

| | |
| --- | --- |
| The machine | Debian 10+ or Ubuntu 20.04+, x64 or arm64. 12 GB RAM, 80 GB disk is ample — the whole 75-repo workspace is about 0.1 GB of git data |
| A Claude account | Pro, Max, Team, Enterprise or Console. The free plan does not include Claude Code |
| A GitHub token | A classic PAT with `repo` and `read:org`, for an account that can read the repositories and submit pull request reviews |
| Outbound HTTPS | `github.com`, `api.github.com`, `api.anthropic.com`, `downloads.claude.ai`, `cli.github.com`, and your distribution's mirrors |

Nothing listens on a port. No inbound access is needed.

## 1. Build and copy the binary

From a checkout on any machine with Go — your laptop is fine:

```bash
bash scripts/build-linux.sh
scp dist/reviewer-linux-amd64 <host>:/tmp/reviewer
```

It cross-compiles to a statically linked ELF with `CGO_ENABLED=0`, so the
server needs no Go toolchain and nothing to keep patched beyond git, gh and
claude.

## 2. Provision the box

```bash
scp scripts/setup-linux.sh scripts/reviewer.service <host>:/tmp/
ssh <host> 'sudo bash /tmp/setup-linux.sh'
sudo install -m 0755 /tmp/reviewer /usr/local/bin/reviewer
```

That installs `git`, `gh` and `claude-code`, creates a `reviewer` system user
with its home at `/var/lib/reviewer`, makes the directories, writes a config
template to `/etc/reviewer/config.yaml`, and installs the systemd unit.

Two details worth knowing. `claude` comes from Anthropic's **apt repository**
rather than the `curl | bash` installer, and the script verifies the signing
key's fingerprint before trusting it. An apt install does not auto-update,
which is what you want here: a version cannot change underneath a review that
is already running. Updates arrive with your normal `apt upgrade`.

## 3. Authenticate gh — needs a human once

```bash
# put the PAT in a file so it never reaches your shell history
ssh <host> 'cat > /tmp/token.txt'   # paste, then Ctrl-D
sudo -u reviewer -H gh auth login --with-token < /tmp/token.txt
rm /tmp/token.txt
sudo -u reviewer -H gh auth status
```

`-H` matters: it sets `HOME` to `/var/lib/reviewer`, which is where the
service will look for the credential. Without it the token lands in your own
home and the service cannot see it.

## 4. Authenticate claude — needs a human once

```bash
sudo -u reviewer -H claude setup-token
```

This prints a URL, you open it in a browser **on your own machine**, approve,
and paste the code back. It stores a long-lived token under
`/var/lib/reviewer/.claude`, which is the whole point: the service can then
run unattended.

This is the step that cannot be scripted and the one most likely to go wrong
on a headless box. If it fails, the fallback is an API key —
`ANTHROPIC_API_KEY` in the unit's environment — but a key is billed
differently from a subscription, so check which you are paying for before
reaching for it.

## 5. Configure

Edit `/etc/reviewer/config.yaml`. The template leaves `github_login` as
`CHANGE-ME` on purpose: the service needs it to recognise its own reviews and
to skip pull requests you authored yourself.

`dry_run: true` is also deliberate. Leave it until you have read a sweep's
output and meant it. Set `docs_root` to the docs checkout once the workspace
has one, and reviews gain the compliance dimension.

## 6. Check before starting

```bash
sudo -u reviewer -H reviewer doctor -config /etc/reviewer/config.yaml
```

Every external dependency, in one pass: git, claude, gh's authentication and
whether its token can actually submit reviews, and each configured source's
real query with a count of what came back. Do not skip it — a source that
returns nothing looks exactly like a quiet week.

Then one sweep that writes nothing:

```bash
sudo -u reviewer -H reviewer scan -print-only -config /etc/reviewer/config.yaml
```

## 7. Start it

```bash
sudo systemctl enable --now reviewer
journalctl -u reviewer -f
```

## Running it

```bash
systemctl status reviewer
journalctl -u reviewer --since '1 hour ago'

sudo -u reviewer -H reviewer status -config /etc/reviewer/config.yaml
sudo -u reviewer -H reviewer pause  -config /etc/reviewer/config.yaml
sudo -u reviewer -H reviewer resume -config /etc/reviewer/config.yaml
```

`pause` is the kill switch. It writes a file rather than signalling the
process, so it works even when the daemon is wedged and it survives a
restart — which also means a paused service stays paused across a reboot
until you resume it.

**Stopping it without killing a review mid-flight:** `pause` first, wait for
the current sweep to finish, then `systemctl stop reviewer`. A review killed
part way through may have posted some of its comments, and nothing on GitHub
records that it did not finish.

## Why the service is locked down

The reviewer runs `claude --permission-mode bypassPermissions`, which lets the
review subprocess run arbitrary commands — that is what makes an unattended
review possible, since the alternative is a permission prompt nobody can
answer. The unit is written so the blast radius is one unprivileged user and
one directory: `ProtectSystem=strict` with a single `ReadWritePaths`,
`NoNewPrivileges`, `PrivateTmp`, and no login shell on the account.

It is still not a sandbox. The review subprocess holds that user's GitHub
token and its Claude credentials, and it can reach the network. Treat the box
as something that can write to your repositories, because it can.
