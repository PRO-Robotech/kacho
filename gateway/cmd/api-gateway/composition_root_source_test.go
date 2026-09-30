// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// compositionRoot — исходник композиционного корня.
//
// main() из пробы не исполним (он дозванивается до бэкендов и занимает порты),
// поэтому провязка утверждается там, где она живёт, — в исходнике корня. Чтение
// исходника слабее исполнения и применяется намеренно ровно к свойствам,
// которых «оно собирается» показать не может: что построенное доехало до
// своего потребителя.
func compositionRoot(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("main.go")
	require.NoError(t, err, "composition root must be readable")
	return string(b)
}
