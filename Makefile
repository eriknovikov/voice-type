VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)
export CGO_ENABLED = 1

.PHONY: all build frontend check native-check install clean
all: build

frontend:
	npm --prefix frontend ci
	npm --prefix frontend run build

build: frontend
	pkg-config --exists gtk4 webkitgtk-6.0
	mkdir -p build
	go build -ldflags="$(LDFLAGS)" -o build/voice-type ./cmd/voice-type

check: frontend
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
