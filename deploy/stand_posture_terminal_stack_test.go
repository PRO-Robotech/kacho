// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_posture_terminal_stack_test.go — стенд, который поднимает шард сквозных
// проб, ЗАКАНЧИВАЕТ боевой посадкой, и это объявлено профилем.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Ban #16 требует боевой посадки на ЛЮБОМ развёрнутом стенде. Стенд шарда
// поднимается двумя фазами helm: первая накладывает цепочку разработки, вторая —
// боевую. Значение, с которым процесс в итоге живёт, задаёт ВТОРАЯ; первая — это
// промежуточное состояние, которое вторая замещает.
//
// Отсюда предмет: свойство «стенд заканчивает боевой посадкой» держится СЦЕПКОЙ
// трёх независимых мест — работа конвейера называет цель подъёма, рецепт цели
// называет последнюю цепочку, цепочка объявляет посадку каждому сервису. Разрыв
// в любом звене снимает боевую посадку МОЛЧА: рендер зелен, поды Ready, гейты
// дерева судят другое.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ЭТО ВЫВЕДЕНО (задача #2345)
//
// Задача заводилась с посылкой «шард поднимает службу доступа в посадке
// разработки», и посылка ОПРОВЕРГНУТА замером. Её предикат читал ОДИН профиль
// (`values.dev.yaml`) — то есть измерял первую фазу и молчал о второй; замер по
// цепочке, которой подъём ЗАКАНЧИВАЕТСЯ, даёт `production-strict`, и живой
// процесс на поднятом стенде объявляет о себе то же самое:
//
//	{"msg":"boot security posture","auth_mode":"production-strict","db_sslmode":"require", …}
//
// Работы по переводу не потребовалось — она была сделана раньше, когда фаза
// боевой посадки переехала в рецепт подъёма (см. §«ПОЧЕМУ ФАЗА 3 ПЕРЕЕХАЛА»
// в Makefile). Но ДЕРЖАТЕЛЯ у свойства не было ни одного: снятие фазы из
// рецепта, подмена последней цепочки на цепочку разработки и отзыв посадки у
// любого сервиса не роняли ничего. Этот файл — держатель.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседних dbtls_declaration_test.go и
// posture_parity_test.go: контракт — то, что профиль ОБЪЯВЛЯЕТ. Проверке не
// нужны ни helm, ни собранные зависимости чартов, поэтому она не умеет
// пропуститься. Рендер тут и не помог бы: значение, приехавшее из умолчания
// чарта, в манифесте выглядит ровно так же, как объявленное профилем, — а
// умолчание есть свойство ЧАРТА и меняется под профилем без единой правки
// профиля (этим же доводом требует объявления соседний гейт шифрования канала
// к базе).
//
// ─────────────────────────────────────────────────────────────────────────────
// НИ ОДНО ЗВЕНО НЕ ВЫПИСАНО
//
// Цель подъёма читается из работы конвейера, цепочка — из рецепта цели, состав
// цепочки — из единственной таблицы стендов, словарь посадок — у его дома
// (`servicecontract`), население сервисов — из умолчаний подчартов. Выписанное
// здесь звено разошлось бы с деревом молча — ровно тем классом, который эта
// проверка и ловит.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ДЕЛАЕТ (названо, чтобы «зелено» не читалось шире)
//
//   - не судит ПРОМЕЖУТОЧНЫЕ фазы. Первая фаза подъёма объявляет посадку
//     разработки, и требовать от неё боевой значило бы требовать, чтобы вторая
//     фаза не существовала. Промежуточные печатаются переписью и находкой не
//     являются; окно между фазами — отдельный предмет со своей ценой, и он
//     назван в отчёте задачи, а не спрятан здесь;
//   - не доказывает, что стенд ПОДНИМАЕТСЯ. Это «не выполнилось», третья
//     категория; её закрывает подъём (`dev-prod-up`) и гейт живого процесса
//     (`scripts/assert-production-posture.sh`);
//   - не судит транспорт, шифрование канала к базе и круг отправителей: у
//     каждого свой держатель рядом.
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// shardWorkflow — работа конвейера, поднимающая стенд шарда сквозных проб.
const shardWorkflow = "../.github/workflows/e2e-newman.yml"

// posturKnob — канонический адрес посадки в корне значений сервиса. Тот же
// единственный адрес, который держит гейт «посадка объявляется ОДНИМ адресом»;
// второго написания здесь не заводится намеренно.
const postureKnob = "authMode"

// standUpInvocation — вызов цели make через владельца подъёма стенда. Имя цели
// берётся ОТТУДА, а не выписывается: сменят цель — проверка пойдёт за ней.
var standUpInvocation = regexp.MustCompile(`stand-up\.sh[^\n]*\\\s*\n\s*make\s+([a-z][a-z0-9-]*)`)

