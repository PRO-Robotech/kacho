// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_posture_stack_injection_test.go — доказательство того, что проверка стека
// посадки `own` СПОСОБНА упасть и способна смолчать.
//
// Вход СИНТЕТИЧЕСКИЙ: подделка дерева трогает общий клон, а вердикт обязан
// доказываться на входе, построенном здесь и целиком видном читателю.
//
// Каждый случай меняет РОВНО ОДИН факт против законного близнеца.
package deploy_test

import (
	"strings"
	"testing"
)

// legalOwnStackFacts — законный близнец: обе половины на `own`, обе называют
// один слушатель.
func legalOwnStackFacts() ownStackFacts {
	return ownStackFacts{
		Stack: "own", IAMPosture: "own", EdgePosture: "own",
		LanePort: "9100", LaneURL: "https://kaname-internal.kacho.svc:9100",
		ServiceName: "kaname", AccessKeys: true,
		IssuancePort: "9096", IssuanceURL: "https://kaname.kacho.svc:9096",
	}
}

// legalExternalStackFacts — стенд на `external`: адреса полосы у него нет, и это
// не находка. Нужен в каждом случае, иначе перечень без стека на `own` даст
// находку «ни один стек не объявляет own» и замаскирует предмет оси.
func legalExternalStackFacts() ownStackFacts {
	return ownStackFacts{Stack: "prod", IAMPosture: "external", EdgePosture: "external"}
}

