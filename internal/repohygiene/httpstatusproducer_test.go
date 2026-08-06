// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// httpstatusproducer_test.go — гейт против пробы, ждущей код ответа, которого край
// произвести НЕ МОЖЕТ.
//
// ПРЕДМЕТ. Край своего отображения ошибок не несёт: `runtime.NewServeMux` в
// `gateway/internal/restmux/mux.go` собирается без `WithErrorHandler`, поэтому
// HTTP-статус выбирает `runtime.HTTPStatusFromCode`. Множество производимых кодов
// этим и задано — оно КОНЕЧНО и известно. Утверждение, ждущее кода вне этого
// множества, не может пройти ни при каком состоянии продукта: оно либо всегда
// красное (строгое равенство), либо — хуже — молча расширяет допуск внутри
// `oneOf`, перечисляя исход, у которого нет производителя. Второе не краснеет
// никогда и потому живёт годами.
//
// ЧТО ЭТО НАШЛО, КОГДА ПИСАЛОСЬ (2026-08-04): два кейса ждали строгий 412 на
// `FAILED_PRECONDITION`, а библиотека отдаёт 400 — и говорит об этом собственным
// комментарием в той же ветке («deliberately doesn't translate to the similarly
// named '412 Precondition Failed'»). Ещё четыре места перечисляли 412 (и одно —
// 422) внутри `oneOf`, то есть держали допуск на исход, которого не бывает.
// Тринадцать строк документации iam объявляли то же самое таблицами ошибок.
//
// ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ, А НЕ ОБЪЯВЛЯЕТСЯ. Множество производимых кодов здесь
// не выписано литералом — оно ВЫЧИСЛЯЕТСЯ вызовом самой библиотеки по всем кодам
// gRPC. Поэтому обновление grpc-gateway меняет ответ гейта вместе с собой, и
// правило не может пережить свой предмет. Если край когда-нибудь заведёт свой
// `WithErrorHandler`, предпосылка станет ложной — и об этом сказано в
// `api-conventions.md` §«gRPC-код → HTTP-статус», где живёт канон.
//
// ЧИТАЮТСЯ ОБА МЕСТА, ГДЕ ЖИВУТ УТВЕРЖДЕНИЯ. Рукописный кейс и ГЕНЕРАТОР суиты:
// одна строка генератора раскладывается на десятки шагов. Пока читались только
// кейсы, гейт был зелёным при 124 утверждениях о коде ответа, лежавших в восьми
// генераторах, — то есть корзина, в которой он ищет, не совпадала с местом, где
// пишут. Прежний счёт «781 утверждение» стал 905.
//
// ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ, И ОТДЕЛЬНО — ОБЪЁМ НЕПРОЧИТАННОГО. «Ноль
// находок» обязано быть отличимо и от «ноль прочитанного» (гейт отказывается
// проходить, не прочитав ни одного файла), и от «прочитано не то»: строки,
// называющие код ответа формой, которую предикат не разбирает, считаются
// отдельно, иначе рост слепой зоны неотличим от чистоты.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
)

// muxOwnStatuses — коды, которые край отдаёт САМ, не транслируя ответ бэкенда.
// Они не выводятся из `HTTPStatusFromCode`, поэтому названы поимённо и с
// причиной: список без причин через полгода неотличим от списка удобства, а
// именно в такой список и складывают то, что лень чинить.
//
// Производитель у каждой записи ЖИВОЙ и проверяется отдельным утверждением
// (см. edgeStatusProducers ниже): запись, чей производитель ушёл из дерева, —
// находка, а не тихое послабление.
var muxOwnStatuses = map[int]string{
	405: "маршрут есть, метод не тот — решает сам мультиплексор до вызова бэкенда",
	415: "тело в типе, который marshaler не берёт — тоже до бэкенда",
	413: "объявленная длина тела больше потолка — отдаёт middleware края ДО мультиплексора " +
		"(и ingress своим пределом тоже), поэтому проба, допускающая 413 на большом теле, " +
		"называет исход, у которого есть производитель",
}

// edgeStatusProducers — чем ДОКАЗЫВАЕТСЯ каждая запись muxOwnStatuses, которую
// производит не библиотека, а наш собственный код. Ключ — код, значение — файл
// и литерал, который его выдаёт.
//
// Без этого запись «этот код кто-то производит» была бы утверждением о продукте,
// которое ничем не держится: снимут middleware — запись переживёт свой предмет и
// начнёт освобождать пробу, ждущую невозможного.
var edgeStatusProducers = map[int]struct{ file, literal string }{
	413: {"gateway/internal/middleware/http_body_limit.go", "http.StatusRequestEntityTooLarge"},
}

