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

// engineRetirementExemptions — послабления гейта Г6.
//
// ПУСТ, И ЭТО ЦЕЛЬ, А НЕ УПУЩЕНИЕ. Записи здесь стояли двумя: порождённые стабы
// контракта и файл самого гейта. Обе истекли на первом же прогоне усиленного
// предиката (ниже): стабы перестали называть снятую поверхность после
// перегенерации, а гейт называет её строковыми константами, которые разбор
// исполняемой части не читает.
//
// Перечень оставлен объявленным, а не удалён вместе с механизмом: следующее
// послабление обязано появиться ЗДЕСЬ, рядом со своим предикатом снятия, а не
// условием, вписанным в разбор.
var engineRetirementExemptions = []struct {
	// Prefix — путь либо его начало.
	Prefix string
	// Why — почему исключено.
	Why string
	// Until — при каком факте о дереве запись обязана быть снята.
	Until string
}{}

func exemptFromEngineRetirement(path string) bool {
	for _, e := range engineRetirementExemptions {
		if strings.HasPrefix(path, e.Prefix) {
			return true
		}
	}
	return false
}

// engineRetirementSources — непроверочное дерево Go, спрошенное У ИНДЕКСА.
//
// Обход диска не знает правил игнорирования и судит чужой рабочий каталог —
// произведённые файлы, чужие копии, остатки прогонов.
func engineRetirementSources(t *testing.T) (string, map[string]string) {
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
		b, readErr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		sources[rel] = string(b)
	}
	return root, sources
}

// TestR7_3_26_EngineIsNotInTheDecisionPath — Г6: внешний движок прав не участвует
// в решении НИ ПО ОДНОМУ типу, и это утверждение О ДЕРЕВЕ, а не о намерении.
//
// Разбор класса и граница предиката — в шапке authzengineretired.go. Здесь только
// обход дерева и вердикт.
func TestR7_3_26_EngineIsNotInTheDecisionPath(t *testing.T) {
	_, sources := engineRetirementSources(t)

	findings, census, err := FindRetiredEngineSurface(sources, exemptFromEngineRetirement)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	t.Logf("осмотрено непроверочных файлов Go: %d; идентификаторов: %d; "+
		"файлов, называющих движок ТОЛЬКО в прозе: %d; исключено послаблениями: %d",
		census.Files, census.Idents, census.ProseMentions, census.Exempt)

	// Предпосылка гейта: он обязан ОТКАЗЫВАТЬ на беспредметности, а не молчать.
	// Ноль прочитанных файлов снаружи неотличим от «нарушений нет» — и это ровно
	// тот класс, который гейт ловит.
	if census.Files == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if census.Idents == 0 {
		t.Fatal("осмотрено ноль идентификаторов — разбор не дошёл до исполняемой части")
	}

	for _, f := range findings {
		t.Errorf("%s:%d: %s — %s.\n"+
			"Внешний движок прав снят стадией S6 эпика #747: решение о доступе вычисляет "+
			"реляционная форма в собственной базе службы (`repo/kaname/pg/relverdict`), а дверь "+
			"решения — `internal/authzcascade`. Возвращение сетевого соседа в путь решения "+
			"возвращает и его окно рассогласования, и его домен отказа — то, ради снятия чего "+
			"стадия и делалась",
			f.File, f.Line, f.Symbol, f.Kind)
	}
}

// TestR7_3_26_EngineExemptionsStillHaveASubject — послабление живёт, пока у него
// есть предмет.
//
// «Предмет» здесь — НЕ «под префиксом лежат файлы». Такой предикат зеленел бы на
// послаблении, которому нечего исключать: каталог существует, находок в нём нет, а
// запись стоит и молча накроет следующую слепую зону.
//
// Предмет — «без этой записи гейт нашёл бы ЗДЕСЬ находку». Поэтому разбор
// прогоняется БЕЗ послаблений, и от каждой записи требуется хотя бы одна находка
// под её префиксом.
func TestR7_3_26_EngineExemptionsStillHaveASubject(t *testing.T) {
	_, sources := engineRetirementSources(t)

	// Разбор БЕЗ послаблений: что гейт увидел бы, не будь их вовсе.
	bare, census, err := FindRetiredEngineSurface(sources, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	t.Logf("разбор без послаблений: файлов %d, находок %d", census.Files, len(bare))

	if len(engineRetirementExemptions) == 0 {
		// Пустой перечень — ЦЕЛЬ этой пробы, а не её поломка. Проба, падающая на
		// достижении собственной цели, подталкивает держать запись ради зелёного.
		t.Log("послаблений ноль — исключать нечего, и это исход, к которому проба ведёт")
		return
	}
	for _, e := range engineRetirementExemptions {
		covered := 0
		for _, f := range bare {
			if strings.HasPrefix(f.File, e.Prefix) {
				covered++
			}
		}
		if covered == 0 {
			t.Errorf("послабление %q не исключает НИ ОДНОЙ находки — предмета у него нет, "+
				"и оно обязано быть снято. Оставленное, оно молча накроет следующую слепую "+
				"зону.\nПричина записи: %s\nПредикат снятия: %s",
				e.Prefix, e.Why, e.Until)
			continue
		}
		t.Logf("послабление %q: находок под ним %d", e.Prefix, covered)
	}
}
