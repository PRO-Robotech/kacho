// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// scopedepthagreement_test.go — ТРИ ВЕЛИЧИНЫ ГЛУБИНЫ ЦЕПИ ОБЯЗАНЫ СОВПАДАТЬ.
//
// # Предмет
//
// Предел обхода цепи областей несёт ДВОЙНУЮ нагрузку: тем же числом ограничена
// и рекурсия (сколько уровней вверх пройти), и выборка внутри соединения вбок
// (сколько рёбер взять у одного объекта). Довод «предел не усекает» верен ровно
// пока это число не меньше того, что допускает схема: у объекта не бывает больше
// рёбер, чем глубин, а глубина ограничена проверкой `depth BETWEEN 1 AND N`.
//
// Совпадение сегодня есть, и оно НИЧЕМ НЕ ДЕРЖИТСЯ. Понизить константу обхода —
// и предел начнёт молча отбрасывать рёбра; поднять границу схемы новой миграцией
// — то же самое с другой стороны. Отбрасывать он будет по `ORDER BY pe.depth`,
// то есть ДАЛЬНИХ предков первыми: аккаунт и кластер. Это Б1 через другую дверь:
// область схлопывается вверх, ответ остаётся «нет», и отказ неотличим от
// честного.
//
// # Почему гейт, а не комментарий
//
// Комментарий у запроса это уже объясняет. Но объяснение живёт в одном файле, а
// величины — в трёх местах: константа Go, число в проверке схемы и предел внутри
// соединения. Правка любого из трёх выглядит локальной и безобидной.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

const (
	scopeDepthConstFile = "services/iam/internal/repo/kacho/pg/relverdict/query.go"
	scopeDepthMigration = "services/iam/internal/migrations/0082_resource_parent_edge.sql"
)

var (
	reMaxAncestorDepth = regexp.MustCompile(`MaxAncestorDepth\s*=\s*(\d+)`)
	reDepthCheck       = regexp.MustCompile(`depth BETWEEN 1 AND (\d+)`)
	rePlanDepth        = regexp.MustCompile(`MaxPointerDepth\s*=\s*(\d+)`)
)

func TestScopeDepthBoundsAgreeAcrossAllThreePlaces(t *testing.T) {
	root := repoRoot(t)

	code := readFileForDepth(t, filepath.Join(root, scopeDepthConstFile))
	schema := readFileForDepth(t, filepath.Join(root, scopeDepthMigration))

	goDepth := singleNumber(t, reMaxAncestorDepth, code, "предел обхода в коде (MaxAncestorDepth)")
	sqlDepth := singleNumber(t, reDepthCheck, schema, "граница глубины в проверке схемы")

	// Третья величина — предел компилятора модели. Он объявлен в другом пакете и
	// сверяется собственной пробой рядом с ним; здесь он лишь ИЩЕТСЯ, и его
	// отсутствие названо, а не проглочено.
	planDepth, planFound := findPlanDepth(t, root)

	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: предел обхода %d (%s) · граница схемы %d (%s) · "+
		"предел компилятора модели %s",
		goDepth, scopeDepthConstFile, sqlDepth, scopeDepthMigration,
		planDepthText(planDepth, planFound))

	if goDepth != sqlDepth {
		t.Errorf("предел обхода %d не равен границе схемы %d.\n"+
			"    Тем же числом ограничена выборка внутри соединения вбок, и довод «предел не "+
			"усекает» держится ровно их равенством. При меньшем пределе выборка молча "+
			"отбросит рёбра, причём по ORDER BY pe.depth — ДАЛЬНИХ предков первыми, то есть "+
			"аккаунт и кластер. Область схлопнется вверх, ответ останется «нет», и отказ "+
			"будет неотличим от честного.", goDepth, sqlDepth)
	}
	if planFound && planDepth != goDepth {
		t.Errorf("предел компилятора модели %d не равен пределу обхода %d: модель выводит "+
			"права на глубину, до которой обход не доходит (или наоборот)", planDepth, goDepth)
	}
}

func readFileForDepth(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл %s не читается: %v — гейт не может судить дерево, которого не видит", path, err)
	}
	return string(body)
}

// singleNumber — величина обязана быть НАЙДЕНА и быть ЕДИНСТВЕННОЙ.
//
// Ноль совпадений означает, что объявление переехало и гейт сторожит пустоту;
// два — что величина уже задвоена, и сверять её саму с собой бессмысленно.
func singleNumber(t *testing.T, re *regexp.Regexp, body, what string) int {
	t.Helper()
	ms := re.FindAllStringSubmatch(body, -1)
	if len(ms) == 0 {
		t.Fatalf("%s не найдена: объявление переехало, и «величины совпадают» стало бы "+
			"утверждением ни о чём", what)
	}
	if len(ms) > 1 {
		t.Fatalf("%s объявлена %d раз: она уже задвоена, и расхождение возможно внутри "+
			"одного файла", what, len(ms))
	}
	n, err := strconv.Atoi(ms[0][1])
	if err != nil {
		t.Fatalf("%s не число: %q", what, ms[0][1])
	}
	return n
}

// findPlanDepth — предел компилятора модели, если он в дереве объявлен.
//
// Состав берётся У ИНДЕКСА РЕПОЗИТОРИЯ, а не обходом диска. Под services/ на
// машине, где поднимали стенд или собирали фронтенд, лежит игнорируемое —
// распаковки чартов, сборочные каталоги, отчёты прогонов, — и обход по диску
// судил бы дерево, которого в репозитории нет. Два обхода поддерева уже
// оказались дефектными ровно по этой причине.
func findPlanDepth(t *testing.T, root string) (int, bool) {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services/iam/internal"), ".go")
	if err != nil {
		t.Fatalf("состав поддерева у индекса репозитория: %v — гейт не может судить дерево, "+
			"которого не может назвать", err)
	}
	for _, abs := range files {
		if strings.HasSuffix(abs, "_test.go") {
			continue
		}
		body, rerr := os.ReadFile(abs)
		if rerr != nil {
			continue
		}
		if m := rePlanDepth.FindStringSubmatch(string(body)); m != nil {
			if n, cerr := strconv.Atoi(m[1]); cerr == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func planDepthText(n int, ok bool) string {
	if !ok {
		return "не объявлен в дереве (сверять не с чем — названо, а не проглочено)"
	}
	return strconv.Itoa(n)
}