func TestOwnStackJudgement_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(f *ownStackFacts)
		want    int
		mustSay string
	}{
		{
			name:   "законный близнец: половины согласны об одной двери — молчит",
			mutate: func(*ownStackFacts) {},
			want:   0,
		},
		{
			name:    "порты половин РАЗОШЛИСЬ — находка: класс, невидимый ни с одной стороны",
			mutate:  func(f *ownStackFacts) { f.LaneURL = "https://kaname-internal.kacho.svc:9098" },
			want:    1,
			mustSay: "называют РАЗНЫЕ двери",
		},
		{
			// kacho#2725: край набирает порт СЛУЖБЫ, а не слушателя. Профиль
			// переопределил порт Службы, адрес края остался на порту слушателя —
			// край стучится в порт, которого у Службы нет.
			name:    "Служба переопределила порт, край идёт на порт слушателя — находка",
			mutate:  func(f *ownStackFacts) { f.ServicePort = "9101" },
			want:    1,
			mustSay: "service.internal.loginLanePort",
		},
		{
			// Законный близнец случая выше: переопределение есть, и край идёт
			// ровно на него. Меняется один факт против красного — порт адреса.
			name: "Служба переопределила порт, и край идёт на него — молчит",
			mutate: func(f *ownStackFacts) {
				f.ServicePort = "9101"
				f.LaneURL = "https://kaname-internal.kacho.svc:9101"
			},
			want: 0,
		},
		{
			// `default` шаблона считает нуль пустым: Служба остаётся на порту
			// слушателя, и край, идущий на него, прав.
			name:   "переопределение нулём — умолчание шаблона, молчит",
			mutate: func(f *ownStackFacts) { f.ServicePort = "0" },
			want:   0,
		},
		{
			name:    "адрес полосы не объявлен — находка",
			mutate:  func(f *ownStackFacts) { f.LaneURL = "" },
			want:    1,
			mustSay: "iamLoginLaneUrl",
		},
		{
			name:    "порт слушателя не объявлен — находка",
			mutate:  func(f *ownStackFacts) { f.LanePort = "" },
			want:    2, // нет порта + порт адреса ни с чем не сходится
			mustSay: "ports.loginLane",
		},
		{
			name:    "адрес по открытому протоколу — находка: печенье сессии уехало бы в чистом виде",
			mutate:  func(f *ownStackFacts) { f.LaneURL = "http://kaname-internal.kacho.svc:9100" },
			want:    1,
			mustSay: "не https",
		},
		{
			name:    "адрес ведёт на ПУБЛИЧНЫЙ Service — находка",
			mutate:  func(f *ownStackFacts) { f.LaneURL = "https://kaname.kacho.svc:9100" },
			want:    1,
			mustSay: "ВНУТРЕННЕМ Service",
		},
		{
			name:    "посадку объявила только служба — находка",
			mutate:  func(f *ownStackFacts) { f.EdgePosture = "external" },
			want:    1,
			mustSay: "половины объявили РАЗНОЕ",
		},
		{
			name:    "посадку объявил только край — находка",
			mutate:  func(f *ownStackFacts) { f.IAMPosture = "external" },
			want:    1,
			mustSay: "половины объявили РАЗНОЕ",
		},
		{
			// Привязка ключей доступа не требуется, пока её не требует ПИН:
			// законный близнец сегодняшнего дерева.
			name:   "привязки нет, и пин её не требует — НЕ находка",
			mutate: func(f *ownStackFacts) { f.AccessKeys = false },
			want:   0,
		},
		// ── Вторая цель ретрансляции: слушатель выдачи (kacho#2817) ──────────
		{
			// Состояние дерева до этой задачи: адрес формы задан, адреса выдачи
			// нет, и край под own не стартует.
			name:    "адрес слушателя выдачи не объявлен — находка",
			mutate:  func(f *ownStackFacts) { f.IssuanceURL = "" },
			want:    1,
			mustSay: "iamIssuanceUrl` не объявлен",
		},
		{
			// Правдоподобная ошибка: церемонию направили в дверь полосы формы.
			name:    "церемония ретранслируется на порт формы — находка",
			mutate:  func(f *ownStackFacts) { f.IssuanceURL = "https://kaname.kacho.svc:9100" },
			want:    1,
			mustSay: "слушатель выдачи на \"9096\"",
		},
		{
			name:    "служба переопределила порт выдачи, край на прежнем — находка",
			mutate:  func(f *ownStackFacts) { f.IssuancePort = "9196" },
			want:    1,
			mustSay: "`ports.registryToken` = \"9196\"",
		},
		{
			// Законный близнец случая выше: меняется один факт — порт адреса.
			name: "служба переопределила порт выдачи, и край идёт на него — молчит",
			mutate: func(f *ownStackFacts) {
				f.IssuancePort = "9196"
				f.IssuanceURL = "https://kaname.kacho.svc:9196"
			},
			want: 0,
		},
		{
			// Профиль порт не объявил — Служба на умолчании шаблона, и край,
			// идущий на него, прав.
			name:   "порт выдачи не объявлен профилем — умолчание шаблона, молчит",
			mutate: func(f *ownStackFacts) { f.IssuancePort = "" },
			want:   0,
		},
		{
			name:   "порт выдачи нулём — умолчание шаблона, молчит",
			mutate: func(f *ownStackFacts) { f.IssuancePort = "0" },
			want:   0,
		},
		{
			// Без порта край набрал бы 443 — двери с таким номером у Службы нет.
			name:    "адрес выдачи без порта — находка",
			mutate:  func(f *ownStackFacts) { f.IssuanceURL = "https://kaname.kacho.svc" },
			want:    1,
			mustSay: "на порт \"\"",
		},
		{
			name:    "адрес выдачи по открытому протоколу — находка",
			mutate:  func(f *ownStackFacts) { f.IssuanceURL = "http://kaname.kacho.svc:9096" },
			want:    1,
			mustSay: "код авторизации",
		},
		{
			name:    "адрес выдачи не абсолютный — находка",
			mutate:  func(f *ownStackFacts) { f.IssuanceURL = "kaname.kacho.svc:9096" },
			want:    1,
			mustSay: "`iamIssuanceUrl` = \"kaname.kacho.svc:9096\" не абсолютный",
		},
		{
			name:    "адрес выдачи ведёт на ВНУТРЕННИЙ Service — находка",
			mutate:  func(f *ownStackFacts) { f.IssuanceURL = "https://kaname-internal.kacho.svc:9096" },
			want:    1,
			mustSay: "ПУБЛИЧНОМ Service",
		},
		{
			// Голого имени службы серверный лист не предъявляет.
			name:    "адрес выдачи голым именем службы — находка",
			mutate:  func(f *ownStackFacts) { f.IssuanceURL = "https://kaname:9096" },
			want:    1,
			mustSay: "ПУБЛИЧНОМ Service",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := legalOwnStackFacts()
			c.mutate(&f)
			findings, census := judgeOwnStacks([]ownStackFacts{f, legalExternalStackFacts()}, false)
			if len(findings) != c.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(findings), c.want, findings)
			}
			if c.mustSay != "" {
				var said bool
				for _, got := range findings {
					if strings.Contains(got, c.mustSay) {
						said = true
					}
				}
				if !said {
					t.Errorf("ни одна находка не называет %q: %v", c.mustSay, findings)
				}
			}
			if census.Stacks != 2 {
				t.Errorf("перепись осмотренного %d, подано 2", census.Stacks)
			}
		})
	}
}

