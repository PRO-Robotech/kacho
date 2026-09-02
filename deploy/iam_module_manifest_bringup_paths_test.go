// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_bringup_paths_test.go — КАЖДЫЙ путь выкатки умбреллы зовёт
// производителя манифестов, и зовёт его ПЕРЕД helm (задачи #1901, #1909).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Под монтирует именованный ConfigMap с манифестами модулей, а процесс читает
// смонтированный каталог на старте и на пустом ОТКАЗЫВАЕТСЯ подниматься:
// отсутствие манифеста снятием модуля не является (#1027). Значит объект обязан
// существовать ДО первого прогона helm на всяком пути выкатки, а не появиться
// после него.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПОПУЛЯЦИЯ ВЫВОДИТСЯ, А НЕ ВЫПИСАНА — ЦЕНА ИЗМЕРЕНА
//
// Прежняя редакция этой проверки судила ВЫПИСАННЫЙ перечень из двух целей
// Makefile (`dev-up`, `stack-up`). Путей выкатки в дереве три: третий —
// `deploy/helm/umbrella/cutover-fe3455.sh`, и он поднимает БОЕВУЮ площадку.
// Скриптом он стал не по недосмотру: `stack-up` отказывает стенду `fe3455`
// прямо в рецепте, потому что его цепочке нужен слой учётных данных площадки,
// лежащий ВНЕ дерева. То есть выписанный перечень был слеп именно к той
// посадке, ошибка на которой дороже всего, — и слеп BY CONSTRUCTION, а не
// потому, что кто-то забыл дописать строку (kacho#1909).
//
// Перечень поэтому ВЫВОДИТСЯ обходом отслеживаемого дерева, и объём осмотренного
// печатается: «ноль находок» обязано быть отличимо от «ноль прочитанного».
//
// ─────────────────────────────────────────────────────────────────────────────
// ВСЕ ЗАКОННЫЕ ФОРМЫ ЗАПИСИ ПРЕДМЕТА НАЗВАНЫ (testing.md §«Гейт на класс», п.7)
//
// Носитель, способный исполнять команды, записывается в этом дереве ДВУМЯ
// формами, и обе выводятся, а не перечисляются именами файлов:
//
//	Makefile — файл `Makefile` либо `*.mk`; единица — РЕЦЕПТ (цель), потому что
//	           производитель обязан стоять в том же рецепте, что и helm;
//	скрипт   — файл с шапкой запуска (`#!`); единица — файл целиком.
//
// Чарт умбреллы называется в вызове ТОЖЕ двумя формами:
//
//	путь, оканчивающийся на `helm/umbrella` — так его зовут из каталога deploy;
//	голая точка `.` — так его зовёт скрипт, ЛЕЖАЩИЙ В КАТАЛОГЕ ЧАРТА и перешедший
//	в него перед вызовом. Точка засчитывается ТОЛЬКО такому носителю: в чужом
//	файле она называет чужой чарт.
//
// ГРАНИЦА НАЗВАНА ЧЕСТНО: носитель без шапки запуска и не-Makefile (скажем,
// модуль на Python, запускаемый интерпретатором явно) в популяцию не попадёт.
// Такого пути выкатки в дереве сегодня нет — перепись ниже это и печатает, — но
// появится он молча, и об этом надо знать заранее.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ НЕ ПРОВЕРЯЕТСЯ
//
// Не проверяется, ОБЪЯВЛЯЕТ ли конкретная посадка доставку: это решение посадки,
// записанное прозой в профиле, а не свойство дерева. Соседняя проверка
// (iam_module_manifest_producer_test.go) судит, что ОБЪЯВИВШИЙ доставку стенд
// получает от производителя годный объект. Здесь предмет один: путь выкатки
// зовёт производителя, и зовёт вовремя.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// manifestProducerToken — как записывается ВЫЗОВ производителя.
//
// Один токен покрывает обе формы: цель сборки зовётся по имени
// (`$(MAKE) … module-manifests-configmap`, `make -C … module-manifests-configmap`),
// а прямой запуск — путём `./tools/modulemanifests/cmd/module-manifests-configmap`,
// который это имя содержит. Второго токена завести нельзя: он разошёлся бы с
// первым молча.
const manifestProducerToken = "module-manifests-configmap"

// umbrellaChartDir — каталог чарта умбреллы от корня дерева. Только носителю
// ИЗ ЭТОГО каталога голая точка засчитывается за чарт умбреллы.
const umbrellaChartDir = "deploy/helm/umbrella"

// makefileTargetHeader — заголовок цели Makefile. Присваивания (`X := …`,
// `X ?= …`) целями не являются и рецепта не открывают.
var makefileTargetHeader = regexp.MustCompile(`^[A-Za-z0-9_.\-/%$()]+\s*:(?:[^=]|$)`)

