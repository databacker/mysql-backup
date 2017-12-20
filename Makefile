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
	cd test && ./test.sh
