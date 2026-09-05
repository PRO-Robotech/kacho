// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// systemrolerowisneverdeleted_injection_test.go — доказательство способности
// гейта упасть И смолчать.
//
// Инъекция снимает НОВОЕ свойство у элемента, чьё СТАРОЕ на месте: у настоящего
// оператора удаления дерева убирается сужение на пользовательскую роль. Форма
// «завести ещё один оператор» здесь запрещена — новый оператор нарушал бы всё,
// что требуется от операторов вообще, и красное пришло бы от соседа.
package repohygiene

import (
	"strings"
	"testing"
)

// roleDeleteGuardedSrc — оператор дерева, каков он есть (`role_repo.go:580`):
// сужен на пользовательскую роль, поэтому строку системной роли снять им нельзя.
const roleDeleteGuardedSrc = `package pg

// Delete — удаление пользовательской роли. Системную снять нельзя: сужение
// стоит в самом операторе, а не в проверке перед ним (ban #10).
func (w *roleWriter) Delete(ctx context.Context, id string) error {
	row := w.tx.QueryRow(ctx,
		` + "`DELETE FROM roles WHERE id = $1 AND is_system = false RETURNING 1`" + `, id)
	return row.Scan(new(int))
}
`

// roleDeleteUnguardedSrc — ТОТ ЖЕ оператор без сужения. Ровно это и делает
// отзыв роли модуля выразимым удалением строки.
const roleDeleteUnguardedSrc = `package pg

func (w *roleWriter) Delete(ctx context.Context, id string) error {
	row := w.tx.QueryRow(ctx,
		` + "`DELETE FROM roles WHERE id = $1 RETURNING 1`" + `, id)
	return row.Scan(new(int))
}
`

// roleDeleteTierGuardSrc — второе ЗАКОННОЕ написание того же сужения: системная
// роль опознаётся кластерным якорем, из которого `is_system` и вычисляется.
// Распознаватель, знающий одну форму, объявил бы верный код находкой.
const roleDeleteTierGuardSrc = `package pg

func (w *roleWriter) Delete(ctx context.Context, id string) error {
	row := w.tx.QueryRow(ctx,
		` + "`DELETE FROM kaname.roles WHERE id = $1 AND cluster_id IS NULL RETURNING 1`" + `, id)
	return row.Scan(new(int))
}
`

// roleDeleteProseSrc — законный близнец: тот же оператор словами — в
// комментарии и в тексте отказа. Гейт, судящий подстроку, краснел бы на
// собственном объяснении.
const roleDeleteProseSrc = `package pg

import "errors"

// Прод-код НИКОГДА не производит DELETE FROM roles без сужения на
// пользовательскую роль: отзыв роли модуля — пометка, а не удаление строки.
var errNoSystemRoleDelete = errors.New("DELETE FROM roles здесь не производится безусловно")
`

// roleDeleteProjectionSrc — законный близнец второй оси: удаляется ПРОЕКЦИЯ, а
// не роль. `ReplaceRuleRefs` начинается ровно с этого оператора, и запрещать его
// значило бы запретить приведение правила.
const roleDeleteProjectionSrc = `package pg

func (w *roleWriter) ReplaceRuleRefs(ctx context.Context, id string) error {
	_, err := w.tx.Exec(ctx, ` + "`DELETE FROM kaname.role_rule_ref WHERE role_id = $1`" + `, id)
	return err
}
`

// TestRoleDeleteGateStaysSilentOnTheGuardedStatement — КОНТРОЛЬ. Без него
// молчание гейта на инъекции было бы неотличимо от молчания мёртвого гейта.
func TestRoleDeleteGateStaysSilentOnTheGuardedStatement(t *testing.T) {
	const rel = "services/iam/internal/repo/kacho/pg/role_repo.go"

	sites, census, err := ScanRoleDeletes(rel, []byte(roleDeleteGuardedSrc))
	if err != nil {
		t.Fatalf("разбор контроля: %v", err)
	}
	if census.Statements != 1 {
		t.Fatalf("операторов удаления над `roles` прочитано %d из одного — обход не видит предмета",
			census.Statements)
	}
	if census.Guarded != 1 {
		t.Fatalf("сужённых операторов прочитано %d из одного — распознаватель не знает "+
			"формы, которой дерево пользуется сегодня", census.Guarded)
	}
	if f := roleDeleteFindings(sites); len(f) != 0 {
		t.Fatalf("КОНТРОЛЬ: сужённый оператор объявлен находкой: %v", f)
	}
}

