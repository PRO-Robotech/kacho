// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dataplane_lb_plaintext_test.go — публичный VIP плоскости данных не публикует
// открытый канал.
//
// Что по нему едет. Клиент реестра получает Bearer по TLS, а затем предъявляет
// его на КАЖДЫЙ запрос; ответ blob-GET может нести подписанную ссылку на объектное
// хранилище. Открытая порт-запись на публичном VIP означает, что клиент, который
// сам выбрал cleartext (insecure-registries, plain-http-инструменты, curl-
// автоматизация «по мотивам диагностического 401»), отправляет живой предъявитель
// и имена репозиториев в открытом виде через интернет.
//
// Почему это не лечится комментарием. Шаблон обещал «поставьте перед VIP внешний
// TLS-терминатор», а соседний файл значений того же профиля фиксирует, что
// внешнего терминатора там НЕТ — ради этого и заведён sidecar. Предпосылку
// контроля опровергал соседний артефакт, а порт-запись рендерилась безусловно.
package deploy_test

import (
	"strings"
	"testing"
)

// lbSets — минимальный набор, поднимающий внешний VIP с TLS-терминацией в поде
// (боевая форма fe3455).
var lbSets = []string{
	"zot.auth.password=" + renderPassword,
	"service.dataplaneLB.enabled=true",
	"service.dataplaneLB.tlsSidecar.enabled=true",
}

// TestDataplaneLB_PublishesNoPlaintextPortByDefault — при включённом VIP
// публикуется только TLS-порт; открытая запись не рендерится, пока её не
// потребовали явно.
func TestDataplaneLB_PublishesNoPlaintextPortByDefault(t *testing.T) {
	svc := docOf(t, helmTemplate(t, lbSets...), "service-dataplane-lb.yaml", "kind: Service")
	if strings.Contains(svc, "name: dataplane\n") {
		t.Errorf("the external VIP still publishes a cleartext port straight at the data-plane — "+
			"bearer credentials and repository names would transit the public network in the "+
			"clear for any client that speaks http:\n%s", svc)
	}
	if !strings.Contains(svc, "name: dataplane-https") {
		t.Errorf("the external VIP publishes no TLS port at all:\n%s", svc)
	}
	if !strings.Contains(svc, "targetPort: dp-tls") {
		t.Errorf("the TLS port does not target the in-pod terminator:\n%s", svc)
	}
}

// TestDataplaneLB_PlaintextIsAnExplicitOptIn — обратная сторона инъекции: та же
// форма конфигурации, когда открытый канал ЗАТРЕБОВАН явно, рендерится. Иначе
// гейт запрещал бы форму, а не существо (есть стенды, где перед VIP реально стоит
// внешний терминатор и внутренний хоп открыт осознанно).
func TestDataplaneLB_PlaintextIsAnExplicitOptIn(t *testing.T) {
	svc := docOf(t, helmTemplate(t, append(append([]string{}, lbSets...),
		"service.dataplaneLB.plaintext.enabled=true")...), "service-dataplane-lb.yaml", "kind: Service")
	if !strings.Contains(svc, "name: dataplane\n") {
		t.Errorf("explicit plaintext opt-in did not publish the port — the knob does nothing:\n%s", svc)
	}
}

// TestProfiles_NoProfilePublishesPlaintextDataplane — декларативный гейт: ни один
// профиль, поднимающий внешний VIP, не включает открытую порт-запись.
func TestProfiles_NoProfilePublishesPlaintextDataplane(t *testing.T) {
	examinedLB := 0
	for _, p := range umbrellaProfiles(t) {
		tree := umbrellaValues(t, p)
		on, ok := digOpt(tree, "registry", "service", "dataplaneLB", "enabled")
		if !ok || on != true {
			continue
		}
		examinedLB++
		if v, ok := digOpt(tree, "registry", "service", "dataplaneLB", "plaintext", "enabled"); ok && v == true {
			t.Errorf("%s publishes the data-plane VIP with a cleartext port — bearer credentials "+
				"would transit the public network in the clear", p)
		}
	}
	if examinedLB == 0 {
		t.Fatal("no profile publishes the data-plane VIP — this gate has lost its subject and " +
			"would silently pass forever")
	}
	t.Logf("examined %d profiles publishing the data-plane VIP", examinedLB)
}
