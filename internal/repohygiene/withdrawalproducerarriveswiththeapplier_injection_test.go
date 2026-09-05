// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// withdrawalproducerarriveswiththeapplier_injection_test.go — доказательство
// способности гейта упасть И смолчать по КАЖДОЙ клетке таблицы согласия.
//
// Гейт судит СОГЛАСИЕ двух фактов, поэтому односторонней инъекции ему мало:
// проба, подающая только «применитель приводится в действие», не отличила бы
// гейт, требующий согласия, от гейта, запрещающего приведение вообще. Клеток
// четыре, и красной обязана быть ровно одна.
package repohygiene

import (
	"strings"
	"testing"
)

// withdrawalDriveWired — прод-файл, приводящий применитель в действие. Импорт
// настоящий, вызов настоящий.
const withdrawalDriveWired = `package serve

import (
	"context"

	"github.com/PRO-Robotech/kacho-iam/internal/apps/kacho/moduleroles"
)

func wire(ctx context.Context, tx moduleroles.TxRunner) error {
	a := moduleroles.NewApplier(tx)
	_ = a
	return nil
}
`

// withdrawalDriveAliased — то же приведение под ПСЕВДОНИМОМ. Разбор обязан
// связывать импорт, а не сверять последний сегмент пути: псевдоним законен и
// молча увёл бы обход мимо настоящего вызова.
const withdrawalDriveAliased = `package serve

import (
	mr "github.com/PRO-Robotech/kacho-iam/internal/apps/kacho/moduleroles"
)

func wire(declared []mr.Role) {
	_, _ = mr.Reconcile("vpc", declared, nil)
}
`

// withdrawalDriveInProse — законный близнец приведения: имя пакета и точки
// входа стоят в КОММЕНТАРИИ, импорта нет. Это не гипотеза — так устроен
// `services/iam/internal/moduleroleparity/parity.go` в сегодняшнем дереве.
const withdrawalDriveInProse = `package moduleroleparity

// Паритет ярусов сверяется тем же путём, что и вызовом настоящего применителя
// (` + "`moduleroles.Applier`" + `) с писателем, который ничего не пишет:
// moduleroles.NewApplier и moduleroles.Reconcile здесь только названы.
const note = "moduleroles.Reconcile"
`

// withdrawalMarkWriter — производитель отзыва: пометка снятия В ПРИСВОЕНИИ.
const withdrawalMarkWriter = `package pg

func (w *roleWriter) WithdrawSystemRole(id string) error {
	_, err := w.tx.Exec(ctx, ` + "`UPDATE kaname.roles SET retired_at = now(), live = false WHERE id = $1 AND is_system`" + `, id)
	return err
}
`

// withdrawalMarkOnConflict — второе законное написание того же: пометка в
// присвоении разрешения конфликта. Распознаватель, знающий одно написание,
// объявил бы верный код отсутствующим производителем.
const withdrawalMarkOnConflict = `package pg

const upsert = ` + "`INSERT INTO roles (id, name, live) VALUES ($1,$2,true)\n  ON CONFLICT (id) DO UPDATE SET live = EXCLUDED.live, retired_at = NULL`" + `
`

// withdrawalMarkRead — законный близнец производителя: та же колонка ЧИТАЕТСЯ.
// Без этого близнеца «производитель найден» означало бы «в литерале встретилось
// слово live», то есть любое чтение живых ролей.
const withdrawalMarkRead = `package pg

const listLive = ` + "`UPDATE roles SET description = $2 WHERE id = $1 AND live = true RETURNING id`" + `
`

// scanWithdrawal — сведение половин по набору синтетических файлов. Тот же
// предикат зовёт сам гейт: инъекция обязана судить тем же, чем судит гейт,
// иначе она доказывает свойство своей копии.
func scanWithdrawal(t *testing.T, files map[string]string) (drive, mark []RoleWithdrawalSite, census RoleWithdrawalCensus) {
	t.Helper()
	for rel, src := range files {
		d, m, c, err := ScanRoleWithdrawalWiring(rel, []byte(src))
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		drive = append(drive, d...)
		mark = append(mark, m...)
		census.AppliedImports += c.AppliedImports
		census.Selectors += c.Selectors
		census.StringLiterals += c.StringLiterals
		census.Comments += c.Comments
		census.WritesOverRoles += c.WritesOverRoles
	}
	return drive, mark, census
}