// statusAssertion — литерал кода рядом с `pm.response.code`.
//
// Форм РАВЕНСТВА в этом дереве две, и обе настоящие: chai даёт `.to.eql` и
// `.to.equal`, кейсы пишут и так и так (замер на d24476c1: 169 строк первой формы
// и 66 второй). Предикат, знающий одну, молча освобождал бы вторую — и «ноль
// находок» покрывал бы 66 утверждений, которых он не читал.
var (
	reEql   = regexp.MustCompile(`pm\.response\.code[^;]{0,120}?\.to\.(?:eql|equal)\(\s*(\d{3})\s*\)`)
	reOneOf = regexp.MustCompile(`pm\.response\.code[^;]{0,160}?\.to\.be\.oneOf\(\s*\[([0-9,\s]+)\]`)
)

// reUnreadShape — строка, которая ГОВОРИТ о коде ответа, но ни одной формой выше
// не читается: отрицательное сравнение (`.to.not.equal(403)`), ветвление
// (`if (pm.response.code === 404)`), список с подстановкой. Такие не находки — но
// и не «прочитано». Их число печатается отдельно, иначе рост слепой зоны
// неотличим от чистоты, а смена формы записи тихо выключает гейт.
var reUnreadShape = regexp.MustCompile(`pm\.response\.code`)

func producibleStatuses() map[int]codes.Code {
	out := map[int]codes.Code{}
	// Коды gRPC — непрерывный диапазон 0..16; берём с запасом и полагаемся на то,
	// что библиотека сама решает, что делать с неизвестным.
	for c := codes.Code(0); c <= codes.Code(16); c++ {
		out[runtime.HTTPStatusFromCode(c)] = c
	}
	return out
}

// stripJSLineComment убирает хвостовой `//`-комментарий. Гейт обязан отличать код
// от комментария: иначе он находит слово в тексте, ОБЪЯСНЯЮЩЕМ проверку, и
// краснеет на разборе, который сам же и просил написать.
func stripJSLineComment(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		return s[:i]
	}
	return s
}

