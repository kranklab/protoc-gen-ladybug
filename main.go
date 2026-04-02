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

	"github.com/kranklab/protoc-gen-ladybug/ladybug"
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
	ProtoName  string
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
					ProtoName:  string(field.Desc.Name()),
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
					Name:      columnName(field),
					ProtoName: string(field.Desc.Name()),
					Type:      protoKindToLadybug(field),
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
	return fmt.Sprintf("CREATE REL TABLE IF NOT EXISTS %s(%s)", toScreamingSnake(t.Name), strings.Join(parts, ", "))
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

	// Column metadata types.
	g.P()
	g.P("export type ColumnType =")
	g.P("  | 'STRING' | 'INT32' | 'INT64' | 'UINT32' | 'UINT64'")
	g.P("  | 'FLOAT' | 'DOUBLE' | 'BOOL' | 'BLOB'")
	g.P("  | 'STRING[]' | 'INT32[]' | 'INT64[]' | 'UINT32[]' | 'UINT64[]'")
	g.P("  | 'FLOAT[]' | 'DOUBLE[]' | 'BOOL[]' | 'BLOB[]';")
	g.P()
	g.P("export interface ColumnDef {")
	g.P("  readonly name: string;")
	g.P("  readonly type: ColumnType;")
	g.P("}")
	g.P()

	// NODE_COLUMNS — all columns per node type.
	g.P("export const NODE_COLUMNS: Readonly<Record<NodeType, readonly ColumnDef[]>> = {")
	for _, t := range nodes {
		g.P("  ", t.Name, ": [")
		for _, col := range t.Columns {
			g.P("    { name: '", col.Name, "', type: '", col.Type, "' },")
		}
		g.P("  ],")
	}
	g.P("} as const;")
	g.P()

	// NODE_COLUMN_NAMES — just the name strings per node type.
	g.P("export const NODE_COLUMN_NAMES: Readonly<Record<NodeType, readonly string[]>> = {")
	for _, t := range nodes {
		g.P("  ", t.Name, ": [")
		for _, col := range t.Columns {
			g.P("    '", col.Name, "',")
		}
		g.P("  ],")
	}
	g.P("} as const;")

	// Rel DDL factory functions.
	if len(rels) > 0 {
		g.P()
		g.P("export const REL_SCHEMA = {")
		for _, t := range rels {
			g.P("  ", toScreamingSnake(t.Name), ": (from: string, to: string) =>")
			g.P("    `", buildRelDDLTemplate(t, "${from}", "${to}"), "`,")
		}
		g.P("} as const;")
		g.P()

		// Rel type names.
		g.P("export const REL_TYPES = [")
		for _, t := range rels {
			g.P("  '", toScreamingSnake(t.Name), "',")
		}
		g.P("] as const;")
		g.P()
		g.P("export type RelType = (typeof REL_TYPES)[number];")
		g.P()

		// REL_COLUMNS — all columns per rel type.
		g.P("export const REL_COLUMNS: Readonly<Record<RelType, readonly ColumnDef[]>> = {")
		for _, t := range rels {
			g.P("  ", toScreamingSnake(t.Name), ": [")
			for _, col := range t.Columns {
				g.P("    { name: '", col.Name, "', type: '", col.Type, "' },")
			}
			g.P("  ],")
		}
		g.P("} as const;")
		g.P()

		// REL_COLUMN_NAMES — just the name strings per rel type.
		g.P("export const REL_COLUMN_NAMES: Readonly<Record<RelType, readonly string[]>> = {")
		for _, t := range rels {
			g.P("  ", toScreamingSnake(t.Name), ": [")
			for _, col := range t.Columns {
				g.P("    '", col.Name, "',")
			}
			g.P("  ],")
		}
		g.P("} as const;")
	}

	// Column-name remap maps (only entries where ladybug.column differs from proto field name).
	if hasRemappedColumns(nodes, rels) {
		g.P()
		// Mapping data: column name -> proto field name, per type.
		tsTypeParam := "NodeType"
		if len(rels) > 0 {
			tsTypeParam = "NodeType | RelType"
		}
		g.P("const COLUMN_TO_PROTO: Partial<Readonly<Record<", tsTypeParam, ", Readonly<Record<string, string>>>>> = {")
		for _, t := range nodes {
			entries := remapEntries(t.Columns)
			if len(entries) > 0 {
				g.P("  ", t.Name, ": { ", entries, " },")
			}
		}
		for _, t := range rels {
			entries := remapEntries(t.Columns)
			if len(entries) > 0 {
				g.P("  ", toScreamingSnake(t.Name), ": { ", entries, " },")
			}
		}
		g.P("} as const;")
		g.P()

		g.P("const PROTO_TO_COLUMN: Partial<Readonly<Record<", tsTypeParam, ", Readonly<Record<string, string>>>>> = {")
		for _, t := range nodes {
			entries := reverseRemapEntries(t.Columns)
			if len(entries) > 0 {
				g.P("  ", t.Name, ": { ", entries, " },")
			}
		}
		for _, t := range rels {
			entries := reverseRemapEntries(t.Columns)
			if len(entries) > 0 {
				g.P("  ", toScreamingSnake(t.Name), ": { ", entries, " },")
			}
		}
		g.P("} as const;")
		g.P()

		// ladybugToProto: remap DB row keys to proto field names.
		g.P("/**")
		g.P(" * Remap LadybugDB column names to protobuf field names.")
		g.P(" *")
		g.P(" * Use this when reading a row from the database and converting it to a")
		g.P(" * protobuf message. Only columns with a `(ladybug.column)` override are")
		g.P(" * remapped; all other keys pass through unchanged. Returns the input")
		g.P(" * unchanged for types with no remapped columns.")
		g.P(" *")
		g.P(" * @example")
		g.P(" * ```ts")
		g.P(" * const row = { id: 's1', name: 'api', repoUrl: 'https://...' };")
		g.P(" * const protoObj = ladybugToProto('Service', row);")
		g.P(" * // { id: 's1', name: 'api', repo_url: 'https://...' }")
		g.P(" * ```")
		g.P(" */")
		g.P("export function ladybugToProto(typeName: ", tsTypeParam, ", row: Record<string, unknown>): Record<string, unknown> {")
		g.P("  const mapping = COLUMN_TO_PROTO[typeName];")
		g.P("  if (!mapping) return row;")
		g.P("  const result: Record<string, unknown> = {};")
		g.P("  for (const [key, value] of Object.entries(row)) {")
		g.P("    result[mapping[key] ?? key] = value;")
		g.P("  }")
		g.P("  return result;")
		g.P("}")
		g.P()

		// protoToLadybug: remap proto field names to DB column names.
		g.P("/**")
		g.P(" * Remap protobuf field names to LadybugDB column names.")
		g.P(" *")
		g.P(" * Use this when converting a protobuf message (or its dict representation)")
		g.P(" * to a row suitable for writing to the database. Only fields with a")
		g.P(" * `(ladybug.column)` override are remapped; all other keys pass through")
		g.P(" * unchanged. Returns the input unchanged for types with no remapped columns.")
		g.P(" *")
		g.P(" * @example")
		g.P(" * ```ts")
		g.P(" * const obj = { id: 's1', name: 'api', repo_url: 'https://...' };")
		g.P(" * const row = protoToLadybug('Service', obj);")
		g.P(" * // { id: 's1', name: 'api', repoUrl: 'https://...' }")
		g.P(" * ```")
		g.P(" */")
		g.P("export function protoToLadybug(typeName: ", tsTypeParam, ", obj: Record<string, unknown>): Record<string, unknown> {")
		g.P("  const mapping = PROTO_TO_COLUMN[typeName];")
		g.P("  if (!mapping) return obj;")
		g.P("  const result: Record<string, unknown> = {};")
		g.P("  for (const [key, value] of Object.entries(obj)) {")
		g.P("    result[mapping[key] ?? key] = value;")
		g.P("  }")
		g.P("  return result;")
		g.P("}")
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

	// NodeType is the type for node type name constants.
	g.P("// NodeType identifies a node table type.")
	g.P("type NodeType string")
	g.P()

	// Node type name constants.
	for _, t := range nodes {
		g.P("// NodeType", t.Name, " is the node type name for ", t.Name, " nodes.")
		g.P("const NodeType", t.Name, " NodeType = \"", t.Name, "\"")
	}
	g.P()

	// NodeTypes slice.
	g.P("// NodeTypes contains all node type names.")
	g.P("var NodeTypes = []NodeType{")
	for _, t := range nodes {
		g.P("\tNodeType", t.Name, ",")
	}
	g.P("}")

	// Column metadata type.
	g.P()
	g.P("// ColumnDef describes a single column in a node table.")
	g.P("type ColumnDef struct {")
	g.P("\tName string")
	g.P("\tType string")
	g.P("}")
	g.P()

	// NodeColumns map.
	g.P("// NodeColumns maps each node type to its ordered list of column definitions.")
	g.P("var NodeColumns = map[NodeType][]ColumnDef{")
	for _, t := range nodes {
		g.P("\tNodeType", t.Name, ": {")
		for _, col := range t.Columns {
			g.P("\t\t{Name: \"", col.Name, "\", Type: \"", col.Type, "\"},")
		}
		g.P("\t},")
	}
	g.P("}")
	g.P()

	// NodeColumnNames map.
	g.P("// NodeColumnNames maps each node type to its ordered list of column names.")
	g.P("var NodeColumnNames = map[NodeType][]string{")
	for _, t := range nodes {
		g.P("\tNodeType", t.Name, ": {")
		for _, col := range t.Columns {
			g.P("\t\t\"", col.Name, "\",")
		}
		g.P("\t},")
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

		// RelType type and constants.
		g.P("// RelType identifies a relationship table type.")
		g.P("type RelType string")
		g.P()

		for _, t := range rels {
			g.P("// RelType", t.Name, " is the relationship type name for ", t.Name, " relationships.")
			g.P("const RelType", t.Name, " RelType = \"", toScreamingSnake(t.Name), "\"")
		}
		g.P()

		// RelTypes slice.
		g.P("// RelTypes contains all relationship type names.")
		g.P("var RelTypes = []RelType{")
		for _, t := range rels {
			g.P("\tRelType", t.Name, ",")
		}
		g.P("}")
		g.P()

		// RelColumns map.
		g.P("// RelColumns maps each rel type to its ordered list of column definitions.")
		g.P("var RelColumns = map[RelType][]ColumnDef{")
		for _, t := range rels {
			g.P("\tRelType", t.Name, ": {")
			for _, col := range t.Columns {
				g.P("\t\t{Name: \"", col.Name, "\", Type: \"", col.Type, "\"},")
			}
			g.P("\t},")
		}
		g.P("}")
		g.P()

		// RelColumnNames map.
		g.P("// RelColumnNames maps each rel type to its ordered list of column names.")
		g.P("var RelColumnNames = map[RelType][]string{")
		for _, t := range rels {
			g.P("\tRelType", t.Name, ": {")
			for _, col := range t.Columns {
				g.P("\t\t\"", col.Name, "\",")
			}
			g.P("\t},")
		}
		g.P("}")
	}

	// Column-name remap functions.
	if hasRemappedColumns(nodes, rels) {
		g.P()
		g.P("// columnToProto maps type name -> column name -> proto field name for renamed columns.")
		g.P("var columnToProto = map[string]map[string]string{")
		for _, t := range nodes {
			if entries := goRemapEntries(t.Columns); entries != "" {
				g.P("\tstring(NodeType", t.Name, "): {", entries, "},")
			}
		}
		for _, t := range rels {
			if entries := goRemapEntries(t.Columns); entries != "" {
				g.P("\tstring(RelType", t.Name, "): {", entries, "},")
			}
		}
		g.P("}")
		g.P()

		g.P("// protoToColumn maps type name -> proto field name -> column name for renamed columns.")
		g.P("var protoToColumn = map[string]map[string]string{")
		for _, t := range nodes {
			if entries := goReverseRemapEntries(t.Columns); entries != "" {
				g.P("\tstring(NodeType", t.Name, "): {", entries, "},")
			}
		}
		for _, t := range rels {
			if entries := goReverseRemapEntries(t.Columns); entries != "" {
				g.P("\tstring(RelType", t.Name, "): {", entries, "},")
			}
		}
		g.P("}")
		g.P()

		g.P("// LadybugToProto remaps LadybugDB column names to protobuf field names.")
		g.P("//")
		g.P("// Use this when reading a row from the database and converting it to a")
		g.P("// protobuf message. Only columns with a (ladybug.column) override are")
		g.P("// remapped; all other keys pass through unchanged. Returns the input")
		g.P("// unchanged for types with no remapped columns.")
		g.P("//")
		g.P("// Accepts NodeType, RelType, or any ~string type.")
		g.P("func LadybugToProto[T ~string](typeName T, row map[string]any) map[string]any {")
		g.P("\tmapping := columnToProto[string(typeName)]")
		g.P("\tif mapping == nil {")
		g.P("\t\treturn row")
		g.P("\t}")
		g.P("\tresult := make(map[string]any, len(row))")
		g.P("\tfor k, v := range row {")
		g.P("\t\tif mapped, ok := mapping[k]; ok {")
		g.P("\t\t\tresult[mapped] = v")
		g.P("\t\t} else {")
		g.P("\t\t\tresult[k] = v")
		g.P("\t\t}")
		g.P("\t}")
		g.P("\treturn result")
		g.P("}")
		g.P()

		g.P("// ProtoToLadybug remaps protobuf field names to LadybugDB column names.")
		g.P("//")
		g.P("// Use this when converting a protobuf message (or its map representation)")
		g.P("// to a row suitable for writing to the database. Only fields with a")
		g.P("// (ladybug.column) override are remapped; all other keys pass through")
		g.P("// unchanged. Returns the input unchanged for types with no remapped columns.")
		g.P("//")
		g.P("// Accepts NodeType, RelType, or any ~string type.")
		g.P("func ProtoToLadybug[T ~string](typeName T, obj map[string]any) map[string]any {")
		g.P("\tmapping := protoToColumn[string(typeName)]")
		g.P("\tif mapping == nil {")
		g.P("\t\treturn obj")
		g.P("\t}")
		g.P("\tresult := make(map[string]any, len(obj))")
		g.P("\tfor k, v := range obj {")
		g.P("\t\tif mapped, ok := mapping[k]; ok {")
		g.P("\t\t\tresult[mapped] = v")
		g.P("\t\t} else {")
		g.P("\t\t\tresult[k] = v")
		g.P("\t\t}")
		g.P("\t}")
		g.P("\treturn result")
		g.P("}")
	}

	return nil
}

