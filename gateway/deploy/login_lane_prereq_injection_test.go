// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_prereq_injection_test.go — ДОКАЗАТЕЛЬСТВО В ОБЕ СТОРОНЫ для
// login_lane_prereq_test.go.
//
// Каждая проба ниже — ПАРА: дефект обязан краснеть и назвать стенд поимённо,
// законный близнец той же формы обязан молчать. Вход синтетический и подаётся
// судящим функциям напрямую: они чистые, дерева не читают, и инъекция потому не
// трогает ни профилей, ни рабочего каталога, ни кэша модулей.
//
// Отрицательная половина здесь обязательна не «для полноты». Гейт, чей предмет —
// ОТСУТСТВИЕ («ручку никто не снял»), молчит одинаково и когда дерево чисто, и
// когда предикат перестал узнавать свой предмет. Различает эти два случая
// только положительная проба: место, которое гейт ОБЯЗАН найти.
package deploy_test

import (
	"strings"
	"testing"
)

// lawfulLaneStack — законный стенд: ровно то, что объявляет профиль после
// правки. Каждая инъекция ниже меняет в нём РОВНО ОДИН факт против этого
// близнеца — иначе красное пришло бы от соседа, а не от предмета.
func lawfulLaneStack() lanePrereqStack {
	return lanePrereqStack{
		Stack:       "prod",
		LaneURL:     "https://kaname-internal.kacho.svc:9100",
		LanePort:    "9100",
		ServiceName: "kaname",
		Namespace:   "kacho",
	}
}

func laneFindingsText(findings []string) string { return strings.Join(findings, "\n") }

// mustSayLane — находка есть, и она называет каждую названную координату.
func mustSayLane(t *testing.T, findings []string, must ...string) {
	t.Helper()
	if len(findings) == 0 {
		t.Fatalf("инъекция не дала НИ ОДНОЙ находки — гейт слеп к своему предмету")
	}
	text := laneFindingsText(findings)
	for _, m := range must {
		if !strings.Contains(text, m) {
			t.Errorf("находка не называет %q; напечатано:\n%s", m, text)
		}
	}
}

func mustBeSilentLane(t *testing.T, findings []string) {
	t.Helper()
	if len(findings) != 0 {
		t.Fatalf("законный близнец дал находки — гейт красит исправное дерево:\n%s", laneFindingsText(findings))
	}
}

// TestLanePrereqInjection_LawfulTwinIsSilent — положительная половина ВСЕХ пар
// ниже: на законном стенде судящая функция молчит.
func TestLanePrereqInjection_LawfulTwinIsSilent(t *testing.T) {
	findings, census := judgeLanePrereq([]lanePrereqStack{lawfulLaneStack()})
	mustBeSilentLane(t, findings)
	if census.Stacks != 1 || census.WithURL != 1 || census.WithPort != 1 {
		t.Fatalf("перепись законного стенда не сошлась: %s", census)
	}
}

// TestLanePrereqInjection_MissingAddressIsFoundAndNamesTheStack — ПРЕДИКАТ 1:
// снятие `iamLoginLaneUrl` у ЛЮБОГО стенда краснеет, и в отказе назван стенд.
//
// Стенды перебираются все семь имён таблицы, и ни одно из них не привилегировано
// посадкой: предмет проверки в том и состоит, что оговорки «только own» нет.
func TestLanePrereqInjection_MissingAddressIsFoundAndNamesTheStack(t *testing.T) {
	for _, name := range []string{"dev", "dev-prod", "prod", "own", "fe3455", "prorobotech", "a8f60d"} {
		t.Run(name, func(t *testing.T) {
			twin := lawfulLaneStack()
			twin.Stack = name
			mustBeSilentLane(t, first(judgeLanePrereq([]lanePrereqStack{twin})))

			injected := twin
			injected.LaneURL = "" // РОВНО ОДИН факт против близнеца
			mustSayLane(t, first(judgeLanePrereq([]lanePrereqStack{injected})),
				"стенд "+name, "iamLoginLaneUrl", "не объявлен")
		})
	}
}

// TestLanePrereqInjection_MissingPortIsFoundAndNamesTheStack — ПРЕДИКАТ 2,
// первая его половина: снятие `kaname.ports.loginLane` краснеет с именем стенда.
func TestLanePrereqInjection_MissingPortIsFoundAndNamesTheStack(t *testing.T) {
	for _, name := range []string{"dev", "dev-prod", "prod", "own", "fe3455", "prorobotech", "a8f60d"} {
		t.Run(name, func(t *testing.T) {
			injected := lawfulLaneStack()
			injected.Stack = name
			injected.LanePort = ""
			mustSayLane(t, first(judgeLanePrereq([]lanePrereqStack{injected})),
				"стенд "+name, "kaname.ports.loginLane", "не объявлен")
		})
	}
}

