// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanjumptarget_test.go — буквальный переход между шагами обязан РАЗРЕШАТЬСЯ
// в шаг своей коллекции, и ровно в один.
//
// # Предмет
//
// `pm.execution.setNextRequest('<имя>')` — единственный способ, которым один шаг
// передаёт управление другому. Newman резолвит цель ПО ИМЕНИ внутри прогона: имя,
// которому в коллекции не соответствует ни один шаг, прогон не роняет — он его
// ЗАВЕРШАЕТ. Остаток набора не исполняется, при этом ни одного упавшего
// утверждения не появляется, потому что утверждать стало нечему. По вердикту это
// неотличимо от зелёного, а по существу — третья категория, «не выполнилось»
// (`testing.md` §E2E: «не выполнилось» никогда не вычитается из вердикта и
// никогда не зачитывается в успех). Ровно тем же кончается неоднозначность: два
// шага с одним именем, и переход уходит в первый, а не в тот, который имели в виду.
//
// # Почему гейт заведён вместе с расширением предиката обёртки
//
// Обёртка окна видимости (`_wrap_own_fresh_reads`, соседний гейт
// newmanfreshreadwrap_test.go) ПЕРЕИМЕНОВЫВАЕТ шаг, который оборачивает
// (`<имя>-rya<N>`), — иначе самоповтор резолвился бы не в себя. Условие обёртки
// расширено с адреса шага на адрес И ТЕЛО, и переименованных шагов в
// закоммиченных коллекциях стало 2574 против 2182 — на 392 больше за одно
// изменение. Переименованный шаг, на который кто-то ссылается буквально, и есть
// описанное выше молчаливое усечение — поэтому свойство держится проверкой, а не
// рассуждением о том, что сегодня таких ссылок нет.
//
// Замер на дереве, где гейт заведён: 87 коллекций, 8788 шагов, 72 буквальных
// перехода, 0 неразрешимых. То есть гейт заводится ЗЕЛЁНЫМ на исправном дереве и
// его молчание — утверждение о дереве, а не следствие того, что читать было
// нечего: объём осмотренного он печатает сам.
//
// # Что здесь НЕ проверяется
//
// Задержка между самоповторами (`testing.md` §Newman e2e — busy-wait перед
// `setNextRequest`) — отдельное свойство и отдельный предмет: самоповтор
// (`setNextRequest(pm.info.requestName)`) целью себя резолвит всегда и в находки
// этого гейта не попадает никогда. Он считается в переписи, чтобы «ноль
// буквальных переходов» было отличимо от «распознаватель перехода ослеп».
package artifactgates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestNewmanLiteralJumpResolvesToExactlyOneStep(t *testing.T) {
	root := repoRoot(t)

	// Состав берётся из ИНДЕКСА git, а не обходом диска: под корнем лежат рабочие
	// копии агентов и распаковки отчётов прогонов, и вердикт по ним был бы
	// свойством чужого рабочего каталога, а не коммита (trackedtree_test.go).
	tt := newTrackedTree(t, root)
	var cols []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			cols = append(cols, rel)
		}
	}
	sort.Strings(cols)

	findings, cen, err := auditJumpTargets(root, cols)
	if err != nil {
		t.Fatal(err)
	}

	// ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ.
	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать; " +
			"чинить надо обход, а не выходить успехом")
	}
	if cen.steps == 0 {
		t.Fatalf("прочитано коллекций %d, шагов 0 — обход не узнал ни одного шага", cen.collections)
	}
	// Распознаватель перехода: механизм в дереве присутствует, а разобрано ноль
	// переходов — значит форма сменилась и гейт ослеп. Ноль переходов ПРИ
	// отсутствии механизма — законное состояние, и падать на нём нельзя: это было
	// бы падением на достижении собственной цели.
	if cen.scriptsWithMechanism > 0 && cen.selfLoops+cen.literalJumps == 0 {
		t.Fatalf("в %d скриптах есть setNextRequest, но разобрано 0 переходов — "+
			"распознаватель читает не ту форму", cen.scriptsWithMechanism)
	}

	t.Logf("осмотрено: коллекций %d, шагов %d, скриптов с переходом %d, из них самоповторов %d, буквальных переходов %d",
		cen.collections, cen.steps, cen.scriptsWithMechanism, cen.selfLoops, cen.literalJumps)

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "буквальных переходов, не разрешающихся ровно в один шаг своей коллекции: %d\n", len(findings))
		fmt.Fprintf(&b, "\nNewman резолвит цель перехода ПО ИМЕНИ. Не нашлось — прогон ЗАВЕРШАЕТСЯ на этом шаге:\n")
		fmt.Fprintf(&b, "остаток набора не исполняется, упавших утверждений при этом ноль, и вердикт выглядит зелёным.\n")
		fmt.Fprintf(&b, "Нашлось несколько — переход уходит в первый одноимённый, а не в тот, который имели в виду.\n")
		fmt.Fprintf(&b, "Частая причина: шаг-цель переименован проходом обёртки окна видимости (`<имя>-rya<N>`),\n")
		fmt.Fprintf(&b, "а ссылка на него осталась под прежним именем.\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}

// nmJumpCensus — объём осмотренного. Печатается вместе с вердиктом, чтобы «ноль
// находок» было отличимо от «ноль прочитанного».
type nmJumpCensus struct {
	collections          int
	steps                int
	scriptsWithMechanism int
	selfLoops            int
	literalJumps         int
}

// nmJumpFinding — координата находки: гейт, который лишь считает, чинить нечем.
type nmJumpFinding struct {
	collection string
	folder     string
	step       string
	target     string
	seen       int
}

func (f nmJumpFinding) String() string {
	what := "цели с таким именем в коллекции нет"
	if f.seen > 1 {
		what = fmt.Sprintf("имя носят %d шага(ов) — переход неоднозначен", f.seen)
	}
	return fmt.Sprintf("%s :: %s :: %s → setNextRequest(%q): %s",
		f.collection, f.folder, f.step, f.target, what)
}

// nmJumpLiteralRe — переход с БУКВАЛЬНЫМ именем цели, в обеих кавычках.
// Самоповтор (`pm.info.requestName`) под неё не подпадает по построению: там нет
// строкового литерала, и это ровно то различие, которое гейту нужно.
var nmJumpLiteralRe = regexp.MustCompile(`setNextRequest\(\s*(?:'([^']*)'|"([^"]*)")\s*\)`)

// nmJumpSelfRe — самоповтор. Считается только в переписи.
var nmJumpSelfRe = regexp.MustCompile(`setNextRequest\(\s*pm\.info\.requestName\s*\)`)

// auditJumpTargets — весь разбор одним входом: чтение коллекций, обход папок,
// сбор имён шагов, находки и перепись.
//
// Вынесено из тела гейта намеренно: проба, доказывающая способность гейта упасть,
// обязана гонять ТУ ЖЕ функцию — иначе она доказывала бы свойство своей копии.
func auditJumpTargets(root string, cols []string) ([]nmJumpFinding, nmJumpCensus, error) {
	var findings []nmJumpFinding
	var cen nmJumpCensus

	for _, rel := range cols {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, cen, fmt.Errorf("чтение %s: %w", rel, err)
		}
		var col nmCollection
		if err := json.Unmarshal(b, &col); err != nil {
			return nil, cen, fmt.Errorf("разбор %s: %w", rel, err)
		}
		cen.collections++

		// Первый проход — перепись имён шагов коллекции. Именно по ним newman и
		// резолвит цель, поэтому считать надо шаги, а не папки: у папки имя есть,
		// а перейти в неё нельзя.
		names := map[string]int{}
		var collect func(items []nmItem)
		collect = func(items []nmItem) {
			for _, it := range items {
				if it.isFolder() {
					collect(it.Item)
					continue
				}
				names[it.Name]++
				cen.steps++
			}
		}
		collect(col.Item)

		// Второй проход — переходы.
		var scan func(folder string, items []nmItem)
		scan = func(folder string, items []nmItem) {
			for _, it := range items {
				if it.isFolder() {
					name := folder
					if it.Name != "" {
						name = it.Name
					}
					scan(name, it.Item)
					continue
				}
				for _, ev := range it.Event {
					src := strings.Join(ev.Script.Exec, "\n")
					if !strings.Contains(src, "setNextRequest") {
						continue
					}
					cen.scriptsWithMechanism++
					cen.selfLoops += len(nmJumpSelfRe.FindAllString(src, -1))
					for _, m := range nmJumpLiteralRe.FindAllStringSubmatch(src, -1) {
						target := m[1]
						if target == "" {
							target = m[2]
						}
						cen.literalJumps++
						if seen := names[target]; seen != 1 {
							findings = append(findings, nmJumpFinding{
								collection: filepath.Base(rel), folder: folder,
								step: it.Name, target: target, seen: seen,
							})
						}
					}
				}
			}
		}
		scan("", col.Item)
	}
	return findings, cen, nil
}