func generatePython(gen *protogen.Plugin, nodes []nodeTable, rels []relTable) error {
	g := gen.NewGeneratedFile("schema_gen.py", "")
	g.P("# Auto-generated by protoc-gen-ladybug — do not edit.")
	g.P()
	g.P("from typing import Final, Literal")
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

	// NodeType literal union.
	{
		var literals []string
		for _, t := range nodes {
			literals = append(literals, fmt.Sprintf("Literal[\"%s\"]", t.Name))
		}
		g.P("NodeType = ", strings.Join(literals, " | "))
	}
	g.P()

	// Node types list.
	g.P("NODE_TYPES: Final[list[NodeType]] = [")
	for _, t := range nodes {
		g.P("    NODE_TYPE_", toScreamingSnake(t.Name), ",")
	}
	g.P("]")

	// NODE_COLUMNS — all columns per node type.
	g.P()
	g.P("NODE_COLUMNS: Final[dict[NodeType, list[tuple[str, str]]]] = {")
	for _, t := range nodes {
		g.P("    \"", t.Name, "\": [")
		for _, col := range t.Columns {
			g.P("        (\"", col.Name, "\", \"", col.Type, "\"),")
		}
		g.P("    ],")
	}
	g.P("}")
	g.P()

	// NODE_COLUMN_NAMES — just the name strings per node type.
	g.P("NODE_COLUMN_NAMES: Final[dict[NodeType, list[str]]] = {")
	for _, t := range nodes {
		g.P("    \"", t.Name, "\": [")
		for _, col := range t.Columns {
			g.P("        \"", col.Name, "\",")
		}
		g.P("    ],")
	}
	g.P("}")

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
			g.P("REL_TYPE_", toScreamingSnake(t.Name), ": Final[str] = \"", toScreamingSnake(t.Name), "\"")
		}
		g.P()

		// RelType literal union.
		{
			var literals []string
			for _, t := range rels {
				literals = append(literals, fmt.Sprintf("Literal[\"%s\"]", toScreamingSnake(t.Name)))
			}
			g.P("RelType = ", strings.Join(literals, " | "))
		}
		g.P()

		// Rel types list.
		g.P("REL_TYPES: Final[list[RelType]] = [")
		for _, t := range rels {
			g.P("    REL_TYPE_", toScreamingSnake(t.Name), ",")
		}
		g.P("]")
		g.P()

		// REL_COLUMNS — all columns per rel type.
		g.P("REL_COLUMNS: Final[dict[RelType, list[tuple[str, str]]]] = {")
		for _, t := range rels {
			g.P("    \"", toScreamingSnake(t.Name), "\": [")
			for _, col := range t.Columns {
				g.P("        (\"", col.Name, "\", \"", col.Type, "\"),")
			}
			g.P("    ],")
		}
		g.P("}")
		g.P()

		// REL_COLUMN_NAMES — just the name strings per rel type.
		g.P("REL_COLUMN_NAMES: Final[dict[RelType, list[str]]] = {")
		for _, t := range rels {
			g.P("    \"", toScreamingSnake(t.Name), "\": [")
			for _, col := range t.Columns {
				g.P("        \"", col.Name, "\",")
			}
			g.P("    ],")
		}
		g.P("}")
	}

	// Column-name remap functions.
	if hasRemappedColumns(nodes, rels) {
		g.P()
		g.P()
		pyTypeParam := "NodeType"
		if len(rels) > 0 {
			pyTypeParam = "NodeType | RelType"
		}
		g.P("_COLUMN_TO_PROTO: Final[dict[", pyTypeParam, ", dict[str, str]]] = {")
		for _, t := range nodes {
			if entries := pyRemapEntries(t.Columns); entries != "" {
				g.P("    \"", t.Name, "\": {", entries, "},")
			}
		}
		for _, t := range rels {
			if entries := pyRemapEntries(t.Columns); entries != "" {
				g.P("    \"", toScreamingSnake(t.Name), "\": {", entries, "},")
			}
		}
		g.P("}")
		g.P()

		g.P("_PROTO_TO_COLUMN: Final[dict[", pyTypeParam, ", dict[str, str]]] = {")
		for _, t := range nodes {
			if entries := pyReverseRemapEntries(t.Columns); entries != "" {
				g.P("    \"", t.Name, "\": {", entries, "},")
			}
		}
		for _, t := range rels {
			if entries := pyReverseRemapEntries(t.Columns); entries != "" {
				g.P("    \"", toScreamingSnake(t.Name), "\": {", entries, "},")
			}
		}
		g.P("}")
		g.P()
		g.P()

		g.P("def ladybug_to_proto(type_name: ", pyTypeParam, ", row: dict[str, object]) -> dict[str, object]:")
		g.P("    \"\"\"Remap LadybugDB column names to protobuf field names.")
		g.P()
		g.P("    Use this when reading a row from the database and converting it to a")
		g.P("    protobuf message via ``ParseDict``. Only columns with a ``(ladybug.column)``")
		g.P("    override are remapped; all other keys pass through unchanged. Returns the")
		g.P("    input unchanged for types with no remapped columns.")
		g.P()
		g.P("    Example::")
		g.P()
		g.P("        row = {\"id\": \"s1\", \"name\": \"api\", \"repoUrl\": \"https://...\"}")
		g.P("        msg = ParseDict(ladybug_to_proto(\"Service\", row), ServiceNode())")
		g.P("    \"\"\"")
		g.P("    mapping = _COLUMN_TO_PROTO.get(type_name)")
		g.P("    if not mapping:")
		g.P("        return row")
		g.P("    return {mapping.get(k, k): v for k, v in row.items()}")
		g.P()
		g.P()

		g.P("def proto_to_ladybug(type_name: ", pyTypeParam, ", obj: dict[str, object]) -> dict[str, object]:")
		g.P("    \"\"\"Remap protobuf field names to LadybugDB column names.")
		g.P()
		g.P("    Use this when converting a protobuf message (or its dict representation)")
		g.P("    to a row suitable for writing to the database. Only fields with a")
		g.P("    ``(ladybug.column)`` override are remapped; all other keys pass through")
		g.P("    unchanged. Returns the input unchanged for types with no remapped columns.")
		g.P()
		g.P("    Example::")
		g.P()
		g.P("        obj = MessageToDict(msg, preserving_proto_field_name=True)")
		g.P("        row = proto_to_ladybug(\"Service\", obj)")
		g.P("        # {\"id\": \"s1\", \"name\": \"api\", \"repoUrl\": \"https://...\"}")
		g.P("    \"\"\"")
		g.P("    mapping = _PROTO_TO_COLUMN.get(type_name)")
		g.P("    if not mapping:")
		g.P("        return obj")
		g.P("    return {mapping.get(k, k): v for k, v in obj.items()}")
	}

	return nil
}

