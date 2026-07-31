// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package softopengate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// filterRoots — все шесть фильтров сужения страницы. Список перечислен ЦЕЛИКОМ, а не
// образцом: утверждение «у остальных так же» проверяется перечислением, иначе это
// догадка. iam и registry входят, хотя мягкого прохода у них нет вовсе — именно это и
// подтверждается их нулём.
func filterRoots(t *testing.T) []string {
	t.Helper()
	repo := repoRoot(t)
	roots := []string{
		"services/compute/internal/authzfilter",
		"services/nlb/internal/authzfilter",
		"services/storage/internal/authzfilter",
		"services/vpc/internal/authzfilter",
		"services/iam/internal/authzfilter",
		"services/registry/internal/handler",
	}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		p := filepath.Join(repo, r)
		st, err := os.Stat(p)
		require.NoError(t, err, "%s не найден — гейт нацелен не на то дерево, и его «чисто» ничего не значит", r)
		require.True(t, st.IsDir(), "%s не директория", r)
		out = append(out, p)
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if filepath.Dir(dir) == dir {
			t.Fatal("go.mod не найден вверх по дереву")
		}
	}
}

// TestSoftOpenPassesAreObservable — предмет гейта на реальном дереве.
func TestSoftOpenPassesAreObservable(t *testing.T) {
	rep, err := Run(filterRoots(t))
	require.NoError(t, err)
	t.Log(rep.Census())

	// Перепись — отдельное утверждение: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	assert.Greater(t, rep.Files, 0, "гейт не прочитал ни одного файла")
	assert.Greater(t, rep.SwitchReads, 0, "ни одна ветка не читает ручку мягкого прохода")
	assert.Greater(t, rep.SoftPasses, 0, "гейт не осудил ни одного мягкого прохода")

	assert.Empty(t, rep.PremiseNotes, "предпосылка гейта перестала держаться")
	assert.Empty(t, rep.Findings, strings.Join(rep.Findings, "\n"))
}

// TestGateIsRedOnAnUnobservableSoftPass — ИНЪЕКЦИЯ №1: возвращаем дефект, гейт обязан
// покраснеть И НАЗВАТЬ КООРДИНАТУ.
func TestGateIsRedOnAnUnobservableSoftPass(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "filter.go", `package p

type cfg struct{ FailOpen bool }
type F struct{ cfg cfg }

func (f *F) handleErr(ids []string, err error) ([]string, error) {
	if f.cfg.FailOpen {
		// Мягкий проход: страница уходит целиком. Считаем и пишем предупреждение.
		// Комментарий описывает и счётчик, и запись — но их здесь НЕТ.
		return ids, nil
	}
	return nil, err
}
`)
	rep, err := Run([]string{dir})
	require.NoError(t, err)
	require.Len(t, rep.Findings, 1, rep.Census())
	assert.Contains(t, rep.Findings[0], "filter.go:7:2", "находка обязана назвать координату — файл, строку и колонку")
	assert.Contains(t, rep.Findings[0], "handleErr")
	assert.Contains(t, rep.Findings[0], "neither logged nor counted")
	assert.False(t, rep.OK())
}

// TestGateIsSilentOnALegitimateRefusal — ИНЪЕКЦИЯ №2 (обратная сторона): ЗАКОННАЯ
// конструкция ТОЙ ЖЕ ФОРМЫ — загрузочный страж, читающий ту же ручку и ОТКАЗЫВАЮЩИЙ,
// — обязана пройти молча. Без этой пары гейт ловил бы форму, а не существо, и первый
// же ложный срабат его бы отключил: он краснел бы ровно на тех стражах, которые и
// делают мягкий проход переживаемым.
func TestGateIsSilentOnALegitimateRefusal(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "validate.go", `package p

import "fmt"

type conf struct{ ListFilterFailOpen bool }

func requireListFilter(c conf, scopeFiltered []string) error {
	if c.ListFilterFailOpen {
		return fmt.Errorf("production requires fail-open=false (%d scope-filtered RPC)", len(scopeFiltered))
	}
	return nil
}
`)
	rep, err := Run([]string{dir})
	require.NoError(t, err)
	assert.Empty(t, rep.Findings, "отказ — не проход; гейт обязан молчать")
	assert.Equal(t, 1, rep.SwitchReads, "ветка увидена")
	assert.Equal(t, 0, rep.SoftPasses)
	assert.Equal(t, 1, rep.Refusals, "и классифицирована как отказ")
	assert.True(t, rep.OK())
}