// TestOwnStackJudgement_OneTargetDoesNotMaskTheOther — суждения о двух целях
// независимы: незаданный адрес формы не снимает суждения о выдаче. Случай
// складывается из двух одно-фактных случаев таблицы выше («адрес полосы не
// объявлен», «адрес слушателя выдачи не объявлен») и требует обе их находки:
// ранний выход на первой цели молча оставил бы вторую без суждения.
func TestOwnStackJudgement_OneTargetDoesNotMaskTheOther(t *testing.T) {
	f := legalOwnStackFacts()
	f.LaneURL = ""
	f.IssuanceURL = ""
	findings, _ := judgeOwnStacks([]ownStackFacts{f}, false)
	if len(findings) != 2 {
		t.Fatalf("находок %d, ожидалось 2 (по одной на цель): %v", len(findings), findings)
	}
	for _, want := range []string{"iamLoginLaneUrl` не объявлен", "iamIssuanceUrl` не объявлен"} {
		var said bool
		for _, got := range findings {
			if strings.Contains(got, want) {
				said = true
			}
		}
		if !said {
			t.Errorf("ни одна находка не называет %q: %v", want, findings)
		}
	}
}

// TestOwnStackJudgement_AbsentOwnStackIsAFinding — состояние дерева ДО этой
// задачи: стеков шесть, на `own` ни одного. Проверка, молчащая здесь, не поймала
// бы ровно тот предмет, ради которого заведена.
func TestOwnStackJudgement_AbsentOwnStackIsAFinding(t *testing.T) {
	facts := []ownStackFacts{
		{Stack: "dev", IAMPosture: "external", EdgePosture: "external"},
		{Stack: "prod", IAMPosture: "external", EdgePosture: "external"},
	}
	findings, census := judgeOwnStacks(facts, false)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "не объявляет посадку `own`") {
		t.Errorf("находка не называет предмета: %s", findings[0])
	}
	if census.Own != 0 {
		t.Errorf("на посадке own насчитано %d, подан 0", census.Own)
	}
}

// TestOwnStackJudgement_BindingArmsItselfWithThePin — условие вооружается пином:
// та же раскладка молчит, пока пин привязки не требует, и краснеет, когда
// требует. Без этой оси «сегодня зелено» было бы неотличимо от «проверка мертва».
func TestOwnStackJudgement_BindingArmsItselfWithThePin(t *testing.T) {
	f := legalOwnStackFacts()
	f.AccessKeys = false

	silent, _ := judgeOwnStacks([]ownStackFacts{f}, false)
	if len(silent) != 0 {
		t.Fatalf("пин привязки не требует, а проверка заговорила: %v", silent)
	}

	armed, _ := judgeOwnStacks([]ownStackFacts{f}, true)
	if len(armed) != 1 {
		t.Fatalf("пин требует привязки, находок %d, ожидалась 1: %v", len(armed), armed)
	}
	if !strings.Contains(armed[0], "access-keys") {
		t.Errorf("находка не называет ручек привязки: %s", armed[0])
	}
}

