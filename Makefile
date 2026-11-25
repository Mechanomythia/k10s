.PHONY: build test vet clean

build:
	go build -o bin/ ./cmd/...

test:
	go test -race ./...

vet:
	go vet ./...

clean:
	rm -rf bin dist
