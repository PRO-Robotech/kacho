// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// TestProtoCommentsNameContainersThatExist — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_vpc_tenancylevel_injection_test.go`): здесь только вердикт.
func TestProtoCommentsNameContainersThatExist(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditTenancyLevels(TenancyLevelOptions{
		Tree:      clientTruthRepoTree(t),
		ProtoRoot: "proto",
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.ProtoFiles < 20 {
		t.Fatalf("файлов контракта %d — обход пуст, вердикт беспредметен", census.ProtoFiles)
	}
	if census.Fields < 100 {
		t.Fatalf("объявленных полей %d — следы искать негде", census.Fields)
	}
	// Вторая половина вердикта выносится ТОЛЬКО об утверждениях. Ноль означал бы,
	// что она не вынесена ни разу, — «находок ноль» получено даром.
	if census.Claims == 0 {
		t.Fatalf("утверждений о контейнере распознано 0 — сверка не состоялась")
	}
	// След обязан находиться у подавляющего большинства: анализатор, у которого
	// след не нашёлся НИ РАЗУ, судит не то, что думает.
	if census.Traced == 0 {
		t.Fatalf("утверждений со следом 0 из %d — поиск следа не работает", census.Claims)
	}
	// Составное имя контейнера обязано РАСПОЗНАВАТЬСЯ. Ноль здесь означает, что
	// захват именной группы выродился обратно в одно слово — и тогда «PARENT LOAD
	// BALANCER» снова читается как несуществующий `PARENT`, а трёхсловное имя не
	// видно вовсе. Проверяется числом, а не комментарием: расширение, ставшее
	// холостым, иначе неотличимо от работающего.
	if census.MultiWord == 0 {
		t.Fatalf("составных имён контейнера 0 из %d утверждений — захват именной группы "+
			"выродился в одно слово", census.Claims)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("контракт называет контейнер, которого в дереве нет "+
		"(утверждений %d, со следом %d):\n%s\n\n"+
		"Иерархию аренды и область уникальности имени клиент планирует по справочнику "+
		"контрактов, а переделка после запуска — это миграция всех имён и всех выдач. "+
		"Либо назовите существующий контейнер, либо заведите названный по-настоящему "+
		"(поле `<контейнер>_id`) — тогда анализатор замолчит сам.",
		census.Claims, census.Traced, strings.Join(lines, "\n"))
}
