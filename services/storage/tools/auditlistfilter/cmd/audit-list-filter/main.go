// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command audit-list-filter is the CI entry point of kacho-storage's public-List
// gate. What is checked lives in package auditlistfilter.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/services/storage/tools/auditlistfilter"
)

func main() {
	root := flag.String("root", ".", "service root to audit (the directory holding internal/…)")
	// --allow is gone. It excluded a whole RESOURCE, so the exclusion written for
	// disk_type would also have covered any further listing method added to that
	// use-case — including one nobody had considered. Exclusions are now declared
	// per METHOD in the analyser's `listings` table, carry their reason where the
	// gate can read it, and become a finding once the method is gone.
	flag.Parse()

	if _, err := auditlistfilter.Audit(auditlistfilter.Options{Root: *root}, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