// TestLanePrereqInjection_EmptyAndRelativeAddressesDoNotPassAsDeclared —
// ПРЕДИКАТ 4: пустое значение и относительный адрес не проходят за объявленное,
// `https` обязателен, адрес абсолютный. Законный адрес — молчит.
func TestLanePrereqInjection_EmptyAndRelativeAddressesDoNotPassAsDeclared(t *testing.T) {
	for _, c := range []struct {
		name string
		url  string
		must []string
	}{
		{"пусто", "", []string{"не объявлен"}},
		{"пробелы", "   ", []string{"не объявлен"}},
		{"относительный путь", "/iam/login", []string{"не абсолютный адрес"}},
		{"относительный путь без слеша", "kaname-internal.kacho.svc:9100", []string{"не абсолютный адрес"}},
		{"схема без хоста", "https://", []string{"не абсолютный адрес"}},
		{"http вместо https", "http://kaname-internal.kacho.svc:9100", []string{"не https"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			injected := lawfulLaneStack()
			injected.LaneURL = c.url
			mustSayLane(t, first(judgeLanePrereq([]lanePrereqStack{injected})), append([]string{"стенд prod"}, c.must...)...)
		})
	}

	// Законный близнец в том же прогоне: адрес, отличающийся от негодных ровно
	// тем, ради чего проверка заведена, обязан молчать.
	mustBeSilentLane(t, first(judgeLanePrereq([]lanePrereqStack{lawfulLaneStack()})))
}

// TestLanePrereqInjection_AddressAndListenerPortsAreComparedNotAsserted —
// сравнение имеет ДВУХ производителей: номер в адресе приходит из профиля края,
// номер двери — из профиля службы. Подстановка третьего значения делает
// утверждение ложным, то есть это сравнение, а не тождество, записанное
// синтаксисом утверждения.
func TestLanePrereqInjection_AddressAndListenerPortsAreComparedNotAsserted(t *testing.T) {
	injected := lawfulLaneStack()
	injected.LanePort = "9101" // дверь переехала, адрес остался
	mustSayLane(t, first(judgeLanePrereq([]lanePrereqStack{injected})),
		"стенд prod", "РАЗНЫЕ двери", "9100", "9101")

	// Внутренний Service вправе выставить СВОЙ номер — тогда сверять адрес надо с
	// ним, а не с портом контейнера. Законный близнец этой формы обязан молчать.
	own := lawfulLaneStack()
	own.LanePort = "9101"
	own.ServicePort = "9100"
	mustBeSilentLane(t, first(judgeLanePrereq([]lanePrereqStack{own})))
}

// TestLanePrereqInjection_HostIsDerivedFromTheTreeNotFromALiteral — хост
// выводится из `kaname.name` и `STACK_NAMESPACE`; адрес, ведущий на публичный
// Service или в чужое пространство имён, — находка.
func TestLanePrereqInjection_HostIsDerivedFromTheTreeNotFromALiteral(t *testing.T) {
	for _, c := range []struct{ name, url string }{
		{"публичный Service", "https://kaname.kacho.svc:9100"},
		{"чужое пространство имён", "https://kaname-internal.default.svc:9100"},
	} {
		t.Run(c.name, func(t *testing.T) {
			injected := lawfulLaneStack()
			injected.LaneURL = c.url
			mustSayLane(t, first(judgeLanePrereq([]lanePrereqStack{injected})),
				"стенд prod", "kaname-internal.kacho.svc")
		})
	}

	// Переименована служба — законный адрес переезжает вместе с ней и молчит.
	renamed := lawfulLaneStack()
	renamed.ServiceName = "kaname-alt"
	renamed.LaneURL = "https://kaname-alt-internal.kacho.svc:9100"
	mustBeSilentLane(t, first(judgeLanePrereq([]lanePrereqStack{renamed})))
}

