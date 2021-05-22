EXTENSION ?= 
DIST_DIR ?= dist/
GOOS ?= linux
ARCH ?= $(shell uname -m)
BUILDINFOSDET ?= 

DOCKER_REPO   := netsampler/
NAME          := goflow2
VERSION       ?= $(shell git describe --abbrev --long HEAD)
ABBREV        ?= $(shell git rev-parse --short HEAD)
COMMIT        ?= $(shell git rev-parse HEAD)
TAG           ?= $(shell git describe --tags --abbrev=0 HEAD)
VERSION_PKG   ?= $(shell echo $(VERSION) | sed 's/^v//g')
ARCH          := x86_64
LICENSE       := BSD-3-Clause
URL           := https://github.com/netsampler/goflow2
DESCRIPTION   := GoFlow2: Open-Source and Scalable Network Sample Collector
DATE          :=  $(shell date +%FT%T%z)
BUILDINFOS    ?=  ($(DATE)$(BUILDINFOSDET))
LDFLAGS       ?= '-X main.version=$(VERSION) -X main.buildinfos=$(BUILDINFOS)'
MAINTAINER    := lspgn@users.noreply.github.com

OUTPUT := $(DIST_DIR)goflow2-$(VERSION_PKG)-$(GOOS)-$(ARCH)$(EXTENSION)

.PHONY: proto
proto:
	@echo generating protobuf
	protoc --go_out=. pb/*.proto
	protoc --go_out=. cmd/enricher/pb/*.proto

.PHONY: vet
vet:
	go vet cmd/goflow2/main.go

.PHONY: test
test:
	go test -v ./...

.PHONY: prepare
prepare:
	mkdir -p $(DIST_DIR)

PHONY: clean
clean:
	rm -rf $(DIST_DIR)

.PHONY: build
build: prepare
	go build -ldflags $(LDFLAGS) -o $(OUTPUT) cmd/goflow2/main.go 

.PHONY: docker
docker:
	docker build \
        --build-arg LDFLAGS=$(LDFLAGS) \
        --build-arg CREATED="$(DATE)" \
        --build-arg MAINTAINER="$(MAINTAINER)" \
        --build-arg URL="$(URL)" \
        --build-arg NAME="$(NAME)" \
        --build-arg DESCRIPTION="$(DESCRIPTION)" \
        --build-arg LICENSE="$(LICENSE)" \
        --build-arg VERSION="$(VERSION)" \
        --build-arg REV="$(COMMIT)" \
        -t $(DOCKER_REPO)$(NAME):$(ABBREV) .

.PHONY: push-docker
push-docker:
	docker push $(DOCKER_REPO)$(NAME):$(ABBREV)
	docker tag $(DOCKER_REPO)$(NAME):$(ABBREV) $(DOCKER_REPO)$(NAME):latest
	docker push $(DOCKER_REPO)$(NAME):latest

.PHONY: push-docker-release
push-docker-release:
	docker tag $(DOCKER_REPO)$(NAME):$(ABBREV) $(DOCKER_REPO)$(NAME):$(VERSION)
	docker push $(DOCKER_REPO)$(NAME):$(VERSION)

.PHONY: package-deb
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