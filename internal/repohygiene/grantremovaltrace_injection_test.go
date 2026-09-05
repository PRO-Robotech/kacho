// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

// TestGrantRemovalTraceGateInjection — доказательство способности храповика
// упасть и смолчать (IAM-RM-1-12, приёмка §7.2).
//
// Вход подаётся СИНТЕТИКОЙ, а не правкой дерева: писать в индекс или в файлы
// репозитория, из которого запущена проба, запрещено. Граница отсюда следует и
// названа честно: инъекция не говорит НИЧЕГО о том, производит ли дерево предмет
// сегодня — это утверждает сам гейт своей переписью.
//
// Оси — каждая с обеими сторонами:
//
//	перебор     лишний файл без следа → находка С ИМЕНЕМ; без него → молчание
//	след        тот же файл СО следом → молчание (половина предиката различает)
//	половина    удаление в ОТКАТНОЙ половине → не считается (снимает свою вставку)
//	маска       оператор в комментарии и в литерале → не считается
//	терпимость  иное написание того же оператора → считается
//	недобор     текст находки требует сознательного движения храповика ВНИЗ
//
// # Синтетика РАЗМЕРЯЕТСЯ храповиком, а не числом 6
//
// Прежняя редакция строила базу из шести файлов — по числу прощённых на день
// заведения. Свод миграций iam (2026-09-04) опустил храповик до нуля: прощать
// стало нечего, файлов больше нет. База, выписанная числом, пережила бы свой
// предмет и краснела бы на исправном гейте — тот самый класс, который эта
// инъекция и стережёт. Поэтому база строится ПО КОНСТАНТЕ и следует за ней сама.
func TestGrantRemovalTraceGateInjection(t *testing.T) {
	// Прощённые сегодня — минимальные тела, несущие ровно предмет. Их РОВНО
	// столько, сколько объявляет храповик: при нуле база пуста, и все оси ниже
	// остаются осмысленными, потому что каждая считает ОТНОСИТЕЛЬНО базы.
	base := func() []grantMigrationSource {
		out := make([]grantMigrationSource, 0, grantRemovalRatchet)
		for i := 0; i < grantRemovalRatchet; i++ {
			out = append(out, grantMigrationSource{
				Name: fmt.Sprintf("00%02d_retire.sql", i),
				Body: "-- +goose Up\nDELETE FROM kaname.access_bindings WHERE role_id = 'r';\n" +
					"-- +goose Down\nSELECT 1;\n",
			})
		}
		return out
	}

	// КОНТРОЛЬ: корпус, каким гейт его застаёт, — молчание и непустая перепись.
	silent, c := auditGrantRemovalTrace(base())
	if len(silent) != grantRemovalRatchet {
		t.Fatalf("контроль: ожидалось %d прощённых, получено %d (%v)",
			grantRemovalRatchet, len(silent), silent)
	}
	// Перепись контроля сверяется с базой, а не с нулём: при храповике 0 база
	// пуста by construction, и требовать от неё непустоты значило бы требовать
	// прощённых там, где их нет.
	if c.FilesRead != grantRemovalRatchet || c.WithDelete != grantRemovalRatchet ||
		c.InUpHalf != grantRemovalRatchet {
		t.Fatalf("контроль: перепись (%+v) разошлась с базой в %d файлов — «ноль находок» "+
			"стало бы неотличимо от «ноль прочитанного»", c, grantRemovalRatchet)
	}

	// ОСЬ «перебор»: седьмой файл, снимающий выдачи без следа.
	over := append(base(), grantMigrationSource{
		Name: "0099_seventh.sql",
		Body: "-- +goose Up\nDELETE FROM kaname.access_bindings WHERE subject_id = 's';\n" +
			"-- +goose Down\nSELECT 1;\n",
	})
	got, _ := auditGrantRemovalTrace(over)
	if len(got) != grantRemovalRatchet+1 {
		t.Errorf("перебор: ожидалось %d, получено %d (%v)", grantRemovalRatchet+1, len(got), got)
	}
	msg := grantRemovalFinding(len(got), got)
	if !strings.Contains(msg, "0099_seventh.sql") {
		t.Errorf("перебор: находка не НАЗЫВАЕТ виновника: %s", msg)
	}
	if !strings.Contains(msg, "стало больше") {
		t.Errorf("перебор: находка не отличает перебор от недобора: %s", msg)
	}

	// ОСЬ «след»: ТОТ ЖЕ файл, но со следом в журнале, находкой не является.
	//
	// Это единственное доказательство того, что половина предиката «без записи в
	// журнал» вообще РАЗЛИЧАЕТ: на корпусе дерева она отсекает ноль файлов —
	// прежде потому, что след не оставляла ни одна миграция, теперь ещё и потому,
	// что удалений в корпусе нет вовсе. Ось не зависит от храповика намеренно:
	// она о предикате, а не о числе прощённых.
	traced := append(base(), grantMigrationSource{
		Name: "0099_seventh.sql",
		Body: "-- +goose Up\nDELETE FROM kaname.access_bindings WHERE subject_id = 's';\n" +
			"INSERT INTO kaname.audit_outbox (event_type) VALUES ('AccessBindingDeleted');\n" +
			"-- +goose Down\nSELECT 1;\n",
	})
	got, _ = auditGrantRemovalTrace(traced)
	if len(got) != grantRemovalRatchet {
		t.Errorf("след: удаление СО следом посчитано находкой — половина предиката "+
			"«без записи в журнал» не различает: %v", got)
	}

	// ОСЬ «недобор»: текст находки требует двигать храповик ВНИЗ сознательно.
	//
	// При храповике 0 эта ветвь из гейта НЕДОСТИЖИМА by construction: числом
	// прощённых меньше нуля не бывает. Ветвь не снимается, потому что храповик —
	// подвижная величина: подняв его, следующий получит нижнюю сторону обратно
	// вместе с её проверкой. Условие ниже самоистекает — оно начнёт гонять ось
	// через гейт в тот день, когда храповик поднимут.
	if grantRemovalRatchet > 0 {
		under := base()
		under[0].Body = "-- +goose Up\nDELETE FROM kaname.access_bindings WHERE role_id = 'r';\n" +
			"INSERT INTO kaname.audit_outbox (event_type) VALUES ('AccessBindingDeleted');\n" +
			"-- +goose Down\nSELECT 1;\n"
		got, _ = auditGrantRemovalTrace(under)
		if len(got) != grantRemovalRatchet-1 {
			t.Errorf("недобор: ожидалось %d, получено %d (%v)", grantRemovalRatchet-1, len(got), got)
		}
	}
	msg = grantRemovalFinding(grantRemovalRatchet-1, []string{"0000_retire.sql"})
	if !strings.Contains(msg, "стало меньше") || !strings.Contains(msg, "СОЗНАТЕЛЬНО") {
		t.Errorf("недобор: находка не требует сознательного движения храповика: %s", msg)
	}

	// ОСЬ «половина»: законный близнец — удаление в ОТКАТНОЙ половине. Такая
	// миграция снимает СВОЮ ЖЕ вставку, откатываясь, и доступа не отбирает.
	twin := append(base(), grantMigrationSource{
		Name: "0100_rollback_only.sql",
		Body: "-- +goose Up\nINSERT INTO kaname.access_bindings (id) VALUES ('b1');\n" +
			"-- +goose Down\nDELETE FROM kaname.access_bindings WHERE id = 'b1';\n",
	})
	got, ctwin := auditGrantRemovalTrace(twin)
	if len(got) != grantRemovalRatchet {
		t.Errorf("половина: откатное удаление посчитано снятием: %v", got)
	}
	if ctwin.WithDelete != grantRemovalRatchet+1 || ctwin.InUpHalf != grantRemovalRatchet {
		t.Errorf("половина: перепись не различает «где угодно» и «в накатной половине»: %+v", ctwin)
	}

	// ОСЬ «маска»: оператор, названный ПРОЗОЙ и ЛИТЕРАЛОМ, снятием не является.
	// Предикат, считающий собственное объяснение проверяемого, даёт число,
	// которого в дереве нет.
	prose := append(base(), grantMigrationSource{
		Name: "0101_prose.sql",
		Body: "-- +goose Up\n" +
			"-- Здесь НЕ делается DELETE FROM kaname.access_bindings — выдачи остаются.\n" +
			"SELECT 'DELETE FROM kaname.access_bindings' AS explanation;\n" +
			"-- +goose Down\nSELECT 1;\n",
	})
	got, cprose := auditGrantRemovalTrace(prose)
	if len(got) != grantRemovalRatchet {
		t.Errorf("маска: проза и литерал посчитаны оператором: %v", got)
	}
	if cprose.WithDelete != grantRemovalRatchet {
		t.Errorf("маска: перепись посчитала прозу удалением: %+v", cprose)
	}

	// ОСЬ «терпимость»: то же снятие, записанное иначе. Предикат, узнающий одну
	// запись из многих законных, МОЛЧИТ на остальных, и молчание читается как
	// факт о дереве.
	spelling := append(base(), grantMigrationSource{
		Name: "0102_other_spelling.sql",
		Body: "-- +goose Up\ndelete\n  from   kaname . access_bindings\n where id = 'x';\n" +
			"-- +goose Down\nSELECT 1;\n",
	})
	got, _ = auditGrantRemovalTrace(spelling)
	if len(got) != grantRemovalRatchet+1 {
		t.Errorf("терпимость: иное написание оператора не узнано: %v", got)
	}
}

