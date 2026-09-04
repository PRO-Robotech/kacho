// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package authz

// hide_existence_parity_internal_test.go — the two places that refuse a read on
// a foreign object must speak with one voice.
//
// The api-gateway refuses such a read one hop before the service does, and both
// answer NOT_FOUND with the owning service's own miss text. If the two tables
// drift apart, one of them stops matching the owner and becomes the tell that
// distinguishes "not yours" from "not there" — the very oracle both are written
// to close. Neither copy can import the other (the gateway's table lives under
// its own internal/ tree), so the agreement is checked at the source level: the
// gateway's map literal is parsed and compared entry by entry.
//
// A failure here is not a formality: it means one of the two answers no longer
// matches the backend.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const gatewayTableFile = "../../gateway/internal/middleware/permission_denied_response.go"

// parseGatewayFormats extracts `hideExistenceNotFoundFormats` from the gateway
// source.
func parseGatewayFormats(t *testing.T) map[string]string {
	t.Helper()
	path, err := filepath.Abs(gatewayTableFile)
	if err != nil {
		t.Fatalf("resolve gateway table path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the gateway hide-existence table is not where this guard expects it (%s): %v — move the guard with it, do not delete it", gatewayTableFile, err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse gateway table: %v", err)
	}

	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "hideExistenceNotFoundFormats" {
			return true
		}
		if len(vs.Values) != 1 {
			return false
		}
		lit, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			return false
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			k, kok := kv.Key.(*ast.BasicLit)
			v, vok := kv.Value.(*ast.BasicLit)
			if !kok || !vok {
				continue
			}
			key, kerr := strconv.Unquote(k.Value)
			val, verr := strconv.Unquote(v.Value)
			if kerr != nil || verr != nil {
				continue
			}
			out[key] = val
		}
		return false
	})
	if len(out) == 0 {
		t.Fatalf("no entries parsed from the gateway table — the guard would pass vacuously")
	}
	return out
}

// TestHideExistenceFormats_MatchTheGateway — same keys, same texts.
func TestHideExistenceFormats_MatchTheGateway(t *testing.T) {
	gateway := parseGatewayFormats(t)

	for objectType, gatewayText := range gateway {
		ours, ok := hideExistenceNotFoundFormats[objectType]
		if !ok {
			t.Errorf("object type %q is refused with the owner's text at the gateway but falls back to the neutral text here — the two answers differ", objectType)
			continue
		}
		if ours != gatewayText {
			t.Errorf("object type %q: gateway answers %q, this interceptor answers %q — one of them no longer matches the owning service", objectType, gatewayText, ours)
		}
	}
	for objectType, ourText := range hideExistenceNotFoundFormats {
		if gatewayText, ok := gateway[objectType]; !ok {
			t.Errorf("object type %q is known here (%q) but not at the gateway — the gateway's refusal for it is the neutral text and is therefore distinguishable", objectType, ourText)
		} else if gatewayText != ourText {
			t.Errorf("object type %q: texts differ (here %q, gateway %q)", objectType, ourText, gatewayText)
		}
	}
}

// TestHideExistenceMessage_ShapeOfTheFallback — the two cases where byte-identity
// is impossible must yield the least-informative answer, never the internal type.
func TestHideExistenceMessage_ShapeOfTheFallback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		objectType string
		objectID   string
		want       string
	}{
		{"known type and id", "vpc_subnet", "sub00000000000000abc", "Subnet sub00000000000000abc not found"},
		{"unknown type", "something_new", "xyz00000000000000abc", "not found"},
		{"wildcard id", "vpc_subnet", "*", "not found"},
		{"absent id", "vpc_subnet", "", "not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hideExistenceMessage(tc.objectType, tc.objectID); got != tc.want {
				t.Fatalf("hideExistenceMessage(%q,%q) = %q; want %q", tc.objectType, tc.objectID, got, tc.want)
			}
		})
	}
}
