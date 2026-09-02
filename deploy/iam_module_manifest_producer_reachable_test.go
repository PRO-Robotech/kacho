// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_producer_reachable_test.go — вызов производителя манифестов
// обязан быть ИСПОЛНИМ на том стенде, откуда он сделан (задача #1901).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ЭТОТ ПРЕДМЕТ ОТЛИЧАЕТСЯ ОТ СОСЕДНЕГО
//
// Соседняя проверка (iam_module_manifest_bringup_paths_test.go) судит, что путь
// выкатки ЗОВЁТ производителя и зовёт его перед helm. Она отвечает на вопрос
// «вызов написан?» и by construction не отвечает на вопрос «вызов ДОЙДЁТ до
// производителя?»: строка вызова на месте, а цель отказывает раньше первой своей
// команды — по предпосылке.
//
// Разница не педантская, и цена измерена. Цель `module-manifests-configmap`
// несла предпосылку `guard-kind-context` — аварийный стоп, отвергающий всякий
// контекст, кроме нашего kind-кластера. Путей выкатки три, и ДВА из них по
// замыслу работают на настоящем кластере:
//
//	deploy/Makefile:stack-up            — прод / prorobotech / a8f60d; он потому и
//	                                      гейтится `guard-destructive`, а НЕ kind-стражем,
//	                                      что рецепт прямо это объявляет;
//	helm/umbrella/cutover-fe3455.sh     — боевая площадка fe3455; скрипт сам пинит
//	                                      апи-сервер площадки перед вызовом.
//
// На обоих вызов производителя ОТКАЗЫВАЛ до единой своей команды, и оба пути
// доставку не заводили никогда. Замер (подставной kubectl, изображающий боевой
// контекст, настоящий рецепт под настоящим make):
//
//	контекст fe3455-prod → «ABORT: активный kube-контекст 'fe3455-prod' — НЕ kind-kacho», код 2
//	контекст kind-kacho  → «ConfigMap "kacho-module-manifests", ключей 6», код 0
//
// Вместе с `manifests.required: true` в `values.prod.yaml` это означало, что
// боевая посадка не поднимается: доставка обязательна, а произвести её на боевом
// пути нельзя.
//
// ─────────────────────────────────────────────────────────────────────────────
// НОРМА, КОТОРУЮ ДЕРЖИТ ЭТА ПРОВЕРКА
//
//	Путь выкатки, который НЕ пинит себя к kind, не вправе звать цель,
//	отвергающую не-kind контекст.
//
// Обратное — законно и обязано молчать: путь, пинящий себя к kind (`dev-up`),
// зовёт ту же цель и получает от её стража ровно то, ради чего страж заведён.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ВЫВОДИТСЯ, А ЧТО ВЫПИСАНО
//
//	kind-стражи        — ВЫВОДЯТСЯ: цель, чей рецепт разрешает активный контекст
//	                     (`current-context`) и сверяет его с именем, начинающимся
//	                     на `kind-`. Сегодня такая цель одна; вторая попадёт в
//	                     перепись сама. Имя стража НЕ выписано — выписанное имя
//	                     разошлось бы с деревом молча.
//	отвергающие цели   — ЗАМЫКАНИЕ по вызовам: цель, чья предпосылка или рецепт
//	                     зовут kind-стража, отвергает чужой контекст сама; и так
//	                     далее по цепочке. Один уровень пропустил бы обёртку.
//	пути выкатки       — те же, что у соседней проверки, и теми же предикатами
//	                     (`helmUpgradeInvocation`, `namesUmbrellaChart`,
//	                     `isCommentLine`, `makefileTargetHeader`): второй экземпляр
//	                     этих предикатов разошёлся бы с первым молча.
//
// ПИНИТ ЛИ ПУТЬ СЕБЯ К kind — судится по ЕГО СОБСТВЕННОМУ стражу (предпосылка
// заголовка либо строка его же рецепта), а НЕ по стражам целей, которые он зовёт.
// Иначе вопрос стал бы круговым: `stack-up` зовёт цель со стражем — значит он
// «пинит себя к kind» — значит находки нет, и проверка не смогла бы найти ровно
// тот дефект, ради которого заведена.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА НАЗВАНА ЧЕСТНО
//
// Проверка судит ОБЪЯВЛЕНИЕ (текст рецептов), а не поведение живого кластера:
// страж, отвергающий чужой контекст не сверкой имени, а, скажем, отказом
// апи-сервера, в вывод не попадёт. Такого стража в дереве сегодня нет — перепись
// печатает, сколько их выведено, — но появится он молча.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// contextResolutionToken — как рецепт РАЗРЕШАЕТ активный контекст.
const contextResolutionToken = "current-context"

