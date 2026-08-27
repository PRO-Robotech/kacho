// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

import (
	"os"
	"strconv"
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
		SubscriptionStream struct {
			Owners       string `yaml:"owners"`
			StreamBudget string `yaml:"streamBudget"`
			Heartbeat    string `yaml:"heartbeat"`
			MaxStreams   int    `yaml:"maxStreams"`
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
}