// deployCarrier — отслеживаемый файл, СПОСОБНЫЙ исполнять команды.
type deployCarrier struct {
	Path string // путь от корня дерева
	Kind string // "Makefile" | "скрипт"
	Text string
}

// bringUpUnit — единица выкатки: то, внутри чего производитель обязан стоять
// перед helm.
type bringUpUnit struct {
	Name string // "deploy/Makefile:dev-up" либо путь скрипта
	Kind string
	// HelmLine — номер строки первого вызова helm по чарту умбреллы (1-based,
	// в пределах единицы).
	HelmLine int
	// ProducerLine — номер строки первого ИСПОЛНЯЕМОГО вызова производителя;
	// ноль означает «не зовётся вовсе».
	ProducerLine int
}

// bringUpCensus — объём осмотренного. Печатается ВСЕГДА, на всяком исходе.
type bringUpCensus struct {
	Carriers      int // носителей осмотрено
	Makefiles     int
	Scripts       int
	LinesJudged   int // исполняемых строк (без строк-комментариев)
	HelmCalls     int // вызовов `helm upgrade` любого чарта
	UmbrellaCalls int // из них — по чарту умбреллы
	Units         int // путей выкатки найдено
	UnitsCalling  int // из них зовущих производителя вовремя
}

// Summary — перепись одной строкой.
func (c bringUpCensus) Summary() string {
	return fmt.Sprintf("носителей %d (Makefile %d · скриптов %d) · исполняемых строк %d · "+
		"вызовов helm upgrade %d · из них по умбрелле %d · путей выкатки %d · зовут производителя вовремя %d",
		c.Carriers, c.Makefiles, c.Scripts, c.LinesJudged, c.HelmCalls, c.UmbrellaCalls,
		c.Units, c.UnitsCalling)
}

