// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEveryIdentityAdapterVerbIsBounded — каждый вызов края к службе доступа
// через адаптер идёт с бюджетом ручки KACHO_API_GATEWAY_IDENTITY_CALL_BUDGET.
func TestEveryIdentityAdapterVerbIsBounded(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(repoRoot(t), IdentityAdapterFile))
	if err != nil {
		t.Fatalf("носитель адаптера %s не читается (%v) — предпосылка переписи исчезла", IdentityAdapterFile, err)
	}
	findings, c, err := FindUnboundedIdentityAdapterVerbs(string(src))
	if err != nil {
		t.Fatalf("носитель адаптера не разбирается: %v", err)
	}
	t.Logf("перепись: методов адаптера %d · вызовов соседа %d · из них с бюджетом %d", c.Methods, c.Verbs, c.Bounded)
	if c.Methods == 0 || c.Verbs == 0 {
		t.Fatalf("обход пуст (методов %d, вызовов %d) — «ноль находок» здесь означало бы «ноль прочитанного»", c.Methods, c.Verbs)
	}
	for _, f := range findings {
		t.Errorf("%s:%d (%s): вызов службы доступа без бюджета — возьмите контекст a.bounded(ctx) до вызова",
			IdentityAdapterFile, f.Line, f.Method)
	}
}
