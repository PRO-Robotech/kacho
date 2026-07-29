// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command audit-list-filter is the CI entry point of the public-List gate.
// The behaviour it drives — and why it parses rather than greps — is documented
// on package auditlistfilter.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/PRO-Robotech/kacho/services/storage/tools/auditlistfilter"
)

// allowFlag collects repeated --allow=<resource> values.
type allowFlag []string

func (a *allowFlag) String() string { return strings.Join(*a, ",") }

func (a *allowFlag) Set(v string) error {
	*a = append(*a, v)
	return nil
}

func main() {
	var allow allowFlag
	root := flag.String("root", ".", "service root to audit (the directory holding internal/…)")
	flag.Var(&allow, "allow", "resource excluded from the checks (repeatable); an entry matching no resource is a finding")
	flag.Parse()

	if _, err := auditlistfilter.Audit(auditlistfilter.Options{Root: *root, Allow: allow}, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
