// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_prereq_test.go — ПРЕДПОСЫЛКА ПОЛОСЫ ФОРМЫ ОБЪЯВЛЕНА КАЖДЫМ СТЕНДОМ,
// КАКОВА БЫ НИ БЫЛА ЕГО ПОСАДКА (kacho#2735; приёмка Ф3 — Р3, Р11, Ф3-42).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ И ПОЧЕМУ ЕГО НЕ ДЕРЖАЛ НИКТО
//
// Две величины полосы входа паролем — адрес ретрансляции у края
// (`api-gateway.authn.iamLoginLaneUrl`) и порт слушателя у службы
// (`kaname.ports.loginLane`) — объявляются стендам ЗАРАНЕЕ, на посадке
// `external`, где ни одна из них не читается. Смысл заблаговременности ровно
// один: перевод стенда на `own` не заводит полосу с нуля, а под `own`
// отсутствие любой из двух есть отказ СТАРТА процесса с именем ручки.
//
// Отсюда же следует, почему такое объявление не держится ничем само собой:
// величина, которую сегодня никто не читает, снимается молча. Перепись
// читателей на `bec320cf47d`:
//
//	git grep -n 'iamLoginLaneUrl' -- '*.go'
//	    → единственный читатель по стендам — deploy/own_posture_stack_test.go,
//	      и он входит в проверку под `if f.IAMPosture != "own" && f.EdgePosture
//	      != "own" { continue }`
//	git grep -n '"ports", "loginLane"' -- '*.go'
//	    → deploy/login_lane_umbrella_test.go — под тем же `ip == "own"`
//
// Стендов на посадке `own` сегодня один из семи. То есть шесть седьмых
// объявлений не судит НИКТО: снятие любого из них проходит молча, и обнаружится
// оно отказом пода на первом же переводе стенда — то есть в кластере, а не в
// сборке. Это ровно класс «барьер, снятие которого не ловится ничем, барьером не
// является».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ СУДИТСЯ, ЧЕГО НЕ СУДЯТ СОСЕДИ
//
//	deploy/own_posture_stack_test.go       — согласие двух половин об одном
//	                                         числе, ТОЛЬКО на стендах `own`
//	deploy/login_lane_umbrella_test.go     — что подчарт выражает полосу и что
//	                                         боевой профиль объявил её С ПРИЧИНОЙ
//	gateway/deploy/login_lane_knob_test.go — что ручка объявлена базовым
//	                                         профилем края и эмитится один раз
//
// Здесь — ВСЕ стенды таблицы, без оговорки о посадке, и число стендов выведено
// обходом `deploy/stacks.txt`, а не выписано. Появится восьмой стенд без ручки —
// он попадёт в обход и станет находкой; «семь из семи» сошлось бы и молчало.
//
// ─────────────────────────────────────────────────────────────────────────────
// КОРЕНЬ ДЕРЕВА — ИЗ РАСПОЛОЖЕНИЯ ЭТОГО ФАЙЛА, А НЕ ИЗ РАБОЧЕГО КАТАЛОГА
//
// Соседи по каталогу берут корень как `filepath.Join("..", "..")` от рабочего
// каталога. Такая проверка судит дерево, из которого её ЗАПУСТИЛИ, а не то, из
// которого её собрали: запущенная из чужой копии, она молча оценивает чужие
// профили, а не найдя их — выходит нулём находок. Корень здесь берётся из
// `runtime.Caller`, то есть из координаты исходника на сборке, и предпосылка
// «это тот самый корень» проверяется тремя опорными путями.
package deploy_test

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
	"gopkg.in/yaml.v3"
)

// laneTraceName — имя порта, под которым слушатель полосы формы виден ОБА раза:
// в контейнере и на внутреннем Service. Следов у ручки порта ровно два, и
// каждый снимается отдельной правкой отдельного шаблона.
const laneTraceName = "http-login-lane"

