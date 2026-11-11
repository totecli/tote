VERSION ?= dev
KEYS ?=
LDKEYS = $(if $(KEYS),-X 'github.com/totecli/tote/internal/update.releaseKeys=$(KEYS)')
export CGO_ENABLED = 0
export GOTOOLCHAIN = local

.PHONY: build check test short e2e slow race cover golden snapshot

# Test layers:
#   test    everything, in several minutes (the frame tests of internal/ui alone take about five):
#           unit tests, view frames against the reference files of internal/ui/testdata, the
#           terminal injection probes and the tmux end-to-end tests of internal/e2e (skipped with
#           a message when tmux is not installed; set TOTE_REQUIRE_TOOLS=1 to fail instead).
#           Every target that runs the interface tests sets its own -timeout, the default of
#           Go (10 minutes) is too short for a loaded machine.
#   short   the same without tmux (go test -short skips internal/e2e).
#   e2e     the tmux end-to-end tests only, verbose.
#   slow    the single core passes (30 ssh round trips with a short pause, then 100 round trips
#           of a child that returns at once, under taskset -c, GOMAXPROCS=1 when taskset is missing): skipped unless TOTE_SLOW_TESTS=1, which this target sets.
#   race    the data race detector on the interface, the update code, the command line and the
#           end-to-end tests (the only target that needs a C toolchain). The interface frames
#           take over 30 minutes under it on a CI runner, so CI does not run them with it: run
#           this target before every release (docs/RELEASING.md).
#   cover   statement coverage of internal/ui, written to bin/coverage.out (git-ignored).
#   golden  rewrites the reference frames: review them with git diff internal/ui/testdata.

# build takes VERSION, and KEYS for a release build: the minisign public keys that sign the
# releases, separated by spaces, which "tote update" trusts (without them it refuses to update).
build:
	go build -trimpath -mod=readonly -ldflags "-s -w -X main.version=$(VERSION) $(LDKEYS)" -o bin/tote ./cmd/tote

test:
	go test -timeout 15m ./...

short:
	go test -short -timeout 15m ./...

e2e:
	go test -count=1 -v -timeout 15m ./internal/e2e

slow:
	TOTE_SLOW_TESTS=1 go test -count=1 -v -timeout 10m -run 'TestSingleCore|TestReviewSingleCoreQuickChild' ./internal/e2e

race:
	CGO_ENABLED=1 go test -race -count=1 -timeout 30m ./internal/ui ./internal/update ./cmd/tote ./internal/e2e

cover:
	mkdir -p bin
	go test -count=1 -timeout 15m -coverprofile=bin/coverage.out ./internal/ui
	go tool cover -func=bin/coverage.out | tail -n 1

golden:
	go test -count=1 -timeout 15m -run '^(TestFrames|TestHoverFrames|TestSelectionFrames)$$' ./internal/ui -update

# snapshot is a release dry run: archives, .deb files and checksums in dist/ (git-ignored), unsigned
# and uploaded nowhere. It needs goreleaser v2 in PATH. Set TOTE_RELEASE_KEYS to embed real keys.
snapshot:
	goreleaser release --snapshot --skip=sign --clean

check:
	test -z "$$(gofmt -l .)"
	scripts/docs.sh
	go vet ./...
	golangci-lint run
	go test -timeout 15m ./...
	govulncheck ./...
