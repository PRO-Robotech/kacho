// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

// tfprovider_gates_test.go — изоляция модуля Terraform-провайдера от графа продукта.
//
// ПРЕДМЕТ. Провайдер живёт отдельным Go-модулем `terraform/`. Причина не в опрятности:
// `terraform-plugin-framework` тянет за собой большое дерево зависимостей, и в едином
// модуле оно вошло бы в граф КАЖДОГО сервиса и каждой сборки образа. На момент заведения
// гейта корневой `go.sum` не содержал ни одной строки `hashicorp` — это и есть свойство,
// которое здесь защищается.
//
// ПОЧЕМУ ГЕЙТ ЖИВЁТ В МОДУЛЕ ПРОДУКТА, А НЕ РЯДОМ С ПРОВАЙДЕРОМ. Две причины, и вторая
// решающая:
//   1. гейт внутри `terraform/` исчез бы вместе с тем изменением, которое запрещает —
//      вернули провайдер в корневой модуль, и проверки не стало;
//   2. `repoRoot` в этом семействе поднимается до ближайшего каталога с `go.mod`. Из
//      `terraform/` он резолвился бы в сам `terraform/`, и условие про `go.sum` читало бы
//      `terraform/go.sum`, где строки `hashicorp` есть ПО ОПРЕДЕЛЕНИЮ. Гейт мерил бы не
//      тот файл и был бы уверенно неправ. Отсюда же он безопасен здесь: подъём от
//      `internal/repohygiene/artifactgates` встречает корневой `go.mod`, а `terraform/`
//      лежит в стороне от этого пути.
//
// ЕДИНИЦА СЧЁТА — отслеживаемый git-элемент: объявление, `.gitignore` и поведение не могут
// разъехаться молча.
//
// ОТСУТСТВИЕ ПРЕДМЕТА — ОТКАЗ, А НЕ ТИХИЙ УСПЕХ. Нет `terraform/go.mod` — гейту нечего
// утверждать, и он падает, а не печатает «ноль находок». Тем же порядком поступает обход
// каталогов в `deferredwork.go`.

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// importsOf — пути импорта файла, полученные РАЗБОРОМ, а не поиском по тексту.
//
// Первая редакция этого гейта искала строку подстрокой и поймала сама себя: имя модуля
// провайдера лежит константой в её же коде, поэтому условие «продукт не импортирует
// провайдер» краснело на файле, который это условие и реализует. Тот же класс, что ловит
// class-guard под именем «гейт читает текст там, где нужен разбор»: поиск по тексту
// находит своё слово в комментарии, ОБЪЯСНЯЮЩЕМ защиту, и одинаково реагирует на код,
// строковый литерал и прозу. Разбор различает их by construction — и заодно снимает нужду
// в исключении для самого гейта, а исключение пришлось бы держать живым вручную.
func importsOf(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		// Непарсимый файл — не находка этого гейта: синтаксис стережёт сборка.
		return nil
	}
	out := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

const (
	tfModuleDir  = "terraform"
	tfModulePath = "github.com/PRO-Robotech/kacho/terraform"
	productPath  = "github.com/PRO-Robotech/kacho"
	tfGenDir     = "terraform/internal/api"
	productGen   = "pkg/api"
)

