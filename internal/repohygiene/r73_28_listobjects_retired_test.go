// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// r73_28_listobjects_retired_test.go — сценарий R7-3-28: публичная поверхность
// перечисления снята ВМЕСТЕ со своей записью каталога, маршрутом края и записью
// разрешённых методов.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОТДЕЛЬНАЯ ПРОБА, КОГДА РЯДОМ ЕСТЬ ОБЩАЯ ВЕДОМОСТЬ СНЯТОГО
//
// Общая ведомость (`TestRetiredRPCSurface_NoRetiredNameCameBack`) держит СВОЙСТВО
// дерева на все 28 снятых имён сразу и не несёт имени сценария. Трассировка
// приёмки требует обратного: у каждого идентификатора сценария есть проба, чьё имя
// его несёт, и проба с именем без сценария — тоже находка.
//
// Поэтому здесь не заводится второй разбор: проба зовёт ТОТ ЖЕ анализатор, сужая
// его вход до имён этой стадии. Второй разбор разошёлся бы с первым молча — и
// разошёлся бы там, где расхождение не видно, потому что оба отвечают «находок
// ноль» на исправном дереве.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
// Запись каталога БЕЗ метода — не безобидный остаток: она даёт отказ в правах на
// путь, которого нет, то есть отвечает «доступа нет» там, где верный ответ «такого
// метода не существует». Маршрут края без метода — то же самое одним слоем выше.

import (
	"io"
	"strings"
	"testing"
)

// r73RetiredEnumerationSurface — имена, снятые ЭТОЙ стадией.
//
// Перечень ВЫВОДИТСЯ из общей ведомости по префиксу пакета и по составу службы, а
// не выписывается: выписанный рядом второй перечень не сдвинулся бы от следующего
// снятия и продолжал бы сторожить прежние имена.
func r73RetiredEnumerationSurface() []RetiredRPC {
	var out []RetiredRPC
	for _, r := range retiredRPCSurface {
		switch {
		case strings.HasSuffix(r.FQN, "AuthorizeService/ListObjects"),
			strings.Contains(r.FQN, "InternalAuthorizeService/"):
			out = append(out, r)
		}
	}
	return out
}

// TestR7_3_28_RetiredEnumerationSurfaceIsGoneEverywhere — R7-3-28.
func TestR7_3_28_RetiredEnumerationSurfaceIsGoneEverywhere(t *testing.T) {
	retired := r73RetiredEnumerationSurface()
	if len(retired) == 0 {
		t.Fatal("из общей ведомости не выведено НИ ОДНОГО имени этой стадии — " +
			"предикат отбора разошёлся с ведомостью, и проба утверждала бы о пустоте")
	}
	t.Logf("имён стадии в ведомости: %d", len(retired))

	findings, census, err := AuditRetiredRPCSurface(RetiredRPCSurfaceOptions{
		Root:      repoRoot(t),
		APIRoot:   "pkg/api",
		ProtoRoot: "proto",
		CatalogPaths: []string{
			"gateway/internal/middleware/embed/permission_catalog.json",
			"services/iam/internal/apps/kaname/seed/embedded/permission_catalog.json",
		},
		Retired: retired,
	}, io.Discard)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	t.Logf("осмотрено: стабов %d файлов (%d служб, %d методов); контракта %d файлов (%d служб); "+
		"каталога %d копий (%d строк)",
		census.StubFiles, census.DeclaredSvcs, census.DeclaredMethods,
		census.ProtoFiles, census.ProtoSvcs, census.CatalogFiles, census.CatalogRows)

	// Предпосылка: анализатор что-то прочитал. Ноль прочитанных копий каталога
	// снаружи неотличим от «записей нет» — и это ровно тот класс, который ловит
	// сама проба.
	if census.CatalogFiles == 0 || census.CatalogRows == 0 {
		t.Fatal("прочитано ноль строк каталога прав — «записи каталога нет» здесь " +
			"означало бы «каталог не читали»")
	}
	if census.DeclaredMethods == 0 {
		t.Fatal("в стабах не разобрано ни одного метода — разбор не дошёл до контракта")
	}

	for _, f := range findings {
		t.Errorf("%s\nПеречисление объектов и служба администрирования хранилища сняты "+
			"стадией S6 эпика #747. Остаток на любой из трёх поверхностей — находка: запись "+
			"каталога без метода даёт отказ В ПРАВАХ на путь, которого нет, а маршрут края "+
			"без метода ведёт в никуда", f)
	}
}
