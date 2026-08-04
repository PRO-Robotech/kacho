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
// ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ. «Ноль находок» обязано быть отличимо от «ноль
// прочитанного»: гейт отказывается проходить, если не прочитал ни одного файла.
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

// muxOwnStatuses — коды, которые мультиплексор края отдаёт САМ, не транслируя
// ответ бэкенда. Они не выводятся из `HTTPStatusFromCode`, поэтому названы
// поимённо и с причиной: список без причин через полгода неотличим от списка
// удобства, а именно в такой список и складывают то, что лень чинить.
var muxOwnStatuses = map[int]string{
	405: "маршрут есть, метод не тот — решает сам мультиплексор до вызова бэкенда",
	415: "тело в типе, который marshaler не берёт — тоже до бэкенда",
}

// statusAssertion — литерал кода рядом с `pm.response.code`.
var (
	reEql   = regexp.MustCompile(`pm\.response\.code[^;]{0,120}?\.to\.eql\(\s*(\d{3})\s*\)`)
	reOneOf = regexp.MustCompile(`pm\.response\.code[^;]{0,160}?\.to\.be\.oneOf\(\s*\[([0-9,\s]+)\]`)
)

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

	patterns := []string{
		filepath.Join(root, "services", "*", "tests", "newman", "cases", "*.py"),
		filepath.Join(root, "gateway", "tests", "newman", "cases", "*.py"),
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
			if !strings.Contains(line, "pm.response.code") {
				continue
			}
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

	t.Logf("осмотрено: файлов кейсов %d, строк %d, утверждений о коде ответа %d; "+
		"производимых статусов %d (вычислено вызовом библиотеки, не выписано)",
		filesRead, linesScanned, assertionsSeen, len(producible))

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
