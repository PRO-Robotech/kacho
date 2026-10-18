// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// anonymous_subject_test.go — страж старта ручки анонимного субъекта плоскости
// данных (`KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID`, приёмка REG-PUB-1 Р9 п. 2,
// kacho#3274).
//
// Значение ручки — `sub` анонимного токена, который чеканит iam; плоскость данных
// переводит его в класс `system_class:anonymous`. Значение формы порождённого
// идентификатора могло бы совпасть с настоящим принципалом, и тогда его доступ
// получил бы всякий без удостоверения. Поэтому такая форма — отказ в старте.
//
// Входы пробы берутся у ПРОИЗВОДИТЕЛЯ идентификаторов (`corelib/ids`), а не
// выписываются литералом: выписанный литерал проверял бы мою память о форме, а
// не форму, которую продукт действительно порождает.

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/ids"
	corequota "github.com/PRO-Robotech/corelib/quota"
)

// lawfulPosture — законная боевая посадка, от которой каждая проба ниже
// отличается ровно одной ручкой: красное означает ручку, а не соседа.
func lawfulPosture() Config {
	var c Config
	c.AuthMode = "production"
	c.QuotaAuthority = corequota.NotDeployed
	c.AuthZTrustedForwarderSANs = []string{"spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"}
	return c
}

// TestAnonymousSubject_GeneratedIdShapeRefusesStart — значение формы
// порождённого идентификатора (обе формы канона: слитная и через дефис)
// отвергается при старте, и отказ называет ручку и значение.
func TestAnonymousSubject_GeneratedIdShapeRefusesStart(t *testing.T) {
	generated := []string{
		ids.NewID("usr"),                           // человек
		ids.NewID("sva"),                           // служебная учётка
		ids.NewID(ids.PrefixRegistry),              // ресурс платформы
		ids.NewHyphenID(ids.PrefixAccessKeyHyphen), // форма через дефис
		ids.NewHyphenID(ids.PrefixMachineTypeHyphen),
	}
	for _, v := range generated {
		c := lawfulPosture()
		c.AnonymousSubjectID = v
		err := c.Validate()
		if err == nil {
			t.Errorf("значение %q формы порождённого идентификатора принято: анонимный токен "+
				"с таким sub совпал бы с настоящим принципалом", v)
			continue
		}
		for _, want := range []string{"KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID", v} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("отказ не называет %q — оператору нечем чинить: %v", want, err)
			}
		}
	}
}

// TestAnonymousSubject_RefusesEvenInBreakglass — аварийный режим снимает вопрос
// о правах, а не о том, чей это идентификатор: форма судится до освобождения.
func TestAnonymousSubject_RefusesEvenInBreakglass(t *testing.T) {
	c := lawfulPosture()
	c.AuthZBreakglass = true
	c.AnonymousSubjectID = ids.NewID("usr")
	if err := c.Validate(); err == nil {
		t.Fatal("аварийный режим пропустил анонимный субъект формы порождённого идентификатора")
	}
}

// TestAnonymousSubject_DeclaredNameAndEmptyAreLawful — законные близнецы той же
// посадки: объявленное имя закрытой формы и пустое значение (поток выключен)
// принимаются. Без них отказ выше зеленел бы и на страже, отвергающем всё.
func TestAnonymousSubject_DeclaredNameAndEmptyAreLawful(t *testing.T) {
	for _, v := range []string{"", "registry-anonymous", "kacho-anon-pull"} {
		c := lawfulPosture()
		c.AnonymousSubjectID = v
		if err := c.Validate(); err != nil {
			t.Errorf("законное значение %q отвергнуто: %v", v, err)
		}
	}
}