// stackArgsCall — резолв цепочки стенда внутри рецепта make. Имя стенда —
// литерал: рецепт подъёма называет свои фазы поимённо.
var stackArgsCall = regexp.MustCompile(`\$\(call\s+STACK_ARGS,\s*([a-z][a-z0-9-]*)\s*\)`)

// Заголовок цели читается ТЕМ ЖЕ выражением, что и у соседней проверки рецептов
// (`makeTargetLine`): второе выражение об одном предмете разошлось бы молча.
// ─────────────────────────────────────────────────────────────────────────────
// ФАКТЫ, снятые с дерева.

// standBringUp — как шард поднимает стенд.
type standBringUp struct {
	Target string   // цель make, названная работой конвейера
	Phases []string // имена стендов, которые резолвит рецепт цели, ПО ПОРЯДКУ
}

// Terminal — цепочка, которой подъём ЗАКАНЧИВАЕТСЯ. Именно её значения
// переживают подъём: каждая следующая фаза замещает предыдущую.
func (b standBringUp) Terminal() string {
	if len(b.Phases) == 0 {
		return ""
	}
	return b.Phases[len(b.Phases)-1]
}

// shardBringUp читает цель подъёма из работы конвейера и её фазы из рецепта.
func shardBringUp(t *testing.T) standBringUp {
	t.Helper()

	wf, err := os.ReadFile(shardWorkflow)
	if err != nil {
		t.Fatalf("работа конвейера %s не читается (%v) — предпосылка проверки исчезла, "+
			"а не дерево стало чистым", shardWorkflow, err)
	}
	m := standUpInvocation.FindSubmatch(wf)
	if m == nil {
		t.Fatalf("в %s не найден вызов цели make через владельца подъёма стенда — "+
			"распознаватель перестал узнавать работу конвейера, и «стенд поднимается "+
			"боевым» было бы объявлено о непрочитанном", shardWorkflow)
	}
	target := string(m[1])

	return standBringUp{Target: target, Phases: makeRecipeStacks(t, target)}
}

// makeRecipeStacks достаёт имена стендов, которые резолвит рецепт цели, по
// порядку появления. Читается ТЕЛО рецепта (строки с отступом-табуляцией), а не
// весь файл: то же имя цепочки стоит в соседних целях и в объяснениях, и
// проверка по всему файлу судила бы чужой рецепт заодно со своим.
func makeRecipeStacks(t *testing.T, target string) []string {
	t.Helper()
	raw, err := os.ReadFile(standMakefile)
	if err != nil {
		t.Fatalf("рецепты подъёма %s не читаются: %v", standMakefile, err)
	}
	var out []string
	inRecipe := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "\t") {
			if inRecipe {
				for _, mm := range stackArgsCall.FindAllStringSubmatch(line, -1) {
					out = append(out, mm[1])
				}
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue // пустая строка рецепт не закрывает
		}
		if m := makeTargetLine.FindStringSubmatch(line); m != nil {
			inRecipe = m[1] == target
			continue
		}
		inRecipe = false
	}
	if len(out) == 0 {
		t.Fatalf("рецепт цели %q не резолвит ни одной цепочки стенда — "+
			"распознаватель перестал узнавать рецепт, а не подъём перестал "+
			"накладывать профили", target)
	}
	return out
}