// bringUpCarriers — носители, ВЫВЕДЕННЫЕ обходом отслеживаемого дерева.
//
// Состав берётся из ИНДЕКСА git, а не обходом диска: обход читал бы
// игнорируемые каталоги (рабочие копии агентов, распаковки чартов, отчёты
// прогонов) и вердикт стал бы свойством рабочего каталога. `treecorpus.Under`
// заодно ОТКАЗЫВАЕТСЯ отвечать на прогоне, чей результат `go test` положит в
// кеш: состав дерева он берёт подпроцессом, инструменту невидимым, и над
// красным деревом напечаталось бы `ok (cached)`.
func bringUpCarriers(t *testing.T) []deployCarrier {
	t.Helper()
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("абсолютный путь корня дерева: %v", err)
	}
	files, err := treecorpus.Under(filepath.Join(absRoot, "deploy"))
	if err != nil {
		t.Fatalf("состав deploy/: %v — «ноль находок» стало бы свойством рабочего каталога", err)
	}

	var out []deployCarrier
	for _, abs := range files {
		rel, rerr := filepath.Rel(absRoot, abs)
		if rerr != nil {
			t.Fatalf("путь %s от корня дерева: %v", abs, rerr)
		}
		rel = filepath.ToSlash(rel)
		info, serr := os.Lstat(abs)
		if serr != nil || !info.Mode().IsRegular() {
			continue // ссылка либо запись индекса без файла на диске — исполнять нечего
		}
		// #nosec G304 -- путь пришёл из индекса git через treecorpus.
		raw, rerr := os.ReadFile(abs)
		if rerr != nil {
			t.Fatalf("не прочитан отслеживаемый файл %s (%v) — обход сузился бы молча", rel, rerr)
		}
		base := filepath.Base(rel)
		switch {
		case base == "Makefile" || strings.HasSuffix(base, ".mk"):
			out = append(out, deployCarrier{Path: rel, Kind: "Makefile", Text: string(raw)})
		case strings.HasPrefix(string(raw), "#!"):
			out = append(out, deployCarrier{Path: rel, Kind: "скрипт", Text: string(raw)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// helmUpgradeInvocation — строка ВЫЗЫВАЕТ `helm upgrade`.
//
// Судится команда, а не вхождение подстроки: `log "helm upgrade …"` и
// `warn "helm upgrade FAILED"` вызовами не являются, и зачесть их значило бы
// принять за исполнение собственное объяснение проверяемого.
func helmUpgradeInvocation(line string) bool {
	s := strings.TrimSpace(line)
	for {
		trimmed := s
		for _, p := range []string{"if ", "! ", "then ", "@", "+"} {
			if strings.HasPrefix(trimmed, p) {
				trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, p))
			}
		}
		if trimmed == s {
			break
		}
		s = trimmed
	}
	return strings.HasPrefix(s, "helm upgrade")
}

// namesUmbrellaChart — вызов называет чарт умбреллы. Обе законные формы, и
// голая точка засчитывается ТОЛЬКО носителю из каталога чарта.
func namesUmbrellaChart(line, carrierPath string) bool {
	inChartDir := strings.HasPrefix(carrierPath, umbrellaChartDir+"/")
	for _, arg := range strings.Fields(line) {
		a := strings.Trim(arg, `"'`)
		if strings.HasSuffix(strings.TrimRight(a, "/"), "helm/umbrella") {
			return true
		}
		if a == "." && inChartDir {
			return true
		}
	}
	return false
}

// isCommentLine — строка-комментарий Makefile либо оболочки.
func isCommentLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

// auditBringUpPaths — находки по путям выкатки. Функция ЧИСТАЯ: инъекция подаёт
// ей изменённые носители, не трогая дерева.
func auditBringUpPaths(carriers []deployCarrier) ([]string, bringUpCensus) {
	var census bringUpCensus
	units := map[string]*bringUpUnit{}
	var order []string

	unitFor := func(name, kind string) *bringUpUnit {
		if u, ok := units[name]; ok {
			return u
		}
		u := &bringUpUnit{Name: name, Kind: kind}
		units[name] = u
		order = append(order, name)
		return u
	}

	for _, c := range carriers {
		census.Carriers++
		switch c.Kind {
		case "Makefile":
			census.Makefiles++
		default:
			census.Scripts++
		}

		target := ""
		lineNo := 0
		for _, line := range strings.Split(c.Text, "\n") {
			lineNo++
			if isCommentLine(line) {
				continue
			}
			if strings.TrimSpace(line) != "" {
				census.LinesJudged++
			}

			unitName := c.Path
			if c.Kind == "Makefile" {
				if !strings.HasPrefix(line, "\t") {
					target = ""
					if m := makefileTargetHeader.FindString(line); m != "" &&
						!strings.HasPrefix(strings.TrimSpace(line), ".") {
						target = strings.TrimSpace(strings.SplitN(line, ":", 2)[0])
					}
					continue
				}
				if target == "" {
					continue
				}
				unitName = c.Path + ":" + target
			}

			if strings.Contains(line, manifestProducerToken) {
				u := unitFor(unitName, c.Kind)
				if u.ProducerLine == 0 {
					u.ProducerLine = lineNo
				}
			}
			if !helmUpgradeInvocation(line) {
				continue
			}
			census.HelmCalls++
			if !namesUmbrellaChart(line, c.Path) {
				continue
			}
			census.UmbrellaCalls++
			u := unitFor(unitName, c.Kind)
			if u.HelmLine == 0 {
				u.HelmLine = lineNo
			}
		}
	}

	var findings []string
	for _, name := range order {
		u := units[name]
		if u.HelmLine == 0 {
			continue // производителя зовёт, а умбреллу не катит — не путь выкатки
		}
		census.Units++
		switch {
		case u.ProducerLine == 0:
			findings = append(findings, fmt.Sprintf(
				"путь выкатки %s не зовёт %s — под смонтирует ConfigMap, которого никто "+
					"не создал, каталог доставки приедет пустым, и служба откажется "+
					"стартовать (kacho#1901, #1909)", u.Name, manifestProducerToken))
		case u.ProducerLine > u.HelmLine:
			findings = append(findings, fmt.Sprintf(
				"путь выкатки %s зовёт %s строкой %d, ПОСЛЕ первого прогона helm (строка %d) — "+
					"объект появится позже, чем под его смонтирует: доставка есть предусловие, "+
					"а не следствие", u.Name, manifestProducerToken, u.ProducerLine, u.HelmLine))
		default:
			census.UnitsCalling++
		}
	}
	sort.Strings(findings)
	return findings, census
}

// TestEveryUmbrellaBringUpPathCallsTheManifestProducer — популяция ВЫВЕДЕНА из
// дерева, и каждый её элемент зовёт производителя перед helm.
func TestEveryUmbrellaBringUpPathCallsTheManifestProducer(t *testing.T) {
	carriers := bringUpCarriers(t)
	findings, census := auditBringUpPaths(carriers)
	t.Logf("осмотрено: %s", census.Summary())

	if census.Carriers == 0 {
		t.Fatal("носителей не прочитано ни одного — вердикт беспредметен: " +
			"«ноль находок» здесь означало бы «ноль прочитанного»")
	}
	if census.LinesJudged == 0 {
		t.Fatal("после снятия строк-комментариев не осталось ни одной исполняемой строки — " +
			"судить было нечего")
	}
	if census.UmbrellaCalls == 0 {
		t.Fatal("вызовов helm по чарту умбреллы не найдено ни одного — предпосылка " +
			"проверки исчезла, а не дерево стало чистым: умбреллу чем-то катят")
	}
	if census.Units == 0 {
		t.Fatal("путей выкатки не найдено ни одного при непустом обходе — распознаватель " +
			"перестал узнавать предмет")
	}
	for _, f := range findings {
		t.Errorf("доставка манифестов: %s", f)
	}
}
