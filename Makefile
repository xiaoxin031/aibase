.PHONY: test build

test:
	go test ./...
	go vet ./...

build:
	@mkdir -p dist
	@set -e; for os in darwin linux windows; do \
	  for arch in amd64 arm64; do \
	    suffix=""; if [ "$$os" = windows ]; then suffix=".exe"; fi; \
	    CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '-s -w' -o dist/aibase-$$os-$$arch$$suffix ./cmd/aibase; \
	  done; \
	done
