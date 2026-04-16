# protoc-gen-ladybug

A [protoc](https://github.com/protocolbuffers/protobuf) plugin that generates
[LadybugDB](https://github.com/opentrace) schema DDL from Protocol Buffer
message definitions.

- Messages ending in **`Node`** become `CREATE NODE TABLE` statements.
  The first field becomes the primary key.
- Messages ending in **`Rel`** become REL TABLE factory functions.
  The `source_id` and `target_id` fields are excluded from columns (they map
  to the `FROM`/`TO` clause which the caller supplies at runtime).

Proto types are mapped to LadybugDB column types automatically. Column names
default to the proto field name, but can be overridden with the
`(ladybug.column)` field option.

## Installation

```bash
go install github.com/kranklab/protoc-gen-ladybug@latest
```

Or build from source:

```bash
make build
```

The binary must be on your `$PATH` (or in the same directory as `protoc`) so
that `protoc` can discover it.

## Usage

```bash
# Emit TypeScript (default)
protoc --ladybug_out=. graph.proto

# Emit Go
protoc --ladybug_out=. --ladybug_opt=lang=go graph.proto

# Emit Python
protoc --ladybug_out=. --ladybug_opt=lang=py graph.proto

# Emit raw Cypher DDL
protoc --ladybug_out=. --ladybug_opt=lang=cypher graph.proto
```

Output files are written alongside each input `.proto` file, one schema per
source directory. Multiple protos in the same directory aggregate into a
single schema; protos in different directories produce separate schemas.

### Output formats

| Option | Output file | Description |
|---|---|---|
| `lang=ts` (default) | `<dir>/schema.gen.ts` | TypeScript module with node DDL arrays, rel DDL factory functions, and union types |
| `lang=go` | `<dir>/schema.gen.go` | Go package with node DDL slices, rel DDL factory functions, and typed constants. The `package` declaration uses the proto's `go_package` short name. |
| `lang=py` | `<dir>/schema_gen.py` | Python module with node DDL lists, rel DDL factory functions, and typed constants |
| `lang=cypher` | `<dir>/schema.cypher` | Plain Cypher DDL statements (rel tables as commented templates) |

## Example

Full examples are provided under [`example/v1/`](example/v1/). Two separate
packages — [`example/v1/graph/graph.proto`](example/v1/graph/graph.proto) and
[`example/v1/commits/commits.proto`](example/v1/commits/commits.proto) —
demonstrate how protos in different directories produce separate schema
files (`example/v1/graph/schema.gen.go`, `example/v1/commits/schema.gen.go`,
etc.), each in its own Go package.

Given proto definitions like:

```protobuf
syntax = "proto3";
import "ladybug/options.proto";

message FunctionNode {
  string id         = 1;
  string name       = 2;
  int32  start_line = 3 [(ladybug.column) = "startLine"];
  int32  end_line   = 4 [(ladybug.column) = "endLine"];
  bool   exported   = 5;
}

// "Rel" suffix → REL TABLE factory function.
// "source_id" and "target_id" are excluded from DDL columns.
message CallsRel {
  string id         = 1;
  string source_id  = 2;
  string target_id  = 3;
  string args       = 4;
  float  confidence = 5;
}
```

### TypeScript output (default)

```typescript
export const NODE_SCHEMA_STATEMENTS = [
  `CREATE NODE TABLE IF NOT EXISTS Function(id STRING PRIMARY KEY, name STRING, startLine INT32, endLine INT32, exported BOOL)`,
] as const;

export const NODE_TYPES = ['Function'] as const;
export type NodeType = (typeof NODE_TYPES)[number];

export const REL_SCHEMA = {
  Calls: (from: string, to: string) =>
    `CREATE REL TABLE IF NOT EXISTS Calls(FROM ${from} TO ${to}, id STRING, args STRING, confidence FLOAT)`,
} as const;

export const REL_TYPES = ['Calls'] as const;
export type RelType = (typeof REL_TYPES)[number];
```

Note how `start_line` became `startLine` via `(ladybug.column)`.

Usage: `REL_SCHEMA.Calls('Function', 'Function')` returns the full DDL.

### Go output

```go
package schema

import "fmt"

var NodeSchemaStatements = []string{
	`CREATE NODE TABLE IF NOT EXISTS Function(id STRING PRIMARY KEY, name STRING, startLine INT32, endLine INT32, exported BOOL)`,
}

const NodeTypeFunction = "Function"
var NodeTypes = []string{NodeTypeFunction}

func RelSchemaCalls(from, to string) string {
	return fmt.Sprintf(`CREATE REL TABLE IF NOT EXISTS Calls(FROM %s TO %s, id STRING, args STRING, confidence FLOAT)`, from, to)
}

const RelTypeCalls = "Calls"
var RelTypes = []string{RelTypeCalls}
```

### Python output

```python
from typing import Final

NODE_SCHEMA_STATEMENTS: Final[list[str]] = [
    "CREATE NODE TABLE IF NOT EXISTS Function(id STRING PRIMARY KEY, name STRING, startLine INT32, endLine INT32, exported BOOL)",
]

NODE_TYPE_FUNCTION: Final[str] = "Function"
NODE_TYPES: Final[list[str]] = [NODE_TYPE_FUNCTION]


def rel_schema_calls(from_node: str, to_node: str) -> str:
    """Return the CREATE REL TABLE DDL for Calls relationships."""
    return f"CREATE REL TABLE IF NOT EXISTS Calls(FROM {from_node} TO {to_node}, id STRING, args STRING, confidence FLOAT)"


REL_TYPE_CALLS: Final[str] = "Calls"
REL_TYPES: Final[list[str]] = [REL_TYPE_CALLS]
```

## Column name overrides

Import `ladybug/options.proto` and use the `(ladybug.column)` field option to
set a custom DDL column name:

```protobuf
import "ladybug/options.proto";

message FunctionNode {
  string id         = 1;
  string name       = 2;
  int32  start_line = 3 [(ladybug.column) = "startLine"];
  int32  end_line   = 4 [(ladybug.column) = "endLine"];
}
```

This generates `startLine INT32` instead of `start_line INT32` in the DDL.
Fields without the option use their proto name as-is.

When invoking `protoc`, add the proto include path so it can find the options
file:

```bash
protoc --ladybug_out=./gen -I path/to/protoc-gen-ladybug/proto -I . graph.proto
```

## Type mapping

| Proto type | LadybugDB type |
|---|---|
| `string` | `STRING` |
| `int32`, `sint32`, `sfixed32` | `INT32` |
| `int64`, `sint64`, `sfixed64` | `INT64` |
| `uint32`, `fixed32` | `UINT32` |
| `uint64`, `fixed64` | `UINT64` |
| `float` | `FLOAT` |
| `double` | `DOUBLE` |
| `bool` | `BOOL` |
| `bytes` | `BLOB` |
| `repeated T` | `T[]` |

Unsupported kinds (messages, enums, etc.) fall back to `STRING`.

## Development

```bash
make build    # Build the binary
make test     # Run tests
make example  # Generate example output (requires protoc)
make proto    # Regenerate ladybug/options.pb.go (requires protoc-gen-go)
make clean    # Remove build artifacts
```

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
