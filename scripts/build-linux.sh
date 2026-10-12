#!/usr/bin/env bash
# Build the Linux binary for the service box.
#
# Cross-compiled rather than built on the box, because the result is a
# statically linked ELF with no libc dependency: the server needs no Go
# toolchain, no build tools, and nothing to keep patched beyond git, gh and
# claude. CGO is off explicitly -- the default is on, and a cgo build would
# link against the build machine's libc and then fail to start on a box with
# an older one.
set -euo pipefail

cd "$(dirname "$0")/.."

OUT=${1:-dist/reviewer-linux-amd64}
mkdir -p "$(dirname "$OUT")"

# -s -w strips the symbol table and DWARF: about 30% off a binary that nobody
# debugs on the server. The build id survives, so a crash can still be tied to
# a commit.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
	-trimpath -ldflags "-s -w" -o "$OUT" ./cmd/reviewer

echo "built $OUT"
ls -l "$OUT"
if command -v sha256sum >/dev/null; then
	sha256sum "$OUT"
fi
echo
echo "copy it over and install with:"
echo "  scp $OUT <host>:/tmp/reviewer"
echo "  ssh <host> 'sudo install -m 0755 /tmp/reviewer /usr/local/bin/reviewer'"
