// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"github.com/PRO-Robotech/kacho/pkg/filter"

	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ParseNameFilter is the single name= filter parser shared by all nlb List
// use-cases (loadbalancer / targetgroup / listener). It delegates to
// kacho-corelib/filter.Parse with the canonical whitelist {"name"} so the
// grammar + error texts are identical across resources (api-conventions:
// `filter` — kacho-corelib/filter.Parse с whitelist полей).
//
// Contract (reconciled from three divergent local parsers — see review report):
//   - empty input            → (nil, nil)              // no filter
//   - name="value"           → (узел `=`, nil)
//   - name = "value" (spaced) → (узел `=`, nil)
//   - name CONTAINS "value"  → (узел CONTAINS, nil)     // оператор ДОЕЗЖАЕТ до репозитория
//   - name=value (unquoted)  → InvalidArgument          // strict: value must be quoted
//   - unknown="x"            → InvalidArgument          // whitelist rejects unknown field
//   - garbage                → InvalidArgument
func TestParseNameFilter(t *testing.T) {
	t.Parallel()
	t.Run("valid", func(t *testing.T) {
		// Утверждается ПАРА «оператор + значение», а не одно значение: пока
		// проверялось только значение, потеря оператора была невидима (#460).
		cases := map[string]struct{ op, value string }{
			`name="edge"`:         {filter.OpEquals, "edge"},
			`name="api-1"`:        {filter.OpEquals, "api-1"},
			`name = "spaced"`:     {filter.OpEquals, "spaced"},
			`name=""`:             {filter.OpEquals, ""},
			`name CONTAINS "edg"`: {filter.OpContains, "edg"},
			`name CONTAINS "a-1"`: {filter.OpContains, "a-1"},
		}
		for in, want := range cases {
			got, err := ParseNameFilter(in)
			if err != nil {
				t.Fatalf("ParseNameFilter(%q): unexpected err: %v", in, err)
			}
			if got == nil {
				t.Fatalf("ParseNameFilter(%q) = nil, ожидался узел", in)
			}
			if got.Op != want.op || got.Value != want.value {
				t.Fatalf("ParseNameFilter(%q) = {%s %q}, want {%s %q}",
					in, got.Op, got.Value, want.op, want.value)
			}
		}
		// Пустое выражение — отсутствие сужения, а не узел с пустым значением.
		got, err := ParseNameFilter(``)
		if err != nil {
			t.Fatalf("ParseNameFilter(``): unexpected err: %v", err)
		}
		if got != nil {
			t.Fatalf("ParseNameFilter(``) = %#v, ожидался nil (сужения нет)", got)
		}
	})

	t.Run("invalid_arg", func(t *testing.T) {
		// Each of these diverged across the three former local parsers; the
		// unified strict contract rejects them all with InvalidArgument.
		for _, in := range []string{
			`name=edge`,   // unquoted value
			`name=`,       // no value
			`other="foo"`, // unknown field (whitelist)
			`garbage`,     // not a filter expression
			`name "edge"`, // missing operator
			`region="ru"`, // unknown field
		} {
			got, err := ParseNameFilter(in)
			if err == nil {
				t.Fatalf("ParseNameFilter(%q): expected InvalidArgument, got value %q", in, got)
			}
			if code := status.Code(err); code != codes.InvalidArgument {
				t.Fatalf("ParseNameFilter(%q): expected InvalidArgument, got %s (%v)", in, code, err)
			}
		}
	})
}
