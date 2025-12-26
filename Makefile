# Build output knobs (override via env/CLI).
EXTENSION     ?= 
DIST_DIR      ?= dist/
GOOS          ?= linux
ARCH          ?= $(shell uname -m)
BUILDINFOSDET ?= 

# Project metadata used for builds, packaging, and docker.
DOCKER_REPO   ?= netsampler/
NAME          := goflow2
VERSION       ?= $(shell git describe --abbrev --long HEAD)
ABBREV        ?= $(shell git rev-parse --short HEAD)
COMMIT        ?= $(shell git rev-parse HEAD)
TAG           ?= $(shell git describe --tags --abbrev=0 HEAD)
VERSION_PKG   ?= $(shell echo $(VERSION) | sed 's/^v//g')
LICENSE       := BSD-3-Clause
URL           := https://github.com/netsampler/goflow2
DESCRIPTION   := GoFlow2: Open-Source and Scalable Network Sample Collector
DATE          :=  $(shell date +%FT%T%z)
BUILDINFOS    ?=  ($(DATE)$(BUILDINFOSDET))
LDFLAGS       ?= '-X main.version=$(VERSION) -X main.buildinfos=$(BUILDINFOS)'
MAINTAINER    := lspgn@users.noreply.github.com
DOCKER_BIN    ?= docker
DOCKER_CMD    ?= build
DOCKER_SUFFIX ?= 

# Default binary output path.
OUTPUT := $(DIST_DIR)goflow2-$(VERSION_PKG)-$(GOOS)-$(ARCH)$(EXTENSION)

.PHONY: proto
# Generate Go protobuf bindings.
proto:
	@echo generating protobuf
	protoc --go_opt=paths=source_relative --go_out=. pb/*.proto
	protoc --go_opt=paths=source_relative --go_out=. cmd/enricher/pb/*.proto

.PHONY: vet
# Run Go static analysis on the main package.
vet:
	go vet cmd/goflow2/main.go

.PHONY: test
# Run Go test suite.
test:
	go test -v ./...

.PHONY: prepare
# Ensure dist directory exists.
prepare:
	mkdir -p $(DIST_DIR)

PHONY: clean
# Remove build artifacts.
clean:
	rm -rf $(DIST_DIR)

.PHONY: build
# Build the goflow2 binary.
build: prepare
	CGO_ENABLED=0 go build -ldflags $(LDFLAGS) -o $(OUTPUT) cmd/goflow2/main.go

.PHONY: docker
# Build docker image for the current version.
docker:
	$(DOCKER_BIN) $(DOCKER_CMD) \
        --build-arg LDFLAGS=$(LDFLAGS) \
        --build-arg CREATED="$(DATE)" \
        --build-arg MAINTAINER="$(MAINTAINER)" \
        --build-arg URL="$(URL)" \
        --build-arg NAME="$(NAME)" \
        --build-arg DESCRIPTION="$(DESCRIPTION)" \
        --build-arg LICENSE="$(LICENSE)" \
        --build-arg VERSION="$(VERSION)" \
        --build-arg REV="$(COMMIT)" \
        -t $(DOCKER_REPO)$(NAME):$(ABBREV)$(DOCKER_SUFFIX) .

.PHONY: push-docker
# Push docker image tagged with the current abbrev.
push-docker:
	$(DOCKER_BIN) push $(DOCKER_REPO)$(NAME):$(ABBREV)$(DOCKER_SUFFIX)

.PHONY: docker-manifest
# Create and push multi-arch manifest for abbrev and latest tags.
docker-manifest:
	$(DOCKER_BIN) manifest create $(DOCKER_REPO)$(NAME):$(ABBREV) \
	    --amend $(DOCKER_REPO)$(NAME):$(ABBREV)-amd64 \
	    --amend $(DOCKER_REPO)$(NAME):$(ABBREV)-arm64
	$(DOCKER_BIN) manifest push $(DOCKER_REPO)$(NAME):$(ABBREV)

	$(DOCKER_BIN) manifest create $(DOCKER_REPO)$(NAME):latest \
	    --amend $(DOCKER_REPO)$(NAME):$(ABBREV)-amd64 \
	    --amend $(DOCKER_REPO)$(NAME):$(ABBREV)-arm64
	$(DOCKER_BIN) manifest push $(DOCKER_REPO)$(NAME):latest

.PHONY: docker-manifest-buildx
# Create multi-arch manifest using buildx imagetools (abbrev tag).
docker-manifest-buildx:
	$(DOCKER_BIN) buildx imagetools create \
	    -t $(DOCKER_REPO)$(NAME):$(ABBREV) \
	    $(DOCKER_REPO)$(NAME):$(ABBREV)-amd64 \
	    $(DOCKER_REPO)$(NAME):$(ABBREV)-arm64

.PHONY: docker-manifest-release
# Create and push multi-arch manifest for release version.
docker-manifest-release:
	$(DOCKER_BIN) manifest create $(DOCKER_REPO)$(NAME):$(VERSION) \
	    --amend $(DOCKER_REPO)$(NAME):$(ABBREV)-amd64 \
	    --amend $(DOCKER_REPO)$(NAME):$(ABBREV)-arm64
	$(DOCKER_BIN) manifest push $(DOCKER_REPO)$(NAME):$(VERSION)

.PHONY: docker-manifest-release-buildx
# Create release manifest using buildx imagetools.
docker-manifest-release-buildx:
	$(DOCKER_BIN) buildx imagetools create \
	    -t $(DOCKER_REPO)$(NAME):$(VERSION) \
	    $(DOCKER_REPO)$(NAME):$(ABBREV)-amd64 \
	    $(DOCKER_REPO)$(NAME):$(ABBREV)-arm64

.PHONY: package-deb
# Build a Debian package using fpm.
package-deb: prepare
	fpm -s dir -t deb -n $(NAME) -v $(VERSION_PKG) \
        --maintainer "$(MAINTAINER)" \
        --description "$(DESCRIPTION)"  \
        --url "$(URL)" \
        --architecture $(ARCH) \
        --license "$(LICENSE)" \
        --package $(DIST_DIR) \
        $(OUTPUT)=/usr/bin/goflow2 \
        package/goflow2.service=/lib/systemd/system/goflow2.service \
        package/goflow2.env=/etc/default/goflow2

.PHONY: package-rpm
# Build an RPM package using fpm.
package-rpm: prepare
	fpm -s dir -t rpm -n $(NAME) -v $(VERSION_PKG) \
        --maintainer "$(MAINTAINER)" \
        --description "$(DESCRIPTION)" \
        --url "$(URL)" \
        --architecture $(ARCH) \
        --license "$(LICENSE) "\
        --package $(DIST_DIR) \
        $(OUTPUT)=/usr/bin/goflow2 \
        package/goflow2.service=/lib/systemd/system/goflow2.service \
        package/goflow2.env=/etc/default/goflow2
