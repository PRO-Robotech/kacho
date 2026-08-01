// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command pagination-order-gate reports every list RPC whose page-format answer
// depends on the caller's access, or whose page size is coerced before it is judged.
//
// Exit codes: 0 clean, 1 findings, 2 the walk itself could not be trusted (parse
// failure, or nothing judged — see paginationordergate.Report.PremiseHolds).
package main

import (
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/tools/paginationordergate"
)

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"services", "gateway"}
	}
	rep, err := paginationordergate.Analyse(roots...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println(rep.Census())
	for _, f := range rep.Findings {
		fmt.Println("  " + f.String())
	}
	if bad := rep.PremiseFailures(); len(bad) > 0 {
		for _, p := range bad {
			fmt.Fprintln(os.Stderr, "premise no longer holds: "+p)
		}
		os.Exit(2)
	}
	if len(rep.Findings) > 0 {
		os.Exit(1)
	}
}
