// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// crossmodulegopath_injection_test.go — доказательство того, что соседний гейт
// СПОСОБЕН упасть, и того, что он молчит на законном близнеце.
//
// Каждая проба меняет РОВНО ОДИН факт против положительного близнеца: иначе
// красное могло бы прийти от соседа, а гейт остался бы вакуумным, не показав
// этого ничем. Контроль («всё законно — молчит») стоит первым.
package repohygiene

import (
	"testing"
)

// modules / goDirs — мир инъекции: один вынесенный модуль и один его пакет.
var (
	injModules = []string{"services/iam"}
	injGoDirs  = map[string]bool{
		"services/iam/tools/clagate":      true,
		"services/iam/internal/authzmap":  true,
		"services/other/internal/nothing": true,
	}
)

func injIsGoPkgDir(dir string) bool { return injGoDirs[dir] }

func TestCrossModuleGoPathGateCutsBothWays(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// КОНТРОЛЬ: законная форма — `-C` и путь от корня модуля.
			name: "МОЛЧИТ: законная форма с -C",
			src:  "go test -C services/iam ./tools/clagate/ -count=1\n",
			want: 0,
		},
		{
			// Один изменённый факт против контроля: путь назван от корня монорепо.
			name: "КРАСНО: путь от корня монорепо в строке вызова",
			src:  "go test ./services/iam/tools/clagate/ -count=1\n",
			want: 1,
		},
		{
			// Тот самый носитель, что уронил конвейер: путь в ПЕРЕМЕННОЙ, вызов
			// собран списком аргументов. Построчный предикат его пропускал.
			name: "КРАСНО: путь лежит в переменной, а не в строке вызова",
			src:  "MINT_PKG = \"./services/iam/tools/clagate\"\nsubprocess.run([\"go\", \"run\", MINT_PKG])\n",
			want: 1,
		},
		{
			// Законный близнец предыдущего: та же переменная, путь от корня модуля.
			name: "МОЛЧИТ: переменная несёт путь от корня модуля",
			src:  "MINT_PKG = \"./tools/clagate\"\nsubprocess.run([\"go\", \"run\", \"-C\", \"services/iam\", MINT_PKG])\n",
			want: 0,
		},
		{
			// Не пакет Go: каталога с файлами .go по этому пути нет. Гейт судит
			// путь, а не написание — файл данных совпадением не является.
			name: "МОЛЧИТ: путь не указывает на пакет Go",
			src:  "SEED=./services/iam/tests/newman/collections/x.json\n",
			want: 0,
		},
		{
			// Граница слева: `../services/iam/…` — путь ОТНОСИТЕЛЬНО другого
			// каталога, к предмету отношения не имеет. Без границы совпал бы хвостом.
			name: "МОЛЧИТ: относительный путь из соседнего каталога",
			src:  "FGA ?= ../services/iam/tools/clagate\n",
			want: 0,
		},
		{
			// Модуль не вынесен — предмета нет: путь от корня монорепо законен.
			name: "МОЛЧИТ: пакет службы, своего модуля НЕ несущей",
			src:  "go test ./services/other/internal/nothing/\n",
			want: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findCrossModuleGoPaths(
				map[string]string{"carrier.sh": tc.src}, injModules, injIsGoPkgDir)
			if len(got) != tc.want {
				t.Fatalf("находок %d, ожидалось %d: %+v", len(got), tc.want, got)
			}
		})
	}
}

// Находка обязана НАЗЫВАТЬ координату: номер строки, отсчитанный по носителю, а
// не по совпадению. Гейт, чья находка не называет места, посылает читателя
// искать вручную — и его снимают как непонятный.
func TestCrossModuleGoPathGateNamesTheCoordinate(t *testing.T) {
	src := "# шапка\n# ещё строка\ngo test ./services/iam/internal/authzmap/ -run X\n"
	got := findCrossModuleGoPaths(
		map[string]string{"services/vpc/manifest.yaml": src}, injModules, injIsGoPkgDir)
	if len(got) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %+v", len(got), got)
	}
	if got[0].Line != 3 {
		t.Errorf("строка %d, ожидалась 3 — номер отсчитан не по носителю", got[0].Line)
	}
	if got[0].File != "services/vpc/manifest.yaml" {
		t.Errorf("носитель %q — координата потеряна", got[0].File)
	}
	if got[0].Text != "./services/iam/internal/authzmap" {
		t.Errorf("путь %q — находка не называет предмет дословно", got[0].Text)
	}
	if got[0].Module != "services/iam" {
		t.Errorf("модуль %q — находка не называет владельца пакета", got[0].Module)
	}
}

// Предпосылка: без вынесенных модулей гейт беспредметен, и это ИСХОД, а не
// молчание. Пустой перечень модулей обязан давать ноль находок при любом входе —
// иначе гейт судил бы дерево, в котором его предмета нет.
func TestCrossModuleGoPathGateIsVacuousWithoutASecondModule(t *testing.T) {
	src := "go test ./services/iam/tools/clagate/\n"
	got := findCrossModuleGoPaths(map[string]string{"carrier.sh": src}, nil, injIsGoPkgDir)
	if len(got) != 0 {
		t.Fatalf("находок %d при пустом перечне модулей — гейт судит несуществующий предмет: %+v",
			len(got), got)
	}
}
