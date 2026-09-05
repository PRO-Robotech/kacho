// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// applierneverdeletes_injection_test.go — доказательство способности Г3 упасть
// И смолчать (приёмка §7).
//
// Инъекция снимает НОВОЕ свойство у элемента, чьё СТАРОЕ на месте: удаляющий
// глагол добавляется в уже существующий порт, а не заводится новый порт целиком.
// Форма «завести ещё один элемент» здесь запрещена — новый порт нарушал бы всё,
// что требуется от портов вообще, и красное пришло бы от соседа.
package repohygiene

import (
	"strings"
	"testing"
)

// applierPortClean — порт применителя, каков он есть: удаляющего глагола нет.
const applierPortClean = `package moduleroles

import "context"

// RoleWriter — то, что применителю нужно от писателя.
type RoleWriter interface {
	UpsertSystemRole(ctx context.Context, r Role) (Role, bool, error)
	ReplaceRuleRefs(ctx context.Context, id string, refs []Ref) error
}
`

// applierPortWithDelete — тот же порт ПЛЮС удаляющий глагол. Ось 1.
const applierPortWithDelete = `package moduleroles

import "context"

type RoleWriter interface {
	UpsertSystemRole(ctx context.Context, r Role) (Role, bool, error)
	ReplaceRuleRefs(ctx context.Context, id string, refs []Ref) error
	DeleteSystemRole(ctx context.Context, id string) error
}
`

// applierSQLDelete — оператор удаления строковым литералом. Ось 2.
const applierSQLDelete = `package moduleroles

func sweep() string {
	return ` + "`DELETE FROM kaname.roles WHERE cluster_id IS NOT NULL`" + `
}
`

// applierDeleteInProse — то же слово в КОММЕНТАРИИ и в тексте отказа: законный
// близнец. Пакет объясняет сам запрет, и гейт, судящий подстроку, краснел бы на
// собственном объяснении.
const applierDeleteInProse = `package moduleroles

import "errors"

// Применитель НИКОГДА не производит DELETE FROM roles: роль с выдачами удалить
// нельзя, а каскад унёс бы проекции молча.
var errNoDelete = errors.New("moduleroles: DELETE FROM roles здесь не производится")
`

// TestApplierDeleteGateRedsOnAPortVerb — ось 1: инъекция обязана краснеть.
func TestApplierDeleteGateRedsOnAPortVerb(t *testing.T) {
	const rel = applierPackageDir + "apply.go"

	base, census, err := ScanApplierDeletes(rel, []byte(applierPortClean))
	if err != nil {
		t.Fatalf("разбор контроля: %v", err)
	}
	if census.InterfaceMethods != 2 {
		t.Fatalf("прочитано %d методов порта из двух — обход не видит предмета", census.InterfaceMethods)
	}
	if f := applierDeleteFindings(base); len(f) != 0 {
		t.Fatalf("КОНТРОЛЬ: чистый порт объявлен находкой: %v", f)
	}

	hit, census, err := ScanApplierDeletes(rel, []byte(applierPortWithDelete))
	if err != nil {
		t.Fatalf("разбор инъекции: %v", err)
	}
	if census.InterfaceMethods != 3 {
		t.Fatalf("прочитано %d методов порта из трёх", census.InterfaceMethods)
	}
	f := applierDeleteFindings(hit)
	if len(f) != 1 {
		t.Fatalf("удаляющий глагол порта НЕ стал находкой: находок %d\n"+
			"Пока порт его не несёт, применитель не может удалить ничего by construction; "+
			"как только несёт — это перестаёт быть верным", len(f))
	}
	if !strings.Contains(f[0], "DeleteSystemRole") || !strings.Contains(f[0], "port-verb") {
		t.Errorf("находка не называет глагол и ось: %q", f[0])
	}
}

// TestApplierDeleteGateRedsOnASQLLiteral — ось 2.
func TestApplierDeleteGateRedsOnASQLLiteral(t *testing.T) {
	sites, census, err := ScanApplierDeletes(applierPackageDir+"sweep.go", []byte(applierSQLDelete))
	if err != nil {
		t.Fatalf("разбор инъекции: %v", err)
	}
	if census.StringLiterals == 0 {
		t.Fatalf("прочитано ноль строковых литералов — обход не видит предмета")
	}
	f := applierDeleteFindings(sites)
	if len(f) != 1 {
		t.Fatalf("оператор удаления в литерале НЕ стал находкой: находок %d", len(f))
	}
	if !strings.Contains(f[0], "sql-literal") {
		t.Errorf("находка не называет ось: %q", f[0])
	}
}

