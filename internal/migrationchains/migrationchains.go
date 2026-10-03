// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrationchains — перечень цепочек миграций дерева: ЕДИНСТВЕННОЕ
// место, где выводится, какой каталог миграций какая точка наката применяет и
// на какую базу (kacho#2915, замысел З32, CX1-114).
//
// # Почему не «services/<svc>/internal/migrations»
//
// До kacho#2915 каждый гейт, обходящий миграции, выводил каталог цепочки из
// имени службы. Вывод был верен, пока у каталога службы была одна база и одна
// цепочка. Каталог `services/notify` несёт две базы (database per service):
// `kacho_notifyprobe` пробы-источника и `kacho_notify` шлюза, и цепочка пробы
// лежит в `services/notify/internal/probemigrations`. Вывод из имени службы её
// НЕ ВИДИТ — гейт не краснеет, а молча не читает целую цепочку.
//
// # Откуда перечень
//
// Точка наката — `services/<svc>/cmd/migrator/main.go`. Таблица «имя базы →
// каталог цепочки» объявлена у точки один раз файлом данных
// `services/<svc>/cmd/migrator/chains.yaml` (точка читает его `//go:embed`,
// перечень — по пути). У точки без таблицы цепочка одна —
// `services/<svc>/internal/migrations`, и по имени базы точка не выбирает.
//
// Состав берётся у ИНДЕКСА git (List), а не с диска: неотслеживаемая точка или
// миграция, оставшаяся в рабочем каталоге, не меняет перечня. Инъекции гейтов
// на синтетических деревьях во временном каталоге зовут FromTree с
// treecorpus.SyntheticTree — индекса там нет, и диск там единственный
// авторитет.
package migrationchains

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// TableFile — имя файла таблицы цепочек рядом с main.go точки наката.
const TableFile = "chains.yaml"

// Chain — одна цепочка миграций дерева.
type Chain struct {
	// Service — каталог службы под `services/`.
	Service string
	// Point — каталог точки наката от корня (`services/<svc>/cmd/migrator`).
	Point string
	// Database — имя базы строки таблицы цепочек. ПУСТО — «точка одна на
	// цепочку и по имени базы не выбирает» (таблицы у точки нет). Имени базы
	// из имени службы не выводит никто: его объявляют чарт и таблица.
	Database string
	// Dir — каталог цепочки от корня, через `/`.
	Dir string
}

// table — разбор chains.yaml.
type table struct {
	Chains []struct {
		Database string `yaml:"database"`
		Dir      string `yaml:"dir"`
	} `yaml:"chains"`
}

// ParseTable разбирает таблицу цепочек точки point (каталог от корня). Строки
// возвращаются с заполненными Service и Point. Ни одной строки, строка без
// базы или каталога, каталог вне `services/<svc>/internal/`, повтор базы или
// каталога — ошибка с именем точки.
func ParseTable(point string, data []byte) ([]Chain, error) {
	svc, ok := serviceOfPoint(point)
	if !ok {
		return nil, fmt.Errorf("точка наката %s: путь не в форме services/<служба>/cmd/migrator", point)
	}
	var tb table
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&tb); err != nil {
		return nil, fmt.Errorf("точка наката %s: %s не разобран: %w", point, TableFile, err)
	}
	if len(tb.Chains) == 0 {
		return nil, fmt.Errorf("точка наката %s: в %s строк 0 — точка с таблицей без строк "+
			"не накатывает ничего, и это отказ, а не пустой перечень", point, TableFile)
	}
	prefix := "services/" + svc + "/internal/"
	seenDB := map[string]bool{}
	seenDir := map[string]bool{}
	out := make([]Chain, 0, len(tb.Chains))
	for i, row := range tb.Chains {
		if row.Database == "" || row.Dir == "" {
			return nil, fmt.Errorf("точка наката %s: строка %d %s без database или dir", point, i+1, TableFile)
		}
		dir := path.Clean(row.Dir)
		if dir != row.Dir || !strings.HasPrefix(dir, prefix) {
			return nil, fmt.Errorf("точка наката %s: каталог строки %q вне %s — точка накатывает "+
				"только цепочки своего каталога службы", point, row.Dir, prefix)
		}
		if seenDB[row.Database] {
			return nil, fmt.Errorf("точка наката %s: база %s названа в %s дважды", point, row.Database, TableFile)
		}
		if seenDir[dir] {
			return nil, fmt.Errorf("точка наката %s: каталог %s назван в %s дважды", point, dir, TableFile)
		}
		seenDB[row.Database], seenDir[dir] = true, true
		out = append(out, Chain{Service: svc, Point: point, Database: row.Database, Dir: dir})
	}
	return out, nil
}

// serviceOfPoint — служба точки `services/<svc>/cmd/migrator`.
func serviceOfPoint(point string) (string, bool) {
	parts := strings.Split(point, "/")
	if len(parts) != 4 || parts[0] != "services" || parts[2] != "cmd" || parts[3] != "migrator" || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

// List — цепочки дерева root по его индексу git. Недоступный индекс — отказ.
func List(root string) ([]Chain, error) {
	chains, _, err := Survey(root)
	return chains, err
}

// Census — объём, осмотренный поиском сирот: файлов `.sql` под
// `services/<svc>/internal/**` и каталогов, в которых они лежат. Ноль файлов
// при непустом перечне — не «сирот нет», а «смотреть было не на что»: судит
// вызывающий, перечень цепочек такой ответ отдаёт без отказа.
type Census struct {
	SQLFiles int
	SQLDirs  int
}

// Survey — то же, что List, и объём осмотренного поиском сирот.
func Survey(root string) ([]Chain, Census, error) {
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		return nil, Census{}, fmt.Errorf("перечень цепочек: %w", err)
	}
	return SurveyTree(tree)
}

