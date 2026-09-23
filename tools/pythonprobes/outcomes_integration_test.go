//go:build ci_integration

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pythonprobes

import (
	"testing"
	"time"
)

// These real Helm/Make/workflow paths belong to the mandatory integration lane.
// The ordinary unit invocation remains bounded and cannot enter them recursively.
// A missing declared tagged run is a failure of the existing reach/selection gates.
func TestPythonOutcomesChain(t *testing.T) {
	pythonOutcomes(t, "chain", 4, 55*time.Minute)
}

func TestPythonOutcomesCallers(t *testing.T) {
	pythonOutcomes(t, "callers", 1, 22*time.Minute)
}
