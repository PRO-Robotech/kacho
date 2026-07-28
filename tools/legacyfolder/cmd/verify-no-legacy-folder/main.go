// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command verify-no-legacy-folder fails the build when the retired name of a
// tenant container appears anywhere in this repository.
//
// The verdict is the number of violations, never the fact that the command
// ran, and it is reported alongside how many files were inspected — a scan that
// covered nothing would otherwise exit zero and read as clean.
//
//	go run ./tools/legacyfolder/cmd/verify-no-legacy-folder [repo-root]
//
// The root defaults to the working directory, which is the repository root
// under the CI checkout.
package main

import (
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/tools/legacyfolder"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	findings, stats, err := legacyfolder.Scan(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-no-legacy-folder:", err)
		os.Exit(2)
	}
	// The lists are written in terms of this repository, so auditing them is
	// only meaningful against it. Pointed at a subtree, every entry would look
	// dangling and drown the answer to the question being asked.
	if legacyfolder.IsRepositoryRoot(root) {
		findings = append(findings, legacyfolder.AuditExemptions(root)...)
	}

	// A walk that reached no files is not a clean tree; it is a broken gate,
	// and the two must not share an exit status.
	if stats.Inspected == 0 {
		fmt.Fprintf(os.Stderr, "verify-no-legacy-folder: inspected 0 files under %q — nothing was checked\n", root)
		os.Exit(2)
	}

	if len(findings) == 0 {
		fmt.Printf("verify-no-legacy-folder: OK — inspected %d file(s); %d exempt; %d awaiting another pass\n",
			stats.Inspected, stats.Exempt, stats.Awaiting)
		return
	}

	fmt.Fprint(os.Stderr, legacyfolder.Report(findings, stats))
	fmt.Fprintf(os.Stderr, "\nverify-no-legacy-folder: %d violation(s)\n", len(findings))
	os.Exit(1)
}