// TestLanePrereqInjection_EighthStackWithoutTheKnobIsFound — ПРЕДИКАТ 3: число
// стендов выведено обходом, а не выписано.
//
// Семь законных стендов молчат; восьмой, появившийся без ручки, — находка с его
// именем. Гейт, у которого число выписано, на этом входе сошёлся бы («семь из
// семи объявили») и промолчал.
func TestLanePrereqInjection_EighthStackWithoutTheKnobIsFound(t *testing.T) {
	var seven []lanePrereqStack
	for _, name := range []string{"dev", "dev-prod", "prod", "own", "fe3455", "prorobotech", "a8f60d"} {
		s := lawfulLaneStack()
		s.Stack = name
		seven = append(seven, s)
	}
	findings, census := judgeLanePrereq(seven)
	mustBeSilentLane(t, findings)
	if census.Stacks != len(seven) || census.WithURL != len(seven) || census.WithPort != len(seven) {
		t.Fatalf("перепись семи законных стендов не сошлась: %s", census)
	}

	eighth := lanePrereqStack{Stack: "восьмой", ServiceName: "kaname", Namespace: "kacho"}
	findings, census = judgeLanePrereq(append(seven, eighth))
	mustSayLane(t, findings, "стенд восьмой", "iamLoginLaneUrl", "kaname.ports.loginLane")
	if census.Stacks != len(seven)+1 {
		t.Fatalf("знаменатель обхода не вырос с появлением восьмого стенда: %s", census)
	}
	if census.WithURL != len(seven) || census.WithPort != len(seven) {
		t.Fatalf("перепись объявивших не отличила восьмой стенд от остальных: %s", census)
	}
}

