// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package manifest

// ПРОМЕЖУТОЧНОЕ СОСТОЯНИЕ TDD, НЕ КОММИТИТСЯ.
const (
	CheckOK     = 0
	CheckFailed = 1
	CheckVoid   = 2
)

type CheckReport struct {
	ManifestsRead int
	PathsSeen     int
	Paths         []string
	Findings      []string
}

func (r CheckReport) ExitCode() int { return CheckOK }
func (r CheckReport) Summary() string { return "" }

func CheckTree(root string) CheckReport { return CheckReport{} }
