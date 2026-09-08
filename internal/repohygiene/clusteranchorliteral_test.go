// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
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
	return sources
}

// TestClusterAnchorIsNamedOnlyByItsDeclaration — написание якоря кластера стоит
// в дереве ТОЛЬКО в своём объявлении.
//
// Разбор класса и граница предиката — в шапке clusteranchorliteral.go. Здесь
// только обход дерева и вердикт.
func TestClusterAnchorIsNamedOnlyByItsDeclaration(t *testing.T) {
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

// TestClusterAnchorDeclarationsAgree — ВТОРАЯ половина того же свойства:
// объявлений два (модуля два), и они обязаны нести ОДНУ строку.
//
// Без этой пробы гейт выше был бы удовлетворён деревом, где фундамент и служба
// объявляют РАЗНЫЕ написания: литералов мимо объявлений нет, а край и служба
// спрашивают про разные объекты — и вопрос о доступе не совпадает ни с одним
// ответом.
func TestClusterAnchorDeclarationsAgree(t *testing.T) {
	decls, _, census, err := FindClusterAnchorLiterals(clusterAnchorSources(t))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	if census.Declarations < 2 {
		t.Fatalf("объявлений написания %d — модуля два, и у каждого своё; "+
			"проба на одном объявлении вакуумна: сравнивать не с чем", census.Declarations)
	}

	want := decls[0].Value
	for _, d := range decls[1:] {
		if d.Value != want {
			t.Errorf("объявления написания якоря РАСХОДЯТСЯ: %s:%d = %q против %s:%d = %q.\n"+
				"Край и служба спросят про разные объекты, и вопрос о доступе не совпадёт "+
				"ни с одним ответом — молча, потому что оба модуля соберутся",
				decls[0].File, decls[0].Line, want, d.File, d.Line, d.Value)
		}
	}
	t.Logf("объявлений написания: %d, значение: %q", census.Declarations, want)
}
