VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)
export CGO_ENABLED = 1
export PKG_CONFIG_PATH := $(CURDIR)/build/transcribe/pkgconfig:$(PKG_CONFIG_PATH)

.PHONY: all build frontend native-engine check native-check install clean dev
all: build

frontend:
	npm --prefix frontend ci
	npm --prefix frontend run build

native-engine:
	./scripts/build-transcribe.sh

build: frontend native-engine
	pkg-config --exists gtk4 webkitgtk-6.0
	mkdir -p build
	go build -tags production -ldflags="$(LDFLAGS)" -o build/voice-type ./cmd/voice-type
	@go version -m build/voice-type | grep -q -- '-tags=production' || { echo 'release build is missing the production tag' >&2; exit 1; }

dev:
	wails3 dev -config ./build/config.yml

check: frontend native-engine
	@test -z "$$(gofmt -l *.go cmd/voice-type/*.go)" || { gofmt -l *.go cmd/voice-type/*.go; exit 1; }
	npm --prefix frontend test
	go vet ./...
	go test -race ./...
	python3 -c 'import ast, pathlib; [ast.parse(p.read_text()) for p in pathlib.Path("scripts").glob("*.py")]'
	sh -n install.sh uninstall.sh scripts/build-transcribe.sh
	shellcheck -s sh install.sh uninstall.sh scripts/build-transcribe.sh

native-check: build
	VOICE_TYPE_DESKTOP_TEST_BINARY="$(CURDIR)/build/voice-type" go test -race -count=1 -run '^TestNativeTray$$' -v
	python3 scripts/smoke.py

install:
	./install.sh --local

clean:
	@if [ -d build ]; then find build -mindepth 1 -maxdepth 1 ! -name config.yml -exec rm -rf {} +; fi
	rm -rf frontend/dist
