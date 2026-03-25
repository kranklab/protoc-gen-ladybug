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
			{"id", "STRING", true},
			{"name", "STRING", false},
			{"path", "STRING", false},
			{"language", "STRING", false},
			{"start_line", "INT32", false},
			{"labels", "STRING[]", false},
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
			{"id", "STRING", true},
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
			{"name", "STRING", false},
			{"timestamp", "INT64", false},
			{"event_id", "STRING", true},
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
			{"id", "STRING", false},
			{"args", "STRING", false},
			{"confidence", "FLOAT", false},
		},
	}
	got := buildRelDDLTemplate(table, "${from}", "${to}")
	want := "CREATE REL TABLE IF NOT EXISTS Calls(FROM ${from} TO ${to}, id STRING, args STRING, confidence FLOAT)"
	if got != want {
		t.Errorf("buildRelDDLTemplate() =\n  %s\nwant:\n  %s", got, want)
	}
}

func TestBuildRelDDLTemplate_GoFormat(t *testing.T) {
	table := relTable{
		Name: "DefinedIn",
		Columns: []column{
			{"id", "STRING", false},
			{"start_line", "INT32", false},
			{"end_line", "INT32", false},
		},
	}
	got := buildRelDDLTemplate(table, "%s", "%s")
	want := "CREATE REL TABLE IF NOT EXISTS DefinedIn(FROM %s TO %s, id STRING, start_line INT32, end_line INT32)"
	if got != want {
		t.Errorf("buildRelDDLTemplate() =\n  %s\nwant:\n  %s", got, want)
	}
}
