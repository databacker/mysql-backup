.PHONY: build push test

TAG ?= $(shell git log -n 1 --pretty=format:"%H")
IMAGE ?= databack/mysql-backup
BUILDIMAGE ?= $(IMAGE):build
TARGET ?= $(IMAGE):$(TAG)
OCIPLATFORMS ?= linux/amd64,linux/arm64
LOCALPLATFORMS ?= linux/386 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 windows/386
DIST ?= dist
GOOS?=$(shell uname -s | tr '[:upper:]' '[:lower:]')
GOARCH?=$(shell uname -m)
BIN ?= $(DIST)/mysql-backup-$(GOOS)-$(GOARCH)

build-docker:
	docker buildx build -t $(BUILDIMAGE) --platform $(OCIPLATFORMS) .

.PRECIOUS: $(foreach platform,$(LOCALPLATFORMS),$(DIST)/mysql-backup-$(subst /,-,$(platform)))

build-all: $(foreach platform,$(LOCALPLATFORMS),build-local-$(subst /,-,$(platform)))

build-local-%: $(DIST)/mysql-backup-%;

$(DIST):
	mkdir -p $@

$(DIST)/mysql-backup-%: GOOS=$(word 1,$(subst -, ,$*))
$(DIST)/mysql-backup-%: GOARCH=$(word 2,$(subst -, ,$*))
$(DIST)/mysql-backup-%: $(DIST) 
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $@ .

build-local: $(BIN)

push: build
	docker tag $(BUILDIMAGE) $(TARGET)
	docker push $(TARGET)

integration_test:
	go test -v ./test --tags=integration

integration_test_debug:
	dlv --wd=./test test ./test --build-flags="-tags=integration"

vet:
	go vet --tags=integration ./...

test: unit_test integration_test

unit_test:
	go test -v ./...

.PHONY: clean-test-stop clean-test-remove clean-test
clean-test-stop:
	@echo Kill Containers
	$(eval IDS:=$(strip $(shell docker ps --filter label=mysqltest -q)))
	@if [ -n "$(IDS)" ]; then docker kill $(IDS); fi
	@echo

clean-test-remove:
	@echo Remove Containers
	$(eval IDS:=$(shell docker ps -a --filter label=mysqltest -q))
	@if [ -n "$(IDS)" ]; then docker rm $(IDS); fi
	@echo

clean-test: clean-test-stop clean-test-remove
