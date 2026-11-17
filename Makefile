VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)
DEV_LDFLAGS := -s -w -X main.version=$(VERSION)-dev -X github.com/eriknovikov/voice-type.developmentBuild=true
HOST_OS := $(shell uname -s)
export CGO_ENABLED = 1
export PKG_CONFIG_PATH := $(CURDIR)/build/transcribe/pkgconfig:$(PKG_CONFIG_PATH)
ifeq ($(HOST_OS),Darwin)
export MACOSX_DEPLOYMENT_TARGET = 13.0
export CGO_CFLAGS := -O2 -g $(CGO_CFLAGS) -mmacosx-version-min=13.0
export CGO_CXXFLAGS := -O2 -g $(CGO_CXXFLAGS) -mmacosx-version-min=13.0
export CGO_LDFLAGS := $(CGO_LDFLAGS) -mmacosx-version-min=13.0
endif

PACKAGE_DIR := build/packages
PACKAGE_GOARCH := $(shell go env GOARCH)
PACKAGE_EPOCH := $(shell git log -1 --format=%ct)
PACKAGERS := deb rpm
# Arch Linux officially supports x86_64 only; Arch Linux ARM is a separate
# distribution, so do not publish an aarch64 package under Arch's name.
ifeq ($(PACKAGE_GOARCH),amd64)
PACKAGERS += archlinux
endif
NFPM_VERSION := v2.46.3
NFPM ?= nfpm

.PHONY: all build frontend native-engine check native-check install clean dev dev-backend dev-run system-deps mac-bundle mac-verify mac-release packages verify-packages
all: build

frontend:
	npm --prefix frontend ci
	npm --prefix frontend run build

native-engine:
	./scripts/build-transcribe.sh

system-deps:
ifeq ($(HOST_OS),Linux)
	pkg-config --exists gtk4 webkitgtk-6.0
endif

build: frontend native-engine system-deps
	mkdir -p build
	go build -tags production -ldflags="$(LDFLAGS)" -o build/voice-type ./cmd/voice-type
	@go version -m build/voice-type | grep -q -- '-tags=production' || { echo 'release build is missing the production tag' >&2; exit 1; }
ifeq ($(HOST_OS),Darwin)
	$(MAKE) mac-bundle
endif

mac-bundle:
	@test "$(HOST_OS)" = Darwin || { echo 'mac-bundle requires macOS' >&2; exit 1; }
	python3 scripts/bundle-macos.py --binary build/voice-type --output 'build/Voice Type.app' --version '$(VERSION)'
	codesign --force --sign - --entitlements packaging/macos/entitlements.plist 'build/Voice Type.app'

mac-verify:
	python3 scripts/macos-release.py verify --arch '$(PACKAGE_GOARCH)' --version '$(VERSION)'

mac-release: build mac-verify
	python3 scripts/macos-release.py package --arch '$(PACKAGE_GOARCH)' --version '$(VERSION)'

dev-backend: native-engine system-deps
	mkdir -p frontend/dist
	@test -f frontend/dist/index.html || printf '%s\n' '<!doctype html><html><body></body></html>' > frontend/dist/index.html
	go build -ldflags="$(DEV_LDFLAGS)" -o build/voice-type-dev ./cmd/voice-type
ifeq ($(HOST_OS),Darwin)
	python3 scripts/bundle-macos.py --binary build/voice-type-dev --output 'build/Voice Type Dev.app' --version '$(VERSION)-dev' --development
	codesign --force --sign - --entitlements packaging/macos/entitlements.plist 'build/Voice Type Dev.app'
endif

dev-run:
ifeq ($(HOST_OS),Darwin)
	'./build/Voice Type Dev.app/Contents/MacOS/voice-type'
else
	./build/voice-type-dev
endif

dev:
	wails3 dev -config ./build/config.yml

check: frontend native-engine
	@test -z "$$(gofmt -l *.go cmd/voice-type/*.go scripts/*.go)" || { gofmt -l *.go cmd/voice-type/*.go scripts/*.go; exit 1; }
	npm --prefix frontend test
	go vet ./...
	go test -race ./...
	go test -race -tags malgo_test -run '^TestMalgo' .
	python3 -c 'import ast, pathlib; [ast.parse(p.read_text()) for p in pathlib.Path("scripts").glob("*.py")]'
	python3 -m unittest discover -s scripts -p 'test_*.py'
	sh -n install.sh uninstall.sh scripts/build-transcribe.sh scripts/verify-packages.sh
	shellcheck -s sh install.sh uninstall.sh scripts/build-transcribe.sh scripts/verify-packages.sh

packages:
	@command -v "$(NFPM)" >/dev/null 2>&1 || { printf '%s\n' 'nfpm is required: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@$(NFPM_VERSION)'; exit 1; }
	@$(MAKE) VOICE_TYPE_VULKAN=0 build
	@set -e; \
	command -v readelf >/dev/null 2>&1 || { printf '%s\n' 'readelf is required to determine the glibc floor.' >&2; exit 1; }; \
	GLIBC_MIN=$$(readelf -V build/voice-type | grep -o 'GLIBC_[0-9.]*' | sed 's/^GLIBC_//' | sort -Vu | tail -1); \
	[ -n "$$GLIBC_MIN" ] || { printf '%s\n' 'Cannot determine the glibc floor.' >&2; exit 1; }; \
	rm -rf "$(PACKAGE_DIR)"; \
	mkdir -p "$(PACKAGE_DIR)"; \
	for packager in $(PACKAGERS); do \
		printf 'Packaging %s (glibc >= %s)...\n' "$$packager" "$$GLIBC_MIN"; \
		VERSION="$(VERSION)" GOARCH="$(PACKAGE_GOARCH)" GLIBC_MIN="$$GLIBC_MIN" SOURCE_DATE_EPOCH="$(PACKAGE_EPOCH)" \
			"$(NFPM)" package --config nfpm.yaml --packager "$$packager" --target "$(PACKAGE_DIR)"; \
	done; \
	ls -l "$(PACKAGE_DIR)"

verify-packages:
	./scripts/verify-packages.sh "$(PACKAGE_DIR)"

native-check: build
	VOICE_TYPE_DESKTOP_TEST_BINARY="$(CURDIR)/build/voice-type" go test -race -count=1 -run '^TestNativeTray$$' -v
	python3 scripts/smoke.py

install:
	./install.sh

clean:
	@if [ -d build ]; then find build -mindepth 1 -maxdepth 1 ! -name config.yml -exec rm -rf {} +; fi
	rm -rf frontend/dist