// TestGateIsSilentOnAnObservableSoftPass — вторая законная конструкция той же формы:
// мягкий проход, который НАЗВАН и ПОСЧИТАН, в том числе когда наблюдаемость живёт в
// делегате. Иначе гейт требовал бы писать всё в одной ветке.
func TestGateIsSilentOnAnObservableSoftPass(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "filter.go", `package p

import (
	"log/slog"
	"sync/atomic"
)

type cfg struct{ FailOpen bool }
type F struct {
	cfg    cfg
	logger *slog.Logger
	passes atomic.Uint64
}

func (f *F) handleErr(ids []string, err error) ([]string, error) {
	if f.cfg.FailOpen {
		return f.openPass(ids, err)
	}
	return nil, err
}

func (f *F) openPass(ids []string, err error) ([]string, error) {
	total := f.passes.Add(1)
	f.logger.Warn("unfiltered page returned", "error", err, "total", total)
	return ids, nil
}
`)
	rep, err := Run([]string{dir})
	require.NoError(t, err)
	assert.Empty(t, rep.Findings, "наблюдаемый проход — не находка")
	assert.Equal(t, 1, rep.SoftPasses, "и он именно проход, а не отказ")
	assert.True(t, rep.OK())
}

// TestGateRefusesToVouchForAnEmptyWalk — гейт несёт проверку СВОЕЙ предпосылки: «ноль
// находок» на дереве, где нечего было находить, обязано читаться как непроверенное.
func TestGateRefusesToVouchForAnEmptyWalk(t *testing.T) {
	t.Run("nothing_parsed", func(t *testing.T) {
		rep, err := Run([]string{t.TempDir()})
		require.NoError(t, err)
		assert.Empty(t, rep.Findings)
		assert.False(t, rep.OK(), "пустой обход не бывает чистым результатом")
		require.NotEmpty(t, rep.PremiseNotes)
		assert.Contains(t, rep.PremiseNotes[0], "examined nothing")
	})
	t.Run("knob_renamed_away", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "filter.go", `package p

type cfg struct{ SoftPassOnPeerError bool }
type F struct{ cfg cfg }

func (f *F) handleErr(ids []string, err error) ([]string, error) {
	if f.cfg.SoftPassOnPeerError {
		return ids, nil
	}
	return nil, err
}
`)
		rep, err := Run([]string{dir})
		require.NoError(t, err)
		assert.Empty(t, rep.Findings)
		assert.False(t, rep.OK(), "переименованная ручка делает прогон бездоказательным")
		require.NotEmpty(t, rep.PremiseNotes)
		assert.Contains(t, rep.PremiseNotes[0], "premise no longer holds")
	})
}

// TestGateReadsCodeNotComments — запись и счётчик, УПОМЯНУТЫЕ в комментарии, но не
// вызванные, не спасают ветку. Комментарий рядом с такой веткой — обычное дело именно
// потому, что он объясняет удалённый вызов.
func TestGateReadsCodeNotComments(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "filter.go", `package p

type cfg struct{ FailOpen bool }
type F struct{ cfg cfg }

func (f *F) handleErr(ids []string, err error) ([]string, error) {
	if f.cfg.FailOpen {
		// f.logger.Warn("unfiltered page returned", "error", err)
		// f.passes.Add(1)
		return ids, nil
	}
	return nil, err
}
`)
	rep, err := Run([]string{dir})
	require.NoError(t, err)
	require.Len(t, rep.Findings, 1, "закомментированные вызовы — не вызовы")
	assert.Contains(t, rep.Findings[0], "neither logged nor counted")
}

func write(t *testing.T, dir, name, src string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
}
