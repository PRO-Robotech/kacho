// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

// subscription_stream_budget_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// проверка предела чтения посредника способна упасть.
//
// Настоящее дерево нельзя ни сломать, ни вернуть, а вердикт о нём о способности
// падать не говорит ничего: зелёный получает и та проверка, у которой предмета
// нет вовсе — ровно то состояние, из-за которого kacho#1402 и заведена.
//
// Стенд синтетический и повторяет форму настоящего дерева: чарт края со своим
// входом, умбрелла, объявившая его зависимостью `file://`, и её собственный вход.
// Каждое утверждение стоит ПАРОЙ: внесённый дефект краснеет и называет
// координату, законный близнец той же формы — молчит.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type budgetStand struct{ root string }

const (
	standChartDir = "gateway/deploy"
	standUmbrella = "deploy/helm/umbrella"

	// Шаблон, ВЫДАЮЩИЙ предел чтения из величины чарта края.
	tplEmitsTimeout = "kind: Ingress\nmetadata:\n  annotations:\n" +
		"    nginx.ingress.kubernetes.io/proxy-read-timeout: \"{{ .Values.ingress.proxyReadTimeout }}\"\n"
	// ЗАКОННЫЙ БЛИЗНЕЦ: шаблон, который о величине только УПОМИНАЕТ.
	// Комментарий рядом с механизмом — не механизм, и считать его потребителем
	// значило бы объявить величину живой по её же объяснению.
	tplMentionsOnly = "kind: Deployment\n# срок жизни потока обязан быть меньше " +
		"`ingress.proxyReadTimeout` края\n"
)

// newBudgetStand — ЗАКОННОЕ состояние: у величины два потребителя, один включён.
func newBudgetStand(t *testing.T) *budgetStand {
	t.Helper()
	s := &budgetStand{root: t.TempDir()}
	s.write(t, standChartDir+"/Chart.yaml", "apiVersion: v2\nname: api-gateway\nversion: 0.0.1\n")
	s.write(t, standChartDir+"/values.yaml",
		"ingress:\n  enabled: true\n  proxyReadTimeout: \"120\"\n"+
			"subscriptionStream:\n  streamBudget: 90s\n")
	s.write(t, standChartDir+"/templates/ingress.yaml", tplEmitsTimeout)
	s.write(t, standChartDir+"/templates/deployment.yaml", tplMentionsOnly)

	s.write(t, standUmbrella+"/Chart.yaml", "apiVersion: v2\nname: umbrella\nversion: 0.0.1\n"+
		"dependencies:\n  - name: api-gateway\n    version: \">= 0.0.0\"\n"+
		"    repository: file://../../../gateway/deploy\n")
	s.write(t, standUmbrella+"/values.yaml",
		"api-gateway:\n  ingress:\n    enabled: false\napiGatewayIngress:\n  enabled: true\n")
	s.write(t, standUmbrella+"/templates/api-gateway-ingress.yaml", tplEmitsTimeout)
	return s
}

func (s *budgetStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *budgetStand) remove(t *testing.T, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(s.root, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("remove %s: %v", rel, err)
	}
}

// standCatalogue — перечень потребителей стенда, той же формы, что настоящий.
var standCatalogue = map[string]proxyConsumerRecord{
	standChartDir + "/templates/ingress.yaml":             {Toggle: "api-gateway.ingress.enabled", Why: "вход подчарта"},
	standUmbrella + "/templates/api-gateway-ingress.yaml": {Toggle: "apiGatewayIngress.enabled", Why: "вход умбреллы"},
}

func (s *budgetStand) auditConsumers(t *testing.T, cat map[string]proxyConsumerRecord) ([]string, proxyConsumerCensus) {
	t.Helper()
	return auditProxyConsumers(t, s.root, standChartDir, cat)
}

// TestBudgetGateStaysSilentWhenTheValueHasALiveConsumer — КОНТРОЛЬ.
//
// Без него отрицания ниже зеленели бы на разборе, переставшем что-либо узнавать:
// «потребителей нет» и «потребителей не искали» дают один и тот же пустой список.
func TestBudgetGateStaysSilentWhenTheValueHasALiveConsumer(t *testing.T) {
	s := newBudgetStand(t)
	findings, census := s.auditConsumers(t, standCatalogue)
	if len(findings) != 0 {
		t.Fatalf("законное дерево объявлено негодным: %v", findings)
	}
	if len(census.Parents) != 1 {
		t.Fatalf("чартов-родителей найдено %d, ожидался 1: зависимость `file://` не выведена", len(census.Parents))
	}
	if len(census.Consumers) != 2 || census.Live != 1 {
		t.Fatalf("потребителей %d (ожидалось 2), включённых %d (ожидался 1): %v",
			len(census.Consumers), census.Live, census.Consumers)
	}
	// Близнец ПРОЧИТАН, а не пропущен обходом: шаблон-упоминание осмотрен и
	// осознанно не признан потребителем.
	if census.Templates != 3 {
		t.Fatalf("шаблонов осмотрено %d, ожидалось 3: близнец-упоминание не прочитан", census.Templates)
	}
}

