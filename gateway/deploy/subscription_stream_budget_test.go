// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// subscription_stream_budget_test.go — срок жизни потока МЕНЬШЕ предела чтения
// посредника.
//
// # Предмет
//
// Две величины принадлежат разным слоям и никем не сверяются: срок жизни потока
// объявляет край, предел чтения — посредник. Разойдись они в одну сторону —
// поток рвёт ПОСРЕДНИК, и клиент читает это как сетевой сбой, а не как чистое
// закрытие по сроку, после которого он возобновляется со своей позиции.
//
// Дефект тихий: поток работает, события приходят, и только через две минуты
// клиент получает обрыв, объяснить который нечем.
//
// # Почему проба читает ОБЪЯВЛЕНИЕ, а не рендер
//
// Рендер требует helm, которого в этом харнессе нет. Проба, требующая
// недоступного средства, пропускается — то есть не краснеет никогда. Объявление
// же читается всегда и здесь достаточно: обе величины стоят в нём литералами.
//
// Цена этого выбора названа и закрыта отдельно (kacho#1402): читая объявление,
// проба не знает, есть ли у величины ПОТРЕБИТЕЛЬ и рендерится ли он, — а без
// этого её «зелёное» означало бы «смотреть нечего». Держат это
// `TestSubscriptionStreamBudgetHasALiveProxyToFitUnder` (потребитель есть и
// включён) и `TestSubscriptionStreamBudgetIsCheckedAgainstTheWinningValue`
// (сверка идёт с побеждающим значением профиля, а не с умолчанием подчарта).
// Здесь они не пересказываются — два места об одном предмете расходятся молча.
func TestSubscriptionStreamBudgetFitsUnderTheProxyReadTimeout(t *testing.T) {
	raw, err := os.ReadFile("values.yaml")
	if err != nil {
		t.Fatalf("чтение объявления чарта: %v", err)
	}
	var values struct {
		Ingress struct {
			ProxyReadTimeout string `yaml:"proxyReadTimeout"`
		} `yaml:"ingress"`
		Replicas           int `yaml:"replicas"`
		SubscriptionStream struct {
			Owners               string `yaml:"owners"`
			StreamBudget         string `yaml:"streamBudget"`
			Heartbeat            string `yaml:"heartbeat"`
			MaxStreams           int    `yaml:"maxStreams"`
			MaxStreamsPerSubject int    `yaml:"maxStreamsPerSubject"`
			OwnerStreamCeiling   string `yaml:"ownerStreamCeiling"`
		} `yaml:"subscriptionStream"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("разбор объявления чарта: %v", err)
	}

	proxySeconds, err := strconv.Atoi(values.Ingress.ProxyReadTimeout)
	if err != nil {
		t.Fatalf("ingress.proxyReadTimeout = %q не число секунд: %v",
			values.Ingress.ProxyReadTimeout, err)
	}
	proxy := time.Duration(proxySeconds) * time.Second

	budget, err := time.ParseDuration(values.SubscriptionStream.StreamBudget)
	if err != nil {
		t.Fatalf("subscriptionStream.streamBudget = %q не срок: %v",
			values.SubscriptionStream.StreamBudget, err)
	}
	heartbeat, err := time.ParseDuration(values.SubscriptionStream.Heartbeat)
	if err != nil {
		t.Fatalf("subscriptionStream.heartbeat = %q не срок: %v",
			values.SubscriptionStream.Heartbeat, err)
	}

	t.Logf("перепись: предел чтения посредника %v · срок жизни потока %v · "+
		"кадр поддержания связи %v · потолок потоков %d · владельцев объявлено %q",
		proxy, budget, heartbeat, values.SubscriptionStream.MaxStreams,
		values.SubscriptionStream.Owners)

	if proxy <= 0 || budget <= 0 || heartbeat <= 0 {
		t.Fatalf("одна из величин не объявлена (посредник %v, срок %v, кадр %v) — "+
			"гейт ничего не сверял", proxy, budget, heartbeat)
	}
	if budget >= proxy {
		t.Errorf("срок жизни потока %v не меньше предела чтения посредника %v: "+
			"поток будет рвать посредник, и клиент прочтёт это как сетевой сбой", budget, proxy)
	}
	if heartbeat >= proxy {
		t.Errorf("кадр поддержания связи %v не чаще предела чтения посредника %v: "+
			"молчащий поток закроется прежде первого кадра", heartbeat, proxy)
	}
	if heartbeat >= budget {
		t.Errorf("кадр поддержания связи %v не чаще срока жизни потока %v", heartbeat, budget)
	}
	if values.SubscriptionStream.MaxStreams <= 0 {
		t.Errorf("потолок одновременных потоков %d — величина посадки, а не вкус",
			values.SubscriptionStream.MaxStreams)
	}
	if n := values.SubscriptionStream.MaxStreamsPerSubject; n <= 0 || n > values.SubscriptionStream.MaxStreams {
		t.Errorf("предел на субъекта %d при потолке реплики %d: без него один арендатор "+
			"занимает потолок целиком, а выше потолка он предела не ставит",
			n, values.SubscriptionStream.MaxStreams)
	}
}

// TestReplicaFanoutFitsUnderTheOwnerCeiling — арифметика «число реплик × потолок
// помещается в потолок владельца» СВЕРЯЕТСЯ, а не только объявляется.
//
// # Почему потолок владельца читается У ВЛАДЕЛЬЦА
//
// Копия его величины в чарте края была бы вторым местом об одном предмете:
// разойдясь, они разошлись бы молча — обе непусты, обе выглядят действующими, и
// ни одна не знает о другой. Плюс ключ профиля, которого не читает ни один
// шаблон, до процесса не доедет никогда, и оператор распоряжался бы тем, чего
// нет. Поэтому здесь резолвится объявление ВЛАДЕЛЬЦА.
//
// # Почему сверка привязана к появлению владельца
//
// Пока `owners` пуст, у произведения нет второй стороны: сравнивать не с чем, и
// требование величины было бы требованием её выдумать. Как только владелец
// назван — арифметика становится настоящей, и проба требует её в тот же момент.
//
// # Чем это отличается от прежнего состояния
//
// Правило «реплики × потолок обязаны помещаться» стояло в трёх местах прозой и
// НЕ сверялось ничем: ни гейтом, ни стражем старта. Прозы достаточно, пока никто
// не поднимает потолок; ошибка же наступает у ВЛАДЕЛЬЦА — то есть у всех
// арендаторов сразу, а не у того, кто её сделал.
func TestReplicaFanoutFitsUnderTheOwnerCeiling(t *testing.T) {
	raw, err := os.ReadFile("values.yaml")
	if err != nil {
		t.Fatalf("чтение объявления чарта: %v", err)
	}
	var values struct {
		Replicas           int `yaml:"replicas"`
		SubscriptionStream struct {
			Owners     string `yaml:"owners"`
			MaxStreams int    `yaml:"maxStreams"`
		} `yaml:"subscriptionStream"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("разбор объявления чарта: %v", err)
	}

	owners := make([]string, 0, 2)
	for _, name := range strings.Split(values.SubscriptionStream.Owners, ",") {
		if name = strings.TrimSpace(name); name != "" {
			owners = append(owners, name)
		}
	}
	product := values.Replicas * values.SubscriptionStream.MaxStreams
	t.Logf("перепись: владельцев объявлено %d %v · реплик %d · потолок реплики %d · "+
		"произведение %d", len(owners), owners, values.Replicas,
		values.SubscriptionStream.MaxStreams, product)

	if values.Replicas <= 0 || values.SubscriptionStream.MaxStreams <= 0 {
		t.Fatalf("реплик %d, потолок %d — гейт ничего не считал",
			values.Replicas, values.SubscriptionStream.MaxStreams)
	}
	if len(owners) == 0 {
		// Второй стороны у произведения нет — сравнивать не с чем.
		//
		// Профиль ВПРАВЕ не объявить владельцев, и тогда ручка отвечает `501`;
		// требовать здесь непустоты значило бы судить поставку, а её предмет
		// другой и держат его deploy/subscription_shipped_to_production_test.go
		// (боевая посадка не беднее стенда) и проба посадки в пакете ручки.
		return
	}

	for _, owner := range owners {
		ceiling, where, found := ownerStreamCeiling(t, owner)
		if !found {
			t.Errorf("владелец %q объявлен, а его собственный потолок потоков не найден "+
				"(искали в %s): тогда арифметика «реплики × потолок» остаётся прозой, "+
				"а исчерпание наступает у ВЛАДЕЛЬЦА — у всех арендаторов сразу", owner, where)
			continue
		}
		if product > ceiling {
			t.Errorf("владелец %q: реплики × потолок = %d × %d = %d превосходит его потолок %d "+
				"(%s) — край исчерпает владельца прежде собственного предела",
				owner, values.Replicas, values.SubscriptionStream.MaxStreams, product, ceiling, where)
		}
	}
}

