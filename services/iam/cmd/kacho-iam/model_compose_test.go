// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// model_compose_test.go — модель процесса СОБИРАЕТСЯ из доставленного и
// СТАВИТСЯ до первого чтения (задачи продукта #1969, #2002).
//
// # Почему проба живёт здесь, а не у владельцев звеньев
//
// Замок, который она стережёт, не принадлежит ни одному звену по отдельности:
// разбор доставки был исправен, установка была исправна, неисполним был их
// ПОРЯДОК. Такой класс ловится только сквозным вызовом — обе половины по
// отдельности зелены (`multi-agent-flow.md` §14, столкновение смыслов).
//
// # Почему собственный тестовый бинарь важен
//
// Признак «модель уже прочитана» — состояние ПРОЦЕССА. В одном бинаре пробы
// делят его, поэтому проба о первом чтении обязана быть единственной, кто до
// него доходит. Пакет `main` службы к `authzmodel` не обращался ни одним
// файлом до этой работы (предикат: `git grep -l authzmodel --
// 'services/iam/cmd/kacho-iam/*'` → пусто), значит здесь состояние чистое.

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmodel"
	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
)

// deliveryDeclaringRelationGrant — манифест, объявляющий НЕПУСТОЙ
// `seed.accessBindings[].grantedRelation`.
//
// Вход выбран не произвольно: ровно на нём разбор доставки доходил до
// `authzmodel.Shared()`. Манифест без этого ключа проходил и ДО работы, и
// ПОСЛЕ — проба на нём была бы вакуумной (сценарий `IAM-MB-1-17`, §11.3
// приёмки #1969: таких манифестов в дереве 2 из 6).
const deliveryDeclaringRelationGrant = `apiVersion: iam/v1
module: vpc
seed:
  serviceAccounts:
    - name: kacho-vpc
      account: kacho-system
      description: "Module SA: kacho-vpc (SEC-C least-priv)"
  accessBindings:
    - subjects:
        - {type: serviceAccount, name: kacho-vpc}
      grantedRelation: system_viewer
      scopeType: iam.cluster
      scopeId: cluster_kacho_root
      target: allInScope
`

// deliveryWithoutRelationGrant — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к предпосылке: манифест
// без выдачи отношением. Он не доходил до модели и до этой работы, поэтому
// доказывает, что предпосылка пробы выше измеряет именно выдачу отношением, а
// не «любой манифест».
const deliveryWithoutRelationGrant = `apiVersion: iam/v1
module: loadbalancer
`

// TestDeliveryParseDoesNotReadTheProcessModel — разбор доставки НЕ читает модель
// процесса, поэтому установка ПОСЛЕ разбора проходит.
//
// Это дословный предикат #2002: композиция ставится ДО первого чтения
// безусловно, а не «там, где модель читается».
func TestDeliveryParseDoesNotReadTheProcessModel(t *testing.T) {
	// Предпосылка: вход и вправду тот, о котором проба. Манифест обязан
	// разобраться — иначе «установка прошла» означало бы лишь то, что разбор
	// сорвался раньше, чем дошёл бы до модели.
	m, err := manifest.Load([]byte(deliveryDeclaringRelationGrant))
	if err != nil {
		t.Fatalf("предпосылка не создана: манифест с grantedRelation не разобрался: %v", err)
	}
	if len(m.Seed.AccessBindings) != 1 || strings.TrimSpace(m.Seed.AccessBindings[0].GrantedRelation) == "" {
		t.Fatalf("предпосылка не создана: вход обязан нести НЕПУСТОЙ grantedRelation, получено %+v",
			m.Seed.AccessBindings)
	}

	// Положительный контроль предпосылки: манифест БЕЗ выдачи отношением тоже
	// разбирается. Без него проба не отличала бы «разбор не читает модель» от
	// «разбор вообще ничего не делает».
	if _, err := manifest.Load([]byte(deliveryWithoutRelationGrant)); err != nil {
		t.Fatalf("контроль: манифест без grantedRelation обязан разбираться: %v", err)
	}

	// СУТЬ: после разбора доставки модель процесса ещё НЕ прочитана, поэтому
	// установка проходит. До #2002 здесь приезжал ErrModelAlreadyRead.
	if err := authzmodel.Install(authzmodel.DSL); err != nil {
		t.Fatalf("установка ПОСЛЕ разбора доставки отвергнута — значит разбор прочитал "+
			"модель процесса раньше, чем композиция успела её поставить: %v", err)
	}
}
