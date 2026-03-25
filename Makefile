MODULE := github.com/opentrace/opentrace/protoc-gen-ladybug
BINARY := protoc-gen-ladybug

.PHONY: build test clean example proto

build:
	go build -o $(BINARY) .

test:
	go test -v ./...

example: build
	protoc --plugin=./$(BINARY) --ladybug_out=example -I proto -I . example/graph.proto

proto:
	protoc --go_out=. --go_opt=module=$(MODULE) -I proto proto/ladybug/options.proto

clean:
	rm -f $(BINARY)
	rm -f example/schema.gen.ts example/schema.gen.go example/schema_gen.py example/schema.cypher
