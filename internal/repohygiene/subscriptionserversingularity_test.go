// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// subscriptionStreamAllowances — ведомость послаблений: частные потоковые
// глаголы, ещё не переведённые на общую форму.
//
// # ПЕРЕЧЕНЬ ПУСТ, и это ЦЕЛЬ, а не недосмотр
//
// Обе прежние формы подписки сняты со своими контрактами до начала этой фазы
// (kacho#813 и kacho#814, имена лежат в надгробии `retiredRPCSurface`). Запись
// под любую из них была бы истёкшей в момент внесения — исключать ей нечего, и
// гейт назвал бы её находкой, ровно как и должен.
//
// # Когда запись здесь появится
//
// Тогда и только тогда, когда домен заведёт свой потоковый глагол раньше, чем
// возьмёт общий. Запись обязана назвать причину и предикат истечения; снятие
// глагола из контракта делает её находкой само.
var subscriptionStreamAllowances []SubscriptionStreamAllowance

func subscriptionServerOptions(t *testing.T) SubscriptionServerOptions {
	t.Helper()
	return SubscriptionServerOptions{
		Root:      repoRoot(t),
		ProtoRoot: "proto",
		GoRoots:   []string{"pkg", "services", "gateway", "terraform", "internal", "cmd"},
		Allow:     subscriptionStreamAllowances,
	}
}

// TestSubscriptionServerIsSingularAndLivesInTheFoundation — вердикт о НАСТОЯЩЕМ
// дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`subscriptionserversingularity_injection_test.go`): здесь только вердикт.
func TestSubscriptionServerIsSingularAndLivesInTheFoundation(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditSubscriptionServerSingularity(subscriptionServerOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок» было бы
	// достижимо пустым обходом.
	if census.ProtoFiles < 20 || census.GoFiles < 500 {
		t.Fatalf("файлов контракта %d, файлов прод-кода %d — обход пуст, вердикт беспредметен",
			census.ProtoFiles, census.GoFiles)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("серверов потока подписки %d при ожидаемом одном, потоковых глаголов %d при ожидаемых %d:\n%s",
		census.ServerImpls, census.StreamRPCs, census.Expected, strings.Join(lines, "\n"))
}
