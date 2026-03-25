// Copyright 2026 OpenTrace Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// protoc-gen-ladybug generates LadybugDB schema DDL from protobuf message
// definitions.
//
//   - Messages ending in "Node" become CREATE NODE TABLE statements.
//   - Messages ending in "Rel" become REL TABLE factory functions whose
//     FROM/TO node types are supplied by the caller at runtime.
//
// The "source_id" and "target_id" fields on Rel messages are excluded from the
// generated columns — they are implicit in the REL TABLE's FROM/TO clause.
//
// Usage:
//
//	protoc --ladybug_out=<dir> [--ladybug_opt=lang=ts] <proto files>
//
// Options:
//
//	lang=ts     (default) Emit a TypeScript file exporting schema constants.
//	lang=go              Emit a Go file exporting schema constants.
//	lang=py              Emit a Python file exporting schema constants.
//	lang=cypher          Emit raw Cypher DDL statements.
package main

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/opentrace/opentrace/protoc-gen-ladybug/ladybug"
)

// protoKindToLadybug maps proto field kinds to LadybugDB column types.
func protoKindToLadybug(field *protogen.Field) string {
	if field.Desc.IsList() {
		return protoKindToLadybugScalar(field.Desc.Kind()) + "[]"
	}
	return protoKindToLadybugScalar(field.Desc.Kind())
}

func protoKindToLadybugScalar(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.StringKind:
		return "STRING"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "INT32"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "INT64"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "UINT32"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "UINT64"
	case protoreflect.FloatKind:
		return "FLOAT"
	case protoreflect.DoubleKind:
		return "DOUBLE"
	case protoreflect.BoolKind:
		return "BOOL"
	case protoreflect.BytesKind:
		return "BLOB"
	default:
		return "STRING"
	}
}

// columnName returns the DDL column name for a field. If the field has a
// (ladybug.column) option set, that value is used; otherwise the proto
// field name is returned.
func columnName(field *protogen.Field) string {
	opts, _ := field.Desc.Options().(*descriptorpb.FieldOptions)
	if opts != nil && proto.HasExtension(opts, ladybug.E_Column) {
		if name := proto.GetExtension(opts, ladybug.E_Column).(string); name != "" {
			return name
		}
	}
	return string(field.Desc.Name())
}

// isPrimaryKey returns true if the field has (ladybug.primary_key) = true.
func isPrimaryKey(field *protogen.Field) bool {
	opts, _ := field.Desc.Options().(*descriptorpb.FieldOptions)
	if opts != nil && proto.HasExtension(opts, ladybug.E_PrimaryKey) {
		return proto.GetExtension(opts, ladybug.E_PrimaryKey).(bool)
	}
	return false
}

// nodeTable holds the parsed schema for a single node table.
type nodeTable struct {
	// Name is the table name (message name minus "Node" suffix).
	Name string
	// Columns are (name, type) pairs in proto field order.
	Columns []column
}

type column struct {
	Name       string
	Type       string
	PrimaryKey bool
}

// extractNodeTables finds all messages ending in "Node" and converts them
// to nodeTable definitions.
func extractNodeTables(gen *protogen.Plugin) []nodeTable {
	var tables []nodeTable
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		for _, msg := range f.Messages {
			name := string(msg.Desc.Name())
			if !strings.HasSuffix(name, "Node") {
				continue
			}
			tableName := strings.TrimSuffix(name, "Node")
			var cols []column
			hasExplicitPK := false
			for _, field := range msg.Fields {
				pk := isPrimaryKey(field)
				if pk {
					hasExplicitPK = true
				}
				cols = append(cols, column{
					Name:       columnName(field),
					Type:       protoKindToLadybug(field),
					PrimaryKey: pk,
				})
			}
			// Default: first field is primary key if none explicitly marked.
			if !hasExplicitPK && len(cols) > 0 {
				cols[0].PrimaryKey = true
			}
			tables = append(tables, nodeTable{Name: tableName, Columns: cols})
		}
	}
	return tables
}

// relTable holds the parsed schema for a single relationship table.
type relTable struct {
	// Name is the relationship name (message name minus "Rel" suffix).
	Name string
	// Columns are property columns, excluding source_id and target_id.
	Columns []column
}

// relImplicitFields are field names excluded from rel DDL columns because
// they are represented by the FROM/TO clause.
var relImplicitFields = map[string]bool{
	"source_id": true,
	"target_id": true,
}

