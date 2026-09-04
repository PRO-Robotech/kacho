// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция гейта «ответ операции над ролью не несёт вычисленного состояния».
//
// Вход НАСТОЯЩИЙ по форме и синтетический по содержанию: дерево собирается в
// t.TempDir(), поэтому вердикт не зависит ни от состояния репозитория, ни от
// порядка прогонов. Каждая ось двигает РОВНО ОДИН факт против положительного
// близнеца, и близнец стоит ПЕРВЫМ — иначе отрицание зеленело бы на
// анализаторе, находящем нарушение в любом дереве.

func roleOpStateInjOptions(t *testing.T, src string) RoleOperationResponseStateOptions {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "services", "iam", "internal", "role")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("каталог: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helpers.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("файл: %v", err)
	}
	// Проба рядом: не-тестовое дерево судится, тестовое — нет. Без неё гейт мог
	// бы находить нарушения в фикстурах и молчать о продукте.
	if err := os.WriteFile(filepath.Join(dir, "helpers_test.go"),
		[]byte(roleOpStateInjTestFile), 0o600); err != nil {
		t.Fatalf("файл пробы: %v", err)
	}
	return RoleOperationResponseStateOptions{
		Root:             root,
		ServiceRoot:      "services/iam",
		DomainPkg:        "domain",
		RoleType:         "Role",
		TransferFunc:     "Transfer",
		ProjectionMethod: "WithoutComputedState",
	}
}

// roleOpStateInjTestFile — ТЕСТОВЫЙ файл с заведомым нарушением. Гейт обязан его
// не видеть: он судит продукт.
const roleOpStateInjTestFile = `package role

func marshalRoleInFixture(r domain.Role) (*anypb.Any, error) {
	var dst *iamv1.Role
	if err := dto.Transfer(dto.FromTo(r, &dst)); err != nil {
		return nil, err
	}
	return anypb.New(dst)
}
`

// roleOpStateInjSound — ЗАКОННОЕ дерево. Рядом с верным переводчиком роли стоят
// четыре соседа, ни один из которых судиться не должен.
const roleOpStateInjSound = `package role

// marshalRole — переводчик роли: проекцию зовёт.
func marshalRole(r domain.Role) (*anypb.Any, error) {
	var dst *iamv1.Role
	if err := dto.Transfer(dto.FromTo(r.WithoutComputedState(), &dst)); err != nil {
		return nil, err
	}
	return anypb.New(dst)
}

// marshalGroup — переводчик ЧУЖОГО ресурса: та же форма, другой тип.
func marshalGroup(g domain.Group) (*anypb.Any, error) {
	var dst *iamv1.Group
	if err := dto.Transfer(dto.FromTo(g, &dst)); err != nil {
		return nil, err
	}
	return anypb.New(dst)
}

// doCreate — вызывающий, который перевод ДЕЛЕГИРУЕТ: роль принимает, ответ
// операции возвращает, но не переводит.
func (u *CreateRoleUseCase) doCreate(ctx context.Context, r domain.Role) (*anypb.Any, error) {
	return marshalRole(r)
}

// getRole — СИНХРОННОЕ чтение: состояние заполняет и обязано его нести.
func getRole(r domain.Role) (*iamv1.Role, error) {
	var dst *iamv1.Role
	if err := dto.Transfer(dto.FromTo(r, &dst)); err != nil {
		return nil, err
	}
	return dst, nil
}
`

func roleOpStateInjRun(t *testing.T, src string) ([]RoleOperationResponseStateFinding, RoleOperationResponseStateCensus, error) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditRoleOperationResponseState(roleOpStateInjOptions(t, src), &log)
	t.Log(strings.TrimSpace(log.String()))
	return f, c, err
}

