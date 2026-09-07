// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// withdrawalproducerarriveswiththeapplier_test.go — гейт задачи #1913:
// применитель ролей модуля не приводится в действие, пока у отзыва роли нет
// производителя.
//
// Гейт — САМОИСТЕКАЮЩЕЕ послабление, а не запрет. Сегодня применитель в проде
// не вызывается, производителя отзыва нет, и это остаток: перечень расхождений
// не производится, потому что не производится ничего. Гейт молчит. В день,
// когда применение приезжает (`#1034`), отсутствие производителя перестаёт быть
// остатком и становится находкой — само, без чьей-либо памяти.
//
// Способность упасть и смолчать по каждой клетке таблицы согласия доказана
// инъекцией — `withdrawalproducerarriveswiththeapplier_injection_test.go`.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// withdrawalCensusFloor — прод-файлов Go сервиса iam, ниже которого обход
	// беспредметен. Замер на `lane/s3`: 518. Порог взят с запасом вниз — он
	// стережёт обвал обхода, а не рост дерева.
	withdrawalCensusFloor = 300
	// reconcileDeclFile — файл, объявляющий вид расхождения без исхода.
	reconcileDeclFile = "services/iam/internal/apps/kaname/moduleroles/reconcile.go"
	// liveNotDeclaredKind — сам вид. Предмет гейта — именно он: строка живёт,
	// объявления нет, снять нечем.
	liveNotDeclaredKind = "LiveNotDeclared"
)

// roleWithdrawalFinding — предикат находки. Тот же зовёт инъекция: проба,
// судящая своей копией предиката, доказывает свойство копии.
//
// Находка — НЕСОГЛАСИЕ: применитель приводится в действие, а производителя
// отзыва нет. Ни одна половина по отдельности находкой не является.
func roleWithdrawalFinding(drive, mark []RoleWithdrawalSite) bool {
	return len(drive) > 0 && len(mark) == 0
}

// withdrawalSiteLines — координаты для текста отказа.
func withdrawalSiteLines(sites []RoleWithdrawalSite) []string {
	out := make([]string, 0, len(sites))
	for _, s := range sites {
		out = append(out, fmt.Sprintf("%s:%d  %s", s.File, s.Line, s.What))
	}
	sort.Strings(out)
	return out
}

// TestWithdrawalProducerArrivesWithTheApplier — сам гейт.
func TestWithdrawalProducerArrivesWithTheApplier(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	rels := make([]string, 0, 512)
	declSeen := false
	for rel := range tt.files {
		if rel == reconcileDeclFile {
			declSeen = true
		}
		if !strings.HasPrefix(rel, iamGoPrefix) || !strings.HasSuffix(rel, ".go") {
			continue
		}
		// Тесты исключены НАМЕРЕННО, и исключение имеет предмет: пакет
		// применителя импортируют пять `_test.go`, и ни один из них применителя
		// в проде не приводит. Считать их значило бы объявить работу сделанной
		// пробами.
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed      int
		census      RoleWithdrawalCensus
		drive, mark []RoleWithdrawalSite
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		d, m, c, err := ScanRoleWithdrawalWiring(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		drive = append(drive, d...)
		mark = append(mark, m...)
		census.AppliedImports += c.AppliedImports
		census.Selectors += c.Selectors
		census.StringLiterals += c.StringLiterals
		census.Comments += c.Comments
		census.WritesOverRoles += c.WritesOverRoles
	}

	t.Logf("перепись: прод-файлов Go %s разобрано %d, обращений вида `пакет.Имя` %d, "+
		"импортов применителя %d, приведений применителя в действие %d, "+
		"строковых литералов %d, операторов записи над `roles` %d, "+
		"производителей отзыва %d, комментариев %d",
		iamGoPrefix, parsed, census.Selectors, census.AppliedImports, len(drive),
		census.StringLiterals, census.WritesOverRoles, len(mark), census.Comments)

	if parsed < withdrawalCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d прод-файлов при пороге %d — обход "+
			"читает не то дерево, и «ноль находок» здесь неотличимо от «ноль прочитанного»",
			parsed, withdrawalCensusFloor)
	}
	if census.Selectors == 0 || census.StringLiterals == 0 || census.Comments == 0 {
		t.Fatalf("прочитано обращений %d, литералов %d, комментариев %d — обе половины "+
			"гейта беспредметны, а вместе с ними и различение «код против прозы»",
			census.Selectors, census.StringLiterals, census.Comments)
	}

	// ПРЕДПОСЫЛКА ГЕЙТА. Он стережёт вид расхождения, у которого нет исхода.
	// Исчез вид — исчез предмет, и молчание было бы сказано ни о чём.
	if !declSeen {
		t.Fatalf("файла сверки %s в индексе нет — предмет гейта переехал либо снят",
			reconcileDeclFile)
	}
	decl, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(reconcileDeclFile)))
	if err != nil {
		t.Fatalf("чтение %s: %v", reconcileDeclFile, err)
	}
	if !strings.Contains(string(decl), liveNotDeclaredKind) {
		t.Fatalf("сверка больше не объявляет вид расхождения %s (%s) — предмет гейта "+
			"исчез. Это ОТКАЗ, а не чистота: проверка, которой нечего стеречь, "+
			"неотличима от проверки, ничего не нашедшей. Снимайте гейт вместе с видом",
			liveNotDeclaredKind, reconcileDeclFile)
	}

	if roleWithdrawalFinding(drive, mark) {
		t.Fatalf("применитель ролей модуля приводится в действие, а производителя отзыва "+
			"роли нет — %d приведение(й), производителей 0:\n  %s\n\n"+
			"Роль, объявленная манифестом и потом из него убранная, остаётся живой "+
			"НАВСЕГДА, и право, выданное через неё, продолжает действовать: сверка "+
			"объявляет вид %s, а исход у него не производится ничем. Единственный "+
			"производитель снятия системной роли в дереве — разовая миграция, то есть "+
			"выкатка образа iam, ровно то, ради устранения чего заведён манифест.\n"+
			"Сильнее того: правка, снимающая роль ВМЕСТЕ с её ресурсом, роняет ПУСК — "+
			"ключ role_rule_ref_res_fk объявлен ON UPDATE NO ACTION, а переселение "+
			"последствий сужено is_system = false намеренно, — и выхода правкой "+
			"манифеста нет: снятие роли из раздела строку не убирает. Путь старта роли "+
			"при этом НЕ сверяет (прод-вызывающих у moduleroles.Reconcile ноль), "+
			"поэтому одна лишь убранная роль пуск не роняет.\n"+
			"Форма отзыва ВЫБРАНА и удаления не допускает: строка помечается снятой — "+
			"services/iam/docs/engineering/architecture/role-withdrawal-is-a-mark.md. "+
			"Производитель — предмет задачи #1913, триггер — #1034.",
			len(drive), strings.Join(withdrawalSiteLines(drive), "\n  "), liveNotDeclaredKind)
	}
}