// kindContextName — имя контекста, к которому пинится kind-страж. Значение имени
// (`$(CLUSTER_NAME)`) страж подставляет сам; здесь важен только префикс.
var kindContextName = regexp.MustCompile(`kind-[A-Za-z0-9_.$(){}-]+`)

// makefileWord — слово имени цели. Границы обязательны: сверка подстрокой
// оставила бы зелёным переименование в `guard-kind-context-lite`.
var makefileWord = regexp.MustCompile(`[A-Za-z0-9_./-]+`)

// makeInvocation — строка ЗОВЁТ make. Предпосылка вызова цели из рецепта или
// скрипта; без неё за вызов сошло бы любое упоминание имени, включая эхо.
func makeInvocation(line string) bool {
	if strings.Contains(line, "$(MAKE)") {
		return true
	}
	for _, w := range makefileWord.FindAllString(line, -1) {
		if w == "make" {
			return true
		}
	}
	return false
}

// namesTarget — строка называет ИМЕННО эту цель, а не её однокоренную соседку.
func namesTarget(line, target string) bool {
	for _, w := range makefileWord.FindAllString(line, -1) {
		if w == target {
			return true
		}
	}
	return false
}

// makeTarget — цель Makefile: заголовок, предпосылки, рецепт.
type makeTarget struct {
	Carrier string   // путь носителя от корня дерева
	Name    string   // имя цели
	Prereqs []string // предпосылки из заголовка
	Recipe  []string // исполняемые строки рецепта (без строк-комментариев)
}

// reachCensus — объём осмотренного. Печатается ВСЕГДА, на всяком исходе.
type reachCensus struct {
	Carriers    int // носителей осмотрено
	Targets     int // целей Makefile разобрано
	KindGuards  int // kind-стражей ВЫВЕДЕНО
	KindOnly    int // целей, отвергающих чужой контекст (с замыканием)
	Units       int // путей выкатки найдено
	UnitsKind   int // из них пинящих СЕБЯ к kind
	CallsJudged int // вызовов целей Makefile из путей выкатки осмотрено
}

// Summary — перепись одной строкой.
func (c reachCensus) Summary() string {
	return fmt.Sprintf("носителей %d · целей Makefile %d · kind-стражей выведено %d · "+
		"целей, отвергающих чужой контекст %d · путей выкатки %d · из них пинят себя к kind %d · "+
		"вызовов целей осмотрено %d",
		c.Carriers, c.Targets, c.KindGuards, c.KindOnly, c.Units, c.UnitsKind, c.CallsJudged)
}