// lanePrereqStack — что цепочка одного стенда объявила о полосе формы.
type lanePrereqStack struct {
	Stack string
	// LaneURL — `api-gateway.authn.iamLoginLaneUrl`.
	LaneURL string
	// LanePort — `kaname.ports.loginLane`: порт, на котором служба поднимает
	// слушатель, и он же targetPort контейнера.
	LanePort string
	// ServicePort — порт, который ретранслятор края НАБИРАЕТ: внутренний Service
	// вправе выставить свой номер (`service.internal.loginLanePort`), и тогда
	// сверять адрес с портом контейнера было бы сверкой не с тем производителем.
	ServicePort string
	// ServiceName — `kaname.name`; из него выводится имя внутреннего Service.
	ServiceName string
	// Namespace — пространство имён, в котором стенд ставится (`STACK_NAMESPACE`
	// из `deploy/Makefile`). Одно на все стенды, читается из дерева.
	Namespace string
	// values — поддерево `kaname:` этого стенда целиком: ровно то, что видит
	// подчарт как `.Values`. Не заполнено у синтетических стендов инъекции —
	// судящие функции его не читают, оно нужно только рендеру.
	values map[string]any
}

// lanePrereqCensus — объём осмотренного. Печатается ОТДЕЛЬНО от находок: «ноль
// находок» и «ноль прочитанных стендов» обязаны быть различимы в выводе.
type lanePrereqCensus struct {
	Stacks   int // знаменатель обхода
	WithURL  int
	WithPort int
}

func (c lanePrereqCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · объявили адрес полосы %d · объявили порт полосы %d",
		c.Stacks, c.WithURL, c.WithPort)
}

// judgeLanePrereq — НАХОДКИ по перечню стендов. Чистая: инъекция подаёт ей
// синтетический вход, не трогая ни дерева, ни рабочего каталога.
//
// Пустой вход — НАХОДКА, а не молчание: обход, не нашедший ни одного стенда,
// означает «сверять было не с чем», и зелёное на нём неотличимо от чистого
// дерева.
func judgeLanePrereq(stacks []lanePrereqStack) ([]string, lanePrereqCensus) {
	var findings []string
	var census lanePrereqCensus

	if len(stacks) == 0 {
		return []string{
			"обход не нашёл НИ ОДНОГО стенда: таблица `deploy/stacks.txt` пуста либо не прочиталась. " +
				"«Находок нет» здесь означало бы «проверять было нечего» — это третья категория, а не зелёное",
		}, census
	}

	for _, s := range stacks {
		census.Stacks++

		raw := strings.TrimSpace(s.LaneURL)
		port := strings.TrimSpace(s.LanePort)
		svcPort := strings.TrimSpace(s.ServicePort)
		if svcPort == "" {
			svcPort = port
		}

		switch {
		case raw == "":
			findings = append(findings, fmt.Sprintf(
				"стенд %s: `api-gateway.authn.iamLoginLaneUrl` не объявлен. Ручка объявляется ЗАРАНЕЕ, на "+
					"любой посадке: под `own` её отсутствие — отказ старта края с именем ручки, и перевод "+
					"стенда положил бы край на первом же подъёме (kacho#2735, приёмка Ф3 Р3/Р11/Ф3-42)", s.Stack))
		default:
			findings = append(findings, judgeLaneURL(s, raw, svcPort)...)
			census.WithURL++
		}

		if port == "" {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: `kaname.ports.loginLane` не объявлен — слушатель формы не поднимается, и под "+
					"`own` служба откажет в старте с именем ручки", s.Stack))
			continue
		}
		census.WithPort++
	}
	return findings, census
}

