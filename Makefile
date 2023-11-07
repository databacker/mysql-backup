.PHONY: build push test

TAG ?= $(shell git log -n 1 --pretty=format:"%H")
IMAGE ?= databack/mysql-backup
BUILDIMAGE ?= $(IMAGE):build
TARGET ?= $(IMAGE):$(TAG)
ARCH ?= linux/amd64,linux/arm64

build:
	docker buildx build -t $(BUILDIMAGE) --platform $(ARCH)  .

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
