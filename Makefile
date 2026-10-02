BIN ?= $(HOME)/.local/bin

.PHONY: build test install

build:
	go build -o kb .

test:
	go vet ./... && go test ./...

install:
	mkdir -p $(BIN)
	go build -o $(BIN)/kb .