// judgeLaneURL — находки об адресе. Пустая строка сюда не приходит: её случай
// назван отдельно, потому что «не объявлен» и «объявлен негодным» — разные
// починки.
func judgeLaneURL(s lanePrereqStack, raw, svcPort string) []string {
	var findings []string

	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return append(findings, fmt.Sprintf(
			"стенд %s: `iamLoginLaneUrl` = %q не абсолютный адрес со схемой и хостом — относительный путь "+
				"ретранслятору некуда набрать, и страж старта края откажет", s.Stack, raw))
	}
	if u.Scheme != "https" {
		findings = append(findings, fmt.Sprintf(
			"стенд %s: `iamLoginLaneUrl` = %q не https — по ретрансляции едет печенье сессии человека, а "+
				"слушатель полосы взаимный по TLS", s.Stack, raw))
	}

	// Хост ВЫВОДИТСЯ из производящих величин, а не сверяется с литералом: имя
	// внутреннего Service — из `kaname.name`, пространство имён — из
	// `STACK_NAMESPACE` в `deploy/Makefile`. Литерал в проверке был бы вторым
	// объявлением об одном предмете и разъехался бы с первым молча.
	if svc := strings.TrimSpace(s.ServiceName); svc != "" && strings.TrimSpace(s.Namespace) != "" {
		want := svc + "-internal." + strings.TrimSpace(s.Namespace) + ".svc"
		if host := u.Hostname(); host != want {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: `iamLoginLaneUrl` ведёт на хост %q, а слушатель формы выставлен на ВНУТРЕННЕМ "+
					"Service службы — выведенный из дерева адрес %q (`kaname.name` = %q, `STACK_NAMESPACE` = %q). "+
					"Публичного пути к полосе нет by construction", s.Stack, host, want, svc, s.Namespace))
		}
	}

	// Сверка с портом, который ретранслятор НАБИРАЕТ. Два производителя: номер в
	// адресе приходит из профиля края, номер двери — из профиля службы; подстановка
	// третьего значения делает утверждение ложным, то есть это сравнение, а не
	// тождество, записанное синтаксисом утверждения.
	if svcPort != "" {
		if got := u.Port(); got != svcPort {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: край ретранслирует на порт %q, а внутренний Service службы выставляет полосу на "+
					"%q — половины называют РАЗНЫЕ двери. Обе исправны по отдельности: край постучится туда, "+
					"где двери нет, и ответит 503 на каждом запросе, неотличимо от «служба лежит»",
				s.Stack, got, svcPort))
		}
	}
	return findings
}

