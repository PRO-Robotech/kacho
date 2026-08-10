// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// trusted_forwarders_wiring_test.go — страж РАЗМЕЩЕНИЯ: круг доверенных
// отправителей обязан приходить из конфигурации и доезжать до того значения, по
// которому носитель строит цепочку.
//
// Поведенческие замки (кто именно принимается, а кто отвергается) живут в
// trusted_forwarders_test.go. Этот файл закрывает другой класс: поведенческий тест
// можно прогнать с любым кругом и получить зелёное, а живой процесс при этом будет
// подниматься с зашитым пустым. Разрыв между «проверено» и «развёрнуто» — ровно та
// «форма без содержания», из-за которой дыра и прожила до сих пор: цепочка звеньев
// была написана правильно и получала пустой аргумент.
//
// # Почему страж больше НЕ читает исходник, и почему это усиление
//
// Прежняя редакция искала в тексте `serve.go` присваивание переменной `forwarders`
// и считала, сколько раз она уезжает в сборщик цепочки. Оба предмета исчезли
// вместе с собственной сборкой: цепочку строит носитель, а круг едет ему полем
// дескриптора. Текстовый страж на такое дерево не переносится — он либо падал бы
// на верном коде, либо (после «починки» регулярного выражения) утверждал бы про
// строку, а не про значение.
//
// Здесь утверждается ЗНАЧЕНИЕ: круг, который несёт ПРИНЯТЫЙ дескриптор, совпадает
// с тем, что дала конфигурация, и меняется вместе с ней. Литерал в коде такую
// проверку пройти не может — он не зависит от входа.
//
// «Оба слушателя получают один и тот же круг» здесь больше не считается по числу
// вызовов: у носителя цепочка строится ОДИН раз и подаётся обоим серверам, то есть
// свойство стало свойством построения. Наблюдаемую его половину держит
// `pkg/servicehost.TestBothListenersRefuseIdenticallyOnTheWire`.

import (
	"io"
	"log/slog"
	"testing"
)

// circleOfDescriptor — круг, который несёт принятый дескриптор storage.
func circleOfDescriptor(t *testing.T, cfg configForCircle) []string {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := prodCfg(cfg.sans...)
	c.AuthMode = "dev" // предмет — круг, а не боевая строгость транспорта
	desc, err := describe(c, logger, buildListFilter(c, nil, logger))
	if err != nil {
		t.Fatalf("дескриптор не принят на круге %v: %v", cfg.sans, err)
	}
	return desc.Spec().Forwarders.SANs()
}

type configForCircle struct{ sans []string }

// TestDescriptorCarriesTheConfiguredCircle — круг доезжает до дескриптора ИЗ
// конфигурации, и разный вход даёт разный круг.
//
// Второй случай обязателен: утверждение «круг равен ожидаемому» на ОДНОМ входе
// зеленело бы и на литерале, случайно совпавшем с ожиданием.
func TestDescriptorCarriesTheConfiguredCircle(t *testing.T) {
	both := circleOfDescriptor(t, configForCircle{sans: []string{gatewaySAN, computeSAN}})
	if len(both) != 2 || both[0] != gatewaySAN || both[1] != computeSAN {
		t.Fatalf("дескриптор несёт круг %v, конфигурация давала [%s %s]", both, gatewaySAN, computeSAN)
	}

	one := circleOfDescriptor(t, configForCircle{sans: []string{gatewaySAN}})
	if len(one) != 1 || one[0] != gatewaySAN {
		t.Fatalf("на сужённой конфигурации дескриптор несёт %v — значение не зависит от входа, "+
			"то есть в дескриптор уезжает не конфигурация", one)
	}
}

// TestDescriptorCircleIsFilteredLikeTheTransportFilters — значение, уезжающее в
// дескриптор, отфильтровано так же, как фильтрует общий фундамент (пустые записи
// отбрасываются).
//
// Иначе страж считал бы круг заполненным там, где транспорт видит пустой: список из
// одних пустых записей проходил бы как сужение и возвращал «доверяем любому».
func TestDescriptorCircleIsFilteredLikeTheTransportFilters(t *testing.T) {
	got := circleOfDescriptor(t, configForCircle{sans: []string{" ", gatewaySAN + " ", ""}})
	if len(got) != 1 || got[0] != gatewaySAN {
		t.Fatalf("круг дескриптора = %#v, ожидался ровно [%q]", got, gatewaySAN)
	}
}