// parseMakeTargets — цели из всех носителей-Makefile. Функция ЧИСТАЯ: инъекция
// подаёт ей изменённые носители, дерева не трогая.
func parseMakeTargets(carriers []deployCarrier) []makeTarget {
	var out []makeTarget
	for _, c := range carriers {
		if c.Kind != "Makefile" {
			continue
		}
		var cur *makeTarget
		flush := func() {
			if cur != nil {
				out = append(out, *cur)
				cur = nil
			}
		}
		for _, line := range strings.Split(c.Text, "\n") {
			if isCommentLine(line) {
				continue
			}
			if strings.HasPrefix(line, "\t") {
				if cur != nil {
					cur.Recipe = append(cur.Recipe, line)
				}
				continue
			}
			flush()
			m := makefileTargetHeader.FindString(line)
			if m == "" || strings.HasPrefix(strings.TrimSpace(line), ".") {
				continue
			}
			head := strings.SplitN(line, ":", 2)
			name := strings.TrimSpace(head[0])
			var prereqs []string
			if len(head) == 2 {
				prereqs = makefileWord.FindAllString(head[1], -1)
			}
			cur = &makeTarget{Carrier: c.Path, Name: name, Prereqs: prereqs}
		}
		flush()
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Carrier != out[j].Carrier {
			return out[i].Carrier < out[j].Carrier
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// invokes — цель зовёт названную: предпосылкой заголовка либо строкой рецепта,
// зовущей make по имени.
func (t makeTarget) invokes(name string) bool {
	for _, p := range t.Prereqs {
		if p == name {
			return true
		}
	}
	for _, line := range t.Recipe {
		if makeInvocation(line) && namesTarget(line, name) {
			return true
		}
	}
	return false
}

// kindGuards — цели, отвергающие всякий контекст, кроме kind. ВЫВОДЯТСЯ.
func kindGuards(targets []makeTarget) map[string]bool {
	out := map[string]bool{}
	for _, t := range targets {
		body := strings.Join(t.Recipe, "\n")
		if strings.Contains(body, contextResolutionToken) && kindContextName.MatchString(body) {
			out[t.Name] = true
		}
	}
	return out
}

// kindOnlyTargets — замыкание: цель, зовущая kind-стража прямо или через
// обёртку, отвергает чужой контекст сама.
func kindOnlyTargets(targets []makeTarget, guards map[string]bool) map[string]bool {
	out := map[string]bool{}
	for name := range guards {
		out[name] = true
	}
	for grew := true; grew; {
		grew = false
		for _, t := range targets {
			if out[t.Name] {
				continue
			}
			for name := range out {
				if t.invokes(name) {
					out[t.Name] = true
					grew = true
					break
				}
			}
		}
	}
	return out
}

// reachUnit — путь выкатки, судимый на исполнимость своих вызовов.
type reachUnit struct {
	Name       string   // "deploy/Makefile:stack-up" либо путь скрипта
	KindPinned bool     // пинит СЕБЯ к kind (собственный страж)
	Calls      []string // строки, зовущие make (для поиска вызываемых целей)
}

// reachUnits — пути выкатки. Предикаты «что есть вызов helm» и «что есть чарт
// умбреллы» берутся у соседней проверки, а не заводятся вторым экземпляром.
func reachUnits(carriers []deployCarrier, guards map[string]bool) []reachUnit {
	type acc struct {
		helm  bool
		kind  bool
		calls []string
	}
	units := map[string]*acc{}
	var order []string
	unitFor := func(name string) *acc {
		if u, ok := units[name]; ok {
			return u
		}
		u := &acc{}
		units[name] = u
		order = append(order, name)
		return u
	}

	for _, c := range carriers {
		target := ""
		prereqKind := false
		for _, line := range strings.Split(c.Text, "\n") {
			if isCommentLine(line) {
				continue
			}
			unitName := c.Path
			if c.Kind == "Makefile" {
				if !strings.HasPrefix(line, "\t") {
					target = ""
					prereqKind = false
					if m := makefileTargetHeader.FindString(line); m != "" &&
						!strings.HasPrefix(strings.TrimSpace(line), ".") {
						head := strings.SplitN(line, ":", 2)
						target = strings.TrimSpace(head[0])
						if len(head) == 2 {
							for _, p := range makefileWord.FindAllString(head[1], -1) {
								if guards[p] {
									prereqKind = true
								}
							}
						}
					}
					continue
				}
				if target == "" {
					continue
				}
				unitName = c.Path + ":" + target
			}

			if makeInvocation(line) {
				u := unitFor(unitName)
				u.calls = append(u.calls, line)
				for name := range guards {
					if namesTarget(line, name) {
						u.kind = true
					}
				}
			}
			if prereqKind {
				unitFor(unitName).kind = true
			}
			if helmUpgradeInvocation(line) && namesUmbrellaChart(line, c.Path) {
				unitFor(unitName).helm = true
			}
		}
	}

	var out []reachUnit
	for _, name := range order {
		u := units[name]
		if !u.helm {
			continue // умбреллу не катит — путём выкатки не является
		}
		out = append(out, reachUnit{Name: name, KindPinned: u.kind, Calls: u.calls})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// auditProducerReachable — находки: путь выкатки на настоящем кластере зовёт
// цель, отвергающую чужой контекст.
func auditProducerReachable(carriers []deployCarrier) ([]string, reachCensus) {
	census := reachCensus{Carriers: len(carriers)}
	targets := parseMakeTargets(carriers)
	census.Targets = len(targets)

	guards := kindGuards(targets)
	census.KindGuards = len(guards)
	kindOnly := kindOnlyTargets(targets, guards)
	census.KindOnly = len(kindOnly)

	byName := map[string][]makeTarget{}
	for _, t := range targets {
		byName[t.Name] = append(byName[t.Name], t)
	}

	units := reachUnits(carriers, guards)
	census.Units = len(units)

	var findings []string
	for _, u := range units {
		if u.KindPinned {
			census.UnitsKind++
		}
		for _, line := range u.Calls {
			for _, w := range makefileWord.FindAllString(line, -1) {
				if _, known := byName[w]; !known {
					continue
				}
				census.CallsJudged++
				// Самоупоминание вызовом не является: имя цели стоит в её же
				// рецепте (строкой подтверждения, эхом, сообщением отказа), и
				// зачесть его значило бы обвинить путь в вызове самого себя.
				if strings.HasSuffix(u.Name, ":"+w) {
					continue
				}
				if !kindOnly[w] || u.KindPinned || guards[w] {
					continue
				}
				findings = append(findings, fmt.Sprintf(
					"путь выкатки %s зовёт цель %q, которая ОТВЕРГАЕТ всякий контекст, кроме "+
						"kind, — сам он себя к kind не пинит, значит вызов не доходит до первой "+
						"команды цели и доставка не заводится НИКОГДА (kacho#1901). Страж "+
						"принадлежит ВЫЗЫВАЮЩЕМУ: путь на настоящем кластере подтверждает цель "+
						"своим гейтом, а не запрещает себе кластер", u.Name, w))
			}
		}
	}
	sort.Strings(findings)
	return uniqueStrings(findings), census
}

// uniqueStrings — одна находка на пару «путь · цель», сколько бы раз вызов ни
// повторялся в рецепте.
func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// TestManifestProducerIsCallableFromEveryBringUpPath — вызов производителя
// исполним на том стенде, откуда он сделан.
func TestManifestProducerIsCallableFromEveryBringUpPath(t *testing.T) {
	carriers := bringUpCarriers(t)
	findings, census := auditProducerReachable(carriers)
	t.Logf("осмотрено: %s", census.Summary())

	if census.Carriers == 0 {
		t.Fatal("носителей не прочитано ни одного — вердикт беспредметен: " +
			"«ноль находок» здесь означало бы «ноль прочитанного»")
	}
	if census.Targets == 0 {
		t.Fatal("целей Makefile не разобрано ни одной — разбор заголовков перестал " +
			"узнавать свой предмет, а не дерево стало чистым")
	}
	if census.KindGuards == 0 {
		t.Fatal("kind-стражей не выведено ни одного — предпосылка проверки исчезла: " +
			"судить об исполнимости вызова стало нечем")
	}
	if census.Units == 0 {
		t.Fatal("путей выкатки не найдено ни одного — умбреллу чем-то катят, " +
			"значит предпосылка исчезла, а не дерево стало чистым")
	}
	if census.CallsJudged == 0 {
		t.Fatal("вызовов целей Makefile из путей выкатки не осмотрено ни одного — " +
			"проверка не дошла до своего предмета")
	}
	for _, f := range findings {
		t.Error(f)
	}
}
