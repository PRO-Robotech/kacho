// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command audit-list-filter is the runnable form of the kacho-registry listauthz
// gate. It is what `make -C services/registry audit-list-filter` issues and what CI
// runs; the same analysis also travels with `go test ./services/registry/...`.
//
// Exit status is the verdict: 0 when every public List enforces per-object
// visibility, 1 when it does not, 2 when the tree could not be inspected at all —
// a gate that cannot read the code must not report success.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/services/registry/tools/auditlistfilter"
)

func main() {
	root := flag.String("root", ".", "path to the kacho-registry service root")
	flag.Parse()

	findings, err := auditlistfilter.Analyze(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit-list-filter: %v\n", err)
		os.Exit(2)
	}
	if len(findings) == 0 {
		fmt.Println("audit-list-filter: OK")
		return
	}

	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "audit-list-filter: %s\n", f)
	}
	fmt.Fprint(os.Stderr, "\n"+
		"Every public List must hand back only the objects its caller may see.\n"+
		"In kacho-registry that decision is made in the handler, in one of two shapes:\n"+
		"  - a page of separately-authorizable objects (registries, repositories,\n"+
		"    repo-scoped operations) is filtered row by row, and the response is built\n"+
		"    from the filtered slice;\n"+
		"  - a page that lives inside one object (tags, referrers) is settled by\n"+
		"    checking that object before the page is read.\n"+
		"Enumerating every allowed id is not a substitute: that enumeration is capped\n"+
		"server-side with no continuation token, so the caller's own objects fall\n"+
		"outside the cap and disappear.\n")
	os.Exit(1)
}