func TestNoCaseAssertsAStatusTheEdgeCannotProduce(t *testing.T) {
	root := repoRoot(t)

	producible := producibleStatuses()
	if len(producible) < 8 {
		t.Fatalf("предпосылка не выполнена: библиотека вернула лишь %d различных статусов — "+
			"множество производимых кодов не вычислено, и утверждать «этого не бывает» не на чем",
			len(producible))
	}

	// Утверждения о коде ответа живут в ДВУХ местах, и оба обязаны читаться:
	// рукописный кейс и ГЕНЕРАТОР, который раскладывает одну строку на десятки
	// шагов. Пока читались только кейсы, четыре строки генераторов давали 42 шага
	// в 6 коллекциях, и ни одна из них не была даже рассмотрена. Соседний гейт
	// того же корпуса (tools/mixedoutcomeaudit) читает оба места — то есть где
	// лежат утверждения, корпус уже знал.
	patterns := []string{
		filepath.Join(root, "services", "*", "tests", "newman", "cases", "*.py"),
		filepath.Join(root, "gateway", "tests", "newman", "cases", "*.py"),
		filepath.Join(root, "services", "*", "tests", "newman", "scripts", "gen.py"),
		filepath.Join(root, "gateway", "tests", "newman", "scripts", "gen.py"),
	}
	var files []string
	for _, p := range patterns {
		m, err := filepath.Glob(p)
		if err != nil {
			t.Fatalf("обход %s: %v", p, err)
		}
		files = append(files, m...)
	}
	sort.Strings(files)

	filesRead, linesScanned, assertionsSeen := 0, 0, 0
	linesMentioning, linesUnread := 0, 0
	var findings []string

	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		filesRead++
		rel, _ := filepath.Rel(root, f)
		for i, raw := range strings.Split(string(b), "\n") {
			linesScanned++
			line := stripJSLineComment(raw)
			if !reUnreadShape.MatchString(line) {
				continue
			}
			linesMentioning++
			var got []int
			if m := reEql.FindStringSubmatch(line); m != nil {
				n, _ := strconv.Atoi(m[1])
				got = append(got, n)
			}
			if m := reOneOf.FindStringSubmatch(line); m != nil {
				for _, part := range strings.Split(m[1], ",") {
					if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
						got = append(got, n)
					}
				}
			}
			if len(got) == 0 {
				linesUnread++
			}
			for _, code := range got {
				assertionsSeen++
				if _, ok := producible[code]; ok {
					continue
				}
				if _, ok := muxOwnStatuses[code]; ok {
					continue
				}
				findings = append(findings, fmt.Sprintf(
					"%s:%d — утверждение ждёт HTTP %d, а край его не производит: "+
						"`runtime.HTTPStatusFromCode` не отдаёт этот код ни для одного кода gRPC, "+
						"и мультиплексор сам его не выдаёт. Строгое равенство здесь красное всегда; "+
						"внутри `oneOf` — допуск на исход, которого не бывает, и он не покраснеет "+
						"никогда. Канон отображения — api-conventions.md §«gRPC-код → HTTP-статус».",
					rel, i+1, code))
			}
		}
	}

	t.Logf("осмотрено: файлов кейсов %d, строк %d; строк, называющих код ответа, %d "+
		"(из них ПРОЧИТАНО предикатом %d, НЕ прочитано %d — отрицательные сравнения, "+
		"ветвления, списки с подстановкой); утверждений о коде ответа %d; "+
		"производимых статусов %d (вычислено вызовом библиотеки, не выписано)",
		filesRead, linesScanned, linesMentioning, linesMentioning-linesUnread, linesUnread,
		assertionsSeen, len(producible))

	if filesRead == 0 {
		t.Fatal("прочитано НОЛЬ файлов кейсов — гейт не проверен ни против чего. " +
			"Это отказ, а не «находок нет».")
	}
	if assertionsSeen == 0 {
		t.Fatal("не найдено НИ ОДНОГО утверждения о коде ответа — предикат перестал " +
			"опознавать форму, которой пишут пробы. Это отказ, а не чистота.")
	}
	if len(findings) > 0 {
		t.Fatalf("утверждений о непроизводимом коде: %d\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// TestEdgeOwnStatusesStillHaveAProducer — освобождение живёт, пока живёт то, что
// оно освобождает.
//
// `muxOwnStatuses` перечисляет коды, которых `HTTPStatusFromCode` не отдаёт, и
// тем ОСВОБОЖДАЕТ пробы, которые их ждут. Для кодов, производимых библиотекой
// grpc-gateway (405, 415), предмет держится самой библиотекой. Для кода, который
// производит НАШ код, предмета в дереве может не стать — и тогда запись начнёт
// освобождать пробу, ждущую исхода, которого больше нет: ровно то освобождение,
// которому нечего исключать.
//
// Поэтому у каждой такой записи назван производитель координатой, и здесь
// проверяется, что он на месте. Утверждается ИСПОЛНЯЕМАЯ часть: литерал ищется
// в файле, а совпадение в комментарии предметом не считается, иначе шапка,
// объясняющая запрет, вечно доказывала бы его предпосылку.
func TestEdgeOwnStatusesStillHaveAProducer(t *testing.T) {
	root := repoRoot(t)

	checked := 0
	for code, src := range edgeStatusProducers {
		if _, declared := muxOwnStatuses[code]; !declared {
			t.Errorf("%d: производитель назван, но код НЕ объявлен в muxOwnStatuses — "+
				"запись доказывает то, чего никто не освобождает. Удали её", code)
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(src.file)))
		if err != nil {
			t.Errorf("%d: производитель %s не читается (%v) — запись muxOwnStatuses[%d] "+
				"пережила свой предмет. Либо верни координату, либо сними освобождение",
				code, src.file, err, code)
			continue
		}
		found := false
		for _, raw := range strings.Split(string(body), "\n") {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, "//") {
				continue // комментарий — не производитель
			}
			if strings.Contains(stripJSLineComment(raw), src.literal) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%d: в %s нет исполняемого %s — код больше никто не производит, "+
				"а muxOwnStatuses[%d] продолжает освобождать пробы, которые его ждут. "+
				"Сними освобождение вместе с производителем",
				code, src.file, src.literal, code)
			continue
		}
		checked++
	}

	t.Logf("перепись: кодов в muxOwnStatuses %d, из них производимых нашим кодом %d "+
		"(производитель проверен у %d)", len(muxOwnStatuses), len(edgeStatusProducers), checked)

	if len(edgeStatusProducers) > 0 && checked == 0 {
		t.Fatal("ни у одной записи производитель не подтверждён — проверка предпосылки " +
			"сама осталась без предмета")
	}
}
