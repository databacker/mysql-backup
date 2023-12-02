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

test_dump:
	cd test && DEBUG=$(DEBUG) ./test_dump.sh

test_cron:
	cd test && ./test_cron.sh

test_source_target:
	cd test && ./test_source_target.sh

test_restore:
	cd test && ./test_restore.sh

test: test_dump test_restore test_cron test_source_target

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
	@echo Remove Volumes
	$(eval IDS:=$(shell docker volume ls --filter label=mysqltest -q))
	@if [ -n "$(IDS)" ]; then docker volume rm $(IDS); fi
	@echo

clean-test-network:
	@echo Remove Networks
	$(eval IDS:=$(shell docker network ls --filter label=mysqltest -q))
	@if [ -n "$(IDS)" ]; then docker network rm $(IDS); fi
	@echo

clean-test: clean-test-stop clean-test-remove clean-test-network