// TestWithdrawalGateRedsOnlyWhenTheApplierIsDrivenWithoutAProducer — таблица
// согласия целиком: четыре клетки, красная ровно одна.
func TestWithdrawalGateRedsOnlyWhenTheApplierIsDrivenWithoutAProducer(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string
		redded bool
	}{
		{
			name:   "не приводится · производителя нет — сегодняшнее дерево, остаток",
			files:  map[string]string{"services/iam/internal/moduleroleparity/parity.go": withdrawalDriveInProse},
			redded: false,
		},
		{
			name: "приводится · производителя нет — НАХОДКА",
			files: map[string]string{
				"services/iam/cmd/iam/serve.go": withdrawalDriveWired,
			},
			redded: true,
		},
		{
			name: "приводится · производитель есть — норма",
			files: map[string]string{
				"services/iam/cmd/iam/serve.go":               withdrawalDriveWired,
				"services/iam/internal/repo/kacho/pg/role.go": withdrawalMarkWriter,
			},
			redded: false,
		},
		{
			name: "не приводится · производитель есть — норма",
			files: map[string]string{
				"services/iam/internal/repo/kacho/pg/role.go": withdrawalMarkWriter,
			},
			redded: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			drive, mark, census := scanWithdrawal(t, c.files)
			if census.StringLiterals == 0 && census.Selectors == 0 {
				t.Fatalf("клетка беспредметна: ни литералов, ни обращений не прочитано")
			}
			got := roleWithdrawalFinding(drive, mark)
			if got != c.redded {
				t.Fatalf("клетка «%s»: находка=%v, ожидалось %v (приведений %d, производителей %d)",
					c.name, got, c.redded, len(drive), len(mark))
			}
		})
	}
}

// TestWithdrawalGateBindsTheImportRatherThanTheLastPathSegment — псевдоним.
func TestWithdrawalGateBindsTheImportRatherThanTheLastPathSegment(t *testing.T) {
	drive, _, census := scanWithdrawal(t, map[string]string{
		"services/iam/cmd/iam/serve.go": withdrawalDriveAliased,
	})
	if census.AppliedImports != 1 {
		t.Fatalf("импортов пакета применителя прочитано %d из одного", census.AppliedImports)
	}
	if len(drive) != 1 || drive[0].What != "Reconcile" {
		t.Fatalf("приведение под псевдонимом НЕ прочитано: %v", drive)
	}
}

// TestWithdrawalGateReadsTheProseAsProse — законный близнец приведения, взятый
// из формы, которая в дереве ЕСТЬ.
func TestWithdrawalGateReadsTheProseAsProse(t *testing.T) {
	drive, _, census := scanWithdrawal(t, map[string]string{
		"services/iam/internal/moduleroleparity/parity.go": withdrawalDriveInProse,
	})
	if census.Comments == 0 {
		t.Fatalf("близнец беспредметен: комментариев прочитано ноль")
	}
	if census.AppliedImports != 0 {
		t.Fatalf("импортов прочитано %d — близнец обязан быть БЕЗ импорта", census.AppliedImports)
	}
	if len(drive) != 0 {
		t.Fatalf("гейт судит слово, а не узел разбора: проза о применителе объявлена "+
			"его приведением в действие: %v", drive)
	}
}

// TestWithdrawalGateKnowsBothWritingsOfTheMark — обе законные формы пометки.
func TestWithdrawalGateKnowsBothWritingsOfTheMark(t *testing.T) {
	for name, src := range map[string]string{
		"присвоение в UPDATE":      withdrawalMarkWriter,
		"присвоение в ON CONFLICT": withdrawalMarkOnConflict,
	} {
		_, mark, census := scanWithdrawal(t, map[string]string{"services/iam/internal/repo/kacho/pg/role.go": src})
		if census.WritesOverRoles == 0 {
			t.Fatalf("%s: операторов записи над `roles` прочитано ноль — форма беспредметна", name)
		}
		if len(mark) != 1 {
			t.Fatalf("%s: производитель отзыва НЕ прочитан (найдено %d) — распознаватель "+
				"знает не все законные написания, и всё записанное в незнакомом "+
				"остаётся вне наблюдения", name, len(mark))
		}
	}
}

// TestWithdrawalGateDoesNotMistakeAReadForAWrite — законный близнец
// производителя: та же колонка в УСЛОВИИ.
func TestWithdrawalGateDoesNotMistakeAReadForAWrite(t *testing.T) {
	_, mark, census := scanWithdrawal(t, map[string]string{
		"services/iam/internal/repo/kacho/pg/role.go": withdrawalMarkRead,
	})
	if census.WritesOverRoles != 1 {
		t.Fatalf("операторов записи над `roles` прочитано %d из одного — близнец беспредметен",
			census.WritesOverRoles)
	}
	if len(mark) != 0 {
		t.Fatalf("чтение живых ролей объявлено производителем отзыва: %v\n"+
			"Тогда гейт молчал бы всегда — производитель находился бы в любом списке "+
			"живых ролей, и согласие двух фактов перестало бы что-либо утверждать", mark)
	}
	// Вторая сторона того же близнеца: сам оператор записи прочитан, то есть
	// молчание пришло от РАЗЛИЧЕНИЯ, а не от того, что литерал вообще не разобран.
	if !strings.Contains(withdrawalMarkRead, "UPDATE roles SET") {
		t.Fatalf("близнец перестал нести оператор записи — он больше ничего не различает")
	}
}
