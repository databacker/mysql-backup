.PHONY: build push test

TAG ?= $(shell git log -n 1 --pretty=format:"%H")
IMAGE ?= deitch/mysql-backup
TARGET ?= $(IMAGE):$(TAG)


build:
	docker build -t $(TARGET) .

push: build
	docker tag $(TARGET) $(IMAGE):latest
	docker push $(TARGET)
	docker push $(IMAGE):latest

test:
	cd test && DEBUG=$(DEBUG) ./test.sh

test_source_target:
	cd test && ./test_source_target.sh
	

clean-test:
	docker kill $(docker ps | awk '/mysql/ {print $1}')
	docker rm $(docker ps -a | awk '/mysql/ {print $1}')
	docker kill s3 && docker rm s3
	docker network rm mysqltest

