// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
)

// safe — посадка, которую страж обязан пропускать. От неё пробы отклоняются по
// ОДНОЙ оси: иначе «отвергнуто» неотличимо от «отвергнуто не за то».
func safe() config.Config {
	return config.Config{
		AuthMode:                  "production",
		DBSSLMode:                 "require",
		AuthZTrustedForwarderSANs: []string{"spiffe://kacho/ns/kacho/sa/api-gateway"},
	}
}

func TestValidateAcceptsSafePosture(t *testing.T) {
	if err := safe().Validate(); err != nil {
		t.Fatalf("безопасная посадка отвергнута — тогда красное у стража означает не то, "+
			"что он объявляет: %v", err)
	}
}

// Прямой пример из ban #16: «sslmode=disable → refuse-to-start».
func TestValidateRefusesInsecureSSLModeInProduction(t *testing.T) {
	for _, mode := range []string{"disable", "", "allow", "prefer"} {
		t.Run("sslmode="+mode, func(t *testing.T) {
			c := safe()
			c.DBSSLMode = mode
			err := c.Validate()
			if err == nil {
				t.Fatal("боевой режим принял незашифрованное соединение с БД — " +
					"страж не исполняет ban #16")
			}
			if !strings.Contains(err.Error(), "KACHO_GEO_DB_SSLMODE") {
				t.Fatalf("отказ не называет ручку, которую надо править, — оператор не "+
					"поднимет стенд по такому тексту: %v", err)
			}
		})
	}
}

// Вне боевого режима та же посадка законна: страж судит РЕЖИМ, а не значение
// само по себе. Без этой пробы он мог бы отвергать всё подряд и выглядеть строгим.
func TestValidateAllowsInsecureSSLModeOutsideProduction(t *testing.T) {
	c := safe()
	c.AuthMode = "dev"
	c.DBSSLMode = "disable"
	c.AuthZTrustAnyForwarder = true // вне боевого — явный опт-ин
	if err := c.Validate(); err != nil {
		t.Fatalf("dev-режим отвергнут за посадку, законную вне боевого: %v", err)
	}
}

// Пустой круг отправителей означает «не сужаем», то есть принимать переданную
// личность будет любой проверенный пир. `security.md` §«Кто вправе ГОВОРИТЬ ЗА
// пользователя»: пустой список — это НЕ «запрещаем».
func TestValidateRefusesUnnarrowedForwarderCircle(t *testing.T) {
	c := safe()
	c.AuthZTrustedForwarderSANs = nil
	if err := c.Validate(); err == nil {
		t.Fatal("несуженный круг отправителей принят: переданную личность примет любой " +
			"проверенный пир, а контроль будет выглядеть включённым")
	}
}

// Вырожденный вход — канонический для этого класса: длина строки настроек не
// ноль, а записей ноль. Страж обязан судить ЗАПИСИ, которые примет транспорт.
func TestValidateRefusesDegenerateForwarderList(t *testing.T) {
	c := safe()
	c.AuthZTrustedForwarderSANs = []string{"", " "}
	if err := c.Validate(); err == nil {
		t.Fatal("круг из пустых записей принят за сужение — страж считает элементы " +
			"сырой строки вместо записей, которые примет транспорт")
	}
}

func TestValidateRefusesUnknownAuthMode(t *testing.T) {
	c := safe()
	c.AuthMode = "поехали"
	err := c.Validate()
	if err == nil {
		t.Fatal("неизвестный режим принят")
	}
	if !strings.Contains(err.Error(), "KACHO_GEO_AUTH_MODE") {
		t.Fatalf("отказ не называет ручку: %v", err)
	}
}
