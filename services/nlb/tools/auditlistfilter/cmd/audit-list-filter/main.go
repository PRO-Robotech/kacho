// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command audit-list-filter is the CI entry point of kacho-nlb's public-List
// gate. What is checked lives in package listfiltergate; how this service is laid
// out lives in package auditlistfilter.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/pkg/listfiltergate"
	"github.com/PRO-Robotech/kacho/services/nlb/tools/auditlistfilter"
)

func main() {
	root := flag.String("root", ".", "service root to audit (the directory holding internal/…)")
	// EdgeGate declarations are verified against the proto options, because the
	// gateway catalog and the iam seed are both generated FROM the proto and held
	// byte-identical to it: reading either copy would only prove the gate agrees
	// with whichever it happened to open.
	protoRoot := flag.String("proto-root", "proto",
		"root of the proto tree, used to verify EdgeGate declarations against the RPC options")
	// --allow is gone. It excluded a whole RESOURCE, so an exclusion written for a
	// cluster catalog also covered every listing method later added to that handler
	// — including ones nobody had considered. Exclusions are now declared per METHOD
	// in the service's Profile.Listings, carry their reason where the gate can read
	// it, and are a finding once the method is gone.
	flag.Parse()

	_, err := listfiltergate.Audit(
		auditlistfilter.Profile,
		listfiltergate.Options{Root: *root, ProtoRoot: *protoRoot},
		os.Stdout,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