// ownerTreeDirAliases — КАТАЛОГ В ДЕРЕВЕ, если он зовётся не так, как владелец.
//
// Ключ — имя владельца, то есть ровно то написание, которое край ПРИНИМАЕТ в
// `?owner=` и в `subscriptionStream.owners`. Значение — каталог сервиса под
// `services/`, и только он: это путь в дереве, а НЕ второе имя владельца.
//
// # Почему псевдоним вообще нужен
//
// Имя владельца — ключ карты соединений края (`config.BackendAddrs`), и он
// совпадает с пакетом контракта: балансировщик объявлен там `loadbalancer`
// (`kacho.cloud.loadbalancer.v1`), а его каталог в дереве зовётся `nlb`. Оба
// написания исторические и живут каждое в своём месте.
//
// # ОМОНИМИЯ, на которой уже спотыкались (kacho#1454)
//
// Слово `backends` означает в этом дереве ДВЕ РАЗНЫЕ вещи, и различать их
// обязательно:
//
//   - `backends:` в объявлении чарта (`values.yaml`) — блок АДРЕСОВ, его ключи
//     `nlb` / `nlbInternal`;
//   - `proxy.Backends` в коде края — карта СОЕДИНЕНИЙ, её ключи `loadbalancer` /
//     `loadbalancerInternal` (шаблон подаёт первый адрес во второй под другим
//     именем, через переменную окружения).
//
// Владельца резолвит ВТОРАЯ (`backends[name+"Internal"]`), поэтому принимаемое
// имя — `loadbalancer`; `nlb` не резолвится и дал бы отказ старта. Согласие этой
// карты с картой соединений края держит не проза, а
// [TestOwnerNameIsTheBackendKeyOfTheEdgeNotTheTreePath].
var ownerTreeDirAliases = map[string][]string{"loadbalancer": {"nlb"}}

