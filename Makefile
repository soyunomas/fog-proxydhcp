APP := fog-proxy
PACKAGE := fog-proxydhcp
VERSION ?= 0.1.0
DEB_ARCH ?= amd64
DEB_BUILD_DIR := build/deb
DEB_DIST_DIR := dist
CONFIG := config.toml
PREFIX ?= /usr/local
SYSCONFDIR ?= /etc/fog-proxydhcp
SYSTEMD_DIR ?= /etc/systemd/system
LOGDIR ?= /var/log/fog-proxydhcp
LOGROTATE_DIR ?= /etc/logrotate.d
LOGFILE := $(LOGDIR)/$(APP).log
GO ?= go
BIN_DIST_DIR := dist/bin
BUILD_FLAGS := -trimpath -ldflags="-w -s"

.PHONY: help deps tidy build \
	openwrt openwrt-amd64 openwrt-armv7 openwrt-arm64 openwrt-mips openwrt-mipsel \
	raspi raspi-armv6 raspi-armv7 raspi-arm64 \
	deb clean run run-debug install uninstall install-service uninstall-service \
	service-start service-stop service-restart service-status logs logs-journal check ports fmt

help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps: ## Download Go dependencies.
	$(GO) mod tidy

tidy: ## Alias for deps.
	$(GO) mod tidy

fmt: ## Format Go code.
	$(GO) fmt ./...

check: fmt ## Run static compile check.
	$(GO) test ./...

build: ## Build a static binary for the current Linux architecture.
	CGO_ENABLED=0 GOOS=linux $(GO) build $(BUILD_FLAGS) -o $(APP) .

openwrt: openwrt-amd64 openwrt-armv7 openwrt-arm64 openwrt-mips openwrt-mipsel ## Build all common OpenWrt targets.

openwrt-amd64: ## Build for OpenWrt x86-64.
	@mkdir -p $(BIN_DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIST_DIR)/$(APP)-openwrt-amd64 .

openwrt-armv7: ## Build for OpenWrt ARMv7.
	@mkdir -p $(BIN_DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIST_DIR)/$(APP)-openwrt-armv7 .

openwrt-arm64: ## Build for OpenWrt ARM64/aarch64.
	@mkdir -p $(BIN_DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIST_DIR)/$(APP)-openwrt-arm64 .