// TestRoleDeleteGateRedsOnAnUnguardedStatement — инъекция обязана краснеть и
// НАЗЫВАТЬ координату: находка, называющая симптом, посылает читателя не туда.
func TestRoleDeleteGateRedsOnAnUnguardedStatement(t *testing.T) {
	const rel = "services/iam/internal/repo/kacho/pg/role_repo.go"

	sites, census, err := ScanRoleDeletes(rel, []byte(roleDeleteUnguardedSrc))
	if err != nil {
		t.Fatalf("разбор инъекции: %v", err)
	}
	if census.Statements != 1 {
		t.Fatalf("операторов удаления прочитано %d из одного", census.Statements)
	}
	if census.Guarded != 0 {
		t.Fatalf("несужённый оператор зачтён сужённым: сужено %d", census.Guarded)
	}
	f := roleDeleteFindings(sites)
	if len(f) != 1 {
		t.Fatalf("несужённое удаление роли НЕ стало находкой: находок %d\n"+
			"Пока оператор сужен на пользовательскую роль, строка системной роли "+
			"неудаляема by construction; как только не сужен — это перестаёт быть верным",
			len(f))
	}
	if !strings.Contains(f[0], rel) || !strings.Contains(f[0], "DELETE FROM roles") {
		t.Errorf("находка не называет ни координату, ни оператор: %q", f[0])
	}
}

// TestRoleDeleteGateKnowsBothGuardForms — вторая законная форма сужения.
func TestRoleDeleteGateKnowsBothGuardForms(t *testing.T) {
	sites, census, err := ScanRoleDeletes("services/iam/internal/repo/kacho/pg/role_repo.go",
		[]byte(roleDeleteTierGuardSrc))
	if err != nil {
		t.Fatalf("разбор близнеца: %v", err)
	}
	if census.Statements != 1 || census.Guarded != 1 {
		t.Fatalf("прочитано операторов %d, сужено %d — распознаватель знает не все "+
			"законные формы записи сужения, и всё, записанное второй, вне наблюдения",
			census.Statements, census.Guarded)
	}
	if f := roleDeleteFindings(sites); len(f) != 0 {
		t.Fatalf("кластерный якорь как форма сужения объявлен находкой: %v", f)
	}
}

// TestRoleDeleteGateStaysSilentOnProse — законный близнец: слово в комментарии и
// в тексте отказа.
func TestRoleDeleteGateStaysSilentOnProse(t *testing.T) {
	sites, census, err := ScanRoleDeletes("services/iam/internal/repo/kacho/pg/doc.go",
		[]byte(roleDeleteProseSrc))
	if err != nil {
		t.Fatalf("разбор близнеца: %v", err)
	}
	if census.Comments == 0 || census.StringLiterals == 0 {
		t.Fatalf("близнец беспредметен: комментариев %d, литералов %d",
			census.Comments, census.StringLiterals)
	}
	if census.Statements != 0 {
		t.Fatalf("проза о запрете прочитана как оператор: операторов %d", census.Statements)
	}
	if f := roleDeleteFindings(sites); len(f) != 0 {
		t.Fatalf("гейт судит текст, а не оператор: он краснел бы на собственном "+
			"объяснении: %v", f)
	}
}

// TestRoleDeleteGateStaysSilentOnProjectionDelete — законный близнец: удаляется
// проекция правила, а не строка роли.
func TestRoleDeleteGateStaysSilentOnProjectionDelete(t *testing.T) {
	sites, census, err := ScanRoleDeletes("services/iam/internal/repo/kacho/pg/role_repo.go",
		[]byte(roleDeleteProjectionSrc))
	if err != nil {
		t.Fatalf("разбор близнеца: %v", err)
	}
	if census.StringLiterals == 0 {
		t.Fatalf("близнец беспредметен: литералов прочитано ноль")
	}
	if census.Statements != 0 {
		t.Fatalf("удаление ПРОЕКЦИИ прочитано как удаление роли: операторов %d — "+
			"под такой образец подпадает `ReplaceRuleRefs`, то есть приведение правила",
			census.Statements)
	}
	if f := roleDeleteFindings(sites); len(f) != 0 {
		t.Fatalf("удаление проекции объявлено находкой: %v", f)
	}
}

// roleDeleteBareSrc — оператор БЕЗ условия вовсе. Самая опасная из представимых
// форм: сужать здесь нечего, и продолжение у неё — конец литерала. Шапка разбора
// обещает ловить именно её, поэтому обещание проверяется отдельным входом.
const roleDeleteBareSrc = `package pg

func (w *roleWriter) wipe(ctx context.Context) error {
	_, err := w.tx.Exec(ctx, ` + "`DELETE FROM roles`" + `)
	return err
}
`

// TestRoleDeleteGateRedsOnAnUnconditionalStatement — оператор без условия.
func TestRoleDeleteGateRedsOnAnUnconditionalStatement(t *testing.T) {
	sites, census, err := ScanRoleDeletes("services/iam/internal/repo/kacho/pg/role_repo.go",
		[]byte(roleDeleteBareSrc))
	if err != nil {
		t.Fatalf("разбор инъекции: %v", err)
	}
	if census.Statements != 1 {
		t.Fatalf("оператор без условия прочитан как НЕ оператор: операторов %d — "+
			"правило продолжения обещает ловить конец литерала и не ловит",
			census.Statements)
	}
	if f := roleDeleteFindings(sites); len(f) != 1 {
		t.Fatalf("безусловное удаление ролей НЕ стало находкой: находок %d", len(f))
	}
}
