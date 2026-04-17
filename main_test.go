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

package main

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestProtoKindToLadybugScalar(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.StringKind, "STRING"},
		{protoreflect.Int32Kind, "INT32"},
		{protoreflect.Int64Kind, "INT64"},
		{protoreflect.Uint32Kind, "UINT32"},
		{protoreflect.Uint64Kind, "UINT64"},
		{protoreflect.FloatKind, "FLOAT"},
		{protoreflect.DoubleKind, "DOUBLE"},
		{protoreflect.BoolKind, "BOOL"},
		{protoreflect.BytesKind, "BLOB"},
		{protoreflect.Sint32Kind, "INT32"},
		{protoreflect.Sfixed64Kind, "INT64"},
		{protoreflect.Fixed32Kind, "UINT32"},
		{protoreflect.Fixed64Kind, "UINT64"},
		// Unknown/message kinds fall back to STRING.
		{protoreflect.MessageKind, "STRING"},
		{protoreflect.EnumKind, "STRING"},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := protoKindToLadybugScalar(tt.kind)
			if got != tt.want {
				t.Errorf("protoKindToLadybugScalar(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestBuildDDL(t *testing.T) {
	table := nodeTable{
		Name: "File",
		Columns: []column{
			{Name: "id", Type: "STRING", PrimaryKey: true},
			{Name: "name", Type: "STRING"},
			{Name: "path", Type: "STRING"},
			{Name: "language", Type: "STRING"},
			{Name: "start_line", Type: "INT32"},
			{Name: "labels", Type: "STRING[]"},
		},
	}
	got := buildDDL(table)
	want := "CREATE NODE TABLE IF NOT EXISTS File(id STRING PRIMARY KEY, name STRING, path STRING, language STRING, start_line INT32, labels STRING[])"
	if got != want {
		t.Errorf("buildDDL() =\n  %s\nwant:\n  %s", got, want)
	}
}

func TestBuildDDL_SingleColumn(t *testing.T) {
	table := nodeTable{
		Name: "Simple",
		Columns: []column{
			{Name: "id", Type: "STRING", PrimaryKey: true},
		},
	}
	got := buildDDL(table)
	want := "CREATE NODE TABLE IF NOT EXISTS Simple(id STRING PRIMARY KEY)"
	if got != want {
		t.Errorf("buildDDL() = %s, want %s", got, want)
	}
}

func TestBuildDDL_ExplicitPrimaryKey(t *testing.T) {
	table := nodeTable{
		Name: "Event",
		Columns: []column{
			{Name: "name", Type: "STRING"},
			{Name: "timestamp", Type: "INT64"},
			{Name: "event_id", Type: "STRING", PrimaryKey: true},
		},
	}
	got := buildDDL(table)
	want := "CREATE NODE TABLE IF NOT EXISTS Event(name STRING, timestamp INT64, event_id STRING PRIMARY KEY)"
	if got != want {
		t.Errorf("buildDDL() =\n  %s\nwant:\n  %s", got, want)
	}
}

func TestToScreamingSnake(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"File", "FILE"},
		{"FunctionCall", "FUNCTION_CALL"},
		{"HTTPRequest", "H_T_T_P_REQUEST"},
		{"simple", "SIMPLE"},
		{"A", "A"},
		{"APIKey", "A_P_I_KEY"},
		{"ServiceDependency", "SERVICE_DEPENDENCY"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toScreamingSnake(tt.input)
			if got != tt.want {
				t.Errorf("toScreamingSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToSnake(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Calls", "calls"},
		{"DefinedIn", "defined_in"},
		{"DependsOn", "depends_on"},
		{"A", "a"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toSnake(tt.input)
			if got != tt.want {
				t.Errorf("toSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildRelDDLTemplate(t *testing.T) {
	table := relTable{
		Name: "Calls",
		Columns: []column{
			{Name: "id", Type: "STRING"},
			{Name: "args", Type: "STRING"},
			{Name: "confidence", Type: "FLOAT"},
		},
	}
	got := buildRelDDLTemplate(table, "${joinRelPairs(pairs)}")
	want := "CREATE REL TABLE IF NOT EXISTS CALLS(${joinRelPairs(pairs)}, id STRING, args STRING, confidence FLOAT)"
	if got != want {
		t.Errorf("buildRelDDLTemplate() =\n  %s\nwant:\n  %s", got, want)
	}
}

func TestBuildRelDDLTemplate_GoFormat(t *testing.T) {
	table := relTable{
		Name: "DefinedIn",
		Columns: []column{
			{Name: "id", Type: "STRING"},
			{Name: "start_line", Type: "INT32"},
			{Name: "end_line", Type: "INT32"},
		},
	}
	got := buildRelDDLTemplate(table, "%s")
	want := "CREATE REL TABLE IF NOT EXISTS DEFINED_IN(%s, id STRING, start_line INT32, end_line INT32)"
	if got != want {
		t.Errorf("buildRelDDLTemplate() =\n  %s\nwant:\n  %s", got, want)
	}
}

// TestBuildRelDDLTemplate_MultiPair verifies that the generated Go template,
// when populated with multiple FROM/TO clauses, yields a single
// CREATE REL TABLE statement that declares every pair — the exact shape
// Ladybug requires for a multi-pair rel label.
func TestBuildRelDDLTemplate_MultiPair(t *testing.T) {
	table := relTable{
		Name: "Emits",
		Columns: []column{
			{Name: "id", Type: "STRING"},
			{Name: "count", Type: "INT64"},
			{Name: "lastSeen", Type: "STRING"},
			{Name: "windowStart", Type: "STRING"},
		},
	}
	template := buildRelDDLTemplate(table, "%s")
	pairs := []string{
		"FROM Service TO LogEvent",
		"FROM Service TO Metric",
		"FROM Service TO Error",
	}
	got := fmt.Sprintf(template, strings.Join(pairs, ", "))
	want := "CREATE REL TABLE IF NOT EXISTS EMITS(FROM Service TO LogEvent, FROM Service TO Metric, FROM Service TO Error, id STRING, count INT64, lastSeen STRING, windowStart STRING)"
	if got != want {
		t.Errorf("multi-pair expansion =\n  %s\nwant:\n  %s", got, want)
	}
	// The result must be a single CREATE REL TABLE statement; all pairs live
	// inside one parenthesized body.
	if strings.Count(got, "CREATE REL TABLE") != 1 {
		t.Errorf("expected exactly one CREATE REL TABLE, got:\n  %s", got)
	}
	for _, p := range pairs {
		if !strings.Contains(got, p) {
			t.Errorf("expected DDL to contain %q, got:\n  %s", p, got)
		}
	}
}

// TestBuildRelDDLTemplate_SinglePair confirms that calling the factory with
// exactly one pair still produces valid DDL — the single-pair path is the
// common case and must not regress when the signature is variadic.
func TestBuildRelDDLTemplate_SinglePair(t *testing.T) {
	table := relTable{
		Name: "Calls",
		Columns: []column{
			{Name: "id", Type: "STRING"},
			{Name: "args", Type: "STRING"},
		},
	}
	template := buildRelDDLTemplate(table, "%s")
	got := fmt.Sprintf(template, "FROM Function TO Function")
	want := "CREATE REL TABLE IF NOT EXISTS CALLS(FROM Function TO Function, id STRING, args STRING)"
	if got != want {
		t.Errorf("single-pair expansion =\n  %s\nwant:\n  %s", got, want)
	}
}
