// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscription_stream_serving_test.go — путь потока изменений обязан ПРИЙТИ НА
// КРАЙ, а не в заглушку одностраничного приложения, и прийти ПОТОКОМ, а не одним
// куском в конце.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Консоль ходит на тот же источник, что и страница, — значит через раздачу
// консоли. Адрес, не покрытый ни одним её блоком, достаётся общему `location /`,
// а тот отдаёт `index.html` с кодом `200` и типом `text/html`.
//
// Для потока это не «страница не открылась», а КЛАСС ХУЖЕ: приёмник событий
// браузера обязан на неверном типе ответа закрыть соединение НАВСЕГДА и выдать
// ошибку без подробностей. Со стороны — тишина: в журнале края ни строки
// (запрос не доехал), в журнале раздачи `200`, метрики нет. Всё зелено.
//
// Вторая половина предмета — БУФЕРИЗАЦИЯ. Посредник по умолчанию копит ответ и
// отдаёт его целиком по завершении. Поток, накопленный целиком, потоком быть
// перестаёт, ничем себя не выдав: коды те же, тип тот же, события те же — они
// просто приходят все сразу в конце.
//
// Третья — СРОК МОЛЧАНИЯ. Край объявляет срок жизни потока; посредник объявляет
// свой предел ожидания следующей порции. Разойдись они в одну сторону — поток
// рвёт посредник, и клиент читает это как сетевой сбой, а не как чистое закрытие
// по сроку, после которого он возобновляется со своей позиции.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТА ПРОБА ЧИТАЕТ
//
// Только ОБЪЯВЛЕНИЯ — шаблон раздачи и шаблон внешнего входа из чарта консоли,
// плюс объявление края ради срока жизни потока. Ни кластера, ни helm: рендер
// требует инструмента развёртывания, которого в наборе проб нет, а проба,
// зависящая от недоступного средства, пропускается молча — и её молчание
// неотличимо от зелёного.
//
// Разбор раздачи здесь НЕ свой: он взят из `identity_serving_precedence_test.go`
// того же набора (`parseServingTemplate`, `selectLocation`). Второй разбор одного
// предмета разошёлся бы с первым молча — оба ведь отвечают «разобрал» на
// разбираемом входе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОБА НЕ УТВЕРЖДАЕТ
//
// Она не утверждает, ЧЕМ край отвечает на этот путь: это предмет проб самой
// ручки (`gateway/internal/subscriptionstream`). Здесь предмет — ДОЕЗЖАЕТ ли до
// него запрос и в какой форме приходит ответ.
//
// Она не утверждает согласия здешней величины пути с той, что объявляет край:
// сравнить их отсюда нечем — ручка живёт под `gateway/internal`, и этот набор её
// импорта не видит by construction. Сверку держит проба со стороны края
// (`gateway/deploy/subscription_stream_serving_declared_test.go`), и здесь она не
// пересказывается: два места об одном предмете расходятся молча.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// subscriptionStreamPath — путь ручки потока, как его видит браузер.
//
// Значение здесь ПОВТОРЕНО, а не импортировано, и это названо вслух: ручка
// объявлена в `gateway/internal/subscriptionstream`, а внутренние пакеты одного
// поддерева невидимы другому. Согласие этой строки с объявлением края держит
// проба со стороны края — она читает ЭТИ ЖЕ шаблоны и требует, чтобы её
// собственная константа в них нашлась. Разойдись значения — покраснеет она.
const subscriptionStreamPath = "/subscription/v1/events"

// edgeUpstreamRe — восходящий узел края, названный блоком раздачи.
//
// Признак — ИМЯ переменной окружения, а не адрес: адрес задаёт профиль, и
// выписывать его здесь значило бы завести вторую копию профиля.
var edgeUpstreamRe = regexp.MustCompile(`\$\{(KACHO_UI_API_GATEWAY_UPSTREAM)\}`)

