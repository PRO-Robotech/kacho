// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanrefusalpolarity_injection_test.go — инъекция в обе стороны для
// различителя «проба отказа против фикстуры» (`expectsRefusal`).
//
// # Зачем отдельный файл
//
// Различитель — общая деталь ДВУХ гейтов (`newmansubnetsupernet_test.go` и
// `newmancarveplananchor_test.go`): он решает, чей шаг вообще подлежит суду. Его
// собственная способность ошибаться поэтому проверяется отдельно от того, что он
// обслуживает, и на входе, СОБРАННОМ ЗДЕСЬ, а не снятом с дерева: дерево движется,
// и проба, привязанная к сегодняшнему набору, истечёт вместе с ним.
//
// # Предмет: утверждение и его отрицание — не одно и то же
//
// `pm.expect(pm.response.code, …).to.equal(403)` утверждает отказ: шаг ради него и
// написан, соседи на его успех не опираются. `…to.not.equal(403)` не утверждает о
// коде НИЧЕГО — это ALLOW-полоса, которая лишь запрещает один исход из пятисот;
// шаг при этом обычный, и всё, что он создаёт, обязано быть создано правильно.
//
// Прежняя редакция различителя судила по ПРИСУТСТВИЮ трёхзначного числа рядом с
// упоминанием кода ответа, поэтому обе формы читала одинаково — как «шаг ждёт
// отказа» — и выводила отрицающий шаг из-под наблюдения обоих гейтов. Следствие
// двустороннее, и вторая сторона хуже: шаг, ПРИВЕДЁННЫЙ В ПОРЯДОК, попадал под
// наблюдение и мог дать красное, читаемое как регресс, — то есть механизм наказывал
// за починку и учил возвращать слабую форму.
//
// # Оси, и каждая проверяется в обе стороны
//
//  1. полярность — отрицание против утверждения;
//  2. комментарий — закомментированное утверждение отказа утверждением НЕ является;
//  3. строковый литерал — число в тексте сообщения («unexpected 200: …») кодом,
//     который шаг допускает, НЕ является;
//  4. управляющий поток — петля ожидания по коду ответа о коде НЕ утверждает;
//  5. перенос строки — утверждение, чья цепочка встала на следующей строке,
//     обязано читаться целиком: «не найдено» здесь означало бы «не читал».
//
// Оси 2-4 — это `testing.md` §«Гейт на класс», п. 4 («гейт читает исполняемую
// часть, а не текст»), применённый к JS внутри сгенерированной коллекции. Ось 5 —
// оттуда же требование «объём осмотренного»: разбор, молча не дочитавший форму,
// возвращает шаг в фикстуры и делает молчание неотличимым от проверки.
package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

// planlessCarveShape — сеть без адресного плана, следом нарезка подсети с адресом.
// Форма ОДНА на все стороны инъекции: различаются только утверждения режущего шага,
// поэтому расхождение вердикта нельзя списать на разный вход.
const planlessCarveShape = `{"item":[{"name":"POL — carve on a planless network","item":[
      {"name":"mk-net","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/networks"},
        "body":{"raw":"{\"projectId\":\"p\",\"name\":\"n\"}"}},
        "event":[{"listen":"test","script":{"exec":[
          "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
          "pm.environment.set('netId', pm.response.json().metadata.networkId);"]}}]},
      {"name":"mk-sub","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
        "body":{"raw":"{\"networkId\":\"{{netId}}\",\"ipv4CidrPrimary\":\"10.1.0.0/24\"}"}},
        "event":[{"listen":"test","script":{"exec":[%s]}}]}
    ]}]}`

func carveWithAssertions(t *testing.T, name string, execLines ...string) subnetSupernetScan {
	t.Helper()
	quoted := make([]string, 0, len(execLines))
	for _, l := range execLines {
		quoted = append(quoted, `"`+strings.ReplaceAll(l, `"`, `\"`)+`"`)
	}
	return analyzeSubnetSupernetFixtures(t, name,
		[]byte(fmt.Sprintf(planlessCarveShape, strings.Join(quoted, ","))))
}

