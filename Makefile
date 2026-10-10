export COMPONENT := amplifi
export MAINTAINER
export PACKAGE_VERSION

BINARIES := $(notdir $(patsubst %/,%,$(wildcard cmd/*/)))

OSES          := darwin linux
ARCHITECTURES := amd64 arm64

# VERSION is the release tag; CI passes it explicitly (VERSION=$GITHUB_REF_NAME).
VERSION         ?= $(shell git describe --tags --match 'v[0-9]*' --dirty --always 2>/dev/null || echo dev)
PACKAGE_VERSION := $(if $(filter v%,$(VERSION)),$(patsubst v%,%,$(VERSION)),0.0.0+$(VERSION))
COMMIT          := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_TIME      := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
MAINTAINER      := Bjørn Borud <borud@users.noreply.github.com>
APPINFO         := github.com/borud/amplifi/internal/appinfo
LINKER_OPTS     := -X $(APPINFO).Version=$(VERSION) \
	-X $(APPINFO).Commit=$(COMMIT) \
	-X $(APPINFO).BuildTime=$(BUILD_TIME)

# Pure Go; libsodium is only needed for the optional sodium-test target.
export CGO_ENABLED := 0

.PHONY: all
.PHONY: build
.PHONY: build-release
.PHONY: package
.PHONY: archives
.PHONY: checksums
.PHONY: dist
.PHONY: install
.PHONY: test
.PHONY: race-test
.PHONY: sodium-test
.PHONY: vet
.PHONY: staticcheck
.PHONY: lint
.PHONY: clean
.PHONY: install-deps

all: vet lint staticcheck test build

build: $(BINARIES)

$(BINARIES):
	@echo "*** building $@"
	@cd cmd/$@ && go build -ldflags "$(LINKER_OPTS)" -trimpath -o ../../bin/$@

# Cross-compile every binary for every OS and architecture as bin/<bin>-<arch>-<os>.
build-release:
	@for os in $(OSES); do \
		for arch in $(ARCHITECTURES); do \
			for bin in $(BINARIES); do \
				echo "*** building $$bin ($$os/$$arch)" && \
				GOOS=$$os GOARCH=$$arch go build -ldflags "$(LINKER_OPTS)" -trimpath -o bin/$$bin-$$arch-$$os ./cmd/$$bin || exit 1; \
			done \
		done \
	done

# Build a .deb per Linux architecture into dist/.
package: build-release
	@mkdir -p dist
	@for arch in $(ARCHITECTURES); do \
		echo "*** packaging $(COMPONENT) $(PACKAGE_VERSION) ($$arch)" && \
		mkdir -p bin/deb && cp bin/$(COMPONENT)-$$arch-linux bin/deb/$(COMPONENT) && \
		GOARCH=$$arch nfpm pkg --packager deb --config deb/nfpm.yaml --target dist/$(COMPONENT)_$(PACKAGE_VERSION)_$$arch.deb || exit 1; \
	done

# Build a tarball per OS and architecture into dist/.
archives: build-release
	@mkdir -p dist
	@for os in $(OSES); do \
		for arch in $(ARCHITECTURES); do \
			name=$(COMPONENT)_$(VERSION)_$${os}_$${arch} && \
			echo "*** archiving $$name" && \
			rm -rf bin/$$name && mkdir -p bin/$$name && \
			for bin in $(BINARIES); do cp bin/$$bin-$$arch-$$os bin/$$name/$$bin || exit 1; done && \
			cp README.md LICENSE bin/$$name/ && \
			tar -czf dist/$$name.tar.gz -C bin $$name || exit 1; \
		done \
	done

checksums:
	@echo "*** $@"
	@cd dist && rm -f SHA256SUMS && shasum -a 256 * > SHA256SUMS

# Everything a release publishes, in dist/.
dist:
	@rm -rf dist
	@$(MAKE) --no-print-directory package archives
	@$(MAKE) --no-print-directory checksums

# Install the binaries into $GOBIN (default $GOPATH/bin, usually ~/go/bin).
install:
	@echo "*** $@"
	@go install -ldflags "$(LINKER_OPTS)" -trimpath ./cmd/...

test:
	@echo "*** $@"
	@go test -timeout 2m ./...

race-test:
	@echo "*** $@"
	@CGO_ENABLED=1 go test -race -timeout 2m ./...

# Cross-check the secretstream port against libsodium (needs libsodium + cgo).
sodium-test:
	@echo "*** $@"
	@CGO_ENABLED=1 go test -tags sodium ./internal/secretstream/...

vet:
	@echo "*** $@"
	@go vet ./...

staticcheck:
	@echo "*** $@"
	@staticcheck ./...

lint:
	@echo "*** $@"
	@revive ./...

clean:
	@echo "*** cleaning"
	@rm -rf bin
	@rm -rf dist

install-deps:
	@go install github.com/mgechev/revive@latest
	@go install honnef.co/go/tools/cmd/staticcheck@latest