// ownerStreamCeiling читает потолок потоков в объявлении ВЛАДЕЛЬЦА.
//
// Перебираются кандидаты, а не строится один путь: каталог сервиса может не
// совпадать с именем владельца (см. [ownerTreeDirAliases]), и несовпадение
// обязано давать НАЗВАННЫЙ отказ с перечнем осмотренного, а не тихое «не найдено».
func ownerStreamCeiling(t *testing.T, owner string) (int, string, bool) {
	t.Helper()
	candidates := append([]string{owner}, ownerTreeDirAliases[owner]...)

	looked := make([]string, 0, len(candidates))
	for _, dir := range candidates {
		path := filepath.Join("..", "..", "services", dir, "deploy", "values.yaml")
		looked = append(looked, filepath.ToSlash(path))
		raw, err := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
		if err != nil {
			continue
		}
		var owned struct {
			Subscription struct {
				MaxStreams int `yaml:"maxStreams"`
			} `yaml:"subscription"`
		}
		if err := yaml.Unmarshal(raw, &owned); err != nil {
			continue
		}
		if owned.Subscription.MaxStreams > 0 {
			return owned.Subscription.MaxStreams, filepath.ToSlash(path), true
		}
	}
	return 0, strings.Join(looked, ", "), false
}

// ─────────────────────────────────────────────────────────────────────────────
// У ВЕЛИЧИНЫ ОБЯЗАН БЫТЬ ПОТРЕБИТЕЛЬ, КОТОРЫЙ ДЕЙСТВИТЕЛЬНО РЕНДЕРИТСЯ (kacho#1402)
//
// # Что было неверно
//
// Проверка выше сверяет срок жизни потока с пределом чтения посредника, читая
// предел из `ingress.proxyReadTimeout` этого чарта. Собственный вход подчарта в
// поставке ВЫКЛЮЧЕН (`api-gateway.ingress.enabled=false` в значениях умбреллы),
// и отсюда напрашивался вывод: величина не рендерится нигде, а проверка зелена
// «потому что смотреть нечего».
//
// # Замер опроверг посылку — и не полностью
//
// Величина рендерится. Умбрелла выключает ШАБЛОН подчарта и рендерит СВОЙ вход
// (`templates/api-gateway-ingress.yaml`), а предел берёт оттуда же — из значений
// подчарта, потому что helm сливает их в вид родителя. Проверено рендером, а не
// чтением спецификации: `api-gateway.ingress` = `{enabled: false}` у родителя
// даёт в его шаблоне `proxyReadTimeout: "120"`, унаследованный от подчарта.
//
// Но неполнота, ради которой задача заведена, НАСТОЯЩАЯ и осталась: проверка
// никогда не спрашивала, есть ли у величины потребитель ВООБЩЕ. Сними завтра
// последний вход — она останется зелёной навсегда, и её «зелёное» будет означать
// ровно то, чего опасалась задача: «смотреть нечего».
//
// # Чем закрыто
//
// Двумя утверждениями, и оба самоистекающие:
//
//  1. ПОТРЕБИТЕЛИ ВЫВОДЯТСЯ ИЗ ДЕРЕВА. Чарты-родители находятся по объявлению
//     зависимости `file://` на этот чарт, а не выписываются; потребителем зовётся
//     шаблон, выдающий заголовок предела чтения и читающий эту величину.
//     Потребителей ноль — ПРОВАЛ: величина стала мёртвой;
//  2. У КАЖДОГО ПОТРЕБИТЕЛЯ НАЗВАН ВЫКЛЮЧАТЕЛЬ, и хотя бы один обязан быть
//     включён в поставке. Потребитель без записи — находка: «новый вход завели, а
//     чем он включается, никто не сказал».
//
// Побеждающее значение читается ПО ПРОФИЛЯМ, а не только из умолчания подчарта:
// профиль вправе переопределить предел, и тогда сверять надо его.