// TestRefusalDiscriminatorTellsNegationFromAssertion — ось 1, обе стороны.
//
// КРАСНАЯ: шаг несёт только отрицание — он обычная фикстура, и нарезка в сети без
// плана обязана быть находкой. ЗЕЛЁНАЯ: та же форма с положительным утверждением
// отказа — предмет шага и есть отказ, гейт молчит.
//
// Без красной различитель ничего не требует; без зелёной он объявил бы фикстурой
// каждую пробу отказа и был бы снят первым же ложным срабатыванием.
func TestRefusalDiscriminatorTellsNegationFromAssertion(t *testing.T) {
	neg := carveWithAssertions(t, "polarity-negation.json",
		"pm.test('[ALLOW] not 403', () => pm.expect(pm.response.code, 'unexpected 403: ' + pm.response.text()).to.not.equal(403));")
	if neg.refusalProbes != 0 {
		t.Errorf("отрицание «не 403» засчитано пробой отказа: refusalProbes=%d, ожидался 0 — "+
			"шаг не утверждает о коде ничего и обязан судиться по существу", neg.refusalProbes)
	}
	if neg.subnetFixtures != 1 {
		t.Fatalf("шаг с отрицанием не распознан фикстурой: subnetFixtures=%d — "+
			"на неразобранном входе ни находка, ни молчание ничего не доказывают",
			neg.subnetFixtures)
	}
	if len(neg.hits) != 1 {
		t.Fatalf("нарезка в сети без плана обязана быть находкой, получено: %v", neg.hits)
	}
	if !strings.Contains(neg.hits[0], "mk-sub") || !strings.Contains(neg.hits[0], "mk-net") {
		t.Errorf("находка обязана назвать координату (режущий шаг и сеть без плана), получено: %q",
			neg.hits[0])
	}
	// Перепись обязана НАЗЫВАТЬ этот класс отдельно, иначе он неотличим от «шаг о
	// коде не утверждает ничего», и рост слабой формы прочитается как чистота.
	if neg.negationOnly != 1 {
		t.Errorf("шаг с отрицанием не попал в перепись отрицаний: negationOnly=%d, ожидался 1",
			neg.negationOnly)
	}

	pos := carveWithAssertions(t, "polarity-assertion.json",
		"pm.test('DENY 403', () => pm.expect(pm.response.code, 'expected 403: ' + pm.response.text()).to.equal(403));")
	if pos.refusalProbes != 1 {
		t.Fatalf("положительное утверждение отказа не распознано: refusalProbes=%d, ожидался 1",
			pos.refusalProbes)
	}
	if pos.subnetFixtures != 0 {
		t.Errorf("проба отказа засчитана фикстурой: subnetFixtures=%d", pos.subnetFixtures)
	}
	if len(pos.hits) != 0 {
		t.Errorf("гейт сработал на пробе, чей предмет и есть отказ: %v", pos.hits)
	}
}

// TestRefusalDiscriminatorReadsCodeNotComments — ось 2, обе стороны на одном тексте.
//
// Закомментированное утверждение отказа — не утверждение: newman его не исполняет,
// и опираться на такой шаг соседи продолжают. Зелёная сторона держит различитель от
// вырождения в «любое упоминание кода в комментарии снимает шаг с наблюдения».
func TestRefusalDiscriminatorReadsCodeNotComments(t *testing.T) {
	commented := carveWithAssertions(t, "comment-only-refusal.json",
		"// pm.test('DENY', () => pm.expect(pm.response.code).to.equal(403));",
		"/* pm.expect(pm.response.code).to.eql(404) */",
		"pm.environment.set('subId', pm.response.json().metadata.subnetId);")
	if commented.refusalProbes != 0 {
		t.Errorf("утверждение отказа ИЗ КОММЕНТАРИЯ засчитано исполняемым: refusalProbes=%d",
			commented.refusalProbes)
	}
	if commented.subnetFixtures != 1 || len(commented.hits) != 1 {
		t.Fatalf("шаг с закомментированным отказом обязан судиться как фикстура: "+
			"subnetFixtures=%d hits=%v", commented.subnetFixtures, commented.hits)
	}
	if commented.noStatusAssert != 1 {
		t.Errorf("шаг без исполняемых утверждений о коде не попал в свою переписную "+
			"строку: noStatusAssert=%d, ожидался 1", commented.noStatusAssert)
	}

	live := carveWithAssertions(t, "comment-beside-live-refusal.json",
		"// шаг проверяет отказ: 403 от края",
		"pm.test('DENY', () => pm.expect(pm.response.code).to.equal(403));")
	if live.refusalProbes != 1 {
		t.Fatalf("исполняемое утверждение отказа рядом с комментарием не распознано: "+
			"refusalProbes=%d — различитель ослеп на законной форме", live.refusalProbes)
	}
	if len(live.hits) != 0 {
		t.Errorf("гейт сработал на пробе отказа с комментарием рядом: %v", live.hits)
	}
}

// TestRefusalDiscriminatorReadsCodeNotMessageStrings — ось 3, обе стороны.
//
// Число в ТЕКСТЕ сообщения ничего не допускает: «unexpected 200: …» — это подпись к
// падению, а не утверждение о коде. Прежняя редакция читала такой шаг как
// допускающий 200, то есть как фикстуру, и требовала от него правильной нарезки —
// хотя нарезки там не происходит вовсе.
func TestRefusalDiscriminatorReadsCodeNotMessageStrings(t *testing.T) {
	inMessage := carveWithAssertions(t, "code-in-message.json",
		"pm.test('DENY', () => pm.expect(pm.response.code, 'unexpected 200: ' + pm.response.text()).to.eql(404));")
	if inMessage.refusalProbes != 1 {
		t.Fatalf("успешный код ИЗ ТЕКСТА СООБЩЕНИЯ снял шаг с учёта проб отказа: "+
			"refusalProbes=%d, ожидался 1", inMessage.refusalProbes)
	}
	if inMessage.subnetFixtures != 0 || len(inMessage.hits) != 0 {
		t.Errorf("проба отказа засчитана фикстурой из-за числа в сообщении: "+
			"subnetFixtures=%d hits=%v", inMessage.subnetFixtures, inMessage.hits)
	}

	inCode := carveWithAssertions(t, "code-in-code.json",
		"pm.test('status 200', () => pm.expect(pm.response.code, 'expected success').to.eql(200));")
	if inCode.refusalProbes != 0 {
		t.Fatalf("шаг, утверждающий УСПЕХ, засчитан пробой отказа: refusalProbes=%d", inCode.refusalProbes)
	}
	if inCode.subnetFixtures != 1 || len(inCode.hits) != 1 {
		t.Errorf("шаг, утверждающий успех, обязан судиться как фикстура: subnetFixtures=%d hits=%v",
			inCode.subnetFixtures, inCode.hits)
	}
}

