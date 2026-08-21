// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_guard_discrimination_test.go — каждый страж старта отказывает СВОИМ
// отказом, и это утверждается, а не предполагается.
//
// # Почему такой файл вообще нужен
//
// Стражей у одного объявления девять, и они стоят подряд. Проба, которая
// подаёт негодный вход и радуется любому отказу, зеленеет, когда её страж снят,
// — потому что отказал соседний. Это не теория: ровно так и случилось с пробой
// вырожденного перечня, и обнаружил это ревьюер инъекцией, а не чтением. Отказ
// приходил от соседа («наш издатель вне перечня принимаемых»), чьё сообщение
// несёт ту же подстроку имени ручки.
//
// Отсюда форма. У каждого стража есть СВОЙ различитель — подстрока, которую не
// печатает больше никто, — и фикстура, на которой отказать может ТОЛЬКО он.
// Тогда снятие стража даёт либо проход (и проба краснеет), либо отказ соседа с
// чужим различителем (и проба краснеет тоже).
//
// # Чего этот файл не делает
//
// Он не проверяет, что страж отказывает ПРАВИЛЬНО — это предмет соседних проб
// с положительными контролями. Здесь предмет один: отказ принадлежит тому, кому
// приписан.
package config_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// f1bGuardCase — один страж: вход, на котором отказать может только он, и его
// собственный различитель.
type f1bGuardCase struct {
	// Name — чем этот страж является для читателя отказа.
	Name string
	// Cfg — минимальное объявление: всё, что могло бы отказать вместо предмета,
	// из него убрано намеренно.
	Cfg config.Config
	// Discriminator — подстрока, которую печатает ТОЛЬКО этот страж.
	Discriminator string
}

func f1bBase(env string) config.Config {
	return config.Config{AppEnv: env, APIDomain: "api.kacho.test"}
}

func TestF1b_EveryStartGuardRefusesWithItsOwnRefusal(t *testing.T) {
	cases := []f1bGuardCase{
		{
			Name:          "вырожденный перечень издателей",
			Cfg:           func() config.Config { c := f1bBase("production"); c.TokenIssuers = ","; return c }(),
			Discriminator: "0 elements",
		},
		{
			Name: "издатель объявлен дважды В ПЕРЕЧНЕ",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy + "," + f1bLegacy
				return c
			}(),
			Discriminator: "one issuer, one record",
		},
		{
			// Стражей повтора ДВА — по одному на каждое объявление, — и они
			// разные: этот пропускался, пока таблица несла только соседний.
			// Обнаружено мутацией: снятие ЭТОГО стража не роняло ничего.
			Name: "издатель объявлен дважды В ПРИВЯЗКЕ ИСТОЧНИКОВ",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS + "," + f1bLegacy + "=" + f1bOursKS
				return c
			}(),
			Discriminator: "one issuer, one key-set record",
		},
		{
			Name: "издатель без объявленной записи источника",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				return c
			}(),
			Discriminator: "has no declared key-set record",
		},
		{
			Name: "запись источника без принимающего её издателя",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS + ",https://third.test=" + f1bLegKS
				return c
			}(),
			Discriminator: "outlives its subject",
		},
		{
			Name: "адрес записи не абсолютен",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=/.well-known/jwks.json"
				return c
			}(),
			Discriminator: "is not absolute",
		},
		{
			Name: "адрес записи из одних разделителей",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=///"
				return c
			}(),
			Discriminator: "consists of separators only",
		},
		{
			Name: "незащищённая схема адреса набора в производственной посадке",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=http://kacho-iam-internal.kacho.svc:9097/x"
				return c
			}(),
			Discriminator: "trust anchor of signature verification",
		},
		{
			Name: "наш издатель вне перечня принимаемых",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS
				c.PlatformTokenIssuer = f1bOurs
				return c
			}(),
			Discriminator: "does not accept",
		},
		{
			Name: "наш издатель принимается без объявленного авторитета отзыва",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bOurs
				c.TokenIssuerKeySets = f1bOurs + "=" + f1bOursKS
				c.PlatformTokenIssuer = f1bOurs
				return c
			}(),
			Discriminator: "is not revocation",
		},
		{
			Name: "два объявления об одном предмете",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS
				c.HydraIssuer = f1bLegacy
				return c
			}(),
			Discriminator: "two declarations of one subject",
		},
		{
			Name: "наш издатель объявлен, а перечня нет вовсе",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.PlatformTokenIssuer = f1bOurs
				return c
			}(),
			Discriminator: "declares no issuer set",
		},
	}

	seen := map[string]string{}
	for _, tc := range cases {
		bindings, err := tc.Cfg.TokenAcceptance()
		if err == nil {
			t.Errorf("страж «%s» не отказал (записей приёма %d) — вход, который он обязан "+
				"отвергать, прошёл до конца", tc.Name, len(bindings))
			continue
		}
		if !strings.Contains(err.Error(), tc.Discriminator) {
			t.Errorf("страж «%s»: отказ пришёл НЕ от него — искали различитель %q, получили: %v\n\n"+
				"Либо страж снят и вместо него отказал сосед, либо его сообщение перестало "+
				"нести собственный различитель. И то и другое делает пробу этого стража "+
				"неспособной упасть: она приняла бы чужой отказ за свой.",
				tc.Name, tc.Discriminator, err)
			continue
		}
		// Различитель обязан быть УНИКАЛЬНЫМ: два стража с одной подстрокой не
		// различаются, и проба одного зеленеет на отказе другого.
		if prev, dup := seen[tc.Discriminator]; dup {
			t.Errorf("различитель %q принадлежит сразу двум стражам («%s» и «%s») — "+
				"они неразличимы, и проба одного зеленеет на отказе другого",
				tc.Discriminator, prev, tc.Name)
		}
		seen[tc.Discriminator] = tc.Name
	}

	t.Logf("перепись: стражей старта в таблице %d, различителей уникальных %d",
		len(cases), len(seen))
	if len(cases) == 0 || len(seen) != len(cases) {
		t.Fatalf("таблица стражей вырождена: случаев %d, различителей %d — «ноль находок» "+
			"на такой таблице означало бы «ноль прочитанного»", len(cases), len(seen))
	}
}