// extractRelTables finds all messages ending in "Rel" and converts them
// to relTable definitions. Fields named "source_id" and "target_id" are
// excluded from the column list.
func extractRelTables(gen *protogen.Plugin) []relTable {
	var tables []relTable
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		for _, msg := range f.Messages {
			name := string(msg.Desc.Name())
			if !strings.HasSuffix(name, "Rel") {
				continue
			}
			tableName := strings.TrimSuffix(name, "Rel")
			var cols []column
			for _, field := range msg.Fields {
				protoName := string(field.Desc.Name())
				if relImplicitFields[protoName] {
					continue
				}
				cols = append(cols, column{
					Name: columnName(field),
					Type: protoKindToLadybug(field),
				})
			}
			tables = append(tables, relTable{Name: tableName, Columns: cols})
		}
	}
	return tables
}

// buildRelDDLTemplate generates a CREATE REL TABLE statement template with
// placeholders for FROM and TO node types. The placeholder format is
// determined by the target language.
func buildRelDDLTemplate(t relTable, fromPlaceholder, toPlaceholder string) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("FROM %s TO %s", fromPlaceholder, toPlaceholder))
	for _, col := range t.Columns {
		parts = append(parts, fmt.Sprintf("%s %s", col.Name, col.Type))
	}
	return fmt.Sprintf("CREATE REL TABLE IF NOT EXISTS %s(%s)", t.Name, strings.Join(parts, ", "))
}

// buildDDL generates a CREATE NODE TABLE statement for a single table.
// The first column is assumed to be the primary key.
func buildDDL(t nodeTable) string {
	var parts []string
	for _, col := range t.Columns {
		entry := fmt.Sprintf("%s %s", col.Name, col.Type)
		if col.PrimaryKey {
			entry += " PRIMARY KEY"
		}
		parts = append(parts, entry)
	}
	return fmt.Sprintf("CREATE NODE TABLE IF NOT EXISTS %s(%s)", t.Name, strings.Join(parts, ", "))
}

func main() {
	protogen.Options{}.Run(func(gen *protogen.Plugin) error {
		nodes := extractNodeTables(gen)
		rels := extractRelTables(gen)
		if len(nodes) == 0 && len(rels) == 0 {
			return nil
		}

		// Determine output language from plugin parameter.
		lang := "ts"
		for _, p := range strings.Split(gen.Request.GetParameter(), ",") {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "lang=") {
				lang = strings.TrimPrefix(p, "lang=")
			}
		}

		switch lang {
		case "cypher":
			return generateCypher(gen, nodes, rels)
		case "go":
			return generateGo(gen, nodes, rels)
		case "py":
			return generatePython(gen, nodes, rels)
		default:
			return generateTS(gen, nodes, rels)
		}
	})
}

func generateCypher(gen *protogen.Plugin, nodes []nodeTable, rels []relTable) error {
	g := gen.NewGeneratedFile("schema.cypher", "")
	g.P("// Auto-generated by protoc-gen-ladybug — do not edit.")
	g.P()
	g.P("// Node tables")
	for _, t := range nodes {
		g.P(buildDDL(t), ";")
	}
	if len(rels) > 0 {
		g.P()
		g.P("// Rel tables — replace %s placeholders with node table names:")
		g.P("//   e.g. CREATE REL TABLE IF NOT EXISTS Calls(FROM Function TO Function, ...)")
		for _, t := range rels {
			g.P("// ", buildRelDDLTemplate(t, "%s", "%s"), ";")
		}
	}
	return nil
}

func generateTS(gen *protogen.Plugin, nodes []nodeTable, rels []relTable) error {
	g := gen.NewGeneratedFile("schema.gen.ts", "")
	g.P("// Auto-generated by protoc-gen-ladybug — do not edit.")
	g.P()

	// Node DDL statements.
	g.P("export const NODE_SCHEMA_STATEMENTS = [")
	for _, t := range nodes {
		g.P("  `", buildDDL(t), "`,")
	}
	g.P("] as const;")
	g.P()

	// Node type names.
	g.P("export const NODE_TYPES = [")
	for _, t := range nodes {
		g.P("  '", t.Name, "',")
	}
	g.P("] as const;")
	g.P()
	g.P("export type NodeType = (typeof NODE_TYPES)[number];")

	// Rel DDL factory functions.
	if len(rels) > 0 {
		g.P()
		g.P("export const REL_SCHEMA = {")
		for _, t := range rels {
			g.P("  ", t.Name, ": (from: string, to: string) =>")
			g.P("    `", buildRelDDLTemplate(t, "${from}", "${to}"), "`,")
		}
		g.P("} as const;")
		g.P()

		// Rel type names.
		g.P("export const REL_TYPES = [")
		for _, t := range rels {
			g.P("  '", t.Name, "',")
		}
		g.P("] as const;")
		g.P()
		g.P("export type RelType = (typeof REL_TYPES)[number];")
	}

	return nil
}

