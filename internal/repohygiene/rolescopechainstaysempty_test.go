// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// roleScopeChainPrefixes — где ищутся производители звена.
//
// Миграции — потому что именно они объявляют ветви; прод-исходники Go — потому
// что оператор может приехать литералом. Пробы исключены НАМЕРЕННО и с
// предметом: фикстура пробы законно строит любую цепь, и считать её
// производителем значило бы краснеть на собственной инъекции.
var roleScopeChainPrefixes = []string{"services/iam/"}

// TestRoleScopeChainOfAModuleRoleStaysEmpty — решение «меточную ось сужать не
// надо» ИСТЕКАЕТ в день, когда у `iam_role` появляется третий производитель
// звена цепи областей.
//
// # Почему гейт зелен сегодня и обязан покраснеть завтра
//
// Ветвей две, и обе законны: роль аккаунта берёт звено у `account_id`, роль
// проекта — у `project_id`. Роль МОДУЛЯ кластерного яруса не попадает ни в одну,
// поэтому цепь у неё пуста, и меточная выдача её не достаёт — на этом факте
// целиком стоит §2.8 приёмки.
//
// Заведёт кто-нибудь третью ветвь — факт перестанет быть верным, а доступ
// вернётся МОЛЧА. Этот гейт и есть то единственное, что об этом скажет.
func TestRoleScopeChainOfAModuleRoleStaysEmpty(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	rels := make([]string, 0, 256)
	for rel := range tt.files {
		if !roleScopeChainWatched(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		filesRead int
		census    RoleScopeChainCensus
		found     []RoleScopeChainSite
	)
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		filesRead++
		f, c := ScanRoleScopeChain(rel, string(b))
		found = append(found, f...)
		census.Statements += c.Statements
		census.Branches += c.Branches
		census.TierSourced += c.TierSourced
	}

	t.Logf("перепись: файлов осмотрено %d, упоминаний таблицы звеньев %d, "+
		"ветвей, производящих звено для %s — %d, из них взявших его у ярусного столбца %d",
		filesRead, census.Statements, roleScopeChainType, census.Branches, census.TierSourced)

	// Предпосылка гейта проверяется, а не предполагается. «Ноль находок» при
	// нулевом обходе неотличимо от исправной работы, и молчание такого гейта
	// означало бы «не читали», а не «чисто».
	if filesRead == 0 {
		t.Fatal("осмотрено ноль файлов: обход беспредметен, и его молчание ничего не значит")
	}
	if census.Branches == 0 {
		t.Fatalf("ветвей, производящих звено для %s, не найдено НИ ОДНОЙ — распознаватель "+
			"потерял предмет: сегодня их две (роль аккаунта и роль проекта), и обе "+
			"обязаны быть видны. Пустой перечень здесь означает, что форма ветви "+
			"изменилась, а не что дерево чисто", roleScopeChainType)
	}

	for _, s := range found {
		t.Errorf(`%s:%d — заведён производитель звена цепи областей для %s, не берущий его
    ни у %s: %s

    ЧТО ЭТО ЗНАЧИТ. Решение приёмки role-withdrawal-has-a-producer.md §2.8 —
    «меточную ось выдачи сужать по живости роли НЕ НАДО» — стояло на факте: у
    роли МОДУЛЯ цепь областей пуста, поэтому выдача её не достаёт ни живую, ни
    снятую. С этим производителем факт перестал быть верным, и снятая роль снова
    может стать достижимой — МОЛЧА.

    ЧТО ЗАКРЫВАТЬ. Цепью объекта гейтятся ВСЕ ТРИ АРМА выдачи — якорь, имена и
    метки, — а не только меточный. Сузив один, вы не сузите две трети и решите,
    что закрыли: закрывать надо по всем трём, и проба обязана утверждать каждый
    отдельно.

    ЕСЛИ ЭТО ЗАКОННО. Ветвь, берущая звено у ярусного столбца роли (%s), законна
    и молчания гейта не нарушает: роль аккаунта и роль проекта предка обязаны
    иметь. Судить по имени типа нельзя — у %s есть и такие роли.`,
			s.File, s.Line, roleScopeChainType,
			strings.Join(roleScopeChainTierSources, " ни у "), s.What,
			strings.Join(roleScopeChainTierSources, ", "), roleScopeChainType)
	}
}

// roleScopeChainWatched — попадает ли путь под наблюдение.
func roleScopeChainWatched(rel string) bool {
	if strings.HasSuffix(rel, "_test.go") {
		return false
	}
	if !strings.HasSuffix(rel, ".sql") && !strings.HasSuffix(rel, ".go") {
		return false
	}
	for _, p := range roleScopeChainPrefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}
