// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package standns

import (
	"fmt"
	"slices"
	"strings"
)

// GoDepsRecord — один вызов GoDeps и его исход: замыкание пакетов mains контекста ctx.
//
// Зачем запись (kacho#3353). Замыкание выводит `go list -deps`, а Go на машине оператора
// не запускается (решение владельца 2026-10-22): его исполняет pod сборки инструментов во
// внешнем кластере над тем же деревом и пишет записи, а суд входов на машине (`stand-ns
// inputs -godeps`) читает их вместо вызова Go. Ключ — контекст и mains ровно в том виде, в
// каком их спрашивает BuildClosure: дерево то же, значит и вопросы те же.
type GoDepsRecord struct {
	Ctx   string   `json:"ctx"`
	Mains []string `json:"mains"`
	Files []string `json:"files"`
}

func goDepsKey(ctx string, mains []string) string {
	return ctx + "\x00" + strings.Join(mains, "\x00")
}

// RecordGoDeps — GoDeps, отвечающий как deps и дописывающий каждый успешный ответ в out.
func RecordGoDeps(deps GoDeps, out *[]GoDepsRecord) GoDeps {
	return func(ctx string, mains []string) ([]string, error) {
		got, err := deps(ctx, mains)
		if err == nil {
			*out = append(*out, GoDepsRecord{Ctx: ctx, Mains: slices.Clone(mains), Files: slices.Clone(got)})
		}
		return got, err
	}
}

// ReplayGoDeps — GoDeps по записям. Вопроса, которого нет в записях, и вопроса с двумя
// разными ответами — отказ: «не записано» не равно «замыкание пусто», и второго ответа о
// том же замыкании суд не выбирает.
func ReplayGoDeps(recs []GoDepsRecord) GoDeps {
	by := map[string][]string{}
	conflict := map[string]bool{}
	for _, r := range recs {
		k := goDepsKey(r.Ctx, r.Mains)
		if prev, ok := by[k]; ok && !slices.Equal(prev, r.Files) {
			conflict[k] = true
		}
		by[k] = r.Files
	}
	return func(ctx string, mains []string) ([]string, error) {
		k := goDepsKey(ctx, mains)
		if conflict[k] {
			return nil, fmt.Errorf("замыкание %s контекста %q записано дважды с разным составом", strings.Join(mains, " "), ctx)
		}
		got, ok := by[k]
		if !ok {
			return nil, fmt.Errorf("замыкание %s контекста %q в записи сборки нет — дерево записи не это дерево", strings.Join(mains, " "), ctx)
		}
		return slices.Clone(got), nil
	}
}
