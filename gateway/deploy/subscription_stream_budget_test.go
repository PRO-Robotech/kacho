// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

import (
	"os"
	"path/filepath"
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
		// Второй стороны у произведения нет. Это законное состояние фазы, а не
		// пропуск: первого владельца заводит kacho#1019.
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

// ownerStreamCeiling читает потолок потоков в объявлении ВЛАДЕЛЬЦА.
//
// Имя владельца — ключ домена края, и каталог сервиса не всегда зовётся так же
// (край знает `loadbalancer`, дерево — `nlb`). Поэтому перебираются кандидаты, а
// не строится один путь: несовпадение имени обязано давать НАЗВАННЫЙ отказ с
// перечнем осмотренного, а не тихое «не найдено».
func ownerStreamCeiling(t *testing.T, owner string) (int, string, bool) {
	t.Helper()
	aliases := map[string][]string{"loadbalancer": {"nlb"}}
	candidates := append([]string{owner}, aliases[owner]...)

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
