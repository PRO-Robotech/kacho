// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package seed

// catalog_parity.go — страж расхождения литерала и строк каталога.
//
// # Зачем это на СТАРТЕ, а не гейтом дерева
//
// Гейт дерева сверяет литерал с ТЕКСТОМ миграции и потому не знает ничего о
// базе, к которой служба подключилась. Между правкой литерала и повторным
// посевом существует состояние, в котором они расходятся, — и снаружи оно
// выглядит не как поломка, а как «прав не выдали»: правило, называющее тип,
// которого нет в таблице, отвергается ключом, а тип, которого нет в литерале, не
// получает пар. Мягкий проход здесь означал бы контроль, который не отказал ни
// разу за свою жизнь.
//
// # Почему ОТКАЗ СТАРТА, а не предупреждение
//
// Пустой каталог отверг бы ВСЕ правила разом, и это читалось бы как «продукт
// сломан», а не «условие не создано» (миграции не применены). Отказ старта
// называет предмет прямо и приходит ДО приёма запросов — то есть арендатор не
// видит ни одного ложного отказа.
//
// # Перепись печатается ВСЕГДА
//
// Обе величины — сколько прочитано из литерала и сколько из таблицы — выводятся
// независимо от исхода: «ноль расхождений» обязано быть отличимо от «ноль
// прочитанного».

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmap"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

// CatalogParityCensus — объём осмотренного и найденное расхождение.
type CatalogParityCensus struct {
	LiteralModules   int
	LiteralResources int
	LiteralVerbs     int
	RowModules       int
	RowResources     int
	RowVerbs         int
	// MissingRows — есть в литерале, нет живой строкой.
	MissingRows []string
	// ExtraRows — есть живой строкой, нет в литерале.
	ExtraRows []string
}

// Diverged — расхождение найдено хотя бы в одну сторону.
func (c CatalogParityCensus) Diverged() bool {
	return len(c.MissingRows) > 0 || len(c.ExtraRows) > 0
}

// Empty — таблиц каталога не прочитано ни строки. Отдельно от расхождения,
// потому что предмет другой: расхождение чинится повторным посевом, пустой
// каталог — применением миграций.
func (c CatalogParityCensus) Empty() bool {
	return c.RowModules == 0 && c.RowResources == 0 && c.RowVerbs == 0
}

// AssertCatalogParity сверяет ЖИВЫЕ строки каталога с литералом-источником и
// возвращает перепись вместе с исходом. Ошибка означает отказ старта.
//
// Читается ПУЛ, а не читающая сторона порта: та предпочитает реплику, а страж
// исполняется на старте, когда отставание реплики наиболее вероятно.
// Прочитанный оттуда пустой каталог дал бы отказ старта на исправной службе.
func AssertCatalogParity(ctx context.Context, pool *pgxpool.Pool) (CatalogParityCensus, error) {
	var c CatalogParityCensus

	wantMod := map[string]bool{}
	for _, m := range domain.KnownModules() {
		wantMod[m] = true
	}
	wantRes := map[string]bool{}
	for _, r := range authzmap.CatalogSeedResources() {
		wantRes[r.Dotted] = true
	}
	wantVerb := map[string]bool{}
	for _, v := range authzmap.CatalogSeedVerbs() {
		wantVerb[v.Module+"."+v.Resource+"."+v.Verb] = true
	}
	c.LiteralModules, c.LiteralResources, c.LiteralVerbs = len(wantMod), len(wantRes), len(wantVerb)

	gotMod, err := readCatalogSet(ctx, pool,
		`SELECT module FROM kacho_iam.catalog_module WHERE live`)
	if err != nil {
		return c, fmt.Errorf("прочитать каталог модулей: %w", err)
	}
	gotRes, err := readCatalogSet(ctx, pool,
		`SELECT dotted FROM kacho_iam.catalog_resource WHERE live`)
	if err != nil {
		return c, fmt.Errorf("прочитать каталог ресурсов: %w", err)
	}
	gotVerb, err := readCatalogSet(ctx, pool,
		`SELECT module || '.' || resource || '.' || verb FROM kacho_iam.catalog_verb WHERE live`)
	if err != nil {
		return c, fmt.Errorf("прочитать каталог глаголов: %w", err)
	}
	c.RowModules, c.RowResources, c.RowVerbs = len(gotMod), len(gotRes), len(gotVerb)

	diffInto(&c.MissingRows, &c.ExtraRows, "модуль", wantMod, gotMod)
	diffInto(&c.MissingRows, &c.ExtraRows, "ресурс", wantRes, gotRes)
	diffInto(&c.MissingRows, &c.ExtraRows, "глагол", wantVerb, gotVerb)
	sort.Strings(c.MissingRows)
	sort.Strings(c.ExtraRows)

	switch {
	case c.Empty():
		return c, fmt.Errorf("каталог модуля пуст: строк catalog_module/catalog_resource/catalog_verb "+
			"прочитано 0/0/0 при %d/%d/%d в литерале — пустой каталог отверг бы ВСЕ правила разом, "+
			"и это читалось бы как поломка продукта, а не как непринятые миграции (kacho#1030, IAM-CT-1-16)",
			c.LiteralModules, c.LiteralResources, c.LiteralVerbs)
	case c.Diverged():
		return c, fmt.Errorf("литерал и строки каталога разошлись: нет строкой [%s]; нет в литерале [%s]. "+
			"Прочитано из литерала %d/%d/%d, строками %d/%d/%d. Расхождение снаружи выглядит как "+
			"«прав не выдали», поэтому старт отказан, а не продолжен (kacho#1030, IAM-CT-1-15)",
			strings.Join(c.MissingRows, ", "), strings.Join(c.ExtraRows, ", "),
			c.LiteralModules, c.LiteralResources, c.LiteralVerbs,
			c.RowModules, c.RowResources, c.RowVerbs)
	}
	return c, nil
}

// diffInto — расхождение в ОБЕ стороны. Одностороннее сравнение (включение)
// молчало бы на строке, которой в литерале нет: она даёт правилу референт, по
// которому оно резолвится, а проекция — нет.
func diffInto(missing, extra *[]string, kind string, want, got map[string]bool) {
	for k := range want {
		if !got[k] {
			*missing = append(*missing, kind+" "+k)
		}
	}
	for k := range got {
		if !want[k] {
			*extra = append(*extra, kind+" "+k)
		}
	}
}

func readCatalogSet(ctx context.Context, pool *pgxpool.Pool, q string) (map[string]bool, error) {
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var s string
		if serr := rows.Scan(&s); serr != nil {
			return nil, serr
		}
		out[s] = true
	}
	return out, rows.Err()
}
