BIN_FILE=mix-scheduler-admission-webhook
VERSION ?= latest
DEP_VERSION ?= v1.30.3
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GOROOT ?= $(shell go env GOROOT)
GIT_COMMIT=$(shell git rev-parse HEAD)
GIT_TREE_STATE=$(shell if git status|grep -q 'clean';then echo clean; else echo dirty; fi)
GOVERSION=${shell go version}
KIND_CLUSTER ?= k1
LDFLAGS="-s -w"

depUpdate:
	@rm -rf go.mod go.sum
	@go mod init github.com/helen-frank/mix-scheduler-admission-webhook
	@bash mod.sh ${DEP_VERSION}
	@go mod tidy
	@go mod vendor

build:
	@GOOS=${GOOS} GOARCH=${GOARCH} go build -ldflags ${LDFLAGS} -o _output/${GOOS}_${GOARCH}/${BIN_FILE} ./

dockerBuild:
	@docker build -t helenfrank/mix-scheduler-admission-webhook:${VERSION} .
	@docker tag helenfrank/mix-scheduler-admission-webhook:${VERSION} helenfrank/mix-scheduler-admission-webhook:latest

dockerBuildKindLoad:
	@docker build -t helenfrank/mix-scheduler-admission-webhook:${VERSION} .
	@docker tag helenfrank/mix-scheduler-admission-webhook:${VERSION} helenfrank/mix-scheduler-admission-webhook:latest
	@kind load docker-image -n ${KIND_CLUSTER} helenfrank/mix-scheduler-admission-webhook:${VERSION}

dockerBuildPush:
	@docker build -t helenfrank/mix-scheduler-admission-webhook:${VERSION} .
	@docker tag helenfrank/mix-scheduler-admission-webhook:${VERSION} helenfrank/mix-scheduler-admission-webhook:latest
	@docker push helenfrank/mix-scheduler-admission-webhook:${VERSION}
	@docker push helenfrank/mix-scheduler-admission-webhook:latest

all:
	@make depUpdate
	@make build

cleanDir:
	@rm -rf _tmp/*
	@rm -rf _output/*

cleanBuild:
	@go clean

help:
	@echo "make; Update dependency formatting go code and compile generated binary files"
	@echo "make depUpdate; update dependency go code"
	@echo "make build; compile generated binary files"
	@echo "make test; run unit tests"

.PHONY: build depUpdate test help cleanBuild cleanDir
