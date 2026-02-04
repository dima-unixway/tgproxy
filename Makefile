CURRENT_DIR := $(shell pwd)
VERSION := 0.1.0
BINARY := tgproxy
IMAGE := $(BINARY):$(VERSION)

.PHONY: all build docker-build release rpm deb extract clean

all: build rpm deb

# Extract static binary using Dockerfile (primary build target)
build: extract

# Docker image build (tags with version)
docker-build:
	docker build \
		-t $(IMAGE) \
		.

# Extract static binary from Docker build (equivalent to old 'release')
release: extract
	@echo "Released $(BINARY)-$(VERSION)"

extract:
	@if [ ! -f $(BINARY) ]; then \
		rm -f $(BINARY); \
		if docker images -q $(IMAGE) | grep -q . ; then \
			echo "Using existing Docker image $(IMAGE)."; \
			docker create --name extract-cont $(IMAGE); \
			docker cp extract-cont:/tgproxy ./$(BINARY); \
			docker rm extract-cont; \
		else \
			echo "Building tdlib-builder stage."; \
			docker build --target tdlib-builder -t $(BINARY)-builder . ; \
			docker create --name extract-cont $(BINARY)-builder; \
			docker cp extract-cont:/app/$(BINARY) ./$(BINARY); \
			docker rm extract-cont; \
			docker rmi $(BINARY)-builder; \
		fi; \
	else \
		echo "Binary $(BINARY) already exists, skipping extraction."; \
	fi;

# RPM package
rpm: extract
	rm -rf .build
	mkdir -p .build/usr/bin
	mkdir -p .build/etc/$(BINARY)
	cp $(BINARY) .build/usr/bin/
	cp .env.example .build/etc/$(BINARY)/env.example
	docker run --rm \
		-v $(CURRENT_DIR):/$(BINARY) \
		-w /$(BINARY) \
		lrdevops/fpm \
		fpm -s dir -t rpm \
		-C .build \
		--name $(BINARY) \
		--version $(VERSION) \
		--iteration 1 \
		--description "Telegram Proxy Server (tgproxy)" \
		--vendor "tgproxy project"

# DEB package
deb: extract
	rm -rf .build
	mkdir -p .build/usr/bin
	mkdir -p .build/etc/$(BINARY)
	cp $(BINARY) .build/usr/bin/
	cp .env.example .build/etc/$(BINARY)/env.example
	docker run --rm \
		-v $(CURRENT_DIR):/$(BINARY) \
		-w /$(BINARY) \
		lrdevops/fpm \
		fpm -s dir -t deb \
		-C .build \
		--name $(BINARY) \
		--version $(VERSION) \
		--iteration 1 \
		--description "Telegram Proxy Server (tgproxy)" \
		--vendor "tgproxy project"

# Clean
clean:
	rm -f $(BINARY)
	docker rmi $(IMAGE) 2>/dev/null || true
	docker rmi $(BINARY)-builder 2>/dev/null || true
	rm -rf .build