// FromTree — цепочки состава tree, упорядоченные по точке и каталогу.
//
// Отказы (а не пустой перечень): точек наката ноль; таблица точки неверна
// (ParseTable); каталога строки в составе нет; файл `.sql` где угодно под
// `services/<svc>/internal/**` вне каталогов перечня — цепочка без точки
// наката, которую гейты иначе не увидели бы.
//
// Сирота ищется по ЛЮБОМУ каталогу под internal/, а не только по
// каноническому `internal/migrations`: цепочка с таблицей лежит в каталоге,
// который называет строка (`internal/probemigrations` у notify), и строка,
// снятая при файлах на месте, оставила бы цепочку вне перечня молча — гейты,
// читающие перечень, её не видят (ORPHAN-CHAIN-BLIND).
func FromTree(tree *treecorpus.Tree) ([]Chain, error) {
	chains, _, err := SurveyTree(tree)
	return chains, err
}

// SurveyTree — FromTree и объём осмотренного поиском сирот.
func SurveyTree(tree *treecorpus.Tree) ([]Chain, Census, error) {
	files := tree.SortedFiles()
	services := map[string]bool{}
	for _, f := range files {
		if rest, ok := strings.CutPrefix(f, "services/"); ok {
			if i := strings.IndexByte(rest, '/'); i > 0 {
				services[rest[:i]] = true
			}
		}
	}
	names := make([]string, 0, len(services))
	for svc := range services {
		names = append(names, svc)
	}
	sort.Strings(names)

	var out []Chain
	covered := map[string]bool{}
	for _, svc := range names {
		point := "services/" + svc + "/cmd/migrator"
		if !tree.HasFile(point + "/main.go") {
			continue
		}
		chains, err := pointChains(tree, point, svc)
		if err != nil {
			return nil, Census{}, err
		}
		for _, c := range chains {
			if !tree.HasDir(c.Dir) {
				return nil, Census{}, fmt.Errorf("точка наката %s: каталога цепочки %s нет", point, c.Dir)
			}
			covered[c.Dir] = true
		}
		out = append(out, chains...)
	}
	if len(out) == 0 {
		return nil, Census{}, errors.New("перечень цепочек: точек наката нет (services/*/cmd/migrator/main.go) — " +
			"пустой перечень здесь означал бы зелёный гейт с нулём прочитанного")
	}
	census, orphans := sqlUnderInternal(files, covered)
	if len(orphans) > 0 {
		return nil, census, fmt.Errorf("перечень цепочек: %s несёт миграции, а её не применяет ни одна точка "+
			"наката — цепочка без точки наката (каталогов-сирот %d)", strings.Join(orphans, ", "), len(orphans))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Point != out[j].Point {
			return out[i].Point < out[j].Point
		}
		return out[i].Dir < out[j].Dir
	})
	return out, census, nil
}

// sqlUnderInternal — перепись `.sql` под services/<svc>/internal/** и
// каталоги-сироты: каталоги с `.sql`, которых нет среди covered (каталогов
// перечня), по возрастанию.
func sqlUnderInternal(files []string, covered map[string]bool) (Census, []string) {
	var census Census
	dirs := map[string]bool{}
	for _, f := range files {
		rest, ok := strings.CutPrefix(f, "services/")
		if !ok || !strings.HasSuffix(f, ".sql") {
			continue
		}
		i := strings.IndexByte(rest, '/')
		if i <= 0 || !strings.HasPrefix(rest[i:], "/internal/") {
			continue
		}
		census.SQLFiles++
		dirs[path.Dir(f)] = true
	}
	census.SQLDirs = len(dirs)
	var orphans []string
	for d := range dirs {
		if !covered[d] {
			orphans = append(orphans, d)
		}
	}
	sort.Strings(orphans)
	return census, orphans
}

// pointChains — цепочки одной точки: строки её таблицы либо одна строка по
// умолчанию.
func pointChains(tree *treecorpus.Tree, point, svc string) ([]Chain, error) {
	table := point + "/" + TableFile
	if !tree.HasFile(table) {
		return []Chain{{Service: svc, Point: point, Dir: "services/" + svc + "/internal/migrations"}}, nil
	}
	data, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(table)))
	if err != nil {
		return nil, fmt.Errorf("точка наката %s: %s не прочитан: %w", point, TableFile, err)
	}
	return ParseTable(point, data)
}

// IsChainSQL — файл rel (путь от корня, через `/`) — миграция одной из цепочек
// chains: `.sql` прямо в каталоге цепочки. Распознаватели «это миграция» по
// сегменту пути `/internal/migrations/` цепочку пробы notify
// (`internal/probemigrations`) не видели бы.
func IsChainSQL(chains []Chain, rel string) bool {
	if !strings.HasSuffix(rel, ".sql") {
		return false
	}
	dir := path.Dir(rel)
	for _, c := range chains {
		if c.Dir == dir {
			return true
		}
	}
	return false
}
