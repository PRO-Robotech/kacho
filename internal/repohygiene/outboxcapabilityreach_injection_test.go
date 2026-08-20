// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// outboxcapabilityreach_injection_test.go — ИНЪЕКЦИЯ: обе стороны гоняют ТУ ЖЕ
// функцию `ocrScan`, что и гейт по дереву.
//
// Дефект и три законных случая отличаются друг от друга РОВНО одним свойством
// способности, и все четыре живут в одном синтетическом дереве. Без стороны
// «молчит» гейт был бы неотличим от проверки «у типа есть экспортируемые методы»
// и краснел бы на каждом; без стороны «краснеет» он утверждал бы свойство,
// которого не проверяет.
//
// Законных близнецов ТРИ, а не один, потому что молчание достигается тремя
// разными путями, и каждый надо предъявить отдельно: исполняется снаружи ·
// является внутренней ступенью · сам является петлёй-исполнителем. Одного
// близнеца хватило бы, чтобы гейт «не краснел на всём», но не хватило бы, чтобы
// показать, что критерий (б) вообще работает.
//
// Четвёртая проба — отрицательный контроль ПРЕДПОСЫЛКИ: дерево, где
// композиционный корень ничего из семьи не конструирует, обязано давать нулевую
// перепись, а не тихий успех. Гейт по дереву на такой переписи падает.

import (
	"os"
	"path/filepath"
	"testing"
)

// ocrSynthCorpus — состав СИНТЕТИЧЕСКОГО дерева. Оно репозиторием не является,
// индекса у него нет, поэтому обход диска здесь не откат, а единственный
// возможный авторитет (та же оговорка, что у treecorpus.SyntheticTree). Гейт по
// дереву состав у диска не берёт НИКОГДА — он берёт его у git ls-files.
func ocrSynthCorpus(t *testing.T, root string) ocrCorpus {
	t.Helper()
	var files []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход синтетического дерева %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Fatalf("синтетическое дерево %s пусто — инъекция ничего не доказывает", root)
	}
	return ocrNewCorpus(root, files)
}

// ocrPkgSrc — пакет семьи с четырьмя способностями на одном конструируемом типе.
//
//	Driven   — зовут снаружи            → молчит по (а)
//	Step     — зовут изнутри, из Loop   → молчит по (б)
//	Loop     — зовут снаружи            → молчит по (а)
//	Stranded — не зовёт никто           → НАХОДКА
const ocrPkgSrc = `package widget

import "context"

type Widget struct{}

func New() (*Widget, error) { return &Widget{}, nil }

func (w *Widget) Driven(ctx context.Context) error { return nil }

func (w *Widget) Step(ctx context.Context) error { return nil }

func (w *Widget) Loop(ctx context.Context) error { return w.Step(ctx) }

func (w *Widget) Stranded(ctx context.Context) error { return nil }
`

// ocrRootSrc — композиционный корень: конструирует тип и исполняет две из
// четырёх способностей.
const ocrRootSrc = `package main

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/outbox/widget"
)

func main() {
	w, _ := widget.New()
	ctx := context.Background()
	_ = w.Driven(ctx)
	_ = w.Loop(ctx)
}
`

// ocrRootNoCtorSrc — корень, который пакет семьи ИМПОРТИРУЕТ, но ничего из него
// не конструирует. Отрицательный контроль распознавания.
const ocrRootNoCtorSrc = `package main

import "github.com/PRO-Robotech/kacho/pkg/outbox/widget"

var _ = widget.Widget{}

func main() {}
`

func ocrSynthTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// ocrFullTree — дерево, где есть и дефект, и все три законные формы.
func ocrFullTree(t *testing.T) ocrResult {
	t.Helper()
	root := ocrSynthTree(t, map[string]string{
		"pkg/outbox/widget/widget.go": ocrPkgSrc,
		"services/x/cmd/x/main.go":    ocrRootSrc,
	})
	res, err := ocrScan(ocrSynthCorpus(t, root))
	if err != nil {
		t.Fatalf("разбор синтетического дерева: %v", err)
	}
	return res
}

// Сторона дефекта: способность без исполнителя названа поимённо, с координатой.
func TestOutboxCapabilityGateRedOnACapabilityNobodyDrives(t *testing.T) {
	res := ocrFullTree(t)

	if len(res.Findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(res.Findings), res.Findings)
	}
	got := res.Findings[0]
	if got.Method != "Stranded" {
		t.Fatalf("гейт назвал не ту способность: %s", got)
	}
	if got.File != "pkg/outbox/widget/widget.go" || got.Line == 0 {
		t.Fatalf("находка обязана нести координату, получено %s:%d", got.File, got.Line)
	}
}

// Законный близнец 1 и 3: исполняется снаружи — гейт молчит.
// Законный близнец 2: внутренняя ступень — гейт молчит по критерию (б).
//
// Оба утверждения в одной пробе намеренно: они про ОДИН прогон одного дерева, и
// разнесение их по двум пробам скрыло бы, что молчание достигнуто разными путями
// ОДНОВРЕМЕННО — то есть ровно то, ради чего критерий сделан двойным.
func TestOutboxCapabilityGateSilentOnAllThreeLegitimateShapes(t *testing.T) {
	res := ocrFullTree(t)

	for _, f := range res.Findings {
		if f.Method != "Stranded" {
			t.Fatalf("гейт покраснел на законной форме: %s", f)
		}
	}
	if res.Capabilities != 4 {
		t.Fatalf("ожидалось 4 способности у конструируемого типа, получено %d", res.Capabilities)
	}
	if res.SilentOutside != 2 {
		t.Fatalf("ожидалось 2 способности, исполняемые снаружи (Driven, Loop), получено %d",
			res.SilentOutside)
	}
	if res.SilentInside != 1 {
		t.Fatalf("ожидалась 1 внутренняя ступень (Step), получено %d — критерий (б) не работает, "+
			"и гейт покраснел бы на законной форме вроде metrics.Collector.Scan", res.SilentInside)
	}
}

// Отрицательный контроль ПРЕДПОСЫЛКИ: импорт без конструирования наблюдателем не
// делает. Перепись обязана показать ноль конструируемых типов — тогда гейт по
// дереву падает на предпосылке, а не отчитывается тихим успехом.
func TestOutboxCapabilityGatePremiseEmptyWhenNothingIsConstructed(t *testing.T) {
	root := ocrSynthTree(t, map[string]string{
		"pkg/outbox/widget/widget.go": ocrPkgSrc,
		"services/x/cmd/x/main.go":    ocrRootNoCtorSrc,
	})
	res, err := ocrScan(ocrSynthCorpus(t, root))
	if err != nil {
		t.Fatalf("разбор синтетического дерева: %v", err)
	}
	if res.Packages != 1 || res.RootFiles != 1 {
		t.Fatalf("синтетическое дерево не прочитано: пакетов %d, файлов корня %d",
			res.Packages, res.RootFiles)
	}
	if res.Constructed != 0 {
		t.Fatalf("импорт без вызова конструктора засчитан за конструирование: %d", res.Constructed)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("находки на дереве без конструируемых типов: %v", res.Findings)
	}
}
