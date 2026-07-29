// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command audit-known-failing is the CI entry point of the known-failing gate.
// What it checks — and why an exclusion must expire by itself — is documented on
// package auditknownfailing.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/services/storage/tools/auditknownfailing"
)

func main() {
	root := flag.String("root", "tests/newman", "newman suite root (holds cases/, collections/, docs/)")
	repo := flag.String("repo", "PRO-Robotech/kacho", "tracker repository the annotations refer to")
	flag.Parse()

	if _, err := auditknownfailing.Audit(auditknownfailing.Options{Root: *root, Repo: *repo}, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
