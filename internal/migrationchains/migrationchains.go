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
// Обход идёт по диску, а не по индексу git: перечень зовут и гейты дерева, и
// их инъекции на синтетических деревьях во временном каталоге, где индекса
// нет. Отслеживаемость файлов цепочки судят сами потребители (эталон длины
// цепочки `internal/migratorapply` берётся у индекса).
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

// List — цепочки дерева root, упорядоченные по точке и каталогу.
//
// Отказы (а не пустой перечень): точек наката ноль; таблица точки неверна
// (ParseTable); каталога строки нет; каталог `services/<svc>/internal/migrations`
// с миграциями, который не применяет ни одна точка, — цепочка без точки
// наката, которую гейты иначе не увидели бы.
func List(root string) ([]Chain, error) {
	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, fmt.Errorf("перечень цепочек: %s не прочитан: %w", servicesDir, err)
	}
	var out []Chain
	covered := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		point := "services/" + e.Name() + "/cmd/migrator"
		if !isFile(filepath.Join(root, filepath.FromSlash(point), "main.go")) {
			continue
		}
		chains, cerr := pointChains(root, point, e.Name())
		if cerr != nil {
			return nil, cerr
		}
		for _, c := range chains {
			if !isDir(filepath.Join(root, filepath.FromSlash(c.Dir))) {
				return nil, fmt.Errorf("точка наката %s: каталога цепочки %s нет", point, c.Dir)
			}
			covered[c.Dir] = true
		}
		out = append(out, chains...)
	}
	if len(out) == 0 {
		return nil, errors.New("перечень цепочек: точек наката нет (services/*/cmd/migrator/main.go) — " +
			"пустой перечень здесь означал бы зелёный гейт с нулём прочитанного")
	}
	for _, e := range entries {
		dir := "services/" + e.Name() + "/internal/migrations"
		if !e.IsDir() || covered[dir] || !hasSQL(filepath.Join(root, filepath.FromSlash(dir))) {
			continue
		}
		return nil, fmt.Errorf("перечень цепочек: %s несёт миграции, а её не применяет ни одна точка "+
			"наката — цепочка без точки наката", dir)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Point != out[j].Point {
			return out[i].Point < out[j].Point
		}
		return out[i].Dir < out[j].Dir
	})
	return out, nil
}

// pointChains — цепочки одной точки: строки её таблицы либо одна строка по
// умолчанию.
func pointChains(root, point, svc string) ([]Chain, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(point), TableFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return []Chain{{Service: svc, Point: point, Dir: "services/" + svc + "/internal/migrations"}}, nil
	case err != nil:
		return nil, fmt.Errorf("точка наката %s: %s не прочитан: %w", point, TableFile, err)
	}
	return ParseTable(point, data)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func hasSQL(dir string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, "*.sql"))
	return len(m) > 0
}