// trackedUnder — отслеживаемые файлы под префиксом, по индексу git.
func trackedUnder(t *testing.T, root, prefix string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "ls-files", "--", prefix).Output()
	if err != nil {
		t.Fatalf("git ls-files %s: %v", prefix, err)
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

func TestProviderModuleIsIsolated(t *testing.T) {
	root := repoRoot(t)

	modFile := filepath.Join(root, tfModuleDir, "go.mod")
	modBytes, err := os.ReadFile(modFile)
	if err != nil {
		t.Fatalf("предмет гейта отсутствует: не читается %s/go.mod (%v).\n"+
			"Гейт защищает изоляцию модуля провайдера; без модуля утверждать нечего, "+
			"и молчание было бы неотличимо от изоляции.", tfModuleDir, err)
	}

	var (
		goFilesSeen  int
		genFilesSeen int
	)

	// (а) модуль провайдера не зависит от модуля продукта.
	//
	// Именно эта строка появилась бы САМА, без чьего-либо решения, если второй выход
	// генерации оставить без переопределения префикса пакета: порождённый код сослался бы
	// на `pkg/api`, и `go mod tidy` дописал бы require. Дальше единственным выходом стал бы
	// `replace`, запрещённый правилами топологии.
	for _, line := range strings.Split(string(modBytes), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "require "+productPath) ||
			strings.HasPrefix(trimmed, productPath+" v") {
			t.Errorf("(а) %s/go.mod зависит от модуля продукта: %q.\n"+
				"Модуль провайдера обязан быть самостоятельным — иначе его дерево зависимостей "+
				"возвращается в граф каждого сервиса.", tfModuleDir, trimmed)
		}
		if strings.HasPrefix(trimmed, "replace "+productPath) {
			t.Errorf("(а) %s/go.mod несёт replace на продукт: %q — запрещено топологией.",
				tfModuleDir, trimmed)
		}
	}

	// (б) продукт не импортирует провайдер: зависимость односторонняя.
	for _, f := range trackedUnder(t, root, "*.go") {
		if strings.HasPrefix(f, tfModuleDir+"/") {
			continue
		}
		goFilesSeen++
		for _, imp := range importsOf(t, filepath.Join(root, f)) {
			if imp == tfModulePath || strings.HasPrefix(imp, tfModulePath+"/") {
				t.Errorf("(б) файл продукта %s импортирует %s.\n"+
					"Направление зависимости обязано быть одностороннее: провайдер знает о продукте "+
					"через контракт, продукт о провайдере — никак.", f, imp)
			}
		}
	}

	// (в) дерево зависимостей провайдера не просочилось в корневой модуль.
	//
	// Число, а не общие слова: именно оно ловит возврат к отвергнутому решению «положить
	// провайдер в корневой модуль» — раньше, чем вырастет время сборки каждого образа.
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatalf("чтение корневого go.sum: %v", err)
	}
	if n := strings.Count(string(sum), "hashicorp"); n != 0 {
		t.Errorf("(в) корневой go.sum содержит %d строк(и) hashicorp — дерево провайдера "+
			"вошло в граф продукта.", n)
	}

	// (г) порождённые типы провайдера не тянут пакеты продукта — причина, а не следствие (а).
	for _, f := range trackedUnder(t, root, tfGenDir) {
		genFilesSeen++
		for _, imp := range importsOf(t, filepath.Join(root, f)) {
			if strings.HasPrefix(imp, productPath+"/"+productGen) {
				t.Errorf("(г) порождённый %s импортирует %s.\n"+
					"Значит генерация прошла без переопределения префикса пакета: почини шаблон "+
					"второго выхода, а не go.mod — иначе зависимость вернётся при следующей генерации.",
					f, imp)
			}
		}
	}

	// (д) зеркальная половина: общий выход не ссылается на выход провайдера.
	//
	// Без неё предикат односторонний: managed-блок, ошибочно положенный в ОБЩИЙ шаблон,
	// ломает ПЕРВЫЙ выход, а условие (г) смотрит только на второй и промолчит.
	for _, f := range trackedUnder(t, root, productGen) {
		for _, imp := range importsOf(t, filepath.Join(root, f)) {
			if strings.HasPrefix(imp, tfModulePath+"/internal/api") {
				t.Errorf("(д) %s импортирует %s — переопределение префикса задето "+
					"в общем шаблоне генерации.", f, imp)
			}
		}
	}

	// (е) отслеживаемого go.work нет.
	//
	// Локальный go.work — штатный приём, чтобы среда разработки видела оба модуля, и он
	// МОЛЧА меняет смысл `./...`, втягивая граф провайдера в корневую сборку. Условие (в)
	// при этом остаётся зелёным: суммы уезжают в go.work.sum. То есть без (е) главное
	// условие изоляции обходится, не покраснев.
	for _, f := range trackedUnder(t, root, "go.work*") {
		t.Errorf("(е) в индексе git есть %s — рабочее пространство Go меняет смысл сборки "+
			"по всему дереву. Место такому файлу в .gitignore, а не в индексе.", f)
	}

	t.Logf("осмотрено: файлов продукта %d, порождённых у провайдера %d; "+
		"условий проверено 6", goFilesSeen, genFilesSeen)
}
