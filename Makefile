VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)
export CGO_ENABLED = 1
export PKG_CONFIG_PATH := $(CURDIR)/build/transcribe/pkgconfig:$(PKG_CONFIG_PATH)

.PHONY: all build frontend native-engine check native-check install clean
all: build

frontend:
	npm --prefix frontend ci
	npm --prefix frontend run build

native-engine:
	./scripts/build-transcribe.sh

build: frontend native-engine
	pkg-config --exists gtk4 webkitgtk-6.0
	mkdir -p build
	go build -ldflags="$(LDFLAGS)" -o build/voice-type ./cmd/voice-type

check: frontend native-engine
	@test -z "$$(gofmt -l *.go cmd/voice-type/*.go)" || { gofmt -l *.go cmd/voice-type/*.go; exit 1; }
	npm --prefix frontend test
	go vet ./...
	go test -race ./...
	sh -n install.sh
	sh -n uninstall.sh
	shellcheck -s sh install.sh uninstall.sh

native-check: build
	VOICE_TYPE_DESKTOP_TEST_BINARY="$(CURDIR)/build/voice-type" go test -race -run '^TestNativeTray$$' -v
	python3 scripts/smoke.py

install:
	./install.sh --local

clean:
	rm -rf build frontend/dist