// bufferingOffRe — явное отключение накопления ответа.
var bufferingOffRe = regexp.MustCompile(`(?m)^\s*proxy_buffering\s+off\s*;`)

// gzipOffRe — явное снятие сжатия на этом блоке.
var gzipOffRe = regexp.MustCompile(`(?m)^\s*gzip\s+off\s*;`)

// streamTimeoutRefRe — предел ожидания, объявленный блоком ССЫЛКОЙ на величину
// чарта, а не своим литералом.
//
// Требуется именно ссылка: литерал здесь был бы вторым объявлением одного числа
// (второе стоит у внешнего входа), и разошлись бы они молча — оба непусты, оба
// выглядят действующими, и ни одно не знает о другом.
var streamTimeoutRefRe = regexp.MustCompile(
	`proxy_read_timeout\s+\{\{-?[^{}]*\.Values\.subscriptionStream\.proxyReadTimeout[^{}]*-?\}\}s\s*;`)

// streamIngressRel — объявление внешнего входа для потока.
var streamIngressRel = filepath.Join("ui-future", "deploy", "templates", "ingress-subscription-stream.yaml")

// edgeValuesRel — объявление края: оттуда берётся срок жизни потока.
var edgeValuesRel = filepath.Join("gateway", "deploy", "values.yaml")

// uiValuesRel — объявление чарта консоли: оттуда берётся предел ожидания
// посредника, ОДИН на обоих посредников.
var uiValuesRel = filepath.Join("ui-future", "deploy", "values.yaml")

