// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verify-known-failing-subject is the CI entry point for the known-failing-subject
// gate. Usage: verify-known-failing-subject [repo-root] (default ".").
//
// Exit codes are three, not two, because "the gate found nothing" and "the gate read
// nothing" must not look alike:
//
//	0 — every declaration has a subject that still exists, still fails where a report
//	    says so, and names an open issue;
//	1 — findings, listed by coordinate;
//	2 — the gate could not do its job (nothing read).
//
// Whichever it is, the census is printed first: a verdict without the volume behind
// it is the same class of statement this gate exists to catch.
package main

import (
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/tools/knownfailingsubject"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	rep, err := knownfailingsubject.Scan(knownfailingsubject.Options{Root: root})
	knownfailingsubject.Print(rep, os.Stdout)

	if rep.Census.Docs == 0 {
		fmt.Fprintln(os.Stderr, "verify-known-failing-subject: read no results doc — "+
			"run it from the repository root")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify-known-failing-subject: %v\n", err)
		os.Exit(1)
	}
}