// TestLoginLanePrereq_EveryStackDeclaresTheLaneWhateverItsPosture — сверка по
// дереву. Ни одного помола о посадке: предмет проверки — ЗАБЛАГОВРЕМЕННОСТЬ
// объявления, и оговорка «только на own» вернула бы ровно ту слепоту, ради
// снятия которой проверка заведена.
func TestLoginLanePrereq_EveryStackDeclaresTheLaneWhateverItsPosture(t *testing.T) {
	root := lanePrereqRoot(t)
	stacks := readLanePrereqStacks(t, root)

	findings, census := judgeLanePrereq(stacks)
	t.Logf("перепись: %s", census)
	for _, s := range stacks {
		t.Logf("  %s: адрес=%q порт=%q порт Service=%q служба=%q пространство=%q",
			s.Stack, s.LaneURL, s.LanePort, s.ServicePort, s.ServiceName, s.Namespace)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// TestLoginLanePrereq_DeclaredPortReachesBothOfItsTraces — объявленный порт
// доезжает до ОБОИХ своих следов.
//
// # Почему рендер, а не чтение шаблона
//
// След ручки порта не один: слушатель виден в контейнере (`containerPort`) и на
// внутреннем Service (`port`/`targetPort`), и это ДВЕ правки в ДВУХ шаблонах.
// Снятие любой одной оставляет вторую на месте: убрали порт Service — под
// слушает, а дозвониться некуда; убрали порт контейнера — Service ведёт в
// никуда. Проверка, читающая текст шаблона, судит объявление; здесь судится
// ИСХОД — то, что рендер действительно кладёт в манифест.
//
// # Предпосылка и третья категория
//
// Рендеру нужен `helm` и каталог подчарта. Зонт целиком отрендерить нечем:
// зависимости зонта (`vpc`, `compute`, `api-gateway`, …) лежат в `charts/`
// неполностью, и `helm template` на нём отказывает до всякого анализа —
// поэтому рендерится ПОДЧАРТ службы, чей каталог в дереве распакован.
//
// ЧЕМ ЭТОТ РЕНДЕР ОТЛИЧАЕТСЯ ОТ ЗОНТИЧНОГО, названо точно, а не «в точности тем
// же»: подчарту подаётся поддерево `kaname:` профилей БЕЗ `global:` зонта (его
// объявляют шесть профилей: `values.dev.yaml`, `values.prod.yaml`,
// `values.a8f60d.yaml`, `values.prorobotech.yaml`, `values.fe3455-prod.yaml`,
// `values.fe3455-ory-posture.yaml`) и с ИНЫМ именем релиза. Оба различия
// наблюдаемы. Замер на цепочке `prod`, единица счёта — СТРОКА МАНИФЕСТА,
// изменившаяся между рендерами (`diff rA rB | grep -cE '^<'`):
//
//	без `global:` против с ним      5 строк — все схема хуков внешнего
//	                                поставщика, `http` против `https`
//	смена имени релиза              12 строк — все `app.kubernetes.io/instance`
//
// `^<` совпадает здесь с этой единицей ТОЛЬКО потому, что все куски — замены, и
// строк с `<` ровно столько же, сколько с `>`. На куске-ДОБАВЛЕНИИ он дал бы
// ноль при выросшем манифесте (проверено на двух строках: `^<` = 0, `^>` = 2,
// файл вырос на 2), поэтому к другому входу его прикладывать нельзя. От вида
// куска не зависит `diff --unchanged-line-format= --old-line-format='%l\n'
// --new-line-format=` — на этом входе он даёт те же 5 и 12. Счёт по `wc -l`
// всего отчёта `diff` дал бы 20 и 48: он меряет размер ОТЧЁТА (на каждый кусок
// две строки бухгалтерии), а не расхождение манифестов.
//
// НА СУДИМЫХ ЗДЕСЬ ВЕЛИЧИНАХ РАСХОЖДЕНИЯ НЕТ, И ЭТО ЗАМЕРЕНО, а не выведено:
// `containerPort` контейнера, `port` и `targetPort` внутреннего Service и его
// `metadata.name` побайтно одинаковы во всех трёх рендерах
// (`{containerPort: 9100, service.port: 9100, service.targetPort:
// http-login-lane, service.metadata.name: kaname-internal}` — трижды). Имя
// Service идёт через `include "kaname.fullname"` = `.Values.name`, то есть от
// `.Release.Name` не зависит вовсе, а оба номера — от `.Values.ports.loginLane`
// и `.Values.service.internal.loginLanePort`, которых `global:` не касается.
// Расширится предмет проверки за пределы этих ЧЕТЫРЁХ величин — замер обязан
// быть повторён, потому что тогда он станет о другом.
//
// Нет `helm` — это ТРЕТЬЯ КАТЕГОРИЯ: не зелёное и не красное. В CI, где
// предпосылка создаётся шагом задания, её отсутствие — отказ: проверка не
// вправе молча становиться инертной на прогоне, который решает о слиянии.
func TestLoginLanePrereq_DeclaredPortReachesBothOfItsTraces(t *testing.T) {
	// Прогон, результат которого `go test` положит в кеш, здесь недействителен:
	// манифест строит ПОДПРОЦЕСС, и читает он файлы чарта, которых журнал
	// обращений пробы не видит. Правка шаблона кеш не сбросит, и над деревом,
	// где след снят, напечаталось бы `ok (cached)`.
	if treecorpus.RunResultWillBeCached() {
		t.Fatalf("рендер подчарта строит ПОДПРОЦЕСС: %s", treecorpus.CachedVerdictRefusal())
	}

	root := lanePrereqRoot(t)
	stacks := readLanePrereqStacks(t, root)
	chart := filepath.Join(root, "deploy", "helm", "umbrella", "charts", "kaname")

	if _, err := os.Stat(filepath.Join(chart, "Chart.yaml")); err != nil {
		t.Fatalf("каталог подчарта службы %s не найден (%v) — предпосылка проверки исчезла, "+
			"а не дерево стало чистым", chart, err)
	}
	if _, err := exec.LookPath("helm"); err != nil {
		msg := fmt.Sprintf("ПРЕДПОСЫЛКА НЕ СОЗДАНА: `helm` не найден на PATH (%v). Это ТРЕТЬЯ "+
			"КАТЕГОРИЯ — не зелёное и не красное; в зелёное не зачитывается. Стендов в обходе %d, "+
			"слотов следов %d, осмотрено 0, найдено 0", err, len(stacks), 2*countDeclaredLanePort(stacks))
		if os.Getenv("CI") != "" {
			t.Fatal(msg + " — на прогоне, решающем о слиянии, проверка обязана исполниться, а не пропуститься")
		}
		t.Skip(msg)
	}

	// Осмотренные СЛОТЫ и НАЙДЕННЫЕ следы считаются раздельно. Отношение «следов
	// проверено 2K из 2K» тождественно единице by construction и потому не
	// сообщает ничего: со снятыми в шаблонах обоими следами оно печатало бы
	// полное покрытие над манифестами, в которых следа нет ни одного. Найденное
	// же отличается от осмотренного ровно на то, ради чего проверка заведена.
	declared, containerFound, serviceFound := 0, 0, 0
	var findings []string
	for _, s := range stacks {
		if strings.TrimSpace(s.LanePort) == "" {
			// Стенд без объявленного порта уже назван находкой соседней проверкой
			// этого же файла; здесь ему нечему доезжать, и молчание тут — не
			// послабление, а отсутствие предмета. В переписи он виден знаменателем.
			continue
		}
		declared++
		rendered := renderKanameSubchart(t, root, chart, s)
		tr, err := readLaneTraces(rendered)
		if err != nil {
			t.Fatalf("стенд %s: манифест подчарта не разбирается (%v) — вердикта нет, а не «находок нет»", s.Stack, err)
		}
		if tr.ContainerPort != "" {
			containerFound++
		}
		if tr.ServicePort != "" {
			serviceFound++
		}
		findings = append(findings, judgeLaneTraces(s, tr)...)
	}

	t.Logf("перепись: стендов осмотрено %d · с объявленным портом %d · слотов следов осмотрено %d · "+
		"следов НАЙДЕНО %d (контейнер %d · Service %d) · находок по следам %d",
		len(stacks), declared, 2*declared, containerFound+serviceFound, containerFound, serviceFound, len(findings))
	if declared == 0 {
		t.Fatal("ни один стенд таблицы не объявил `kaname.ports.loginLane` — рендерить нечего, и «следов не " +
			"найдено» означало бы «проверять было нечего»")
	}
	for _, f := range findings {
		t.Error(f)
	}
}

func countDeclaredLanePort(stacks []lanePrereqStack) int {
	n := 0
	for _, s := range stacks {
		if strings.TrimSpace(s.LanePort) != "" {
			n++
		}
	}
	return n
}

// laneTraces — оба следа ручки порта, снятые с ОТРЕНДЕРЕННОГО манифеста.
// Пустое поле значит «следа в манифесте нет».
type laneTraces struct {
	ContainerPort string // containerPort порта контейнера `http-login-lane`
	ServiceName   string // metadata.name Service, выставившего полосу
	ServicePort   string // spec.ports[].port
	ServiceTarget string // spec.ports[].targetPort
}

// judgeLaneTraces — находки по двум следам. Чистая: инъекция подаёт ей
// синтетические следы.
func judgeLaneTraces(s lanePrereqStack, tr laneTraces) []string {
	var findings []string
	port := strings.TrimSpace(s.LanePort)

	if tr.ContainerPort == "" {
		findings = append(findings, fmt.Sprintf(
			"стенд %s: СЛЕД 1 из 2 — порта контейнера `%s` в отрендеренном манифесте НЕТ, хотя "+
				"`kaname.ports.loginLane` = %s объявлен. Слушатель у процесса есть, а у пода двери нет",
			s.Stack, laneTraceName, port))
	} else if tr.ContainerPort != port {
		findings = append(findings, fmt.Sprintf(
			"стенд %s: СЛЕД 1 из 2 — порт контейнера `%s` отрендерен как %s, а профиль объявил %s",
			s.Stack, laneTraceName, tr.ContainerPort, port))
	}

	switch {
	case tr.ServicePort == "":
		findings = append(findings, fmt.Sprintf(
			"стенд %s: СЛЕД 2 из 2 — порта `%s` на внутреннем Service в отрендеренном манифесте НЕТ. "+
				"Под слушает, а край до него не дозвонится: единственный путь к полосе — внутренний Service",
			s.Stack, laneTraceName))
	default:
		want := strings.TrimSpace(s.ServicePort)
		if want == "" {
			want = port
		}
		if tr.ServicePort != want {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: СЛЕД 2 из 2 — внутренний Service выставил полосу на %s, а профиль объявил %s",
				s.Stack, tr.ServicePort, want))
		}
		if tr.ServiceTarget != laneTraceName {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: СЛЕД 2 из 2 — порт Service ведёт на targetPort %q, а не на `%s` контейнера: "+
					"два следа объявлены, и они не соединены", s.Stack, tr.ServiceTarget, laneTraceName))
		}
		if svc := strings.TrimSpace(s.ServiceName); svc != "" && tr.ServiceName != svc+"-internal" {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: полосу выставил Service %q, а `kaname.name` = %q даёт %q — адрес, который "+
					"объявил край, ведёт не туда", s.Stack, tr.ServiceName, svc, svc+"-internal"))
		}
	}
	return findings
}

