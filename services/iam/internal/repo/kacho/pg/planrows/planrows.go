// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package planrows — прибор порядков: несущая величина замера снимается с плана
// запроса, а не с часов и не со счётчика страниц.
//
// ЭТО НАИВНАЯ РЕДАКЦИЯ, ЗАВЕДЁННАЯ КАК ИНЪЕКЦИЯ. Она воспроизводит извлекатель,
// живущий в дереве сегодня (`services/vpc/internal/repo/kacho/pg/
// address_list_subnet_narrow_integration_test.go`, `explainAnalyze`): сумма
// `Actual Rows` по узлам, отнесение по одному лишь `Relation Name`, группировка
// по имени. Самопроверки обязаны на ней ПОКРАСНЕТЬ — иначе они ловят форму, а не
// существо.
package planrows

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrPreconditionNotMet — условие замера не создано.
//
// Отдельная категория исхода, никогда не сворачиваемая ни в «зелено», ни в
// «красно»: «ноль строк» по плану, где смотреть было негде, означает «не нашли,
// где смотреть», а не «работы не было».
var ErrPreconditionNotMet = errors.New("planrows: условие замера не создано")

// AttributionRule — правило отнесения, объявленное ДО прогона.
const AttributionRule = `ПРАВИЛО ОТНЕСЕНИЯ (наивная редакция)
1. Величина узла — Actual Rows.
2. Узел относится к отношению по Relation Name.
3. Узлы одного отношения складываются.`

// Access — один доступ к отношению.
type Access struct {
	Relation  string
	NodeType  string
	IndexName string
	Loops     int
	Rows      int64
	Removed   int64
	Collapsed bool
}

// RelationCost — разложение по отношению.
type RelationCost struct {
	Relation string
	Accesses int
	Rows     int64
	Removed  int64
}

// JoinFilter — отброшенное соединением, отнесённое к УЗЛУ.
type JoinFilter struct {
	NodeType string
	Removed  int64
}

// TypeCount — корзина узлов одного типа.
type TypeCount struct {
	NodeType string
	Nodes    int
	Rows     int64
}

// Measurement — снятая величина и объём осмотренного.
type Measurement struct {
	Rows              int64
	Removed           int64
	Touched           int64
	AllRows           int64
	ByRelation        []RelationCost
	Accesses          []Access
	Nodes             int
	Attributed        int
	Unattributed      int
	UnattributedRows  int64
	UnattributedShare float64
	UnknownTypes      []TypeCount
	JoinFilters       []JoinFilter
	JoinFilterRemoved int64
	HeapFetches       int64
	WorkersLaunched   int
	Census            string
}

type rawNode struct {
	NodeType                  string    `json:"Node Type"`
	RelationName              string    `json:"Relation Name"`
	IndexName                 string    `json:"Index Name"`
	ActualRows                float64   `json:"Actual Rows"`
	ActualLoops               float64   `json:"Actual Loops"`
	RowsRemovedByFilter       float64   `json:"Rows Removed by Filter"`
	RowsRemovedByIndexRecheck float64   `json:"Rows Removed by Index Recheck"`
	RowsRemovedByJoinFilter   float64   `json:"Rows Removed by Join Filter"`
	HeapFetches               float64   `json:"Heap Fetches"`
	WorkersLaunched           float64   `json:"Workers Launched"`
	Plans                     []rawNode `json:"Plans"`
}

// Extract снимает величину с плана `EXPLAIN (ANALYZE, ..., FORMAT JSON)`.
func Extract(planJSON []byte, want []string) (Measurement, error) {
	var m Measurement
	var top []struct {
		Plan *rawNode `json:"Plan"`
	}
	if err := json.Unmarshal(planJSON, &top); err != nil {
		return m, fmt.Errorf("planrows: разбор плана: %w", err)
	}
	if len(top) == 0 || top[0].Plan == nil {
		return m, fmt.Errorf("%w: в выводе EXPLAIN нет плана", ErrPreconditionNotMet)
	}

	byRelation := map[string]*RelationCost{}
	var walk func(n *rawNode)
	walk = func(n *rawNode) {
		m.Nodes++
		rows := int64(n.ActualRows)
		m.AllRows += rows
		m.HeapFetches += int64(n.HeapFetches)
		m.WorkersLaunched += int(n.WorkersLaunched)
		if n.RelationName != "" {
			m.Attributed++
			m.Rows += rows
			rc := byRelation[n.RelationName]
			if rc == nil {
				rc = &RelationCost{Relation: n.RelationName}
				byRelation[n.RelationName] = rc
			}
			rc.Accesses++
			rc.Rows += rows
			m.Accesses = append(m.Accesses, Access{
				Relation: n.RelationName, NodeType: n.NodeType,
				IndexName: n.IndexName, Loops: int(n.ActualLoops), Rows: rows,
			})
		} else {
			m.Unattributed++
			m.UnattributedRows += rows
		}
		m.Removed += int64(n.RowsRemovedByFilter) + int64(n.RowsRemovedByIndexRecheck)
		for i := range n.Plans {
			walk(&n.Plans[i])
		}
	}
	walk(top[0].Plan)

	for _, rc := range byRelation {
		m.ByRelation = append(m.ByRelation, *rc)
	}
	sort.Slice(m.ByRelation, func(i, j int) bool {
		return m.ByRelation[i].Relation < m.ByRelation[j].Relation
	})
	m.Touched = m.Rows + m.Removed

	var found bool
	for _, w := range want {
		if _, ok := byRelation[w]; ok {
			found = true
			break
		}
	}
	if !found {
		return m, fmt.Errorf("%w: в плане нет ни одного из ожидаемых отношений %v",
			ErrPreconditionNotMet, want)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nузлов разобрано %d, отнесено %d, не отнесено %d, отношений %d\n",
		strings.TrimSpace(AttributionRule), m.Nodes, m.Attributed, m.Unattributed, len(m.ByRelation))
	for _, rc := range m.ByRelation {
		fmt.Fprintf(&b, "  %-24s доступов %d, строк %d\n", rc.Relation, rc.Accesses, rc.Rows)
	}
	m.Census = b.String()
	return m, nil
}