func generateGo(gen *protogen.Plugin, nodes []nodeTable, rels []relTable) error {
	g := gen.NewGeneratedFile("schema.gen.go", "")
	g.P("// Code generated by protoc-gen-ladybug. DO NOT EDIT.")
	g.P()
	g.P("package schema")
	g.P()

	needsFmt := len(rels) > 0
	if needsFmt {
		g.P("import \"fmt\"")
		g.P()
	}

	// Node DDL statements.
	g.P("// NodeSchemaStatements contains the DDL statements for all node tables.")
	g.P("var NodeSchemaStatements = []string{")
	for _, t := range nodes {
		g.P("\t`", buildDDL(t), "`,")
	}
	g.P("}")
	g.P()

	// Node type name constants.
	for _, t := range nodes {
		g.P("// NodeType", t.Name, " is the node type name for ", t.Name, " nodes.")
		g.P("const NodeType", t.Name, " = \"", t.Name, "\"")
	}
	g.P()

	// NodeTypes slice.
	g.P("// NodeTypes contains all node type names.")
	g.P("var NodeTypes = []string{")
	for _, t := range nodes {
		g.P("\tNodeType", t.Name, ",")
	}
	g.P("}")

	// Rel DDL factory functions.
	if len(rels) > 0 {
		g.P()
		for _, t := range rels {
			g.P("// RelSchema", t.Name, " returns the CREATE REL TABLE DDL for ", t.Name, " relationships.")
			g.P("func RelSchema", t.Name, "(from, to string) string {")
			g.P("\treturn fmt.Sprintf(`", buildRelDDLTemplate(t, "%s", "%s"), "`, from, to)")
			g.P("}")
		}
		g.P()

		// Rel type name constants.
		for _, t := range rels {
			g.P("// RelType", t.Name, " is the relationship type name for ", t.Name, " relationships.")
			g.P("const RelType", t.Name, " = \"", t.Name, "\"")
		}
		g.P()

		// RelTypes slice.
		g.P("// RelTypes contains all relationship type names.")
		g.P("var RelTypes = []string{")
		for _, t := range rels {
			g.P("\tRelType", t.Name, ",")
		}
		g.P("}")
	}

	return nil
}

func generatePython(gen *protogen.Plugin, nodes []nodeTable, rels []relTable) error {
	g := gen.NewGeneratedFile("schema_gen.py", "")
	g.P("# Auto-generated by protoc-gen-ladybug — do not edit.")
	g.P()
	g.P("from typing import Final")
	g.P()

	// Node DDL statements.
	g.P("NODE_SCHEMA_STATEMENTS: Final[list[str]] = [")
	for _, t := range nodes {
		g.P("    \"", buildDDL(t), "\",")
	}
	g.P("]")
	g.P()

	// Node type name constants.
	for _, t := range nodes {
		g.P("NODE_TYPE_", toScreamingSnake(t.Name), ": Final[str] = \"", t.Name, "\"")
	}
	g.P()

	// Node types list.
	g.P("NODE_TYPES: Final[list[str]] = [")
	for _, t := range nodes {
		g.P("    NODE_TYPE_", toScreamingSnake(t.Name), ",")
	}
	g.P("]")

	// Rel DDL factory functions.
	if len(rels) > 0 {
		g.P()
		g.P()
		for _, t := range rels {
			g.P("def rel_schema_", toSnake(t.Name), "(from_node: str, to_node: str) -> str:")
			g.P("    \"\"\"Return the CREATE REL TABLE DDL for ", t.Name, " relationships.\"\"\"")
			g.P("    return f\"", buildRelDDLTemplate(t, "{from_node}", "{to_node}"), "\"")
			g.P()
			g.P()
		}

		// Rel type name constants.
		for _, t := range rels {
			g.P("REL_TYPE_", toScreamingSnake(t.Name), ": Final[str] = \"", t.Name, "\"")
		}
		g.P()

		// Rel types list.
		g.P("REL_TYPES: Final[list[str]] = [")
		for _, t := range rels {
			g.P("    REL_TYPE_", toScreamingSnake(t.Name), ",")
		}
		g.P("]")
	}

	return nil
}

// toScreamingSnake converts a PascalCase name to SCREAMING_SNAKE_CASE.
func toScreamingSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		if r >= 'a' && r <= 'z' {
			result.WriteRune(r - 'a' + 'A')
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// toSnake converts a PascalCase name to snake_case.
func toSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			result.WriteRune(r - 'A' + 'a')
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}