// agwChartDir / repoRootFromChart — координаты обхода. Единственный рукописный
// путь здесь — подъём от каталога чарта к корню дерева; всё остальное выводится.
const (
	agwChartDir       = "gateway/deploy"
	repoRootFromChart = "../.."
)

// proxyTimeoutConsumer — шаблон, выдающий предел чтения посредника из величины
// этого чарта, и выключатель, которым его рендер гейтится.
//
// Перечень существует затем, чтобы новый вход не появился МОЛЧА: потребитель без
// записи — находка. Запись без потребителя — тоже находка: перечень, переживший
// свой предмет, читается как действующий.
type proxyConsumerRecord struct {
	Toggle string // путь ключа-выключателя в значениях УМБРЕЛЛЫ
	Why    string
}

var proxyTimeoutConsumers = map[string]proxyConsumerRecord{
	"gateway/deploy/templates/ingress.yaml": {
		Toggle: "api-gateway.ingress.enabled",
		Why: "собственный вход подчарта; в умбрелле ВЫКЛЮЧЕН намеренно (он смотрел бы " +
			"в незашифрованный слушатель), но при отдельной установке чарта он и есть вход",
	},
	"deploy/helm/umbrella/templates/api-gateway-ingress.yaml": {
		Toggle: "apiGatewayIngress.enabled",
		Why: "вход, принадлежащий умбрелле; величину предела наследует из значений подчарта — " +
			"helm сливает их в вид родителя, поэтому второго объявления не заводится",
	},
}

