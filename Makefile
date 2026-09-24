VM_VERSION ?= v1.152.0
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOOS       := $(shell go env GOOS)
GOARCH     := $(shell go env GOARCH)
VM_BIN     := .bin/victoria-metrics-prod
DATA       ?= ./data

.PHONY: all web build test test-integration lint run image clean

all: build

web/node_modules: web/package-lock.json
	cd web && npm ci --no-audit --no-fund
	@touch $@

web: web/node_modules
	cd web && npm run build

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o homedeck ./cmd/homedeck

$(VM_BIN):
	mkdir -p .bin
	curl -fsSL -o .bin/vm.tgz https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/$(VM_VERSION)/victoria-metrics-$(GOOS)-$(GOARCH)-$(VM_VERSION).tar.gz
	curl -fsSL -o .bin/vm.sum https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/$(VM_VERSION)/victoria-metrics-$(GOOS)-$(GOARCH)-$(VM_VERSION)_checksums.txt
	cd .bin && grep "victoria-metrics-$(GOOS)-$(GOARCH)-$(VM_VERSION).tar.gz" vm.sum | sed 's/ .*//' | xargs -I{} sh -c 'echo "{}  vm.tgz" | shasum -a 256 -c -'
	tar -xzf .bin/vm.tgz -C .bin victoria-metrics-prod

test:
	go test -race ./...
	cd web && npm test -- --run

test-integration: $(VM_BIN)
	HOMEDECK_VM_BINARY=$(abspath $(VM_BIN)) go test -race -count=1 ./...

lint:
	golangci-lint run ./...
	cd web && npm run typecheck

run: build $(VM_BIN)
	HOMEDECK_DATA_DIR=$(DATA) HOMEDECK_RUNTIME_DIR=$(abspath $(DATA))/.runtime HOMEDECK_VM_BINARY=$(abspath $(VM_BIN)) ./homedeck serve

image:
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=$(VERSION) -t homedeck:$(VERSION) .

clean:
	rm -rf homedeck web/dist/assets web/dist/index.html .bin
