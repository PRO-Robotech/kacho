// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command audit-list-filter is the runnable form of the kacho-registry listauthz
// gate. It is what `make -C services/registry audit-list-filter` issues and what CI
// runs; the same analysis also travels with `go test ./services/registry/...`.
//
// Every run prints the census first — how many handler and composition-root files
// were read, how many List RPCs were found, how many of them were judged — so the
// verdict can be read against the extent of what produced it. Previously a clean tree
// produced the two words "audit-list-filter: OK", which reads identically whether
// five RPCs were judged or the tree was never opened. What is asserted, and why this
// service carries no whitelist, is documented on package auditlistfilter.
//
// Exit status is the verdict: 0 when every public List enforces per-object
// visibility, 1 when it does not, 2 when the tree could not be inspected at all —
// a gate that cannot read the code must not report success. The 1-versus-2
// distinction is only visible when this command is BUILT; the Makefile target runs it
// under `go run`, which reports any non-zero status as 1. Both remain failures, so
// nothing may key on the number reaching CI — the reason is on the output, where the
// census says how much was read.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/services/registry/tools/auditlistfilter"
)

func main() {
	root := flag.String("root", ".", "path to the kacho-registry service root")
	flag.Parse()

	// The census and the findings both go to stdout, in that order, so that one
	// stream carries the whole answer: what was read, then what was found in it.
	_, err := auditlistfilter.Audit(auditlistfilter.Options{Root: *root}, os.Stdout)
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "audit-list-filter: %v\n", err)
	if errors.Is(err, auditlistfilter.ErrNotInspected) {
		os.Exit(2)
	}
	os.Exit(1)
}
