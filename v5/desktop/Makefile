VERSION := $(shell cat ../VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build check install clean
build:
	pkg-config --exists gtk4 webkitgtk-6.0
	npm --prefix frontend ci
	npm --prefix frontend run build
	mkdir -p build
	CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o build/voice-type-app ./cmd/voice-type-app

check:
	npm --prefix frontend run build
	npm --prefix frontend test
	CGO_ENABLED=1 go test ./...
	CGO_ENABLED=1 go vet ./...

install: build
	./install.sh

clean:
	rm -rf build frontend/dist