var proxyTimeoutHeader = regexp.MustCompile(`proxy-read-timeout`)

// parentChartsOf — чарты, объявившие зависимость `file://` на этот чарт.
//
// Выводятся обходом дерева, а не перечнем: рукописный перечень родителей
// разошёлся бы с деревом молча, и потребитель в новой умбрелле остался бы вне
// наблюдения.
func parentChartsOf(t *testing.T, root, childDir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "Chart.yaml" {
			return err
		}
		raw, rerr := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
		if rerr != nil {
			return rerr
		}
		var chart struct {
			Dependencies []struct {
				Repository string `yaml:"repository"`
			} `yaml:"dependencies"`
		}
		if yaml.Unmarshal(raw, &chart) != nil {
			return nil
		}
		dir := filepath.Dir(path)
		for _, dep := range chart.Dependencies {
			if !strings.HasPrefix(dep.Repository, "file://") {
				continue
			}
			target := filepath.Clean(filepath.Join(dir, strings.TrimPrefix(dep.Repository, "file://")))
			rel, rerr := filepath.Rel(root, target)
			if rerr != nil || filepath.ToSlash(rel) != childDir {
				continue
			}
			relDir, _ := filepath.Rel(root, dir)
			out = append(out, filepath.ToSlash(relDir))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева в поисках чартов-родителей: %v", err)
	}
	sort.Strings(out)
	return out
}