// TestRoleOperationResponseStateInjection — способность падать и молчать.
func TestRoleOperationResponseStateInjection(t *testing.T) {
	// ОСЬ 0 (положительный контроль, первым). Законное дерево молчит, и перепись
	// доказывает, что обход не был пуст и что признак отобрал ОДИН переводчик из
	// пяти функций: без этого молчание было бы достижимо анализатором, который
	// не признаёт переводчиком ничего.
	f, c, err := roleOpStateInjRun(t, roleOpStateInjSound)
	if err != nil {
		t.Fatalf("законный близнец: анализатор не отработал: %v", err)
	}
	if len(f) != 0 {
		t.Fatalf("законный близнец: находок %d: %v", len(f), f)
	}
	if c.RoleTranslators != 1 {
		t.Fatalf("переводчиков роли %d, ожидался 1 — признак отбирает не то: чужой ресурс, "+
			"делегирующий вызывающий и синхронное чтение переводчиками не являются",
			c.RoleTranslators)
	}
	if c.AnypbFuncs != 3 {
		t.Fatalf("функций, возвращающих ответ операции, %d, ожидалось 3 — разбор результатов "+
			"разошёлся с деревом", c.AnypbFuncs)
	}
	if c.ProjectionCalled != 1 {
		t.Fatalf("зовущих проекцию %d, ожидался 1", c.ProjectionCalled)
	}

	// ОСЬ 1. Переводчик роли перестал звать проекцию — сторона, ради которой
	// гейт заведён. Находка обязана назвать КООРДИНАТУ.
	broken := strings.Replace(roleOpStateInjSound,
		"dto.FromTo(r.WithoutComputedState(), &dst)", "dto.FromTo(r, &dst)", 1)
	f, _, err = roleOpStateInjRun(t, broken)
	if err != nil {
		t.Fatalf("проекция снята: анализатор не отработал: %v", err)
	}
	if len(f) != 1 {
		t.Fatalf("проекция снята: находок %d, ожидалась 1: %v", len(f), f)
	}
	if !strings.Contains(f[0].String(), "helpers.go:") || !strings.Contains(f[0].Func, "marshalRole") {
		t.Fatalf("проекция снята: находка не называет координату и функцию: %s", f[0])
	}
	if f[0].Line == 0 {
		t.Fatal("проекция снята: находка без номера строки — координата неполна")
	}

	// ОСЬ 2. Проекция названа только в КОММЕНТАРИИ ВНУТРИ ТЕЛА — там, где её
	// увидел бы всякий разбор по тексту функции. Гейт читает узел вызова, и
	// сверх того разбирает исходник БЕЗ комментариев вовсе, поэтому зазеленеть
	// на собственном объяснении не может ни при какой правке разбора.
	//
	// Комментарий стоит В ТЕЛЕ намеренно: в шапке функции его не увидел бы и
	// разбор по тексту, и ось не отличала бы верный гейт от текстового.
	commented := strings.Replace(broken,
		"	var dst *iamv1.Role\n	if err := dto.Transfer(dto.FromTo(r, &dst)); err != nil {",
		"	// здесь полагалось бы звать WithoutComputedState(), но это ТЕКСТ, а не вызов\n"+
			"	var dst *iamv1.Role\n	if err := dto.Transfer(dto.FromTo(r, &dst)); err != nil {", 1)
	if commented == broken {
		t.Fatal("проекция в комментарии: фикстура не собралась — ось судит не то, что задумано")
	}
	f, _, err = roleOpStateInjRun(t, commented)
	if err != nil {
		t.Fatalf("проекция в комментарии: анализатор не отработал: %v", err)
	}
	if len(f) != 1 {
		t.Fatalf("проекция в комментарии: находок %d, ожидалась 1 — гейт зачёл упоминание "+
			"за вызов: %v", len(f), f)
	}

	// ОСЬ 3. Второй переводчик роли, в другом пакете и с приёмником. Он обязан
	// судиться наравне с первым, а координата — называть его однозначно:
	// `marshalRole` в дереве не одна.
	second := roleOpStateInjSound + `

func (rs *Resolver) marshalRole(role domain.Role) (*anypb.Any, error) {
	var dst *iamv1.Role
	if err := dto.Transfer(dto.FromTo(role, &dst)); err != nil {
		return nil, err
	}
	return anypb.New(dst)
}
`
	f, c, err = roleOpStateInjRun(t, second)
	if err != nil {
		t.Fatalf("второй переводчик: анализатор не отработал: %v", err)
	}
	if len(f) != 1 {
		t.Fatalf("второй переводчик: находок %d, ожидалась 1: %v", len(f), f)
	}
	if f[0].Func != "(*Resolver).marshalRole" {
		t.Fatalf("второй переводчик: находка названа %q — приёмник потерян, и координата "+
			"не различает двух одноимённых", f[0].Func)
	}
	if c.RoleTranslators != 2 {
		t.Fatalf("второй переводчик: переводчиков %d, ожидалось 2", c.RoleTranslators)
	}

	// ОСЬ 4. Нарушение в ТЕСТОВОМ файле (оно лежит там при каждом прогоне,
	// см. roleOpStateInjTestFile). Ось 0 выше уже прошла при нём — значит
	// тестовое дерево не судится. Здесь утверждаем это прямо, чтобы граница
	// была видна, а не подразумевалась.
	if c.Files != 1 {
		t.Fatalf("файлов прочитано %d, ожидался 1 — гейт читает тестовые файлы, и находки "+
			"в фикстурах станут неотличимы от находок в продукте", c.Files)
	}

	// ОСЬ 5. Пустой обход. Дерево без файлов обязано дать ОТКАЗ, а не «находок
	// ноль»: иначе «ноль находок» неотличимо от «прочитано ноль».
	empty := RoleOperationResponseStateOptions{
		Root: t.TempDir(), ServiceRoot: "services/iam",
		DomainPkg: "domain", RoleType: "Role",
		TransferFunc: "Transfer", ProjectionMethod: "WithoutComputedState",
	}
	if _, _, err = AuditRoleOperationResponseState(empty, nil); err == nil {
		t.Fatal("пустое дерево: анализатор отработал успехом — обход пуст, вердикт беспредметен")
	}

	// ОСЬ 6. Дерево БЕЗ переводчиков роли — обход не пуст, а предмет пуст.
	// «Находок ноль» здесь означает «признак разошёлся с деревом», и это тоже
	// отказ, а не успех.
	noRole := `package role

func marshalGroup(g domain.Group) (*anypb.Any, error) {
	var dst *iamv1.Group
	if err := dto.Transfer(dto.FromTo(g, &dst)); err != nil {
		return nil, err
	}
	return anypb.New(dst)
}
`
	f, c, err = roleOpStateInjRun(t, noRole)
	if err == nil {
		t.Fatalf("дерево без переводчиков роли: анализатор отработал успехом (находок %d) — "+
			"пустой предмет прочитан как «нарушений нет»", len(f))
	}
	if !strings.Contains(err.Error(), "переводчиков роли") {
		t.Fatalf("дерево без переводчиков роли: отказ не называет предмет: %v", err)
	}
	if c.Funcs == 0 {
		t.Fatal("дерево без переводчиков роли: разобрано ноль функций — отказ выше пришёл " +
			"от пустого обхода, а не от пустого предмета")
	}
}
