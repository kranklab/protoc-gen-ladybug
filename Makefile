MODULE := github.com/kranklab/protoc-gen-ladybug
BINARY := protoc-gen-ladybug

.PHONY: build test clean example proto

build:
	go build -o $(BINARY) .

test:
	go test -v ./...

EXAMPLE_PROTOS := example/v1/graph/graph.proto example/v1/commits/commits.proto

example: build
	protoc --plugin=./$(BINARY) --ladybug_out=. -I proto -I . $(EXAMPLE_PROTOS)
	protoc --plugin=./$(BINARY) --ladybug_out=. --ladybug_opt=lang=go -I proto -I . $(EXAMPLE_PROTOS)
	protoc --plugin=./$(BINARY) --ladybug_out=. --ladybug_opt=lang=py -I proto -I . $(EXAMPLE_PROTOS)
	protoc --plugin=./$(BINARY) --ladybug_out=. --ladybug_opt=lang=cypher -I proto -I . $(EXAMPLE_PROTOS)

proto:
	protoc --go_out=. --go_opt=module=$(MODULE) -I proto proto/ladybug/options.proto

clean:
	rm -f $(BINARY)
	rm -f example/v1/graph/schema.gen.ts example/v1/graph/schema.gen.go example/v1/graph/schema_gen.py example/v1/graph/schema.cypher
	rm -f example/v1/commits/schema.gen.ts example/v1/commits/schema.gen.go example/v1/commits/schema_gen.py example/v1/commits/schema.cypher
