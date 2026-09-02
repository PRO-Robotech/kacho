// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
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
//	перебор     седьмой файл без следа → находка С ИМЕНЕМ; без него → молчание
//	недобор     у одного из шести дописан след → находка с числом 5
//	половина    удаление в ОТКАТНОЙ половине → не считается (снимает свою вставку)
//	маска       оператор в комментарии и в литерале → не считается
//	терпимость  иное написание того же оператора → считается
func TestGrantRemovalTraceGateInjection(t *testing.T) {
	// Шесть прощённых сегодня — минимальные тела, несущие ровно предмет.
	base := func() []grantMigrationSource {
		var out []grantMigrationSource
		for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
			out = append(out, grantMigrationSource{
				Name: "00" + n + "_retire.sql",
				Body: "-- +goose Up\nDELETE FROM kacho_iam.access_bindings WHERE role_id = 'r';\n" +
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
	if c.FilesRead == 0 || c.WithDelete == 0 || c.InUpHalf == 0 {
		t.Fatalf("контроль: перепись пуста (%+v) — «ноль находок» стало бы "+
			"неотличимо от «ноль прочитанного»", c)
	}

	// ОСЬ «перебор»: седьмой файл, снимающий выдачи без следа.
	over := append(base(), grantMigrationSource{
		Name: "0099_seventh.sql",
		Body: "-- +goose Up\nDELETE FROM kacho_iam.access_bindings WHERE subject_id = 's';\n" +
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

	// ОСЬ «недобор»: у одного из шести дописан след. Это единственное
	// доказательство того, что половина предиката «без записи в журнал» вообще
	// различает: на корпусе дерева она сегодня отсекает ноль файлов.
	under := base()
	under[0].Body = "-- +goose Up\nDELETE FROM kacho_iam.access_bindings WHERE role_id = 'r';\n" +
		"INSERT INTO kacho_iam.audit_outbox (event_type) VALUES ('AccessBindingDeleted');\n" +
		"-- +goose Down\nSELECT 1;\n"
	got, _ = auditGrantRemovalTrace(under)
	if len(got) != grantRemovalRatchet-1 {
		t.Errorf("недобор: ожидалось %d, получено %d (%v)", grantRemovalRatchet-1, len(got), got)
	}
	msg = grantRemovalFinding(len(got), got)
	if !strings.Contains(msg, "стало меньше") || !strings.Contains(msg, "СОЗНАТЕЛЬНО") {
		t.Errorf("недобор: находка не требует сознательного движения храповика: %s", msg)
	}

	// ОСЬ «половина»: законный близнец — удаление в ОТКАТНОЙ половине. Такая
	// миграция снимает СВОЮ ЖЕ вставку, откатываясь, и доступа не отбирает.
	twin := append(base(), grantMigrationSource{
		Name: "0100_rollback_only.sql",
		Body: "-- +goose Up\nINSERT INTO kacho_iam.access_bindings (id) VALUES ('b1');\n" +
			"-- +goose Down\nDELETE FROM kacho_iam.access_bindings WHERE id = 'b1';\n",
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
			"-- Здесь НЕ делается DELETE FROM kacho_iam.access_bindings — выдачи остаются.\n" +
			"SELECT 'DELETE FROM kacho_iam.access_bindings' AS explanation;\n" +
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
		Body: "-- +goose Up\ndelete\n  from   kacho_iam . access_bindings\n where id = 'x';\n" +
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
			"UPDATE kacho_iam.access_bindings SET role_id = 'rol-new' WHERE role_id = 'rol-old';\n" +
			"-- +goose Down\nSELECT 1;\n"},
		// МОЛЧИТ: role_id только в условии отбора — это мягкий отзыв, а не перенос.
		{Name: "0201_soft_revoke.sql", Body: "-- +goose Up\n" +
			"UPDATE kacho_iam.access_bindings\n   SET status = 'REVOKED', revoked_at = now()\n" +
			" WHERE role_id = 'rol-old' AND revoked_at IS NULL;\n" +
			"-- +goose Down\nSELECT 1;\n"},
		// МОЛЧИТ: перенос в ОТКАТНОЙ половине — миграция откатывает свою же правку.
		{Name: "0202_rollback_move.sql", Body: "-- +goose Up\nSELECT 1;\n" +
			"-- +goose Down\nUPDATE kacho_iam.access_bindings SET role_id = 'rol-old';\n"},
		// МОЛЧИТ: перенос, названный ПРОЗОЙ и ЛИТЕРАЛОМ.
		{Name: "0203_prose.sql", Body: "-- +goose Up\n" +
			"-- Переносить нельзя: UPDATE kacho_iam.access_bindings SET role_id = …\n" +
			"SELECT 'UPDATE kacho_iam.access_bindings SET role_id = x' AS explanation;\n" +
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