// TestApplierDeleteGateStaysSilentOnProse — законный близнец обеих осей.
func TestApplierDeleteGateStaysSilentOnProse(t *testing.T) {
	sites, census, err := ScanApplierDeletes(applierPackageDir+"doc.go", []byte(applierDeleteInProse))
	if err != nil {
		t.Fatalf("разбор близнеца: %v", err)
	}
	if census.Comments == 0 {
		t.Fatalf("близнец беспредметен: комментариев прочитано ноль")
	}
	if f := applierDeleteFindings(sites); len(f) != 0 {
		t.Fatalf("гейт судит текст, а не узел разбора: проза о запрете стала находкой — "+
			"он краснел бы на собственном объяснении: %v", f)
	}
	// Отдельная сторона того же близнеца: слово стоит и в ТЕКСТЕ ОТКАЗА, то есть
	// внутри строкового литерала, — но это не оператор над таблицей ролей.
	if census.StringLiterals == 0 {
		t.Fatalf("в близнеце ноль литералов — вторая половина близнеца беспредметна")
	}
}

// applierPortWithRetireMark — тот же порт ПЛЮС глагол ПОМЕТКИ снятия. Законный
// близнец оси 1, и он не гипотетический: форму отзыва роли модуля выбрало
// решение `role-withdrawal-is-a-mark.md` — строка ПОМЕЧАЕТСЯ снятой, а не
// удаляется. `RetireSystemRole` есть `UPDATE`; удаления в нём нет ни в одной
// ветке, и ось, судящая СЛОВО, краснела бы на верном коде.
const applierPortWithRetireMark = `package moduleroles

import "context"

type RoleWriter interface {
	UpsertSystemRole(ctx context.Context, r Role) (Role, bool, error)
	ReplaceRuleRefs(ctx context.Context, id string, refs []Ref) error
	RetireSystemRole(ctx context.Context, id string, reason string) error
}
`

// applierAdapterRetireDeletes — реализация того же глагола, которая СТРОКУ
// УДАЛЯЕТ. Вторая сторона того же близнеца: гейт применителя её не видит by
// construction (адаптер лежит вне пакета применителя), и держать её обязан
// сосед — разбор `ScanRoleDeletes` по всему прод-дереву iam.
const applierAdapterRetireDeletes = `package pg

func (w *roleWriter) RetireSystemRole(id string) error {
	_, err := w.tx.Exec(ctx, ` + "`DELETE FROM kaname.roles WHERE id = $1`" + `, id)
	return err
}
`

// applierAdapterRetireMarks — законный близнец предыдущего: тот же глагол,
// пометкой. Без него «находка» неотличима от гейта, краснеющего на всём.
const applierAdapterRetireMarks = `package pg

func (w *roleWriter) RetireSystemRole(id string) error {
	_, err := w.tx.Exec(ctx, ` + "`UPDATE kaname.roles SET retired_at = now() WHERE id = $1`" + `, id)
	return err
}
`

// TestApplierDeleteGateStaysSilentOnTheWithdrawalMarkVerb — законный близнец
// оси 1: глагол ПОМЕТКИ снятия находкой не является.
//
// Токен `retire` в этом дереве ДВУЗНАЧЕН, и обе стороны измерены: три таблицы
// каталога снимают строку пометкой `retired_at` и её при этом НЕ удаляют
// (`20260901113757`), а десять миграций с именем `_retire*` строку удаляют.
// Ось, судящая слово, ошибается на нём в обе стороны сразу — и вторую сторону
// (`WithdrawSystemRole`, который удаляет) она не ловила никогда.
func TestApplierDeleteGateStaysSilentOnTheWithdrawalMarkVerb(t *testing.T) {
	const rel = applierPackageDir + "apply.go"

	// Контроль: чистый порт молчит. Без него молчание на инъекции неотличимо от
	// молчания разбора, который вообще ничего не читает.
	base, census, err := ScanApplierDeletes(rel, []byte(applierPortClean))
	if err != nil {
		t.Fatalf("разбор контроля: %v", err)
	}
	if census.InterfaceMethods != 2 {
		t.Fatalf("прочитано %d методов порта из двух — обход не видит предмета", census.InterfaceMethods)
	}
	if f := applierDeleteFindings(base); len(f) != 0 {
		t.Fatalf("КОНТРОЛЬ: чистый порт объявлен находкой: %v", f)
	}

	sites, census, err := ScanApplierDeletes(rel, []byte(applierPortWithRetireMark))
	if err != nil {
		t.Fatalf("разбор близнеца: %v", err)
	}
	if census.InterfaceMethods != 3 {
		t.Fatalf("прочитано %d методов порта из трёх — близнец беспредметен", census.InterfaceMethods)
	}
	if f := applierDeleteFindings(sites); len(f) != 0 {
		t.Fatalf("ось судит СЛОВО, а не то, что код делает: глагол пометки снятия стал "+
			"находкой — %v\n"+
			"Форма отзыва роли модуля выбрана решением "+
			"services/iam/docs/engineering/architecture/role-withdrawal-is-a-mark.md: строка "+
			"ПОМЕЧАЕТСЯ снятой. `RetireSystemRole` — это `UPDATE`, и гейт, краснеющий на "+
			"верном коде, отключают первым.", f)
	}

	// Ось 1 не выхолощена: однозначный удаляющий глагол по-прежнему находка.
	hit, _, err := ScanApplierDeletes(rel, []byte(applierPortWithDelete))
	if err != nil {
		t.Fatalf("разбор инъекции: %v", err)
	}
	if f := applierDeleteFindings(hit); len(f) != 1 {
		t.Fatalf("ось 1 выхолощена вместе с двузначным токеном: `DeleteSystemRole` "+
			"перестал быть находкой (находок %d)", len(f))
	}
}

