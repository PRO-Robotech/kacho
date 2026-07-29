// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command verify-no-foreign-clouds fails the build when another cloud provider
// is named anywhere in this repository.
//
// The verdict is the number of violations, never the fact that the command ran:
// it prints each occurrence and exits non-zero when there is at least one.
//
//	go run ./tools/foreignclouds/cmd/verify-no-foreign-clouds [repo-root]
//
// The root defaults to the working directory, which is the repository root
// under the CI checkout.
package main

import (
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/tools/foreignclouds"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	findings, err := foreignclouds.Scan(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-no-foreign-clouds:", err)
		os.Exit(2)
	}
	// The exemption list is written in terms of this repository, so auditing it
	// is only meaningful against this repository. Pointed at a subtree or at
	// somebody else's directory, every entry would look dangling and drown the
	// answer to the only question being asked.
	if foreignclouds.IsRepositoryRoot(root) {
		findings = append(findings, foreignclouds.AuditExemptions(root)...)
	}

	// ЗАЯВЛЕННЫЙ ДОЛГ печатается ВСЕГДА, до вердикта. Двухбуквенное сокращение
	// провайдера гейт намеренно не роняет (одним изменением его не убрать), но
	// молчание об этом делало «OK» неотличимым от чистого дерева: по выводу нельзя
	// было узнать, что запрет нарушен, причём местами оформлен как ЦЕЛЬ. Число не
	// меняет вердикт — оно не даёт долгу стать невидимым.
	if debt, derr := foreignclouds.ScanDebt(root); derr == nil && debt.Occurrences > 0 {
		fmt.Printf("verify-no-foreign-clouds: declared debt — %d occurrence(s) of the "+
			"provider abbreviation in %d file(s); not a violation by this gate, and not clean either\n",
			debt.Occurrences, debt.Files)
		for _, f := range debt.TopFiles {
			fmt.Printf("  · %s\n", f)
		}
	}

	if len(findings) == 0 {
		fmt.Println("verify-no-foreign-clouds: OK (no violation of the token list; see the declared debt above)")
		return
	}

	fmt.Fprint(os.Stderr, foreignclouds.Report(findings))
	fmt.Fprintf(os.Stderr, "\nverify-no-foreign-clouds: %d violation(s)\n", len(findings))
	os.Exit(1)
}