// remapEntries returns a JS object literal fragment for column->proto name mappings.
// Only includes entries where the names differ.
func remapEntries(cols []column) string {
	var parts []string
	for _, col := range cols {
		if col.Name != col.ProtoName {
			parts = append(parts, fmt.Sprintf("'%s': '%s'", col.Name, col.ProtoName))
		}
	}
	return strings.Join(parts, ", ")
}

// reverseRemapEntries returns a JS object literal fragment for proto->column name mappings.
func reverseRemapEntries(cols []column) string {
	var parts []string
	for _, col := range cols {
		if col.Name != col.ProtoName {
			parts = append(parts, fmt.Sprintf("'%s': '%s'", col.ProtoName, col.Name))
		}
	}
	return strings.Join(parts, ", ")
}

// goRemapEntries returns a Go map literal fragment for column->proto name mappings.
func goRemapEntries(cols []column) string {
	var parts []string
	for _, col := range cols {
		if col.Name != col.ProtoName {
			parts = append(parts, fmt.Sprintf("\"%s\": \"%s\"", col.Name, col.ProtoName))
		}
	}
	return strings.Join(parts, ", ")
}

// goReverseRemapEntries returns a Go map literal fragment for proto->column name mappings.
func goReverseRemapEntries(cols []column) string {
	var parts []string
	for _, col := range cols {
		if col.Name != col.ProtoName {
			parts = append(parts, fmt.Sprintf("\"%s\": \"%s\"", col.ProtoName, col.Name))
		}
	}
	return strings.Join(parts, ", ")
}

// pyRemapEntries returns a Python dict literal fragment for column->proto name mappings.
func pyRemapEntries(cols []column) string {
	var parts []string
	for _, col := range cols {
		if col.Name != col.ProtoName {
			parts = append(parts, fmt.Sprintf("\"%s\": \"%s\"", col.Name, col.ProtoName))
		}
	}
	return strings.Join(parts, ", ")
}

// pyReverseRemapEntries returns a Python dict literal fragment for proto->column name mappings.
func pyReverseRemapEntries(cols []column) string {
	var parts []string
	for _, col := range cols {
		if col.Name != col.ProtoName {
			parts = append(parts, fmt.Sprintf("\"%s\": \"%s\"", col.ProtoName, col.Name))
		}
	}
	return strings.Join(parts, ", ")
}

// hasRemappedColumns returns true if any table in either list has a column
// whose DDL name differs from its proto field name.
func hasRemappedColumns(nodes []nodeTable, rels []relTable) bool {
	for _, t := range nodes {
		for _, col := range t.Columns {
			if col.Name != col.ProtoName {
				return true
			}
		}
	}
	for _, t := range rels {
		for _, col := range t.Columns {
			if col.Name != col.ProtoName {
				return true
			}
		}
	}
	return false
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