// TestLanePrereqInjection_EmptyWalkIsAFindingNotSilence — пустой обход даёт
// НАХОДКУ, а не зелёное: «стендов найдено ноль» и «находок ноль» обязаны быть
// различимы, иначе проверка на неоткрытом дереве неотличима от чистого.
func TestLanePrereqInjection_EmptyWalkIsAFindingNotSilence(t *testing.T) {
	findings, census := judgeLanePrereq(nil)
	mustSayLane(t, findings, "НИ ОДНОГО стенда", "третья категория")
	if census.Stacks != 0 {
		t.Fatalf("знаменатель обхода на пустом входе обязан быть нулём, получено: %s", census)
	}

	findings, census = judgeLanePrereq([]lanePrereqStack{})
	mustSayLane(t, findings, "НИ ОДНОГО стенда")
	if census.Stacks != 0 {
		t.Fatalf("пустой срез и nil обязаны читаться одинаково, получено: %s", census)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ДВА СЛЕДА РУЧКИ ПОРТА

// lawfulLaneManifest — манифест той же формы, что эмитит рендер подчарта: оба
// следа на месте и соединены. Аргументы позволяют снять РОВНО ОДИН из них.
func lawfulLaneManifest(containerTrace, serviceTrace bool) string {
	var b strings.Builder
	b.WriteString(`---
# Source: kaname/templates/service-internal.yaml
apiVersion: v1
kind: Service
metadata:
  name: kaname-internal
  namespace: kacho
spec:
  type: ClusterIP
  ports:
    - name: grpc-internal
      port: 9091
      targetPort: grpc-internal
`)
	if serviceTrace {
		b.WriteString(`    # Слушатель полосы входа паролем (Ф3, kacho#1269).
    - name: http-login-lane
      port: 9100
      targetPort: http-login-lane
`)
	}
	b.WriteString(`---
# Source: kaname/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kaname
  namespace: kacho
spec:
  template:
    spec:
      containers:
        - name: kaname
          ports:
            - name: grpc-internal
              containerPort: 9091
`)
	if containerTrace {
		b.WriteString(`            # Слушатель полосы входа паролем (Ф3, kacho#1269).
            - name: http-login-lane
              containerPort: 9100
`)
	}
	return b.String()
}

func laneTracesOf(t *testing.T, manifest string) laneTraces {
	t.Helper()
	tr, err := readLaneTraces(manifest)
	if err != nil {
		t.Fatalf("манифест не разбирается: %v", err)
	}
	return tr
}

// TestLanePrereqInjection_EachTraceIsFoundSeparately — ПРЕДИКАТ 2, вторая его
// половина: следов у ручки порта ДВА, и снятие ЛЮБОГО ОДНОГО краснеет.
//
// Это тот класс, который не виден ни с одной стороны по отдельности: сняли порт
// Service — под слушает, а дозвониться некуда; сняли порт контейнера — Service
// ведёт в никуда. Каждая половина по отдельности выглядит исправной.
func TestLanePrereqInjection_EachTraceIsFoundSeparately(t *testing.T) {
	s := lawfulLaneStack()

	t.Run("оба следа на месте — молчит", func(t *testing.T) {
		tr := laneTracesOf(t, lawfulLaneManifest(true, true))
		if tr.ContainerPort != "9100" || tr.ServicePort != "9100" || tr.ServiceTarget != "http-login-lane" {
			t.Fatalf("законный манифест прочитан неверно: %+v", tr)
		}
		mustBeSilentLane(t, judgeLaneTraces(s, tr))
	})

	t.Run("снят след контейнера", func(t *testing.T) {
		tr := laneTracesOf(t, lawfulLaneManifest(false, true))
		mustSayLane(t, judgeLaneTraces(s, tr), "стенд prod", "СЛЕД 1 из 2", "http-login-lane")
		// Красное пришло ТОЛЬКО от снятого следа: о втором не сказано ни слова.
		if strings.Contains(laneFindingsText(judgeLaneTraces(s, tr)), "СЛЕД 2 из 2") {
			t.Fatal("инъекция уронила соседний след — красное пришло не от предмета")
		}
	})

	t.Run("снят след Service", func(t *testing.T) {
		tr := laneTracesOf(t, lawfulLaneManifest(true, false))
		mustSayLane(t, judgeLaneTraces(s, tr), "стенд prod", "СЛЕД 2 из 2", "внутреннем Service")
		if strings.Contains(laneFindingsText(judgeLaneTraces(s, tr)), "СЛЕД 1 из 2") {
			t.Fatal("инъекция уронила соседний след — красное пришло не от предмета")
		}
	})

	t.Run("сняты оба", func(t *testing.T) {
		tr := laneTracesOf(t, lawfulLaneManifest(false, false))
		mustSayLane(t, judgeLaneTraces(s, tr), "СЛЕД 1 из 2", "СЛЕД 2 из 2")
	})
}

// TestLanePrereqInjection_TracesAreReadFromTheManifestNotFromItsText — след
// опознаётся РАЗБОРОМ манифеста, а не поиском подстроки: имя `http-login-lane`
// стоит в комментариях шаблона и доезжает до вывода рендера, а комментарий
// портом не является.
func TestLanePrereqInjection_TracesAreReadFromTheManifestNotFromItsText(t *testing.T) {
	commentOnly := `---
apiVersion: v1
kind: Service
metadata:
  name: kaname-internal
  namespace: kacho
spec:
  ports:
    # Слушатель полосы http-login-lane выставлен ТОЛЬКО здесь.
    - name: grpc-internal
      port: 9091
      targetPort: grpc-internal
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kaname
spec:
  template:
    spec:
      containers:
        - name: kaname
          ports:
            # порт http-login-lane поднимается посадкой own
            - name: grpc-internal
              containerPort: 9091
`
	if strings.Count(commentOnly, laneTraceName) != 2 {
		t.Fatal("фикстура обязана нести имя следа в тексте — иначе проба ничего не различает")
	}
	tr := laneTracesOf(t, commentOnly)
	if tr.ContainerPort != "" || tr.ServicePort != "" {
		t.Fatalf("имя следа прочитано из КОММЕНТАРИЯ: %+v", tr)
	}
	mustSayLane(t, judgeLaneTraces(lawfulLaneStack(), tr), "СЛЕД 1 из 2", "СЛЕД 2 из 2")
}

// TestLanePrereqInjection_TraceNumbersAndWiringAreJudged — следы не только
// существуют, но и несут объявленное число и соединены между собой.
func TestLanePrereqInjection_TraceNumbersAndWiringAreJudged(t *testing.T) {
	s := lawfulLaneStack()
	base := laneTracesOf(t, lawfulLaneManifest(true, true))

	t.Run("контейнер поднял другую дверь", func(t *testing.T) {
		tr := base
		tr.ContainerPort = "9101"
		mustSayLane(t, judgeLaneTraces(s, tr), "СЛЕД 1 из 2", "9101", "9100")
	})
	t.Run("Service выставил другую дверь", func(t *testing.T) {
		tr := base
		tr.ServicePort = "9101"
		mustSayLane(t, judgeLaneTraces(s, tr), "СЛЕД 2 из 2", "9101")
	})
	t.Run("следы не соединены", func(t *testing.T) {
		tr := base
		tr.ServiceTarget = "http-rest-int"
		mustSayLane(t, judgeLaneTraces(s, tr), "не соединены")
	})
	t.Run("полосу выставил чужой Service", func(t *testing.T) {
		tr := base
		tr.ServiceName = "kaname"
		mustSayLane(t, judgeLaneTraces(s, tr), "ведёт не туда", "kaname-internal")
	})
}

// first — findings из пары (findings, census); перепись проверяется отдельно там,
// где она предмет пробы.
func first(findings []string, _ lanePrereqCensus) []string { return findings }
