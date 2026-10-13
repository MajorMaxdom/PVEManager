VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

.PHONY: all build web test e2e demo screenshots release clean
all: web build

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/pve-manager ./cmd/pve-manager
web:
	cd web/app && npm ci && npm run build
test:
	go vet ./... && go test ./...
e2e:
	web/e2e/run.sh
demo:
	web/e2e/demo.sh
screenshots:
	web/e2e/demo.sh screenshots
release: web
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist/pve-manager-linux-amd64 ./cmd/pve-manager
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist/pve-manager-linux-arm64 ./cmd/pve-manager
	cd dist && sha256sum pve-manager-linux-* > SHA256SUMS
clean:
	rm -rf bin dist
