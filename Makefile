.PHONY: build push test

TAG ?= $(shell git log -n 1 --pretty=format:"%H")
IMAGE ?= databack/mysql-backup
TARGET ?= $(IMAGE):$(TAG)


build:
	docker build -t $(TARGET) .

push: build
	docker tag $(TARGET) $(IMAGE):latest
	docker push $(TARGET)
	docker push $(IMAGE):latest

test_dump:
	cd test && DEBUG=$(DEBUG) ./test_dump.sh

test_cron:
	docker run --rm -e DEBUG=$(DEBUG) -v $(PWD):/data alpine:3.8 sh -c "apk --update add bash; cd /data/test; ./test_cron.sh"

test_source_target:
	cd test && ./test_source_target.sh

test: test_dump test_cron test_source_target	

.PHONY: clean-test-stop clean-test-remove clean-test
clean-test-stop:
	$(eval IDS:=$(strip $(shell docker ps --filter label=mysqltest -q)))
	@if [ -n "$(IDS)" ]; then docker kill $(IDS); fi

clean-test-remove:
	$(eval IDS:=$(shell docker ps -a --filter label=mysqltest -q))
	@if [ -n "$(IDS)" ]; then docker rm $(IDS); fi

clean-test-network:
	$(eval IDS:=$(shell docker network ls --filter label=mysqltest -q))
	@if [ -n "$(IDS)" ]; then docker network rm $(IDS); fi

clean-test: clean-test-stop clean-test-remove clean-test-network

