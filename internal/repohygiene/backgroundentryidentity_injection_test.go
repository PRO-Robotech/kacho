// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// backgroundentryidentity_injection_test.go — гейт УК3-31 доказан инъекцией в
// обе стороны на СИНТЕТИЧЕСКОМ дереве (`t.TempDir()`), а не на живой записи
// перечня: у каждой половины — дефект, который краснеет и называет координату,
// и законный близнец той же формы, который молчит. Каждая инъекция меняет
// ровно один факт против близнеца.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const beiRunnerTmpl = `package jobs

import (
	"context"

	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/operations"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
)

const (
	componentService = "nlb"
	componentRole    = "free-ip-runner"
)

type FreeIPRunner struct{ addrs vpcclient.Client }

func (r *FreeIPRunner) reconcileOne(ctx context.Context) error {
	{{SET}}
	return r.release(ctx)
}

func (r *FreeIPRunner) release(ctx context.Context) error {
	{{OVERRIDE}}
	return r.addrs.{{METHOD}}(ctx, "adr-1")
}

var _ = operations.SystemPrincipal
var _ = journaltx.AsComponent
`

const beiClientTmpl = `package vpc

import (
	"context"

	"github.com/PRO-Robotech/corelib/auth"
)

type Client interface {
	Release(ctx context.Context, id string) error
	Read(ctx context.Context, id string) error
}

type stub interface {
	ReleaseOwnedAddress(ctx context.Context, id string) error
	GetAddress(ctx context.Context, id string) error
}

type client struct{ internal stub }

func (c *client) Release(ctx context.Context, id string) error {
	{{RELEASE}}
}