// consoleServer — серверный блок раздачи, который обслуживает саму консоль.
//
// Он опознаётся по НАЛИЧИЮ восходящего узла края, а не по номеру: серверных
// блоков в этом шаблоне девять (оболочка плюс восемь модулей), и порядковый
// номер пережил бы добавление девятого модуля, ничего не сказав.
func consoleServer(t *testing.T) (nginxServer, string) {
	t.Helper()
	servers, path := servingTemplate(t)
	if len(servers) == 0 {
		t.Fatalf("%s: не разобрано ни одного серверного блока — прочитано ноль, "+
			"и молчание пробы не является утверждением о маршрутизации", path)
	}
	found := -1
	for i, srv := range servers {
		for _, l := range srv.locs {
			if !edgeUpstreamRe.MatchString(l.body) {
				continue
			}
			if found >= 0 && found != i {
				t.Fatalf("%s: восходящий узел края назван в ДВУХ серверных блоках "+
					"(строки %d и %d) — какой из них обслуживает консоль, разбор больше "+
					"не устанавливает", path, servers[found].line, srv.line)
			}
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("%s: ни один блок раздачи не называет `${KACHO_UI_API_GATEWAY_UPSTREAM}` — "+
			"предпосылка пробы исчезла, и её молчание ничего не значило бы", path)
	}
	return servers[found], path
}

// TestSubscriptionStreamPathReachesTheEdgeNotTheAppShell — предмет МАРШРУТИЗАЦИИ.
//
// Утверждается не наличие блока, а то, КАКОЙ блок раздача выберет для этого
// адреса: наличие блока ничего не значит, пока другой блок выигрывает у него по
// правилам разрешения.
//
// Отрицание («не заглушка») стоит рядом с положительным контролем («выбранный
// блок ведёт на край»): без него утверждение зеленело бы на шаблоне, где нет
// вообще ни одного блока.
func TestSubscriptionStreamPathReachesTheEdgeNotTheAppShell(t *testing.T) {
	srv, path := consoleServer(t)

	edgeLocs := 0
	for _, l := range srv.locs {
		if edgeUpstreamRe.MatchString(l.body) {
			edgeLocs++
		}
	}

	idx, why := selectLocation(subscriptionStreamPath, srv.locs)
	chosen := "не выбран ни один блок"
	if idx >= 0 {
		chosen = srv.locs[idx].name()
	}

	t.Logf("осмотрено: %s · серверный блок консоли строка %d · блоков %d, из них ведут на край %d; "+
		"адрес %q выбирает %s (%s)",
		path, srv.line, len(srv.locs), edgeLocs, subscriptionStreamPath, chosen, why)

	if edgeLocs == 0 {
		t.Fatal("ни один блок не ведёт на край — сравнивать не с чем")
	}
	if idx < 0 {
		t.Fatalf("адрес %q не выбирает ни одного блока — разбор потерял предмет",
			subscriptionStreamPath)
	}
	if !edgeUpstreamRe.MatchString(srv.locs[idx].body) {
		t.Errorf("адрес потока %q достаётся блоку %s (%s), который НЕ ведёт на край. "+
			"Непокрытый адрес уходит в заглушку одностраничного приложения: она отдаёт "+
			"`index.html` с кодом `200` и типом `text/html`, приёмник событий браузера "+
			"закрывает соединение навсегда и выдаёт ошибку без подробностей, а в журнале "+
			"края нет ни строки — запрос до него не доехал. Объявлено в %s.",
			subscriptionStreamPath, chosen, why, path)
	}
}

// TestSubscriptionStreamLocationStreamsInsteadOfAccumulating — предмет ФОРМЫ
// ОТВЕТА: выбранный блок обязан отдавать поток, а не копить его.
//
// Три величины, и каждая при умолчании ломает ровно то, ради чего заведена
// ручка: накопление отдаёт поток одним куском в конце; сжатие делает то же по
// другой причине; предел ожидания короче срока жизни потока рвёт поток руками
// посредника.
//
// ЗАКОННЫЙ БЛИЗНЕЦ. Соседние блоки края (полоса доменов, полоса личности) этих
// величин не объявляют и находкой НЕ являются: они обслуживают запрос-ответ, а
// не поток. Проба это утверждает переписью — сколько блоков осмотрено и скольким
// требование адресовано, — а не молчанием.
func TestSubscriptionStreamLocationStreamsInsteadOfAccumulating(t *testing.T) {
	srv, path := consoleServer(t)

	idx, why := selectLocation(subscriptionStreamPath, srv.locs)
	if idx < 0 || !edgeUpstreamRe.MatchString(srv.locs[idx].body) {
		// НЕ пропуск: пропущенная проба молчит так же, как прошедшая, и её
		// молчание читалось бы согласием. Маршрута нет — значит величинам не на
		// чем стоять, и это отказ.
		t.Fatalf("адрес потока %q не приходит на край (%s) — величинам, от которых "+
			"зависит форма ответа, некуда встать. Маршрут держит "+
			"TestSubscriptionStreamPathReachesTheEdgeNotTheAppShell",
			subscriptionStreamPath, why)
	}
	stream := srv.locs[idx]

	// Законный близнец: блоки края, которые потоком не являются. Требование к
	// ним НЕ предъявляется, и это утверждается счётом, а не умолчанием.
	twins := make([]string, 0, len(srv.locs))
	twinsDeclaring := 0
	for i, l := range srv.locs {
		if i == idx || !edgeUpstreamRe.MatchString(l.body) {
			continue
		}
		twins = append(twins, l.name())
		if bufferingOffRe.MatchString(l.body) {
			twinsDeclaring++
		}
	}

	budget := edgeStreamBudget(t)
	timeout := uiStreamProxyReadTimeout(t)
	refsTimeout := streamTimeoutRefRe.MatchString(stream.body)

	t.Logf("осмотрено: %s · блоков %d · требование адресовано 1 (%s) · законных близнецов %d %v "+
		"(из них объявляют снятие накопления %d — требования к ним нет); "+
		"срок жизни потока у края %v · предел ожидания посредника %v · блок ссылается на него: %v",
		path, len(srv.locs), stream.name(), len(twins), twins, twinsDeclaring,
		budget, timeout, refsTimeout)

	if len(twins) == 0 {
		t.Fatal("законных близнецов не осталось — требование стало тождественно-истинным: " +
			"проверять «только этому блоку» не на чем")
	}
	if !bufferingOffRe.MatchString(stream.body) {
		t.Errorf("блок потока %s не объявляет `proxy_buffering off;`. Умолчание раздачи — "+
			"КОПИТЬ ответ: события накопятся и приедут одним куском по закрытию потока. "+
			"Подписка при этом выглядит работающей — код тот же, тип тот же, события те же, "+
			"приходят все сразу в конце. Объявлено в %s.", stream.name(), path)
	}
	if !gzipOffRe.MatchString(stream.body) {
		t.Errorf("блок потока %s не объявляет `gzip off;`. Сегодня сжатие не срабатывает "+
			"лишь потому, что тип потока не назван в перечне `gzip_types` серверного блока — "+
			"то есть поток держится чужим перечнем, о котором ничего не знает. Первая же "+
			"строка в тот перечень накопит ответ обратно. Объявлено в %s.", stream.name(), path)
	}
	if !refsTimeout {
		t.Errorf("блок потока %s не объявляет `proxy_read_timeout` ссылкой на "+
			"`.Values.subscriptionStream.proxyReadTimeout`. Либо величина оставлена умолчанию "+
			"посредника (её никто не выбирал), либо записана здесь своим литералом — и тогда "+
			"это ВТОРОЕ объявление одного числа: первое стоит у внешнего входа, и разойдутся "+
			"они молча. Объявлено в %s.", stream.name(), path)
	}
	if budget <= 0 {
		t.Fatal("срок жизни потока у края не прочитан — вторая сторона сверки отсутствует")
	}
	if timeout <= budget {
		t.Errorf("предел ожидания посредника %v не переживает срок жизни потока %v у края: "+
			"поток будет рвать ПОСРЕДНИК, и клиент прочтёт это сетевым сбоем вместо чистого "+
			"закрытия по сроку, после которого он возобновляется со своей позиции. "+
			"Объявлено в %s и %s.", timeout, budget, uiValuesRel, edgeValuesRel)
	}
}

// TestExternalEntryDeclaresTheSubscriptionStreamPath — предмет ВНЕШНЕГО ВХОДА.
//
// Общий вход консоли объявлен префиксом `/`, поэтому адрес до раздачи ДОЕЗЖАЕТ и
// без отдельного объявления. Отдельный вход заведён не ради маршрута, а ради
// ВЕЛИЧИН: пометки входа действуют на весь его маршрут, и объявить накопление
// снятым можно либо всей консоли разом, либо своим входом на один адрес. Второе
// выбрано намеренно — снимать накопление со всей консоли значило бы менять форму
// раздачи статики ради одной ручки.
func TestExternalEntryDeclaresTheSubscriptionStreamPath(t *testing.T) {
	root := repoRootFromTest(t)
	path := filepath.Join(root, streamIngressRel)
	raw, err := os.ReadFile(path) // #nosec G304 -- путь собран из корня этого дерева
	if err != nil {
		t.Fatalf("объявления внешнего входа для потока нет (%s): адрес доедет до раздачи "+
			"общим входом, но величины, от которых зависит форма ответа, останутся "+
			"умолчанию посредника — а умолчание не является выбором. Ошибка чтения: %v",
			streamIngressRel, err)
	}
	text := string(raw)

	required := map[string]string{
		`nginx.ingress.kubernetes.io/proxy-buffering: "off"`: "накопление ответа снято явно",
		`.Values.subscriptionStream.proxyReadTimeout`:        "предел ожидания взят из той же величины, что у раздачи",
		subscriptionStreamPath:                               "путь потока объявлен",
		`pathType: Exact`:                                    "маршрут точный — под него не подпадает ничего другого",
	}
	missing := make([]string, 0, len(required))
	for token, why := range required {
		if !strings.Contains(text, token) {
			missing = append(missing, fmt.Sprintf("%s (%s)", token, why))
		}
	}
	sort.Strings(missing)

	budget := edgeStreamBudget(t)
	timeout := uiStreamProxyReadTimeout(t)

	t.Logf("осмотрено: %s · строк %d · требований %d · не выполнено %d; "+
		"предел ожидания %v · срок жизни потока у края %v",
		streamIngressRel, strings.Count(text, "\n")+1, len(required), len(missing), timeout, budget)

	if len(missing) > 0 {
		t.Errorf("объявление внешнего входа %s не несёт: %s", streamIngressRel,
			strings.Join(missing, "; "))
	}
	if timeout <= budget {
		t.Errorf("предел ожидания внешнего входа %v не переживает срок жизни потока %v у края: "+
			"поток рвал бы внешний вход прежде, чем край закроет его по сроку. "+
			"Объявлено в %s и %s.", timeout, budget, uiValuesRel, edgeValuesRel)
	}
}

// edgeStreamBudget — срок жизни потока, объявленный КРАЕМ.
//
// Читается у края, а не выписывается здесь: копия его величины была бы вторым
// местом об одном предмете, и разошлись бы они молча — обе непусты, обе выглядят
// действующими, и ни одна не знает о другой.
func edgeStreamBudget(t *testing.T) time.Duration {
	t.Helper()
	path := filepath.Join(repoRootFromTest(t), edgeValuesRel)
	raw, err := os.ReadFile(path) // #nosec G304 -- путь собран из корня этого дерева
	if err != nil {
		t.Fatalf("объявление края %s не читается (%v) — вторая сторона сверки исчезла", path, err)
	}
	var values struct {
		SubscriptionStream struct {
			StreamBudget string `yaml:"streamBudget"`
		} `yaml:"subscriptionStream"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("разбор объявления края %s: %v", path, err)
	}
	if values.SubscriptionStream.StreamBudget == "" {
		t.Fatalf("%s не объявляет `subscriptionStream.streamBudget` — сверять не с чем", edgeValuesRel)
	}
	budget, err := time.ParseDuration(values.SubscriptionStream.StreamBudget)
	if err != nil {
		t.Fatalf("%s: `subscriptionStream.streamBudget` = %q не срок: %v",
			edgeValuesRel, values.SubscriptionStream.StreamBudget, err)
	}
	return budget
}

// uiStreamProxyReadTimeout — предел ожидания посредника, объявленный чартом
// консоли ОДИН раз для обоих посредников (раздача и внешний вход).
func uiStreamProxyReadTimeout(t *testing.T) time.Duration {
	t.Helper()
	path := filepath.Join(repoRootFromTest(t), uiValuesRel)
	raw, err := os.ReadFile(path) // #nosec G304 -- путь собран из корня этого дерева
	if err != nil {
		t.Fatalf("объявление чарта консоли %s не читается (%v)", path, err)
	}
	var values struct {
		SubscriptionStream struct {
			ProxyReadTimeout string `yaml:"proxyReadTimeout"`
		} `yaml:"subscriptionStream"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("разбор %s: %v", uiValuesRel, err)
	}
	if values.SubscriptionStream.ProxyReadTimeout == "" {
		t.Fatalf("%s не объявляет `subscriptionStream.proxyReadTimeout` — величина, от "+
			"которой зависит, доживёт ли поток до своего срока, оставлена умолчанию "+
			"посредника, а умолчание никто не выбирал", uiValuesRel)
	}
	secs, err := strconv.Atoi(values.SubscriptionStream.ProxyReadTimeout)
	if err != nil {
		t.Fatalf("%s: `subscriptionStream.proxyReadTimeout` = %q не число секунд: %v",
			uiValuesRel, values.SubscriptionStream.ProxyReadTimeout, err)
	}
	return time.Duration(secs) * time.Second
}
