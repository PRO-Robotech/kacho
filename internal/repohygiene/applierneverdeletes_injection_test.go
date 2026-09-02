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
	return ` + "`DELETE FROM kacho_iam.roles WHERE cluster_id IS NOT NULL`" + `
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
