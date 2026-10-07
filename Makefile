.PHONY: build test vet fmt fmt-check race verify release

VERSION ?= dev

build:
	go build -trimpath -ldflags '-X main.version=$(VERSION)' -o bin/vitryna ./cmd/vitryna

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd config export internal test

fmt-check:
	test -z "$$(gofmt -l cmd config export internal test)"

race:
	go test -race ./...

verify: test vet fmt-check race build

release:
	go run ./cmd/vitryna-release --version '$(VERSION)' --output 'dist/$(VERSION)'
