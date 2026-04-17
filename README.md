# protoc-gen-ladybug

A [protoc](https://github.com/protocolbuffers/protobuf) plugin that generates
[LadybugDB](https://github.com/opentrace) schema DDL from Protocol Buffer
message definitions.

- Messages ending in **`Node`** become `CREATE NODE TABLE` statements.
  The first field becomes the primary key.
- Messages ending in **`Rel`** become REL TABLE factory functions that take
  a list of `(FROM, TO)` node-type pairs at call time. The `source_id` and
  `target_id` fields are excluded from columns — they map to the `FROM`/`TO`
  clauses supplied at runtime.

Ladybug requires every `(FROM, TO)` pair a rel label spans to be declared
in the *initial* `CREATE REL TABLE` statement. Subsequent
`CREATE REL TABLE IF NOT EXISTS` calls for an existing label are silent
no-ops, so the generated factory accepts a variable list of pairs and
expands them all into a single DDL statement.

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

export interface RelPair {
  readonly from: string;
  readonly to: string;
}

export const REL_SCHEMA = {
  CALLS: (pairs: ReadonlyArray<RelPair>) =>
    `CREATE REL TABLE IF NOT EXISTS CALLS(${joinRelPairs(pairs)}, id STRING, args STRING, confidence FLOAT)`,
} as const;

export const REL_TYPES = ['CALLS'] as const;
export type RelType = (typeof REL_TYPES)[number];
```

Note how `start_line` became `startLine` via `(ladybug.column)`.

Usage (single pair):

```ts
REL_SCHEMA.CALLS([{ from: 'Function', to: 'Function' }]);
```

Usage (multi-pair — a single `CREATE REL TABLE` covering every pair the
label spans):

```ts
REL_SCHEMA.CALLS([
  { from: 'Function', to: 'Function' },
  { from: 'Class',    to: 'Function' },
]);
```

### Go output

```go
package schema

import (
	"fmt"
	"strings"
)

var NodeSchemaStatements = []string{
	`CREATE NODE TABLE IF NOT EXISTS Function(id STRING PRIMARY KEY, name STRING, startLine INT32, endLine INT32, exported BOOL)`,
}

const NodeTypeFunction NodeType = "Function"
var NodeTypes = []NodeType{NodeTypeFunction}

type RelPair struct {
	From string
	To   string
}

func RelSchemaCalls(pairs ...RelPair) string {
	return fmt.Sprintf(`CREATE REL TABLE IF NOT EXISTS CALLS(%s, id STRING, args STRING, confidence FLOAT)`, joinRelPairs(pairs))
}

const RelTypeCalls RelType = "CALLS"
var RelTypes = []RelType{RelTypeCalls}
```

Call sites pass every pair the label needs in one invocation:

```go
schema.RelSchemaCalls(
    schema.RelPair{From: "Function", To: "Function"},
    schema.RelPair{From: "Class",    To: "Function"},
)
```

### Python output

```python
from typing import Final

NODE_SCHEMA_STATEMENTS: Final[list[str]] = [
    "CREATE NODE TABLE IF NOT EXISTS Function(id STRING PRIMARY KEY, name STRING, startLine INT32, endLine INT32, exported BOOL)",
]

NODE_TYPE_FUNCTION: Final[str] = "Function"
NODE_TYPES: Final[list[str]] = [NODE_TYPE_FUNCTION]

RelPair = tuple[str, str]


def rel_schema_calls(pairs: list[RelPair]) -> str:
    """Return the CREATE REL TABLE DDL for Calls relationships."""
    return f"CREATE REL TABLE IF NOT EXISTS CALLS({_join_rel_pairs(pairs)}, id STRING, args STRING, confidence FLOAT)"


REL_TYPE_CALLS: Final[str] = "CALLS"
REL_TYPES: Final[list[str]] = [REL_TYPE_CALLS]
```

Call sites pass a list of `(from, to)` tuples:

```python
rel_schema_calls([
    ("Function", "Function"),
    ("Class",    "Function"),
])
```

### Cypher output

Cypher is emitted as a commented template with a `<pairs>` placeholder —
callers must substitute one or more comma-separated `FROM X TO Y` clauses
before executing the DDL:

```cypher
// CREATE REL TABLE IF NOT EXISTS CALLS(<pairs>, id STRING, args STRING, confidence FLOAT);
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

## Migrating from the single-pair factory

Earlier versions of the plugin emitted a two-argument factory per rel
(`RelSchemaCalls(from, to string)`, `REL_SCHEMA.CALLS(from, to)`, etc.).
That shape could only declare one `(FROM, TO)` pair per label, which
Ladybug silently accepts for the *first* call but then ignores for every
subsequent pair — the extra pairs were never registered, and writes that
needed them failed at bind time with
`Binder exception: Query node X violates schema. Expected labels are Y`.

The current plugin emits a variadic/array factory that takes a list of
`RelPair` values and expands them inline into a single `CREATE REL TABLE`
statement. Update call sites:

| Before | After |
|---|---|
| `RelSchemaCalls("Function", "Function")` | `RelSchemaCalls(RelPair{From: "Function", To: "Function"})` |
| `REL_SCHEMA.CALLS('Function', 'Function')` | `REL_SCHEMA.CALLS([{ from: 'Function', to: 'Function' }])` |
| `rel_schema_calls("Function", "Function")` | `rel_schema_calls([("Function", "Function")])` |

If a rel label needs to span multiple pairs, pass them all in the same
call — Ladybug requires every pair to be declared in the *initial*
statement.

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
