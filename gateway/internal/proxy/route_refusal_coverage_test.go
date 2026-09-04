// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package proxy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// The external-isolation gate enumerates its subject from the proto tree — every
// `service Internal*` and every rpc inside it — and probes each one on the
// advertised external listener. Its verdict is only as complete as the refusal's
// coverage: an Internal* service the refusal does not recognise would answer
// from authorization again, and the gate would report it unverified rather than
// isolated.
//
// So the subject is derived HERE FROM THE SAME SOURCE, rather than from a list
// kept by hand. A hand-kept list is the failure this guards against: a new
// Internal* service added to the proto tree would be probed by the gate and
// missed by a stale list, and nothing would say so.
//
// Note the two predicates are deliberately not identical in wording — the gate
// matches `Internal\w+`, the refusal matches "starts with Internal AND ends with
// Service". This test is what keeps that difference harmless: a service named
// `InternalSomething` without the suffix would be probed and not refused, and
// this is where that shows up.

var (
	serviceDecl = regexp.MustCompile(`(?m)^service\s+(Internal\w+)\s*\{`)
	packageDecl = regexp.MustCompile(`(?m)^package\s+([\w.]+)\s*;`)
	rpcDecl     = regexp.MustCompile(`(?m)^\s*rpc\s+(\w+)\s*\(`)
)

func TestRefusalCoversEveryInternalRPCInTheProtoTree(t *testing.T) {
	root := repoRoot(t)
	// The subject is the whole contract tree, not the `v1/` layout. A domain that
	// puts its contract next to the form rather than under `v1/` used to fall out
	// of the subject silently — measured 2026-08-23: one such domain existed, its
	// Internal* method was in no gate's subject, and the refusal's coverage of it
	// was a promise rather than a walk. `v1/` is a convention, not an invariant.
	//
	// The corpus comes from the git index, not from a disk walk: a walk would read
	// whatever the working copy happens to hold, and the verdict would become a
	// property of the machine rather than of the commit.
	protos, err := treecorpus.UnderWithSuffix(filepath.Join(root, "proto", "kacho", "cloud"), ".proto")
	if err != nil {
		t.Fatalf("proto corpus: %v", err)
	}
	if len(protos) == 0 {
		t.Fatal("no proto files found — this test would then vacuously pass, which is the one outcome it must not have")
	}
	t.Logf("proto files in the index under the contract tree: %d", len(protos))

	var methods []string
	for _, p := range protos {
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("read %s: %v", p, rerr)
		}
		src := string(b)
		pkg := packageDecl.FindStringSubmatch(src)
		if pkg == nil {
			continue
		}
		for _, sm := range serviceDecl.FindAllStringSubmatchIndex(src, -1) {
			name := src[sm[2]:sm[3]]
			body := serviceBody(src, sm[1])
			for _, rm := range rpcDecl.FindAllStringSubmatch(body, -1) {
				methods = append(methods, "/"+pkg[1]+"."+name+"/"+rm[1])
			}
		}
	}
	if len(methods) == 0 {
		t.Fatal("no Internal* rpc found in the proto tree — vacuous pass")
	}

	var uncovered []string
	for _, m := range methods {
		if !proxy.IsInternalRoute(m) {
			uncovered = append(uncovered, m)
		}
	}
	if len(uncovered) > 0 {
		t.Fatalf("%d of %d Internal* methods are NOT recognised by the external listener's route refusal, "+
			"so they would keep answering from authorization — named permission and all:\n  %s",
			len(uncovered), len(methods), strings.Join(uncovered, "\n  "))
	}
	t.Logf("route refusal covers all %d Internal* methods in the proto tree", len(methods))
}

func serviceBody(src string, from int) string {
	depth, i := 1, from
	for i < len(src) && depth > 0 {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
		i++
	}
	return src[from:i]
}

// repoRoot walks up from the package directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("module root not found")
	return ""
}
