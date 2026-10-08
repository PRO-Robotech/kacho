// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command audit-list-filter is the entry point of notify's public-List gate. What
// is checked and how notify is laid out live in package auditlistfilter.
//
// Exit codes: 0 — judged, no findings; 1 — findings; 2 — the tree could not be
// inspected (a gate that opened nothing proved nothing, and says so differently).
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/services/notify/tools/auditlistfilter"
)

func main() {
	root := flag.String("root", ".", "service root to audit (the directory holding internal/ and cmd/)")
	flag.Parse()
	_, err := auditlistfilter.Audit(auditlistfilter.Profile, auditlistfilter.Options{ServiceRoot: *root}, os.Stdout)
	switch {
	case err == nil:
	case errors.Is(err, auditlistfilter.ErrNotInspected):
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
