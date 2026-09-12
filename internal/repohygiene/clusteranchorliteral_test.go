// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// clusterAnchorSources — непроверочное дерево Go, спрошенное У ИНДЕКСА.
//
// Обход диска не знает правил игнорирования и судит чужой рабочий каталог —
// произведённые файлы, чужие копии, остатки прогонов. Порождённые стабы
// контракта исключены отдельно: они называют якорь в комментариях (литералов
// там нет), и читать их — только время.
func clusterAnchorSources(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}
	sources := map[string]string{}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || skipPath(rel) || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if strings.HasPrefix(rel, "pkg/api/") {
			continue // порождённые стабы контракта — правятся генератором
		}
		b, readErr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		sources[rel] = string(b)
	}
	// Объявление переехало ЦЕЛИКОМ: `ClusterSingletonID` предмета в этом дереве
	// больше не несёт ни одного файла — оно живёт в authz/catalogderive общего
	// фундамента (github.com/PRO-Robotech/corelib). Без второго дома объявлений
	// найдено бы 0, и вердикт о согласии/о литералах мимо объявления был бы
	// беспредметен, а не «согласны».
	for rel, body := range corelibPackageGoFiles(t, root, "authz/catalogderive") {
		sources[rel] = string(body)
	}
	return sources
}

// TestClusterAnchorIsNamedOnlyByItsDeclaration — написание якоря кластера стоит
// в дереве ТОЛЬКО в своём объявлении.
//
// Разбор класса и граница предиката — в шапке clusteranchorliteral.go. Здесь
// только обход дерева и вердикт.
func TestClusterAnchorIsNamedOnlyByItsDeclaration(t *testing.T) {
	t.Parallel()
	decls, findings, census, err := FindClusterAnchorLiterals(clusterAnchorSources(t))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	t.Logf("осмотрено непроверочных файлов Go: %d; строковых литералов: %d; "+
		"объявлений написания: %d; литералов с написанием ВНУТРИ фразы: %d",
		census.Files, census.Literals, census.Declarations, census.Embedded)
	for _, d := range decls {
		t.Logf("объявление: %s:%d = %q", d.File, d.Line, d.Value)
	}

	// Предпосылка гейта: он обязан ОТКАЗЫВАТЬ на беспредметности, а не молчать.
	if census.Files == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if census.Literals == 0 {
		t.Fatal("осмотрено ноль литералов — разбор не дошёл до строк")
	}

	for _, f := range findings {
		t.Errorf("%s:%d: написание якоря кластера повторено литералом (%s: %q).\n"+
			"Переход написания правит ОБЪЯВЛЕНИЯ; место, повторившее строку рукой, "+
			"продолжит писать и читать прежний объект после того, как строка переехала, — "+
			"и продолжит молча: код соберётся, типы сойдутся. Возьмите константу %s "+
			"своего модуля (для формы объекта — сложением с приставкой %q)",
			f.File, f.Line, f.Kind, f.Literal, ClusterAnchorConstName, ClusterAnchorObjectPrefix)
	}
}

// ЗДЕСЬ БЫЛА TestClusterAnchorDeclarationsAgree — «объявлений два (модуля два),
// и они обязаны нести ОДНУ строку». Она СНЯТА ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ: второй
// модуль (служба доступа) вынесен отдельным продуктом, объявление написания в
// дереве осталось ОДНО, и сравнивать больше не с чем.
//
// Проба сказала это сама — «проба на одном объявлении вакуумна: сравнивать не с
// чем», — то есть её собственная предпосылка покраснела как задумано. Понизить
// порог до одного означало бы оставить утверждение, истинное при любом дереве:
// одно значение всегда согласно само с собой.
//
// Вместе с ней снята её инъекция TestClusterAnchorInjection_DeclarationsDisagree
// (соседнее свойство «объявления разошлись»): доказательство без предмета
// доказывает несуществующее.
//
// Половина свойства, которая ОСТАЛАСЬ и держится выше: написание якоря стоит в
// дереве только в своём объявлении, литералом его не повторяют. Она от числа
// модулей не зависит и вернётся к паре сама, если модулей снова станет два, —
// тогда эту пробу заводят обратно вместе со вторым объявлением.
