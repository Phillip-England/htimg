IMAGE_NAME ?= htimg
HOST_PORT ?= 9993
CONTAINER_PORT ?= 9993

.PHONY: docker
docker:
	mkdir -p config data
	docker build -t $(IMAGE_NAME) .
	docker run --rm \
		-p $(HOST_PORT):$(CONTAINER_PORT) \
		-v $(CURDIR)/config:/app/config \
		-v $(CURDIR)/data:/app/data \
		$(IMAGE_NAME)
