// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// a8f60d_profile_stands_alone_test.go — пины образов продукта в профиле
// управляемого кластера собраны с ревизии, которая ЕСТЬ в истории этого дерева
// (kacho#3052, п.3).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ЕЩЁ ОДНА ПРОВЕРКА ПИНОВ
//
// Соседние проверки судят пин по одной оси каждая, и ни одна — по этой:
//
//	managed_cluster_profile_test.go             тег РАВЕН выводу из записи
//	                                            «порождено-от» — запись и тег
//	                                            согласны друг с другом;
//	published_image_pin_is_reachable_test.go    манифест с тегом в реестре ЕСТЬ
//	                                            (сеть — по ручке);
//	outsourced_image_pin_agrees_with_module…    тег образа службы доступа равен
//	                                            пину её модуля в go.mod;
//	verifier_capacity_floor_test.go             ёмкость и предел памяти службы
//	                                            доступа — не ниже пола пина.
//
// Запись «порождено-от» может называть коммит, которого в истории дерева НЕТ
// (другая ветка, переписанная история, опечатка в сорока знаках, совпавшая с
// чужим объектом): тогда тег выведен верно, образ тянется — а выкатка ставит код,
// которого это дерево не содержит. Проверка требует, чтобы записанный коммит был
// ПРЕДКОМ рабочего дерева.
//
// ГРАНИЦА, НАЗВАННАЯ ЧЕСТНО: свежесть пина эта проверка не судит и судить не
// может. Изменение не способно запинить образ собственного коммита (образа ещё
// нет), поэтому запись всегда не новее базы изменения; какой коммит развёрнут —
// решение выкатки (шапка профиля). Что стенд после выкатки исполняет ожидаемую
// ревизию, судит провенанс стенда, а не дерево.
//
// Решение вынесено чистой функцией `judgeRecordedRevision`; инъекции (запись не
// разрешается, запись не предок, записи нет) подаются ей вместе с НАСТОЯЩЕЙ
// записью профиля — TestManagedProfileRecordJudge_Injections.
package deploy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// recordedRevision — запись «порождено-от»: ветка и полный коммит.
type recordedRevision struct{ branch, sha string }

func managedProfileRecord(t *testing.T) (recordedRevision, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(umbrellaDir, managedProfile))
	if err != nil {
		t.Fatalf("профиль %s не читается (%v) — предпосылка проверки исчезла", managedProfile, err)
	}
	m := derivedFromRe.FindStringSubmatch(string(raw))
	if m == nil {
		return recordedRevision{}, false
	}
	return recordedRevision{branch: m[1], sha: m[2]}, true
}

// isAncestorOfHead — записанный коммит в истории рабочего дерева.
func isAncestorOfHead(sha string) bool {
	return gitenv.Command("", "merge-base", "--is-ancestor", sha, "HEAD").Run() == nil
}

// judgeRecordedRevision — находки о записи профиля при известных фактах истории.
func judgeRecordedRevision(rec recordedRevision, present, resolves, ancestor bool) []string {
	switch {
	case !present:
		return []string{managedProfile + " не несёт записи «# порождено-от: <ветка> <коммит>» — " +
			"пины не из чего выводить и не с чем сверять"}
	case !resolves:
		return []string{managedProfile + ": записанный коммит " + rec.sha + " (" + rec.branch + ") в этом " +
			"дереве не разрешается — образы собраны с кода, которого здесь нет"}
	case !ancestor:
		return []string{managedProfile + ": записанный коммит " + rec.sha + " (" + rec.branch + ") не предок " +
			"рабочего дерева — выкатка ставит код, которого это дерево не содержит"}
	}
	return nil
}

func TestManagedProfilePinsComeFromAnAncestorOfTheTree(t *testing.T) {
	if !commitResolves(headSHA(t)) {
		t.Fatal("история дерева недоступна (усечённый клон?) — предок не устанавливается, и это " +
			"«не выполнилось», а не «пины в порядке»")
	}
	rec, present := managedProfileRecord(t)
	resolves := present && commitResolves(rec.sha)
	ancestor := resolves && isAncestorOfHead(rec.sha)
	t.Logf("перепись: профиль %s, запись %s %s, разрешается=%t, предок HEAD=%t",
		managedProfile, rec.branch, rec.sha, resolves, ancestor)
	for _, f := range judgeRecordedRevision(rec, present, resolves, ancestor) {
		t.Error(f)
	}
}

func TestManagedProfileRecordJudge_Injections(t *testing.T) {
	rec, present := managedProfileRecord(t)
	if !present {
		t.Fatalf("у %s нет записи — настоящего входа для инъекций нет", managedProfile)
	}
	cases := []struct {
		name                      string
		present, resolves, ancest bool
		red                       bool
	}{
		{"законный близнец: запись разрешается и предок", true, true, true, false},
		{"инъекция: коммит записи не предок HEAD", true, true, false, true},
		{"инъекция: коммит записи не разрешается", true, false, false, true},
		{"инъекция: записи нет", false, false, false, true},
	}
	for _, c := range cases {
		got := judgeRecordedRevision(rec, c.present, c.resolves, c.ancest)
		if (len(got) > 0) != c.red {
			t.Errorf("%s: ждали красный=%t, получили %v", c.name, c.red, got)
		}
	}
	// Ось «не разрешается» — на настоящем входе git: запись с испорченным
	// коммитом обязана не разрешаться, настоящая — разрешаться.
	if !commitResolves(rec.sha) {
		t.Errorf("настоящая запись %s не разрешается — близнец не законен", rec.sha)
	}
	if bad := "0000000000000000000000000000000000000000"; commitResolves(bad) {
		t.Errorf("коммит %s разрешается — ось «не разрешается» не различает", bad)
	}
}