// TestGrantRoleReassignmentDiscriminatorCutsBothWays — доказательство того, что
// разбор переноса судит ПРИСВАИВАНИЯ, а не весь оператор (IAM-RM-1-13).
//
// Законный близнец здесь несущий: `role_id` стоит в УСЛОВИИ ОТБОРА у каждого
// оператора мягкого отзыва, и предикат по всему тексту объявил бы переносом
// дедупликацию выдач. Первая редакция этого разбора так и сделала — одно
// попадание там, где переносов ноль.
func TestGrantRoleReassignmentDiscriminatorCutsBothWays(t *testing.T) {
	corpus := []grantMigrationSource{
		// ЛОВИТСЯ: role_id стоит среди присваиваний.
		{Name: "0200_move.sql", Body: "-- +goose Up\n" +
			"UPDATE kaname.access_bindings SET role_id = 'rol-new' WHERE role_id = 'rol-old';\n" +
			"-- +goose Down\nSELECT 1;\n"},
		// МОЛЧИТ: role_id только в условии отбора — это мягкий отзыв, а не перенос.
		{Name: "0201_soft_revoke.sql", Body: "-- +goose Up\n" +
			"UPDATE kaname.access_bindings\n   SET status = 'REVOKED', revoked_at = now()\n" +
			" WHERE role_id = 'rol-old' AND revoked_at IS NULL;\n" +
			"-- +goose Down\nSELECT 1;\n"},
		// МОЛЧИТ: перенос в ОТКАТНОЙ половине — миграция откатывает свою же правку.
		{Name: "0202_rollback_move.sql", Body: "-- +goose Up\nSELECT 1;\n" +
			"-- +goose Down\nUPDATE kaname.access_bindings SET role_id = 'rol-old';\n"},
		// МОЛЧИТ: перенос, названный ПРОЗОЙ и ЛИТЕРАЛОМ.
		{Name: "0203_prose.sql", Body: "-- +goose Up\n" +
			"-- Переносить нельзя: UPDATE kaname.access_bindings SET role_id = …\n" +
			"SELECT 'UPDATE kaname.access_bindings SET role_id = x' AS explanation;\n" +
			"-- +goose Down\nSELECT 1;\n"},
	}

	moves, statements := auditGrantRoleReassignment(corpus)
	if len(moves) != 1 || moves[0] != "0200_move.sql" {
		t.Errorf("разбор судит не присваивания: ожидалось [0200_move.sql], получено %v", moves)
	}
	// Перепись обязана видеть ОБА настоящих оператора — иначе молчание на
	// втором было бы молчанием ослепшего разбора, а не свойством входа.
	if statements != 2 {
		t.Errorf("перепись прочитала %d операторов вместо 2 — «ноль переносов» стало бы "+
			"неотличимо от «ноль прочитанного»", statements)
	}
}
