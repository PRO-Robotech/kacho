// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// settledWatermarkAllowances — ведомость послаблений: наблюдатели границы
// устоявшегося, законно живущие вне фундамента.
//
// # ПЕРЕЧЕНЬ ПУСТ, и это ЦЕЛЬ, а не недосмотр
//
// Пустая ведомость означает, что техника живёт в единственном экземпляре и в
// фундаменте — то есть ровно то состояние, ради которого гейт заведён. Запись,
// которой больше нечего исключать, делает находкой сама себя: анализатор
// проверяет это отдельной половиной, поэтому забытое послабление не переживёт
// свой предмет молча.
var settledWatermarkAllowances []SettledWatermarkAllowance

func settledWatermarkOptions(t *testing.T) SettledWatermarkOptions {
	t.Helper()
	return SettledWatermarkOptions{
		Root:    repoRoot(t),
		GoRoots: []string{"pkg", "services", "gateway", "terraform", "internal", "cmd"},
		Allow:   settledWatermarkAllowances,
	}
}

// TestSettledWatermarkObserverIsSingularAndLivesInTheFoundation — вердикт о
// НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`settledwatermarksingularity_injection_test.go`): здесь только вердикт.
func TestSettledWatermarkObserverIsSingularAndLivesInTheFoundation(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditSettledWatermarkSingularity(settledWatermarkOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок» было бы
	// достижимо пустым обходом.
	if census.GoFiles < 500 || census.Literals < 5000 {
		t.Fatalf("файлов прод-кода %d, строковых литералов %d — обход пуст, вердикт беспредметен",
			census.GoFiles, census.Literals)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("наблюдателей границы устоявшегося %d при ожидаемом одном (в %s — %d):\n%s",
		census.Observers, SettledWatermarkHome, census.InHome, strings.Join(lines, "\n"))
}