// TestRefusalDiscriminatorIgnoresPollingControlFlow — управляющий поток о коде НЕ
// утверждает. `if (pm.response.code === 200 && c < 50) { … }` — это петля ожидания;
// она не обещает ни успеха, ни отказа, и её число не является допущенным кодом.
//
// Обе стороны на одном шаге: петля рядом с настоящим утверждением отказа не смеет
// это утверждение отменить (красная сторона прежней редакции — она отменяла), а
// петля рядом с утверждением успеха не смеет превратить шаг в пробу отказа.
func TestRefusalDiscriminatorIgnoresPollingControlFlow(t *testing.T) {
	polled := carveWithAssertions(t, "polling-then-refusal.json",
		"let c = 0; if (pm.response.code === 200 && c < 50) { c++; postman.setNextRequest(pm.info.requestName); }",
		"pm.test('gone', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.be.oneOf([404, 403]));")
	if polled.refusalProbes != 1 {
		t.Fatalf("петля ожидания отменила утверждение отказа: refusalProbes=%d, ожидался 1",
			polled.refusalProbes)
	}
	if len(polled.hits) != 0 {
		t.Errorf("гейт сработал на пробе отказа с петлёй ожидания: %v", polled.hits)
	}

	guarded := carveWithAssertions(t, "polling-then-success.json",
		"if (pm.response.code !== 403) { pm.environment.set('ok', '1'); }",
		"pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));")
	if guarded.refusalProbes != 0 {
		t.Fatalf("управляющий поток «!== 403» превратил шаг успеха в пробу отказа: refusalProbes=%d",
			guarded.refusalProbes)
	}
	if guarded.subnetFixtures != 1 || len(guarded.hits) != 1 {
		t.Errorf("шаг обязан судиться как фикстура: subnetFixtures=%d hits=%v",
			guarded.subnetFixtures, guarded.hits)
	}
}

// TestRefusalDiscriminatorReadsChainsBrokenAcrossLines — утверждение, чьи аргументы
// заняли две строки, а `.to.…` встал на третьей, обязано читаться целиком.
//
// Сам перенос снят с дерева: генератор так пишет, когда подпись к падению длинная
// (замер на dfd2c027 — 79 вызовов). Там за переносом стоит проверка типа, здесь —
// код, иначе разницу «прочитано / не прочитано» нечем наблюдать: разбор, считавший
// цепочку от переноса, не видит утверждения вовсе и молча возвращает шаг в фикстуры.
// Обе стороны на одной форме: отказ через перенос — проба; успех через перенос —
// фикстура.
func TestRefusalDiscriminatorReadsChainsBrokenAcrossLines(t *testing.T) {
	wrapped := carveWithAssertions(t, "wrapped-refusal.json",
		"pm.test('DENY', () => {",
		"  pm.expect(pm.response.code,",
		"    'expected a refusal, got: ' + pm.response.text())",
		"    .to.equal(403);",
		"});")
	if wrapped.refusalProbes != 1 {
		t.Fatalf("утверждение отказа с переносом строки не прочитано: refusalProbes=%d, "+
			"ожидался 1 — «не найдено» здесь означало бы «не читал»", wrapped.refusalProbes)
	}
	if len(wrapped.hits) != 0 {
		t.Errorf("гейт сработал на пробе отказа, записанной в несколько строк: %v", wrapped.hits)
	}

	wrappedOK := carveWithAssertions(t, "wrapped-success.json",
		"pm.test('OK', () => {",
		"  pm.expect(pm.response.code,",
		"    'expected success, got: ' + pm.response.text())",
		"    .to.equal(200);",
		"});")
	if wrappedOK.refusalProbes != 0 {
		t.Fatalf("утверждение УСПЕХА с переносом строки прочитано как отказ: refusalProbes=%d",
			wrappedOK.refusalProbes)
	}
	if wrappedOK.subnetFixtures != 1 || len(wrappedOK.hits) != 1 {
		t.Errorf("шаг, утверждающий успех, обязан судиться как фикстура: subnetFixtures=%d hits=%v",
			wrappedOK.subnetFixtures, wrappedOK.hits)
	}
}
