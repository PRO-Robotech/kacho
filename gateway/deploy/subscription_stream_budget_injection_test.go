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