// TestTheDeletingImplementationIsHeldWhateverTheVerbIsCalled — вторая половина
// предиката #1923: сузив перечень оси 1, надо ПОКАЗАТЬ, а не предположить, что
// удаляющую реализацию держит сосед.
//
// Разбор `ScanRoleDeletes` обходит ВСЁ прод-дерево Go сервиса iam
// (`systemrolerowisneverdeleted_test.go`), поэтому имя глагола ему безразлично:
// он судит оператор. Именно этого ось 1 не умела никогда — `WithdrawSystemRole`
// с удалением внутри под её перечень не подпадал.
func TestTheDeletingImplementationIsHeldWhateverTheVerbIsCalled(t *testing.T) {
	const rel = "services/iam/internal/repo/kacho/pg/role_repo.go"

	sites, census, err := ScanRoleDeletes(rel, []byte(applierAdapterRetireDeletes))
	if err != nil {
		t.Fatalf("разбор инъекции: %v", err)
	}
	if census.Statements != 1 {
		t.Fatalf("прочитано %d операторов удаления над `roles` из одного — инъекция "+
			"беспредметна", census.Statements)
	}
	if f := roleDeleteFindings(sites); len(f) != 1 {
		t.Fatalf("удаляющая реализация глагола пометки НЕ стала находкой у соседа "+
			"(находок %d) — значит, сузив ось 1, мы открыли дыру, а не сняли ложное "+
			"срабатывание", len(f))
	}

	// Законный близнец: тот же глагол, но пометкой. Молчание обязательно —
	// иначе «находка» выше означала бы гейт, краснеющий на всём подряд.
	twin, census, err := ScanRoleDeletes(rel, []byte(applierAdapterRetireMarks))
	if err != nil {
		t.Fatalf("разбор близнеца: %v", err)
	}
	if census.StringLiterals == 0 {
		t.Fatalf("в близнеце ноль литералов — близнец беспредметен")
	}
	if f := roleDeleteFindings(twin); len(f) != 0 {
		t.Fatalf("пометка снятия объявлена удалением: %v", f)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// #1926: ОСЬ 1 ЧИТАЕТ ПРЕДМЕТ ГЛАГОЛА, А НЕ ТОЛЬКО ГЛАГОЛ
//
// Удаление ПРОЕКЦИИ правила — штатная работа применителя: `ReplaceRuleRefs`
// заменяет проекцию объявленных сегментов ПОЛНОСТЬЮ, то есть снимает лишние
// строки. Ось 2 это разрешает явно (её образец привязан к таблице `roles`, и
// `role_rule_refs` под него не подпадает by construction). Ось 1 разрешала это
// СЛУЧАЙНО — только потому, что метод назван `Replace`, а не `Remove`.

// applierPortWithProjectionRemoval — тот же порт, где проекция снимается
// глаголом `Remove`. Законный близнец оси 1: код НИЧЕГО не нарушает.
const applierPortWithProjectionRemoval = `package moduleroles

import "context"

type RoleWriter interface {
	UpsertSystemRole(ctx context.Context, r Role) (Role, bool, error)
	RemoveRuleRefs(ctx context.Context, id string, refs []Ref) error
}
`

// applierPortWithRolesPurge — удаление СТРОКИ РОЛИ другим глаголом и во
// множественном числе. Ось 1 обязана краснеть: предмет тот же.
const applierPortWithRolesPurge = `package moduleroles

import "context"

type RoleWriter interface {
	UpsertSystemRole(ctx context.Context, r Role) (Role, bool, error)
	ReplaceRuleRefs(ctx context.Context, id string, refs []Ref) error
	PurgeRoles(ctx context.Context, module string) error
}
`

// applierPortWithUnnamedSubject — удаляющий глагол, НЕ НАЗВАВШИЙ предмета.
// Предмет неизвестен, поэтому вердикт fail-closed: находка.
const applierPortWithUnnamedSubject = `package moduleroles

import "context"

type RoleWriter interface {
	UpsertSystemRole(ctx context.Context, r Role) (Role, bool, error)
	Purge(ctx context.Context) error
}
`

// applierPortWithRoleProjections — удаление ДВУХ проекций правила, обе с
// корнем `Role` в имени. Близнец, отделяющий «предмет — строка роли» от
// «предмет — что-то РОЛИ принадлежащее»: `role_verbs` и `role_rule_selectors`
// суть проекции, и их снятие законно ровно так же, как снятие сегментов.
const applierPortWithRoleProjections = `package moduleroles

import "context"

type RoleWriter interface {
	UpsertSystemRole(ctx context.Context, r Role) (Role, bool, error)
	DropRoleVerbs(ctx context.Context, id string) error
	DeleteRoleRuleSelectors(ctx context.Context, id string) error
}
`

// TestApplierDeleteGateReadsTheSubjectOfTheVerbNotOnlyTheVerb — #1926.
//
// Пять прогонов ОДНОГО предиката, и каждый снимает своё:
//
//	законный близнец   `RemoveRuleRefs`          — молчание (проекция)
//	законный близнец   `DropRoleVerbs`+`Delete…` — молчание (проекции с корнем `Role`)
//	инъекция           `PurgeRoles`              — находка (строка роли, другой глагол)
//	инъекция           `Purge`                   — находка (предмет не назван, fail-closed)
//	контроль           `DeleteSystemRole`        — находка (ось не выхолощена)
func TestApplierDeleteGateReadsTheSubjectOfTheVerbNotOnlyTheVerb(t *testing.T) {
	const rel = applierPackageDir + "apply.go"

	scan := func(t *testing.T, what string, src string, wantMethods int) []string {
		t.Helper()
		sites, census, err := ScanApplierDeletes(rel, []byte(src))
		if err != nil {
			t.Fatalf("разбор %s: %v", what, err)
		}
		if census.InterfaceMethods != wantMethods {
			t.Fatalf("%s: прочитано %d методов порта из %d — вход беспредметен",
				what, census.InterfaceMethods, wantMethods)
		}
		return applierDeleteFindings(sites)
	}

	// ── Законный близнец 1: снятие ПРОЕКЦИИ сегментов удаляющим глаголом ─────
	if f := scan(t, "близнец `RemoveRuleRefs`", applierPortWithProjectionRemoval, 2); len(f) != 0 {
		t.Errorf("ось 1 читает глагол, но не его ПРЕДМЕТ: снятие проекции правила "+
			"объявлено находкой — %v\n"+
			"Удаление проекции есть штатная работа применителя: `ReplaceRuleRefs` заменяет "+
			"проекцию объявленных сегментов ПОЛНОСТЬЮ, то есть снимает лишние строки, и ось 2 "+
			"это разрешает явно. Переименование того же метода делало бы гейт красным на коде, "+
			"который ничего не нарушает, — а гейт, краснеющий на верном коде, отключают "+
			"первым (#1926)", f)
	}

	// ── Законный близнец 2: проекции, чьё имя НЕСЁТ корень `Role` ────────────
	if f := scan(t, "близнец `DropRoleVerbs`", applierPortWithRoleProjections, 3); len(f) != 0 {
		t.Errorf("предмет опознан по КОРНЮ, а не по существительному: проекции роли "+
			"(`role_verbs`, `role_rule_selectors`) объявлены строкой роли — %v", f)
	}

	// ── Инъекция 1: строка роли, ДРУГОЙ глагол, множественное число ──────────
	f := scan(t, "инъекция `PurgeRoles`", applierPortWithRolesPurge, 3)
	if len(f) != 1 {
		t.Fatalf("удаление строки роли НЕ стало находкой (находок %d): предмет тот же, "+
			"меняется только глагол", len(f))
	}
	if !strings.Contains(f[0], "PurgeRoles") || !strings.Contains(f[0], "port-verb") {
		t.Errorf("находка не называет глагол и ось: %q", f[0])
	}

	// ── Инъекция 2: предмет НЕ НАЗВАН — вердикт fail-closed ──────────────────
	f = scan(t, "инъекция `Purge`", applierPortWithUnnamedSubject, 2)
	if len(f) != 1 {
		t.Fatalf("удаляющий глагол без предмета НЕ стал находкой (находок %d): предмет "+
			"неизвестен, и догадка в разрешающую сторону здесь запрещена — по имени нельзя "+
			"установить, что именно метод удаляет", len(f))
	}

	// ── Контроль: ось не выхолощена ─────────────────────────────────────────
	if f := scan(t, "контроль `DeleteSystemRole`", applierPortWithDelete, 3); len(f) != 1 {
		t.Fatalf("ось 1 выхолощена вместе с чтением предмета: `DeleteSystemRole` перестал "+
			"быть находкой (находок %d)", len(f))
	}
}
