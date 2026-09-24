set windows-shell := ["pwsh", "-NoLogo", "-NoProfile", "-Command"]

vm_version := env("VM_VERSION", "v1.152.0")
version    := env("VERSION", `git describe --tags --always --dirty`)
data       := env("DATA", "./data")
exe        := if os() == "windows" { ".exe" } else { "" }
vm_bin     := justfile_directory() / ".bin" / "victoria-metrics" + exe

default: build

# UI собирается Bun (web/bunfig.toml), Node.js не нужен
web:
    bun install --cwd web --frozen-lockfile
    bun run --cwd web build

[env("CGO_ENABLED", "0")]
build: web
    go build -trimpath -ldflags="-s -w -X main.version={{version}}" -o homedeck{{exe}} ./cmd/homedeck

# Скачивает VictoriaMetrics в .bin/ со сверкой checksum
vm:
    go run ./tools/fetchvm -version {{vm_version}} -out {{vm_bin}}

test:
    go test -race ./...
    bun run --cwd web test --run

[env("HOMEDECK_VM_BINARY", vm_bin)]
test-integration: vm
    go test -race -count=1 ./...

lint:
    golangci-lint run ./...
    bun run --cwd web typecheck

[env("HOMEDECK_DATA_DIR", data)]
[env("HOMEDECK_RUNTIME_DIR", absolute_path(data) / ".runtime")]
[env("HOMEDECK_VM_BINARY", vm_bin)]
run: build vm
    ./homedeck{{exe}} serve

image:
    docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION={{version}} -t homedeck:{{version}} .

# Удаляет только игнорируемые git артефакты сборки
clean:
    git clean -fdX -- homedeck homedeck.exe web/dist .bin
