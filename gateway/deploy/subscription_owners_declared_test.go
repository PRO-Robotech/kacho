// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

import (
	"os"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// subscription_owners_declared_test.go — объявленный ПРОФИЛЕМ владелец есть имя,
// которое край примет.
//
// # Предмет — цена в один полный цикл выкатки
//
// Владельца называет оператор, а принимает край, и написания у домена два:
// каталог сервиса и REST-путь зовут балансировщик `nlb`, контракт и карта
// соединений края — `loadbalancer`. Написание вне множества принимаемых край
// отвергает СТАРТОМ: страж отрабатывает как задуман, но узнаётся это на выкатке.
//
// Здесь то же самое узнаётся на прогоне — до сборки образа и до подъёма.
//
// # Почему пустой перечень ПРОХОДИТ
//
// Пусто — «владелец не объявлен», законное состояние посадки: ручка отвечает
// `501` с названной причиной. Требовать непустоты значило бы требовать от гейта
// судить ПОСТАВКУ, а его предмет — согласие написаний. Число объявленных при
// этом печатается всегда, поэтому ноль виден и отличим от «не разобрали».
func TestDeclaredSubscriptionOwnersAreNamesTheEdgeAccepts(t *testing.T) {
	raw, err := os.ReadFile("values.yaml")
	if err != nil {
		t.Fatalf("чтение объявления чарта: %v", err)
	}
	var values struct {
		SubscriptionStream struct {
			Owners string `yaml:"owners"`
		} `yaml:"subscriptionStream"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("разбор объявления чарта: %v", err)
	}

	declared := splitDeclaredOwners(values.SubscriptionStream.Owners)
	accepted := config.Config{}.DomainsWithInternalBackend()
	unknown := ownersTheEdgeWillRefuse(declared, accepted)

	t.Logf("перепись: владельцев объявлено %d %v · принимает край %d %v · не принимается %d %v",
		len(declared), declared, len(accepted), accepted, len(unknown), unknown)

	if len(accepted) == 0 {
		t.Fatal("край не принимает НИ ОДНОГО имени — сверять было не с чем")
	}
	for _, name := range unknown {
		t.Errorf("профиль объявляет владельцем %q, а край принимает %v: край ОТКАЖЕТСЯ "+
			"СТАРТОВАТЬ — имя владельца берётся из карты соединений края (домен контракта "+
			"`kacho.cloud.<домен>.v1`), а не из ключей блока `backends:` этого же файла и "+
			"не из сегмента REST-пути", name, accepted)
	}
}

// splitDeclaredOwners разбирает перечень так же, как его разбирает край.
//
// Вырожденное значение (одинокая запятая) даёт непустую строку и НОЛЬ имён:
// решать «объявлен ли владелец» по длине строки нельзя, и здесь это повторено не
// из осторожности — тем же разрывом однажды разошлись круг отправителей у стража
// и у транспорта.
func splitDeclaredOwners(raw string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// ownersTheEdgeWillRefuse — объявленные имена, которых край не знает.
//
// Отдельной функцией ради того, чтобы доказательство способности гейта упасть
// прогоняло ТУ ЖЕ функцию суждения, а не её пересказ.
func ownersTheEdgeWillRefuse(declared, accepted []string) []string {
	acceptedSet := make(map[string]bool, len(accepted))
	for _, name := range accepted {
		acceptedSet[name] = true
	}
	refused := make([]string, 0, len(declared))
	for _, name := range declared {
		if !acceptedSet[name] {
			refused = append(refused, name)
		}
	}
	return refused
}