// postureKnobServices — сервисы, у которых посадка ЕСТЬ: подчарт объявляет
// канонический адрес в своих умолчаниях. Население выводится из дерева, а не
// выписывается: заведут восьмой сервис — проверка увидит его тем же прогоном.
func postureKnobServices(t *testing.T) []string {
	t.Helper()
	var out []string
	for name, dir := range subchartDirs(t) {
		vals := filepath.Join(dir, "values.yaml")
		if _, err := os.Stat(vals); err != nil {
			continue
		}
		if _, ok := readYAML(t, vals)[postureKnob]; ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// posturePerStack — что КАЖДЫЙ стенд таблицы ОБЪЯВЛЯЕТ каждому сервису с
// ручкой посадки. Пустая строка означает «ни один профиль цепочки посадку не
// назвал»: значение приедет из умолчания подчарта, то есть посадку выберет не
// стенд.
func posturePerStack(t *testing.T, services []string) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	for name, chain := range deployStacks(t) {
		declared := map[string]any{}
		for _, p := range chain {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		row := map[string]string{}
		for _, svc := range services {
			if v, ok := lookup(declared, svc, postureKnob); ok {
				if s, isStr := v.(string); isStr {
					row[svc] = s
					continue
				}
			}
			row[svc] = ""
		}
		out[name] = row
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами, чтобы инъекция подавала ей синтетический
// вход, а не подделывала дерево. Своя копия предиката в инъекции разошлась бы с
// настоящей проверкой молча.

// posturFinding — одно расхождение терминальной цепочки.
type postureFinding struct {
	Stack    string
	Service  string
	Kind     string // "не объявлено" | "не боевая" | "не из словаря"
	Declared string
}

// judgeTerminalPosture судит ТОЛЬКО терминальную цепочку. Промежуточные фазы
// сюда не попадают by construction — требовать от них боевой посадки значило бы
// требовать, чтобы терминальной фазы не существовало.
func judgeTerminalPosture(terminal string, declared map[string]string, services []string) []postureFinding {
	var out []postureFinding
	for _, svc := range services {
		v := declared[svc]
		switch {
		case v == "":
			out = append(out, postureFinding{terminal, svc, "не объявлено", ""})
			continue
		}
		mode, err := servicecontract.ParseMode(v)
		if err != nil {
			out = append(out, postureFinding{terminal, svc, "не из словаря", v})
			continue
		}
		if !mode.IsProduction() {
			out = append(out, postureFinding{terminal, svc, "не боевая", v})
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// САМА ПРОВЕРКА.

func TestShardStandFinishesInProductionPosture(t *testing.T) {
	bringUp := shardBringUp(t)
	services := postureKnobServices(t)
	byStack := posturePerStack(t, services)
	terminal := bringUp.Terminal()

	// Проверка СВОЕЙ предпосылки. «Ноль находок» обязано быть отличимо от «ноль
	// прочитанного»: перестань распознаватель узнавать цель, фазы, стенды или
	// ручку посадки — он объявит дерево чистым, ничего не осмотрев.
	if len(services) == 0 || len(byStack) == 0 || terminal == "" {
		t.Fatalf("обход ничего не прочитал: сервисов с ручкой посадки=%d, стендов таблицы=%d, "+
			"терминальная цепочка=%q — предикат перестал узнавать дерево, "+
			"а не дерево стало чистым", len(services), len(byStack), terminal)
	}
	declared, known := byStack[terminal]
	if !known {
		t.Fatalf("рецепт цели %q заканчивает цепочкой %q, которой НЕТ в таблице стендов. "+
			"Стенд, поднимаемый мимо таблицы, не описан ничем: ни один гейт, читающий "+
			"таблицу, о нём не знает", bringUp.Target, terminal)
	}

	t.Logf("перепись: работа конвейера %s → цель %q · фаз подъёма %d (%s) · "+
		"терминальная цепочка %q · стендов таблицы %d · сервисов с ручкой посадки %d (%s)",
		filepath.Base(shardWorkflow), bringUp.Target, len(bringUp.Phases),
		strings.Join(bringUp.Phases, " → "), terminal, len(byStack),
		len(services), strings.Join(services, " "))

	// Промежуточные фазы печатаются, но НЕ судятся: см. §«ЧЕГО ЭТА ПРОВЕРКА НЕ
	// ДЕЛАЕТ». Одно число («находок 0») скрыло бы ровно тот случай, ради
	// которого перепись печатает обе половины.
	for _, ph := range bringUp.Phases[:len(bringUp.Phases)-1] {
		var pairs []string
		for _, svc := range services {
			v := byStack[ph][svc]
			if v == "" {
				v = "<не объявлено>"
			}
			pairs = append(pairs, svc+"="+v)
		}
		t.Logf("  промежуточная фаза %q (не судится): %s", ph, strings.Join(pairs, " "))
	}

	findings := judgeTerminalPosture(terminal, declared, services)
	production := len(services) - len(findings)
	t.Logf("  терминальная фаза %q: объявлено боевыми %d из %d", terminal, production, len(services))

	for _, f := range findings {
		switch f.Kind {
		case "не объявлено":
			t.Errorf("%s: посадка сервиса %q НЕ ОБЪЯВЛЕНА ни одним профилем терминальной "+
				"цепочки — значение приедет из умолчания подчарта, а умолчание есть "+
				"свойство ЧАРТА: оно меняется под профилем без единой правки профиля. "+
				"Объяви %s.%s в профиле цепочки (допустимы: %s)",
				f.Stack, f.Service, f.Service, postureKnob,
				strings.Join(servicecontract.Modes(), ", "))
		case "не боевая":
			t.Errorf("%s: %s.%s = %q — стенд, которым ЗАКАНЧИВАЕТСЯ подъём, оставляет "+
				"сервис в НЕбоевой посадке. Ban #16: боевая посадка обязательна на любом "+
				"развёрнутом стенде, и «зелёный dev» боевого пути не доказывает — политика "+
				"вызывающего, стражи старта и проверка личности в небоевой посадке "+
				"вырождаются, поэтому сквозной прогон проверяет полосу, которой в боевой "+
				"посадке нет", f.Stack, f.Service, postureKnob, f.Declared)
		case "не из словаря":
			t.Errorf("%s: %s.%s = %q — значения нет в словаре посадок (допустимы: %s). "+
				"Страж старта отвергает такое значение отказом ПУСКА, то есть стенд не "+
				"поднимется вовсе", f.Stack, f.Service, postureKnob, f.Declared,
				strings.Join(servicecontract.Modes(), ", "))
		}
	}
}
