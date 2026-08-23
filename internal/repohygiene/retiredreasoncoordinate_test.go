// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// TestRetiredRPCReasons_NameOnlyLiveCoordinates — WATCH-1-20 на НАСТОЯЩЕМ дереве:
// координата, названная причиной надгробия, обязана резолвиться.
//
// Свойство держится не памятью автора причины, а этим прогоном: причина пишется
// один раз, а метод, который она называет живым, снимает потом кто-то другой и
// в другом изменении.
func TestRetiredRPCReasons_NameOnlyLiveCoordinates(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditRetiredReasonCoordinates(RetiredReasonOptions{
		Root:    repoRoot(t),
		APIRoot: "pkg/api",
		Retired: retiredRPCSurface,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Иначе «ноль находок» получено
	// на пустом обходе.
	if census.Entries < 10 || census.DeclaredMethods < 100 {
		t.Fatalf("записей надгробия %d, методов из стабов %d — обход пуст, вердикт беспредметен",
			census.Entries, census.DeclaredMethods)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("%d причин надгробия называют координату, которой в дереве нет:\n%s",
		len(findings), strings.Join(lines, "\n"))
}

// TestRetiredRPCSurface_LifecycleFeedNamesAreBuried — WATCH-1-19.
//
// Форма фида жизненного цикла ресурсов была объявлена в трёх доменах. Два имени
// легли в надгробие вместе со снятием объявлений; третье — реализованное — сняли
// ОТДЕЛЬНОЙ задачей (kacho#814), и в надгробие оно не попало.
//
// Незарезервированное имя возвращается молча, и вернётся оно именно под замысел
// «а нам нужна подписка» — то есть под предмет, ради которого единая форма и
// заводится. Проба — замок: снять запись можно только осознанно.
func TestRetiredRPCSurface_LifecycleFeedNamesAreBuried(t *testing.T) {
	buried := map[string]bool{}
	for _, r := range retiredRPCSurface {
		buried[r.FQN] = true
	}
	for _, want := range []string{
		"kacho.cloud.compute.v1.InternalResourceLifecycleService/Subscribe",
		"kacho.cloud.vpc.v1.InternalResourceLifecycleService/Subscribe",
		"kacho.cloud.loadbalancer.v1.InternalResourceLifecycleService/Subscribe",
	} {
		if !buried[want] {
			t.Errorf("имя снятой формы фида жизненного цикла не зарезервировано надгробием: %s", want)
		}
	}
}
