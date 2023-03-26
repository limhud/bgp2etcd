VERSION := 0.1

DIST_VERSION = 11

BUILD_DATE := $(shell date +%F_%R)
BUILD_HASH := $(shell git rev-parse --short HEAD)
GO_VERSION := $(shell go version | cut -d ' ' -f 3)
TMP_DIR= tmp
BUILD_DIR = $(TMP_DIR)/build
PACKAGING_TMP_DIR = $(TMP_DIR)/packaging
COVERAGE_DIR = ${TMP_DIR}/coverage

WORKDIR := $(shell pwd)
USER := $(shell id -u)

GOTEST ?= gotest
GODEBUG ?= dlv

GOLINT ?= golint
GOLINT_ARGS =

GOVET ?= go vet
GOVET_ARGS =

BIN_PATH := $(BUILD_DIR)/bin
BIN_LIST = $(patsubst cmd/%,%,$(wildcard cmd/*))
PKG_LIST = "internal/bgp2etcd" "internal/config" "internal/bgp" "internal/etcd" "internal/service" "internal/message" "internal/errorcode"
TESTING_PKG_LIST = $(wildcard internal/testing/*)

mesg_start = echo "$(shell tty -s && tput setaf 4)$(1):$(shell tty -s && tput sgr0) $(2)"
mesg_step = echo "$(1)"
mesg_ok = echo "result: $(shell tty -s && tput setaf 2)ok$(shell tty -s && tput sgr0)"
mesg_fail = (echo "result: $(shell tty -s && tput setaf 1)fail$(shell tty -s && tput sgr0)" && false)
mesg_fail_continue = (echo "result: $(shell tty -s && tput setaf 1)fail$(shell tty -s && tput sgr0)")

all: clean build_linux

clean:
	@(for bin in $(BIN_LIST); do \
		$(call mesg_start,clean,Removing $(BIN_PATH)/$$bin); \
		rm -vf $(BIN_PATH)/$$bin.* && \
		$(call mesg_ok) || $(call mesg_fail); \
		done)

mrproper:
	@$(call mesg_start,clean,Removing $(TMP_DIR))
	@rm -rvf $(TMP_DIR) && \
		$(call mesg_ok) || $(call mesg_fail)

prepare_build: clean
	@$(call mesg_start,build,Preparing build directory...)
	@(install -d -m 0755 $(BUILD_DIR)/bin) && \
	$(call mesg_ok) || $(call mesg_fail)

build_debug: prepare_build
	@$(call mesg_start,build,Preparing vendor directory...)
	@go mod vendor && \
	$(call mesg_ok) || $(call mesg_fail)
	@(for bin in $(BIN_LIST); do \
		$(call mesg_start,build,Building $$bin binary...); \
		go build -v \
		-gcflags "all=-N -l" \
		-ldflags=all="\
		-X main.version=$(VERSION) \
		-X main.buildDate=$(BUILD_DATE) \
		-X main.buildHash=$(BUILD_HASH) \
		" \
		-o $(BIN_PATH)/$$bin ./cmd/$$bin && \
		$(call mesg_ok) || $(call mesg_fail); \
		done)

dockerbuilder:
ifdef BUILD_WITH_DOCKER
	@$(call mesg_start,dockerbuild,Building image for go-debian:$(DIST_VERSION) with go version $(GO_VERSION))
	@(cd build/go-debian/$(DIST_VERSION)/ && docker build --build-arg=go_version=$(GO_VERSION) -t go-debian:$(DIST_VERSION) .) && \
		$(call mesg_ok) || $(call mesg_fail)
endif

build_linux: prepare_build dockerbuilder
ifdef BUILD_WITH_DOCKER
	$(eval DOCKER_BUILD = docker run --user $(USER) --rm=true -e "GOPATH=${GOPATH}" -e "HOME=/tmp" -v "${GOPATH}:${GOPATH}" -w "${PWD}" go-debian:$(DIST_VERSION))
endif
	@$(call mesg_start,build,Preparing vendor directory...)
	@go mod vendor && \
	$(call mesg_ok) || $(call mesg_fail)
	@(for bin in $(BIN_LIST); do \
		$(call mesg_start,build,Building $$bin binary...); \
		$(DOCKER_BUILD) go build -v \
		-ldflags=all="-s -w \
		-X main.version=$(VERSION) \
		-X main.buildDate=$(BUILD_DATE) \
		-X main.buildHash=$(BUILD_HASH) \
		" \
		-o $(BIN_PATH)/$$bin ./cmd/$$bin && \
		$(call mesg_ok) || $(call mesg_fail); \
		done)

check:
	@(for pkg in $(PKG_LIST) $(BIN_LIST:%=cmd/%) $(TESTING_PKG_LIST); do \
    $(call mesg_start,lint,Checking $$pkg sources...); \
    $(GOLINT) $(GOLINT_ARGS) ./$$pkg && \
    $(call mesg_ok) || $(call mesg_fail); \
    $(call mesg_start,vet,Checking $$pkg sources...); \
    $(GOVET) $(GOVET_ARGS) ./$$pkg && \
    $(call mesg_ok) || $(call mesg_fail); \
    done)
	@$(call mesg_start,staticcheck,Checking...)
	@(staticcheck ./...) && \
	$(call mesg_ok) || $(call mesg_fail)
	@$(call mesg_start,gosec,Checking...)
	@(gosec ./...) && \
	$(call mesg_ok) || $(call mesg_fail)


packaging_clean:
	@$(call mesg_start,deb,Cleaning $(PACKAGING_TMP_DIR)...)
	@(rm -rvf ./$(PACKAGING_TMP_DIR)) && \
	$(call mesg_ok) || $(call mesg_fail)

dockerpackager:
	@$(call mesg_start,dockerbuild,Building image for fpm-debian:$(DIST_VERSION))
	@(cd build/package/deb/$(DIST_VERSION)/ && docker build -t fpm-debian:$(DIST_VERSION) .) && \
		$(call mesg_ok) || $(call mesg_fail)

deb: build_linux packaging_clean dockerpackager
	@$(call mesg_start,deb,Preparing packaging source files...)
	@(install -d -m 0755 $(PACKAGING_TMP_DIR) && \
		cp -vrf build/package/deb/scripts $(PACKAGING_TMP_DIR) && \
		install -d -m 0755 $(PACKAGING_TMP_DIR)/rootfs/etc $(PACKAGING_TMP_DIR)/rootfs/usr/share/man/man1 $(PACKAGING_TMP_DIR)/rootfs/usr/share/man/man5 $(PACKAGING_TMP_DIR)/rootfs/usr/bin $(PACKAGING_TMP_DIR)/rootfs/usr/sbin && \
		cp -v init/*.service $(PACKAGING_TMP_DIR) && \
		cp -v configs/bgp2etcd.yml $(PACKAGING_TMP_DIR)/rootfs/etc && \
		cp -v build/package/man/*.1 $(PACKAGING_TMP_DIR)/rootfs/usr/share/man/man1 && \
		cp -v build/package/man/*.5 $(PACKAGING_TMP_DIR)/rootfs/usr/share/man/man5 && \
		cp -v $(BIN_PATH)/bgp2etcd $(PACKAGING_TMP_DIR)/rootfs/usr/bin/bgp2etcd) && \
		$(call mesg_ok) || $(call mesg_fail)
	@$(call mesg_start,deb,Building package for debian:$(DIST_VERSION)...)
	@(docker run --user $(USER) --rm=true -v "$(WORKDIR)/$(PACKAGING_TMP_DIR):/src/" fpm-debian:$(DIST_VERSION) fpm -s dir -t deb -n bgp2etcd -v $(VERSION) \
		--description "Daemon populating etcd with bgp infos" -a all --category misc --vendor limhud --license MIT \
		--prefix=/ -C /src/rootfs \
		--deb-systemd /src/bgp2etcd.service \
		--after-install /src/scripts/after-install.deb.sh --before-remove /src/scripts/before-remove.deb.sh --after-remove /src/scripts/after-remove.deb.sh .) && \
	$(call mesg_ok) || $(call mesg_fail)

test:
	@for pkg in $(PKG_LIST); do \
		$(call mesg_start,test,Testing $$pkg...); \
		install -d -m 0755 $(COVERAGE_DIR)/$$(dirname $$pkg) && \
		$(GOTEST) -cover -coverprofile=$(COVERAGE_DIR)/$$pkg.test ./$$pkg && \
		$(call mesg_ok) || $(call mesg_fail_continue); \
		done
	@for pkg in $(PKG_LIST); do \
		$(call mesg_start,test,Generating html report for $$pkg...); \
		go tool cover -html=$(COVERAGE_DIR)/$$pkg.test -o $(COVERAGE_DIR)/$$pkg.test.html && \
		$(call mesg_ok) || $(call mesg_fail); \
		done