// TestOwnStackServicePortModel_StaleTemplateIsSaid — предпосылка пробы порта
// Службы способна упасть: шаблон, выставляющий порт полосы иным выражением или
// не выставляющий его вовсе, распознаётся, а сегодняшняя форма — нет (kacho#2725).
func TestOwnStackServicePortModel_StaleTemplateIsSaid(t *testing.T) {
	const lawful = "    {{- if .Values.ports.loginLane }}\n" +
		"    - name: http-login-lane\n" +
		"      port: {{ .Values.service.internal.loginLanePort | default .Values.ports.loginLane }}\n" +
		"      targetPort: http-login-lane\n" +
		"    {{- end }}\n"
	cases := []struct {
		name      string
		tmpl      string
		wantFound bool
		wantAgree bool
	}{
		{name: "законный близнец: сегодняшняя форма шаблона — модель верна", tmpl: lawful, wantFound: true, wantAgree: true},
		{
			name:      "комментарий между условием и записью — та же запись, модель верна",
			tmpl:      strings.Replace(lawful, "    - name:", "    # слушатель полосы\n    - name:", 1),
			wantFound: true, wantAgree: true,
		},
		{
			name:      "порт выставлен без переопределения — модель устарела",
			tmpl:      strings.Replace(lawful, ".Values.service.internal.loginLanePort | default .Values.ports.loginLane", ".Values.ports.loginLane", 1),
			wantFound: true,
		},
		{
			name:      "запись под другим условием — модель устарела",
			tmpl:      strings.Replace(lawful, "if .Values.ports.loginLane", "if .Values.service.internal.loginLaneEnabled", 1),
			wantFound: true,
		},
		{
			name: "записи порта полосы нет — не найдена",
			tmpl: strings.Replace(lawful, "http-login-lane\n      port", "http-other\n      port", 1),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cond, expr, ok := laneServicePortTemplate(c.tmpl)
			if ok != c.wantFound {
				t.Fatalf("запись найдена=%v, ожидалось %v (условие %q, выражение %q)", ok, c.wantFound, cond, expr)
			}
			agree := cond == laneServicePortCondition && expr == laneServicePortExpression
			if ok && agree != c.wantAgree {
				t.Errorf("модель согласна=%v, ожидалось %v: условие %q, выражение %q", agree, c.wantAgree, cond, expr)
			}
		})
	}
}

// TestOwnStackIssuancePortModel_StaleTemplateIsSaid — предпосылка суждения о
// второй цели способна упасть: запись `registry-token`, выставленная под
// условием, иным выражением или не выставленная вовсе, распознаётся, а
// сегодняшняя форма — нет (kacho#2817).
func TestOwnStackIssuancePortModel_StaleTemplateIsSaid(t *testing.T) {
	const lawful = "      targetPort: grpc\n" +
		"    # слушатель выдачи\n" +
		"    - name: registry-token\n" +
		"      port: {{ .Values.ports.registryToken | default 9096 }}\n" +
		"      targetPort: registry-token\n"
	cases := []struct {
		name      string
		tmpl      string
		wantFound bool
		wantAgree bool
	}{
		{name: "законный близнец: сегодняшняя форма шаблона — модель верна", tmpl: lawful, wantFound: true, wantAgree: true},
		{
			name:      "условие не над записью, а над соседней — запись без условия, модель верна",
			tmpl:      "    {{- if .Values.ports.rest }}\n    - name: http-rest\n      port: 9098\n    {{- end }}\n" + lawful,
			wantFound: true, wantAgree: true,
		},
		{
			name:      "запись под условием — модель устарела",
			tmpl:      strings.Replace(lawful, "    # слушатель", "    {{- if .Values.ports.registryToken }}\n    # слушатель", 1),
			wantFound: true,
		},
		{
			name:      "иное умолчание порта — модель устарела",
			tmpl:      strings.Replace(lawful, "default 9096", "default 9196", 1),
			wantFound: true,
		},
		{
			name:      "порт переопределяется Службой — модель устарела",
			tmpl:      strings.Replace(lawful, ".Values.ports.registryToken | default 9096", ".Values.service.public.registryTokenPort | default .Values.ports.registryToken", 1),
			wantFound: true,
		},
		{
			name: "записи слушателя выдачи нет — не найдена",
			tmpl: strings.Replace(lawful, "registry-token\n      port", "http-other\n      port", 1),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cond, expr, ok := issuanceServicePortTemplate(c.tmpl)
			if ok != c.wantFound {
				t.Fatalf("запись найдена=%v, ожидалось %v (условие %q, выражение %q)", ok, c.wantFound, cond, expr)
			}
			agree := cond == "" && expr == issuanceServicePortExpression
			if ok && agree != c.wantAgree {
				t.Errorf("модель согласна=%v, ожидалось %v: условие %q, выражение %q", agree, c.wantAgree, cond, expr)
			}
		})
	}
}