openwrt-mips: ## Build for OpenWrt MIPS big-endian with soft-float.
	@mkdir -p $(BIN_DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat $(GO) build $(BUILD_FLAGS) -o $(BIN_DIST_DIR)/$(APP)-openwrt-mips .

openwrt-mipsel: ## Build for OpenWrt MIPS little-endian with soft-float.
	@mkdir -p $(BIN_DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat $(GO) build $(BUILD_FLAGS) -o $(BIN_DIST_DIR)/$(APP)-openwrt-mipsel .

raspi: raspi-armv6 raspi-armv7 raspi-arm64 ## Build binaries for all supported Raspberry Pi variants.

raspi-armv6: ## Build for Raspberry Pi 1 and Zero (32-bit ARMv6).
	@mkdir -p $(BIN_DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIST_DIR)/$(APP)-raspi-armv6 .

raspi-armv7: ## Build for Raspberry Pi 2/3/4 using a 32-bit OS.
	@mkdir -p $(BIN_DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIST_DIR)/$(APP)-raspi-armv7 .

raspi-arm64: ## Build for Raspberry Pi 3/4/5 using a 64-bit OS.
	@mkdir -p $(BIN_DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIST_DIR)/$(APP)-raspi-arm64 .

deb: build ## Build a Debian package in ./dist.
	rm -rf $(DEB_BUILD_DIR)
	install -d $(DEB_BUILD_DIR)/DEBIAN
	install -d $(DEB_BUILD_DIR)$(PREFIX)/bin
	install -d $(DEB_BUILD_DIR)$(SYSCONFDIR)
	install -d $(DEB_BUILD_DIR)$(SYSTEMD_DIR)
	install -d -m 0750 $(DEB_BUILD_DIR)$(LOGDIR)
	install -d $(DEB_BUILD_DIR)$(LOGROTATE_DIR)
	install -m 0755 $(APP) $(DEB_BUILD_DIR)$(PREFIX)/bin/$(APP)
	install -m 0644 $(CONFIG) $(DEB_BUILD_DIR)$(SYSCONFDIR)/config.toml
	install -m 0644 packaging/systemd/fog-proxy.service $(DEB_BUILD_DIR)$(SYSTEMD_DIR)/fog-proxy.service
	install -m 0644 packaging/logrotate/fog-proxy $(DEB_BUILD_DIR)$(LOGROTATE_DIR)/$(PACKAGE)
	printf '%s\n' \
		'Package: $(PACKAGE)' \
		'Version: $(VERSION)' \
		'Section: net' \
		'Priority: optional' \
		'Architecture: $(DEB_ARCH)' \
		'Depends: logrotate' \
		'Maintainer: soyunomas' \
		'Homepage: https://github.com/soyunomas/fog-proxydhcp' \
		'Description: ProxyDHCP service for FOG Project PXE booting' \
		' Provides PXE ProxyDHCP responses for FOG Project environments where' \
		' the existing DHCP server cannot be modified.' \
		> $(DEB_BUILD_DIR)/DEBIAN/control
	printf '%s\n' '$(SYSCONFDIR)/config.toml' '$(LOGROTATE_DIR)/$(PACKAGE)' > $(DEB_BUILD_DIR)/DEBIAN/conffiles
	printf '%s\n' \
		'#!/bin/sh' \
		'set -e' \
		'if command -v systemctl >/dev/null 2>&1; then' \
		'systemctl daemon-reload || true' \
		'fi' \
		'exit 0' \
		> $(DEB_BUILD_DIR)/DEBIAN/postinst
	printf '%s\n' \
		'#!/bin/sh' \
		'set -e' \
		'if command -v systemctl >/dev/null 2>&1; then' \
		'systemctl daemon-reload || true' \
		'fi' \
		'exit 0' \
		> $(DEB_BUILD_DIR)/DEBIAN/postrm
	chmod 0755 $(DEB_BUILD_DIR)/DEBIAN/postinst $(DEB_BUILD_DIR)/DEBIAN/postrm
	install -d $(DEB_DIST_DIR)
	dpkg-deb --build --root-owner-group $(DEB_BUILD_DIR) $(DEB_DIST_DIR)/$(PACKAGE)_$(VERSION)_$(DEB_ARCH).deb

clean: ## Remove generated binaries.
	rm -f $(APP)
	rm -rf $(BIN_DIST_DIR)

run: build ## Run locally with sudo using ./config.toml.
	sudo ./$(APP) -config $(CONFIG)

run-debug: build ## Run locally with verbose DHCP/PXE diagnostics.
	sudo ./$(APP) -config $(CONFIG) --debug

install: build ## Install binary and example config.
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 0755 $(APP) $(DESTDIR)$(PREFIX)/bin/$(APP)
	install -d $(DESTDIR)$(SYSCONFDIR)
	@if [ ! -f "$(DESTDIR)$(SYSCONFDIR)/config.toml" ]; then \
		install -m 0644 $(CONFIG) "$(DESTDIR)$(SYSCONFDIR)/config.toml"; \
		echo "Installed default config to $(DESTDIR)$(SYSCONFDIR)/config.toml"; \
	else \
		echo "Config already exists: $(DESTDIR)$(SYSCONFDIR)/config.toml"; \
	fi

uninstall: uninstall-service ## Remove installed binary and config directory.
	rm -f $(DESTDIR)$(PREFIX)/bin/$(APP)
	rm -rf $(DESTDIR)$(SYSCONFDIR)

install-service: install ## Install and enable systemd service.
	install -d $(DESTDIR)$(SYSTEMD_DIR)
	install -d -m 0750 $(DESTDIR)$(LOGDIR)
	install -d $(DESTDIR)$(LOGROTATE_DIR)
	install -m 0644 packaging/systemd/fog-proxy.service $(DESTDIR)$(SYSTEMD_DIR)/fog-proxy.service
	install -m 0644 packaging/logrotate/fog-proxy $(DESTDIR)$(LOGROTATE_DIR)/$(PACKAGE)
	systemctl daemon-reload
	systemctl enable fog-proxy.service

uninstall-service: ## Disable and remove systemd service.
	-systemctl disable --now fog-proxy.service
	rm -f $(DESTDIR)$(SYSTEMD_DIR)/fog-proxy.service
	rm -f $(DESTDIR)$(LOGROTATE_DIR)/$(PACKAGE)
	-systemctl daemon-reload

service-start: ## Start systemd service.
	systemctl start fog-proxy.service

service-stop: ## Stop systemd service.
	systemctl stop fog-proxy.service

service-restart: ## Restart systemd service.
	systemctl restart fog-proxy.service

service-status: ## Show systemd service status.
	systemctl status fog-proxy.service --no-pager

logs: ## Follow service file logs.
	tail -F $(LOGFILE)

logs-journal: ## Follow systemd journal service lifecycle logs.
	journalctl -u fog-proxy.service -f

ports: ## Show processes bound to ProxyDHCP ports.
	ss -ulpn | grep -E ':(67|4011)\b' || true
