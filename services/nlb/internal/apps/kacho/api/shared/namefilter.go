// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/filter"

	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// nameField is the only whitelisted filter field in the current NLB API surface
// (filter — kacho-corelib/filter.Parse с whitelist полей; текущий whitelist — name=).
const nameField = "name"

// ParseNameFilter parses the request `filter` string and returns the parsed
// NODE, or nil when no filter is set.
//
// It is the single source of truth for `name` filtering across all NLB List
// use-cases (NetworkLoadBalancer / TargetGroup / Listener), replacing three
// divergent local parsers. It delegates to kacho-corelib/filter.Parse so the
// grammar and error texts match every other Kachō service.
//
// Возвращается УЗЕЛ, а не его значение (#460). Прежняя редакция отдавала
// `ast.Value` — одну строку, — и вместе с ней теряла ОПЕРАТОР: репозиторий
// строил предикат с зашитым равенством, поэтому `name CONTAINS "we"` молча
// читался как `name = "we"`. Поиск по части имени отвечал «ничего не найдено»
// на любой неполный ввод, и это было неотличимо от пустого результата.
//
// Contract:
//   - empty input            → (nil, nil)           // no filter
//   - name="value"           → (узел с оператором `=`, nil)
//   - name CONTAINS "value"  → (узел с оператором CONTAINS, nil)
//   - unknown field / unquoted / malformed → InvalidArgument
//
// A malformed or unknown-field filter is a client error → InvalidArgument
// (never silently dropped, which would widen the result set unexpectedly).
func ParseNameFilter(raw string) (*kachorepo.NameFilter, error) {
	ast, err := filter.Parse(raw, []string{nameField})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return ast, nil // nil → no name predicate
}