// proxyTimeoutConsumersInTree — шаблоны, выдающие предел чтения из этой величины.
func proxyTimeoutConsumersInTree(t *testing.T, root string, chartDirs []string) (found []string, scanned int) {
	t.Helper()
	for _, dir := range chartDirs {
		tpl := filepath.Join(root, dir, "templates")
		if _, err := os.Stat(tpl); err != nil {
			continue
		}
		err := filepath.WalkDir(tpl, func(path string, d os.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			scanned++
			raw, rerr := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
			if rerr != nil {
				return rerr
			}
			text := string(raw)
			// Потребителем зовётся шаблон, который ВЫДАЁТ предел чтения и берёт
			// его из величины `proxyReadTimeout`. Одного упоминания величины мало:
			// о ней говорят и комментарии соседних шаблонов.
			if proxyTimeoutHeader.MatchString(text) && strings.Contains(text, "proxyReadTimeout") {
				rel, _ := filepath.Rel(root, path)
				found = append(found, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("обход шаблонов %s: %v", dir, err)
		}
	}
	sort.Strings(found)
	return found, scanned
}

// proxyConsumerCensus — объём осмотренного. Печатается ВСЕГДА.
type proxyConsumerCensus struct {
	Parents   []string
	Templates int
	Consumers []string
	Live      int
	Toggles   []string
}

// auditProxyConsumers — есть ли у сторожимой величины потребитель и рендерится ли
// он. Чистая над деревом: самопроверка подаёт ей синтетический корень, а не
// подделывает настоящий.
func auditProxyConsumers(
	t *testing.T, root, chartDir string, catalogue map[string]proxyConsumerRecord,
) (findings []string, census proxyConsumerCensus) {
	t.Helper()
	census.Parents = parentChartsOf(t, root, chartDir)
	chartDirs := append([]string{chartDir}, census.Parents...)
	census.Consumers, census.Templates = proxyTimeoutConsumersInTree(t, root, chartDirs)

	if census.Templates == 0 {
		return []string{"ни одного шаблона не осмотрено — «потребителей нет» стало неотличимо " +
			"от «дерево не прочитано»"}, census
	}
	if len(census.Consumers) == 0 {
		return []string{"предел чтения посредника не выдаёт НИ ОДИН шаблон: величина, которую " +
			"сторожит проверка срока жизни потока, никуда не попадает, и её «зелёное» означает " +
			"«смотреть нечего», а не «всё в порядке». Снимите проверку вместе с величиной либо " +
			"перенацельте её на профиль, который рендерится"}, census
	}

	base := umbrellaValuesAt(t, root, census.Parents)
	for _, path := range census.Consumers {
		rec, known := catalogue[path]
		if !known {
			findings = append(findings, fmt.Sprintf(
				"%s выдаёт предел чтения, а в перечне потребителей его нет: назовите выключатель, "+
					"которым гейтится его рендер, — иначе «хоть один включён» перестаёт что-либо "+
					"утверждать", path))
			continue
		}
		on, where := toggleState(base, rec.Toggle)
		census.Toggles = append(census.Toggles,
			fmt.Sprintf("%s → %s = %v (%s); %s", path, rec.Toggle, on, where, rec.Why))
		if on {
			census.Live++
		}
	}
	stale := make([]string, 0, len(catalogue))
	for path := range catalogue {
		if !slices.Contains(census.Consumers, path) {
			stale = append(stale, path)
		}
	}
	sort.Strings(stale)
	for _, path := range stale {
		findings = append(findings, fmt.Sprintf(
			"записи перечня потребителей %q больше нечего описывать: шаблон предела чтения не "+
				"выдаёт. Снимите запись — перечень, переживший свой предмет, читается как "+
				"действующий", path))
	}
	if census.Live == 0 {
		findings = append(findings, "ни один потребитель предела чтения не включён в поставке: "+
			"проверка срока жизни потока сторожит величину, которая не рендерится, и зелена "+
			"по отсутствию предмета")
	}
	return findings, census
}

// umbrellaValuesAt — базовые значения умбреллы. Именно они решают, что
// поставляется: профили накладываются поверх, но выключателей входа не трогают
// (проверяется отдельно, чтением каждого профиля).
func umbrellaValuesAt(t *testing.T, root string, parents []string) map[string]any {
	t.Helper()
	for _, dir := range parents {
		raw, err := os.ReadFile(filepath.Join(root, dir, "values.yaml")) // #nosec G304
		if err != nil {
			continue
		}
		var tree map[string]any
		if yaml.Unmarshal(raw, &tree) == nil {
			return tree
		}
	}
	return map[string]any{}
}

// toggleState — значение выключателя по точечному пути. Ненайденный выключатель
// НЕ считается включённым: неизвестное состояние не бывает разрешением.
func toggleState(tree map[string]any, path string) (bool, string) {
	cur := any(tree)
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return false, "путь оборвался на " + seg
		}
		cur, ok = m[seg]
		if !ok {
			return false, "ключ не объявлен"
		}
	}
	b, ok := cur.(bool)
	if !ok {
		return false, fmt.Sprintf("значение %v не логическое", cur)
	}
	return b, "объявлен значениями умбреллы"
}

// TestSubscriptionStreamBudgetHasALiveProxyToFitUnder — вердикт о НАСТОЯЩЕМ
// дереве. Способность падать доказывает не он, а инъекция
// (`subscription_stream_budget_injection_test.go`).
func TestSubscriptionStreamBudgetHasALiveProxyToFitUnder(t *testing.T) {
	findings, census := auditProxyConsumers(t, repoRootFromChart, agwChartDir, proxyTimeoutConsumers)
	t.Logf("перепись: чартов-родителей %d %v · шаблонов осмотрено %d · потребителей предела %d %v · "+
		"из них включено в поставке %d",
		len(census.Parents), census.Parents, census.Templates,
		len(census.Consumers), census.Consumers, census.Live)
	for _, line := range census.Toggles {
		t.Log("  " + line)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// winningValueCensus — объём осмотренного при сверке с побеждающим значением.
type winningValueCensus struct {
	Profiles  int
	Overrides int
	Default   string
	Budget    time.Duration
}

// auditWinningValue — сверка идёт с ПОБЕЖДАЮЩИМ значением каждого профиля, а не
// только с умолчанием подчарта: профиль вправе переопределить предел, и тогда
// умолчание — не то число, под которое обязан помещаться срок жизни потока.
func auditWinningValue(
	t *testing.T, root, chartDir string,
) (findings []string, census winningValueCensus) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, chartDir, "values.yaml")) // #nosec G304
	if err != nil {
		return []string{fmt.Sprintf("чтение объявления чарта: %v", err)}, census
	}
	var own struct {
		Ingress struct {
			ProxyReadTimeout string `yaml:"proxyReadTimeout"`
		} `yaml:"ingress"`
		SubscriptionStream struct {
			StreamBudget string `yaml:"streamBudget"`
		} `yaml:"subscriptionStream"`
	}
	if err := yaml.Unmarshal(raw, &own); err != nil {
		return []string{fmt.Sprintf("разбор объявления чарта: %v", err)}, census
	}
	census.Default = own.Ingress.ProxyReadTimeout
	budget, berr := time.ParseDuration(own.SubscriptionStream.StreamBudget)
	if berr != nil {
		return []string{fmt.Sprintf("subscriptionStream.streamBudget = %q не срок: %v",
			own.SubscriptionStream.StreamBudget, berr)}, census
	}
	census.Budget = budget

	for _, dir := range parentChartsOf(t, root, chartDir) {
		entries, derr := os.ReadDir(filepath.Join(root, dir))
		if derr != nil {
			return append(findings, fmt.Sprintf("чтение каталога умбреллы %s: %v", dir, derr)), census
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasPrefix(name, "values") || !strings.HasSuffix(name, ".yaml") {
				continue
			}
			census.Profiles++
			pr, rerr := os.ReadFile(filepath.Join(root, dir, name)) // #nosec G304
			if rerr != nil {
				findings = append(findings, fmt.Sprintf("чтение профиля %s: %v", name, rerr))
				continue
			}
			var tree map[string]any
			if yaml.Unmarshal(pr, &tree) != nil {
				continue
			}
			val, ok := lookupString(tree, "api-gateway", "ingress", "proxyReadTimeout")
			if !ok {
				continue // величину не трогает: побеждает умолчание подчарта
			}
			census.Overrides++
			secs, cerr := strconv.Atoi(val)
			if cerr != nil {
				findings = append(findings, fmt.Sprintf(
					"%s: api-gateway.ingress.proxyReadTimeout = %q не число секунд", name, val))
				continue
			}
			if proxy := time.Duration(secs) * time.Second; budget >= proxy {
				findings = append(findings, fmt.Sprintf(
					"%s: срок жизни потока %v не меньше переопределённого предела чтения %v — "+
						"поток будет рвать посредник именно на этом стеке", name, budget, proxy))
			}
		}
	}
	if census.Profiles == 0 {
		findings = append(findings, "ни одного профиля не осмотрено — «никто не переопределяет» "+
			"стало неотличимо от «профилей не читали»")
	}
	return findings, census
}

// TestSubscriptionStreamBudgetIsCheckedAgainstTheWinningValue — вердикт о
// НАСТОЯЩЕМ дереве.
func TestSubscriptionStreamBudgetIsCheckedAgainstTheWinningValue(t *testing.T) {
	findings, census := auditWinningValue(t, repoRootFromChart, agwChartDir)
	t.Logf("перепись: профилей осмотрено %d · переопределяющих предел чтения %d · "+
		"умолчание подчарта %q · срок жизни потока %v",
		census.Profiles, census.Overrides, census.Default, census.Budget)
	for _, f := range findings {
		t.Error(f)
	}
}

func lookupString(tree map[string]any, path ...string) (string, bool) {
	cur := any(tree)
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[seg]
		if !ok {
			return "", false
		}
	}
	switch v := cur.(type) {
	case string:
		return v, true
	case int:
		return strconv.Itoa(v), true
	}
	return "", false
}
