.PHONY: build test lint install clean

build:
	go build -o bin/ferry ./cmd/ferry
	go build -o bin/ferryd ./cmd/ferryd

test:
	go test ./...

lint:
	go vet ./...

install:
	go install ./cmd/ferry
	go install ./cmd/ferryd

clean:
	rm -rf bin/ dist/