// renderedDoc — та часть манифеста, которую читает эта проверка.
type renderedDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		// Service
		Ports []struct {
			Name       string `yaml:"name"`
			Port       any    `yaml:"port"`
			TargetPort any    `yaml:"targetPort"`
		} `yaml:"ports"`
		// Deployment
		Template struct {
			Spec struct {
				Containers []struct {
					Name  string `yaml:"name"`
					Ports []struct {
						Name          string `yaml:"name"`
						ContainerPort any    `yaml:"containerPort"`
					} `yaml:"ports"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

// readLaneTraces — оба следа из потока манифестов. Разбор, а не поиск подстроки:
// имя `http-login-lane` встречается в манифесте и в комментариях шаблона, а
// комментарий портом не является.
func readLaneTraces(rendered string) (laneTraces, error) {
	var tr laneTraces
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		var doc renderedDoc
		err := dec.Decode(&doc)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return tr, err
		}
		switch doc.Kind {
		case "Deployment":
			for _, c := range doc.Spec.Template.Spec.Containers {
				for _, p := range c.Ports {
					if p.Name == laneTraceName {
						tr.ContainerPort = scalarText(p.ContainerPort)
					}
				}
			}
		case "Service":
			for _, p := range doc.Spec.Ports {
				if p.Name == laneTraceName {
					tr.ServiceName = doc.Metadata.Name
					tr.ServicePort = scalarText(p.Port)
					tr.ServiceTarget = scalarText(p.TargetPort)
				}
			}
		}
	}
	return tr, nil
}

func scalarText(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// renderKanameSubchart — манифест подчарта службы на значениях ЭТОГО стенда.
//
// Рабочий каталог подпроцесса задан явно: инструмент, которому корень пришёл бы
// из рабочего каталога прогона, рендерил бы чужое дерево.
func renderKanameSubchart(t *testing.T, root, chart string, s lanePrereqStack) string {
	t.Helper()
	raw, err := yaml.Marshal(s.values)
	if err != nil {
		t.Fatalf("стенд %s: поддерево `kaname` не сериализуется: %v", s.Stack, err)
	}
	path := filepath.Join(t.TempDir(), "kaname-"+s.Stack+".yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("стенд %s: запись значений: %v", s.Stack, err)
	}
	cmd := exec.Command("helm", "template", "kaname", chart, "-f", path, "--namespace", s.Namespace)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("стенд %s: рендер подчарта отказал (%v) — вердикта нет ни у одного следа:\n%s",
			s.Stack, err, out)
	}
	return string(out)
}

// ─────────────────────────────────────────────────────────────────────────────
// ЧТЕНИЕ ДЕРЕВА

// lanePrereqRoot — корень дерева ИЗ РАСПОЛОЖЕНИЯ ЭТОГО ФАЙЛА.
func lanePrereqRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller не ответил — корень дерева не найти, и брать его из рабочего каталога значит " +
			"судить дерево, из которого проверку ЗАПУСТИЛИ, а не то, из которого собрали")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(self), "..", ".."))
	// Предпосылка «это тот самый корень»: обе половины полосы и таблица стендов
	// обязаны лежать на своих местах. Не нашлись — проверка отказывает, а не
	// объявляет дерево чистым.
	for _, must := range []string{
		filepath.Join("deploy", "stacks.txt"),
		filepath.Join("deploy", "Makefile"),
		filepath.Join("deploy", "helm", "umbrella"),
		filepath.Join("gateway", "deploy", "values.yaml"),
	} {
		if _, err := os.Stat(filepath.Join(root, must)); err != nil {
			t.Fatalf("корень %s не несёт опорного пути %s (%v) — предпосылка проверки исчезла", root, must, err)
		}
	}
	return root
}

var lanePrereqStackLine = regexp.MustCompile(`^([A-Za-z0-9_.-]+):(.+)$`)

// readLanePrereqStacks — факты каждого стенда: цепочка накладывается слева
// направо поверх умолчаний подчартов, ровно как её получает helm.
func readLanePrereqStacks(t *testing.T, root string) []lanePrereqStack {
	t.Helper()
	umbrella := filepath.Join(root, "deploy", "helm", "umbrella")
	table := filepath.Join(root, "deploy", "stacks.txt")
	ns := readStackNamespace(t, root)

	rawTable, err := os.ReadFile(table)
	if err != nil {
		t.Fatalf("таблица стендов %s не читается (%v) — предпосылка проверки исчезла, а не дерево стало чистым",
			table, err)
	}
	chains := map[string][]string{}
	for _, line := range strings.Split(string(rawTable), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := lanePrereqStackLine.FindStringSubmatch(line)
		if m == nil {
			// Нераспознанная строка — это НЕ «стендов меньше», это «предикат
			// перестал их узнавать». Молчание здесь сузило бы обход.
			t.Fatalf("строка таблицы стендов не разобрана: %q (%s)", line, table)
		}
		chains[m[1]] = strings.Split(m[2], ",")
	}
	if len(chains) == 0 {
		t.Fatalf("в %s нет ни одной строки стенда — проверка не вправе считать, что стендов не осталось", table)
	}

	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]lanePrereqStack, 0, len(names))
	for _, name := range names {
		// Умолчания подчартов читаются ЗАНОВО на каждый стенд: наложение правит
		// карту на месте, и одна общая карта протекала бы из стенда в стенд,
		// приписывая одному профилю объявления другого.
		declared := map[string]any{
			"kaname":      readLaneYAML(t, filepath.Join(umbrella, "charts", "kaname", "values.yaml")),
			"api-gateway": readLaneYAML(t, filepath.Join(root, "gateway", "deploy", "values.yaml")),
		}
		for _, p := range chains[name] {
			path := filepath.Join(umbrella, p)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("стенд %q называет профиль %s, которого нет: %v", name, p, err)
			}
			declared = mergeLaneValues(declared, readLaneYAML(t, path))
		}
		kaname, _ := declared["kaname"].(map[string]any)
		out = append(out, lanePrereqStack{
			Stack:       name,
			LaneURL:     laneString(lookupLane(declared, "api-gateway", "authn", "iamLoginLaneUrl")),
			LanePort:    laneString(lookupLane(declared, "kaname", "ports", "loginLane")),
			ServicePort: laneString(lookupLane(declared, "kaname", "service", "internal", "loginLanePort")),
			ServiceName: laneString(lookupLane(declared, "kaname", "name")),
			Namespace:   ns,
			values:      kaname,
		})
	}
	return out
}

// readStackNamespace — пространство имён стенда из `deploy/Makefile`. Величина
// ВЫВОДИТСЯ из дерева, а не выписывается: выписанная, она стала бы вторым
// объявлением об одном предмете.
func readStackNamespace(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "deploy", "Makefile")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v — пространство имён стенда взять неоткуда", path, err)
	}
	m := regexp.MustCompile(`(?m)^STACK_NAMESPACE\s*\?=\s*(\S+)\s*$`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("%s больше не объявляет `STACK_NAMESPACE ?= …` — предикат перестал узнавать свой предмет, "+
			"и адрес полосы стало не с чем сверять", path)
	}
	return m[1]
}

func readLaneYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	out := map[string]any{}
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s не разбирается: %v", path, err)
	}
	return out
}

// mergeLaneValues накладывает src на dst так же, как helm накладывает файлы
// значений: карты сливаются по ключам, всё остальное замещается целиком.
func mergeLaneValues(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if cur, ok := dst[k].(map[string]any); ok {
				dst[k] = mergeLaneValues(cur, sub)
				continue
			}
			dst[k] = mergeLaneValues(map[string]any{}, sub)
			continue
		}
		dst[k] = v
	}
	return dst
}

func lookupLane(tree map[string]any, path ...string) (any, bool) {
	var cur any = tree
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func laneString(v any, ok bool) string {
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