func (c *client) Read(ctx context.Context, id string) error {
	return c.internal.GetAddress(auth.PropagateOutgoing(ctx), id)
}
`

const (
	beiSetComponent  = `ctx, err := journaltx.AsComponent(ctx, componentService, componentRole); _ = err`
	beiReleaseInline = `return c.internal.ReleaseOwnedAddress(auth.PropagateOutgoing(ctx), id)`
)

type beiSynth struct {
	set, override, method, release string
}

func (s beiSynth) build(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	r := strings.NewReplacer("{{SET}}", s.set, "{{OVERRIDE}}", s.override, "{{METHOD}}", s.method)
	c := strings.NewReplacer("{{RELEASE}}", s.release)
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	write("services/nlb/internal/apps/kacho/jobs/runner.go", r.Replace(beiRunnerTmpl))
	write("services/nlb/internal/clients/vpc/client.go", c.Replace(beiClientTmpl))
	return root
}

var beiSynthEntry = []BackgroundEntry{{Module: "nlb", Dir: "services/nlb/internal/apps/kacho/jobs", Recv: "FreeIPRunner", Func: "reconcileOne"}}

func beiTwin() beiSynth {
	return beiSynth{set: beiSetComponent, method: "Release", release: beiReleaseInline}
}

func beiRun(t *testing.T, s beiSynth, ex []BackgroundEntryException, states map[int]string) ([]BackgroundEntryFinding, []BackgroundEntryCensus) {
	t.Helper()
	if ex == nil {
		ex = []BackgroundEntryException{}
	}
	var log strings.Builder
	f, c, err := AuditBackgroundEntryIdentity(BackgroundEntryOptions{Root: s.build(t), Entries: beiSynthEntry,
		Exceptions: ex, IssueStates: states}, &log)
	require.NoError(t, err)
	t.Log("\n" + log.String())
	return f, c
}

func beiHalves(fs []BackgroundEntryFinding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Half)
	}
	return out
}

func TestBackgroundEntryInjection_Halves(t *testing.T) {
	t.Parallel()

	t.Run("близнец: компонент из таблицы, установка не перекрыта — молчит", func(t *testing.T) {
		f, c := beiRun(t, beiTwin(), nil, nil)
		require.Empty(t, f)
		require.Empty(t, BackgroundEntryPremiseFailures(c))
		require.Equal(t, 1, c[0].Writes(), "близнец обязан достигать пишущего вызова — иначе молчание беспредметно")
	})

	t.Run("(1) снятая установка AsComponent на входе — находка с координатой вызова", func(t *testing.T) {
		s := beiTwin()
		s.set = ""
		f, _ := beiRun(t, s, nil, nil)
		require.Equal(t, []string{BEIHalfNoPrincipal}, beiHalves(f))
		require.True(t, strings.HasPrefix(f[0].Pos, "services/nlb/internal/clients/vpc/client.go:"), f[0].Pos)
		require.Contains(t, f[0].Detail, "ReleaseOwnedAddress")
		require.Contains(t, f[0].Detail, "без явного принципала")
	})

	t.Run("(2) WithPrincipal(SystemPrincipal()) ниже установки — находка, называет место установки", func(t *testing.T) {
		s := beiTwin()
		s.override = `ctx = operations.WithPrincipal(ctx, operations.SystemPrincipal())`
		f, _ := beiRun(t, s, nil, nil)
		require.Equal(t, []string{BEIHalfSystemPrincipal}, beiHalves(f))
		require.Contains(t, f[0].Detail, "установлено services/nlb/internal/apps/kacho/jobs/runner.go:")
	})

	t.Run("(2) близнец: WithPrincipal(auth.SystemPrincipalFor(пара таблицы)) — молчит", func(t *testing.T) {
		s := beiTwin()
		s.set = ""
		s.override = `ctx = operations.WithPrincipal(ctx, auth.SystemPrincipalFor("nlb", "free-ip-runner"))`
		root := s.build(t)
		// импорт auth в файл задания — только для этого случая.
		p := filepath.Join(root, "services/nlb/internal/apps/kacho/jobs/runner.go")
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(p, []byte(strings.Replace(string(b),
			`"context"`, "\"context\"\n\t\"github.com/PRO-Robotech/corelib/auth\"", 1)), 0o644))
		f, c, err := AuditBackgroundEntryIdentity(BackgroundEntryOptions{Root: root, Entries: beiSynthEntry,
			Exceptions: []BackgroundEntryException{}}, nil)
		require.NoError(t, err)
		require.Empty(t, f)
		require.Equal(t, 1, c[0].Writes())
	})

	t.Run("(3) пара вне таблицы фоновых путей — находка", func(t *testing.T) {
		s := beiTwin()
		s.set = `ctx, err := journaltx.AsComponent(ctx, "nlb", "stray-runner"); _ = err`
		f, _ := beiRun(t, s, nil, nil)
		require.Equal(t, []string{BEIHalfUndeclaredPair}, beiHalves(f))
		require.Contains(t, f[0].Detail, "(nlb, stray-runner)")
	})

	t.Run("чтение без принципала — молчит, но считается", func(t *testing.T) {
		s := beiTwin()
		s.set = ""
		s.method = "Read"
		f, c := beiRun(t, s, nil, nil)
		require.Empty(t, f)
		require.Len(t, c[0].Reaches, 1)
		require.False(t, c[0].Reaches[0].Write)
		require.NotEmpty(t, BackgroundEntryPremiseFailures(c), "вход без пишущих вызовов — беспредметен, а не зелёный")
	})

	t.Run("(1) форма присваивания результата PropagateOutgoing — находка", func(t *testing.T) {
		s := beiTwin()
		s.set = ""
		s.release = "ctx = auth.PropagateOutgoing(ctx)\n\treturn c.internal.ReleaseOwnedAddress(ctx, id)"
		f, _ := beiRun(t, s, nil, nil)
		require.Equal(t, []string{BEIHalfNoPrincipal}, beiHalves(f))
	})
}

func TestBackgroundEntryInjection_ExceptionLedger(t *testing.T) {
	t.Parallel()
	row := BackgroundEntryException{Entry: "nlb.reconcileOne", Half: BEIHalfNoPrincipal, Issue: 77,
		Why: "синтетическая причина", Removal: "#77 закрыт"}
	noSet := beiTwin()
	noSet.set = ""

	t.Run("строка поглощает находку своей половины — молчит, исключение печатается в объёме", func(t *testing.T) {
		f, c := beiRun(t, noSet, []BackgroundEntryException{row}, map[int]string{77: "OPEN"})
		require.Empty(t, f)
		require.Len(t, c[0].Exempted, 1)
	})

	t.Run("строка не поглощает чужую половину — находка (2) остаётся", func(t *testing.T) {
		s := beiTwin()
		s.override = `ctx = operations.WithPrincipal(ctx, operations.SystemPrincipal())`
		f, _ := beiRun(t, s, []BackgroundEntryException{row}, nil)
		require.ElementsMatch(t, []string{BEIHalfSystemPrincipal, BEIExceptionRule}, beiHalves(f),
			"находка (2) остаётся, а строка (1) без поглощённого — истекла")
	})

	t.Run("предикат снятия выполнен (задача закрыта), строка стоит — находка", func(t *testing.T) {
		f, _ := beiRun(t, noSet, []BackgroundEntryException{row}, map[int]string{77: "CLOSED"})
		require.Equal(t, []string{BEIExceptionRule}, beiHalves(f))
		require.Contains(t, f[0].Detail, "#77 закрыта")
	})

	t.Run("строка без предмета (исключать нечего) — находка", func(t *testing.T) {
		f, _ := beiRun(t, beiTwin(), []BackgroundEntryException{row}, nil)
		require.Equal(t, []string{BEIExceptionRule}, beiHalves(f))
		require.Contains(t, f[0].Detail, "не поглотила")
	})

	t.Run("строка на вход вне перечня — находка", func(t *testing.T) {
		stray := row
		stray.Entry = "compute.FinishStuckDeletes"
		f, _ := beiRun(t, beiTwin(), []BackgroundEntryException{stray}, nil)
		require.Equal(t, []string{BEIExceptionRule}, beiHalves(f))
		require.Contains(t, f[0].Detail, "вне перечня")
	})

	t.Run("решение о предикате: открыта / неизвестно — молчит, закрыта — находка", func(t *testing.T) {
		ex := []BackgroundEntryException{row}
		require.Empty(t, BackgroundExceptionsWhoseRemovalHolds(ex, map[int]string{77: "OPEN"}))
		require.Empty(t, BackgroundExceptionsWhoseRemovalHolds(ex, nil))
		require.Len(t, BackgroundExceptionsWhoseRemovalHolds(ex, map[int]string{77: "CLOSED"}), 1)
	})
}

func TestBackgroundEntryInjection_Premise(t *testing.T) {
	t.Parallel()
	t.Run("пустой перечень входов — беспредметность", func(t *testing.T) {
		_, c, err := AuditBackgroundEntryIdentity(BackgroundEntryOptions{Root: beiTwin().build(t),
			Entries: []BackgroundEntry{}, Exceptions: []BackgroundEntryException{}}, nil)
		require.NoError(t, err)
		require.NotEmpty(t, BackgroundEntryPremiseFailures(c))
	})
	t.Run("функции входа в дереве нет — беспредметность и находка", func(t *testing.T) {
		f, c, err := AuditBackgroundEntryIdentity(BackgroundEntryOptions{Root: beiTwin().build(t),
			Entries:    []BackgroundEntry{{Module: "nlb", Dir: "services/nlb/internal/apps/kacho/jobs", Recv: "FreeIPRunner", Func: "gone"}},
			Exceptions: []BackgroundEntryException{}}, nil)
		require.NoError(t, err)
		require.NotEmpty(t, f)
		require.NotEmpty(t, BackgroundEntryPremiseFailures(c))
	})
	t.Run("модуля нет — ноль прочитанного, а не ноль находок", func(t *testing.T) {
		_, c, err := AuditBackgroundEntryIdentity(BackgroundEntryOptions{Root: t.TempDir(), Entries: beiSynthEntry,
			Exceptions: []BackgroundEntryException{}}, nil)
		require.NoError(t, err)
		require.NotEmpty(t, BackgroundEntryPremiseFailures(c))
	})
}
