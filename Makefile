SHELL := /bin/bash
IMAGE ?= ghcr.io/example/rebalancer:latest

.PHONY: fmt lint test build run docker-build docker-push helm-package

fmt:
	@echo "Formatting Go sources"
	@go fmt ./...

lint:
	@golangci-lint run ./...

test:
	@go test ./...

build:
	@go build ./cmd/manager

run:
	@go run ./cmd/manager --metrics-bind-address=:8087 --health-probe-bind-address=:8081 --leader-elect=false

docker-build:
	docker build -t $(IMAGE) .

docker-push:
	docker push $(IMAGE)

helm-package:
	helm package charts/rebalancer
