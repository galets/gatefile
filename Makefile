VERSION ?= $(shell cat VERSION 2>/dev/null || echo 0.1.0)
ARCH ?= $(shell dpkg --print-architecture 2>/dev/null || echo amd64)

.PHONY: build test deb clean

build:
	CGO_ENABLED=0 go build -trimpath -o bin/gatefile ./cmd/gatefile

test:
	go test ./...

deb:
	./packaging/build-deb.sh --version $(VERSION) --arch $(ARCH) --out dist

clean:
	rm -rf bin dist
