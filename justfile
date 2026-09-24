set shell := ["bash", "-euo", "pipefail", "-c"]

vm_version := env("VM_VERSION", "v1.152.0")
version    := env("VERSION", `git describe --tags --always --dirty 2>/dev/null || echo dev`)
goos       := `go env GOOS`
goarch     := `go env GOARCH`
vm_bin     := justfile_directory() / ".bin/victoria-metrics-prod"
data       := env("DATA", "./data")

default: build

# Зависимости UI ставятся заново, только если package-lock.json новее node_modules
[private]
web-deps:
    if [ ! -d web/node_modules ] || [ web/package-lock.json -nt web/node_modules ]; then \
        cd web && npm ci --no-audit --no-fund && touch node_modules; \
    fi

web: web-deps
    cd web && npm run build

build: web
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version={{version}}" -o homedeck ./cmd/homedeck

# Скачивает VictoriaMetrics в .bin/ со сверкой checksum
vm:
    #!/usr/bin/env bash
    set -euo pipefail
    [ -x "{{vm_bin}}" ] && exit 0
    name="victoria-metrics-{{goos}}-{{goarch}}-{{vm_version}}"
    base="https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/{{vm_version}}"
    mkdir -p .bin
    curl -fsSL -o .bin/vm.tgz "$base/$name.tar.gz"
    curl -fsSL -o .bin/vm.sum "$base/${name}_checksums.txt"
    sum=$(grep "$name.tar.gz" .bin/vm.sum | sed 's/ .*//')
    (cd .bin && echo "$sum  vm.tgz" | shasum -a 256 -c -)
    tar -xzf .bin/vm.tgz -C .bin victoria-metrics-prod

test:
    go test -race ./...
    cd web && npm test -- --run

test-integration: vm
    HOMEDECK_VM_BINARY={{vm_bin}} go test -race -count=1 ./...

lint:
    golangci-lint run ./...
    cd web && npm run typecheck

run: build vm
    HOMEDECK_DATA_DIR={{data}} HOMEDECK_RUNTIME_DIR={{absolute_path(data)}}/.runtime HOMEDECK_VM_BINARY={{vm_bin}} ./homedeck serve

image:
    docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION={{version}} -t homedeck:{{version}} .

clean:
    rm -rf homedeck web/dist/assets web/dist/index.html .bin