// TestBudgetGateFailsWhenTheValueHasNoConsumerAtAll — ИНЪЕКЦИЯ, воспроизводящая
// РОВНО то состояние, которого опасалась kacho#1402.
//
// Снимаются оба шаблона, ВЫДАЮЩИЕ предел; шаблон-упоминание остаётся на месте.
// Величина при этом продолжает быть объявленной и выглядеть действующей — именно
// поэтому её мёртвость и не видна ничем, кроме этой проверки.
func TestBudgetGateFailsWhenTheValueHasNoConsumerAtAll(t *testing.T) {
	s := newBudgetStand(t)
	s.remove(t, standChartDir+"/templates/ingress.yaml")
	s.remove(t, standUmbrella+"/templates/api-gateway-ingress.yaml")

	findings, census := s.auditConsumers(t, nil)
	if len(findings) != 1 {
		t.Fatalf("величина без потребителя принята молча: находок %d %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "смотреть нечего") {
		t.Fatalf("находка не называет предмет: %q", findings[0])
	}
	if census.Templates == 0 {
		t.Fatal("шаблонов осмотрено 0 — отказ пришёл от пустого обхода, а не от отсутствия потребителя")
	}
}

// TestBudgetGateFailsWhenEveryConsumerIsSwitchedOff — потребители есть, но ни
// один не рендерится: «зелёное» снова означало бы «смотреть нечего».
func TestBudgetGateFailsWhenEveryConsumerIsSwitchedOff(t *testing.T) {
	s := newBudgetStand(t)
	s.write(t, standUmbrella+"/values.yaml",
		"api-gateway:\n  ingress:\n    enabled: false\napiGatewayIngress:\n  enabled: false\n")

	findings, census := s.auditConsumers(t, standCatalogue)
	if census.Live != 0 {
		t.Fatalf("включённых потребителей %d, ожидалось 0: выключатель не прочитан", census.Live)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "не включён в поставке") {
		t.Fatalf("выключенные потребители приняты молча: %v", findings)
	}
}

// TestBudgetGateRefusesAnUnknownToggle — выключатель, которого в значениях нет,
// НЕ считается включённым.
//
// Неизвестное состояние не бывает разрешением: иначе опечатка в имени ключа
// объявляла бы вход живым, и проверка выше снова сторожила бы пустоту.
func TestBudgetGateRefusesAnUnknownToggle(t *testing.T) {
	s := newBudgetStand(t)
	cat := map[string]proxyConsumerRecord{
		standChartDir + "/templates/ingress.yaml":             {Toggle: "api-gateway.ingress.enabled", Why: "вход подчарта"},
		standUmbrella + "/templates/api-gateway-ingress.yaml": {Toggle: "apiGatewayIngress.enabledd", Why: "опечатка"},
	}
	findings, census := s.auditConsumers(t, cat)
	if census.Live != 0 {
		t.Fatalf("ненайденный выключатель принят за включённый (включённых %d)", census.Live)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "не включён в поставке") {
		t.Fatalf("ненайденный выключатель принят молча: %v", findings)
	}
}

// TestBudgetGateNamesANewConsumerAndAnExpiredRecord — перечень потребителей
// самоистекает в ОБЕ стороны.
func TestBudgetGateNamesANewConsumerAndAnExpiredRecord(t *testing.T) {
	t.Run("новый потребитель без записи — находка с координатой", func(t *testing.T) {
		s := newBudgetStand(t)
		s.write(t, standUmbrella+"/templates/second-ingress.yaml", tplEmitsTimeout)
		findings, _ := s.auditConsumers(t, standCatalogue)
		if len(findings) != 1 {
			t.Fatalf("новый вход принят молча: %v", findings)
		}
		if !strings.Contains(findings[0], "second-ingress.yaml") {
			t.Fatalf("находка не называет координату: %q", findings[0])
		}
	})

	t.Run("запись без потребителя — сама находка", func(t *testing.T) {
		s := newBudgetStand(t)
		s.remove(t, standChartDir+"/templates/ingress.yaml")
		findings, _ := s.auditConsumers(t, standCatalogue)
		if len(findings) != 1 {
			t.Fatalf("истёкшая запись пережила свой предмет молча: %v", findings)
		}
		if !strings.Contains(findings[0], "templates/ingress.yaml") {
			t.Fatalf("находка не называет истёкшую запись: %q", findings[0])
		}
	})
}

// TestBudgetGateReadsTheWinningValueNotTheDefault — ИНЪЕКЦИЯ в сверку.
//
// Профиль вправе переопределить предел чтения, и тогда умолчание подчарта — не то
// число, под которое обязан помещаться срок жизни потока. Проверка, читающая одно
// умолчание, на таком профиле молчит.
func TestBudgetGateReadsTheWinningValueNotTheDefault(t *testing.T) {
	t.Run("профиль поднимает предел — молчание, ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ", func(t *testing.T) {
		s := newBudgetStand(t)
		s.write(t, standUmbrella+"/values.prod.yaml",
			"api-gateway:\n  ingress:\n    proxyReadTimeout: \"300\"\n")
		findings, census := auditWinningValue(t, s.root, standChartDir)
		if len(findings) != 0 {
			t.Fatalf("законное переопределение объявлено негодным: %v", findings)
		}
		if census.Overrides != 1 {
			t.Fatalf("переопределений насчитано %d, ожидалось 1: профиль не прочитан", census.Overrides)
		}
	})

	t.Run("профиль опускает предел ПОД срок жизни потока — находка", func(t *testing.T) {
		s := newBudgetStand(t)
		s.write(t, standUmbrella+"/values.prod.yaml",
			"api-gateway:\n  ingress:\n    proxyReadTimeout: \"60\"\n")
		findings, census := auditWinningValue(t, s.root, standChartDir)
		if len(findings) != 1 {
			t.Fatalf("переопределение под сроком жизни принято молча: %v", findings)
		}
		if !strings.Contains(findings[0], "values.prod.yaml") {
			t.Fatalf("находка не называет профиль: %q", findings[0])
		}
		if census.Profiles < 2 {
			t.Fatalf("профилей осмотрено %d: находка пришла с непрочитанного дерева", census.Profiles)
		}
	})

	t.Run("профилей нет вовсе — отказ, а не молчание", func(t *testing.T) {
		s := newBudgetStand(t)
		s.remove(t, standUmbrella+"/values.yaml")
		findings, census := auditWinningValue(t, s.root, standChartDir)
		if census.Profiles != 0 {
			t.Fatalf("профилей осмотрено %d, ожидалось 0", census.Profiles)
		}
		if len(findings) != 1 || !strings.Contains(findings[0], "профилей не читали") {
			t.Fatalf("пустой обход профилей выдан за успех: %v", findings)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Сверка страниц с профилем: доказательство способности упасть.
//
// Дефект, ради которого утверждения ниже написаны, ЖИЛ (kacho#1528): инженерная
// страница объявляла потолок вызывающего `8`, когда профиль поставлял `10`, и
// расходилась при этом сама с собой — четыре места, два верных. Настоящее дерево
// сегодня согласовано, поэтому вердикт о нём о способности гейта упасть не говорит
// ничего; дефект вносится синтетической страницей.
//
// Каждое утверждение стоит ПАРОЙ: внесённый дефект краснеет и называет координату,
// законный близнец той же формы — молчит.

// pageWithDeclaration — инженерная страница той же формы, что настоящая: сперва
// объявление действующих величин, затем ДАТИРОВАННАЯ таблица замера.
//
// Датированная часть здесь не для полноты: её `8` и есть законный близнец —
// верная запись о прошлом, на которой гейт обязан молчать.
func pageWithDeclaration(perSubject string) string {
	return "## Решение\n" +
		"\n" +
		"| ключ профиля края | поставляется | предикат |\n" +
		"|---|---:|---|\n" +
		"| `subscriptionStream.maxStreamsPerSubject` — потолок вызывающего | `" + perSubject +
		"` | `grep -n maxStreamsPerSubject gateway/deploy/values.yaml` |\n" +
		"| `subscriptionStream.maxStreams` — потолок реплики | `16` | `grep -n 'maxStreams:' …` |\n" +
		"\n" +
		"## Замер, на котором решение стоит\n" +
		"\n" +
		"| величина | `ef68f94ec` | перемер `c40719694` | предикат |\n" +
		"|---|---:|---:|---|\n" +
		"| потолок реплики | 64 | **16** | `grep -n maxStreams gateway/deploy/values.yaml` |\n" +
		"| потолок вызывающего | 8 | 8 | там же |\n" +
		"\n" +
		"Оба потолка отвечают `429` и кодом `8` — совпадение знака при другом референте.\n"
}

// shippedClaims — то, что страница обязана называть при поставляемых 10 и 16.
func shippedClaims() []limitClaim {
	return []limitClaim{
		{"subscriptionStream.maxStreamsPerSubject", "10"},
		{"subscriptionStream.maxStreams", "16"},
	}
}

// TestLimitPageGateStaysSilentOnADatedEightBesideALiveTen — КОНТРОЛЬ и законный
// близнец разом.
//
// Страница несёт `8` дважды в датированной таблице и ещё раз кодом
// `RESOURCE_EXHAUSTED`, а объявляет `10`. Гейт, ловящий ЗНАК, покраснел бы здесь и
// был бы отключён первым же читателем; гейт, читающий ОБЪЯВЛЕНИЕ, молчит.
//
// Без этого утверждения отрицания ниже зеленели бы на разборе, переставшем
// что-либо узнавать: «расхождений нет» и «строк не нашли» дают один пустой список.
func TestLimitPageGateStaysSilentOnADatedEightBesideALiveTen(t *testing.T) {
	rows := engineeringLimitRows(pageWithDeclaration("10"))
	if len(rows) != 2 {
		t.Fatalf("строк объявления разобрано %d, ожидалось 2: %v — датированная таблица "+
			"попала в объявление либо объявление не прочитано вовсе", len(rows), rows)
	}
	if got := rows["subscriptionStream.maxStreamsPerSubject"]; got != "10" {
		t.Fatalf("из объявления взято %q вместо %q — разбор берёт не ту ячейку", got, "10")
	}
	if findings := streamLimitFindings(rows, shippedClaims()); len(findings) != 0 {
		t.Fatalf("согласованная страница объявлена расходящейся: %v", findings)
	}
}

// TestLimitPageGateFailsWhenTheDeclarationOutlivesTheChart — ИНЪЕКЦИЯ,
// воспроизводящая РОВНО состояние kacho#1528.
//
// Ломается только НОВОЕ свойство: страница остаётся годной по форме — объявление
// на месте, обе строки разбираются, датированная часть верна. Неверно одно число.
func TestLimitPageGateFailsWhenTheDeclarationOutlivesTheChart(t *testing.T) {
	rows := engineeringLimitRows(pageWithDeclaration("8"))
	if len(rows) != 2 {
		t.Fatalf("строк объявления разобрано %d, ожидалось 2: инъекция сломала не то, "+
			"что проверяется", len(rows))
	}
	findings := streamLimitFindings(rows, shippedClaims())
	if len(findings) != 1 {
		t.Fatalf("пережившее свой предмет объявление принято молча: %v", findings)
	}
	for _, want := range []string{"subscriptionStream.maxStreamsPerSubject", `"8"`, `"10"`} {
		if !strings.Contains(findings[0], want) {
			t.Fatalf("находка не называет %s — читатель не узнает, что и на что править: %q",
				want, findings[0])
		}
	}
}

// TestLimitPageGateFailsWhenTheDeclarationIsRemoved — объявление СНЯТО.
//
// Самый дешёвый способ заглушить сверку — убрать то, что сверяют. Пустой разбор
// обязан быть отказом, иначе «ноль расхождений» становится неотличимо от «ноль
// прочитанного», и страница уходит из-под наблюдения молча.
func TestLimitPageGateFailsWhenTheDeclarationIsRemoved(t *testing.T) {
	page := pageWithDeclaration("10")
	stripped := page[strings.Index(page, "## Замер"):]
	if rows := engineeringLimitRows(stripped); len(rows) != 0 {
		t.Fatalf("после снятия объявления разобрано %d строк: %v — разбор берёт "+
			"датированную таблицу за объявление", len(rows), rows)
	}
	// Утверждения при этом не исчезают: их назначает профиль, и каждое становится
	// находкой «величина не названа вовсе».
	findings := streamLimitFindings(map[string]string{}, shippedClaims())
	if len(findings) != 2 {
		t.Fatalf("снятое объявление дало %d находок, ожидалось 2: %v", len(findings), findings)
	}
}
