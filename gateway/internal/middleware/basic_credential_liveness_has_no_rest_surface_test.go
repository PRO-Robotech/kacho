// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// basic_credential_liveness_has_no_rest_surface_test.go — У ВОПРОСА О ЖИВОСТИ
// УДОСТОВЕРЕНИЯ НЕТ ДРУЖЕЛЮБНОГО ПУТИ (задача #1450, ban #6).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО СТЕРЕЖЁТСЯ, А НЕ ОСТАВЛЯЕТСЯ НА ВНИМАНИЕ
//
// Вопрос отвечается по одному ИДЕНТИФИКАТОРУ, без предъявления секрета. На
// внешней поверхности такой глагол был бы оракулом существования: перебирающий
// получал бы по каждому вводу машинный ответ о том, живо ли чужое
// удостоверение, ничего при этом не предъявляя. Соседний глагол
// (`ResolveBasicCredential`) от этого защищён самой формой вопроса — у него
// нельзя спросить, не предъявив; здесь такой защиты нет by construction.
//
// Поэтому у него НЕ объявлено `google.api.http`, и маршрута края он не
// получает. Проба стережёт именно это решение: аннотация, добавленная позже «за
// компанию с соседом», немедленно заводит путь — и заводит его молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПАРА, А НЕ ОДНО УТВЕРЖДЕНИЕ
//
// «Маршрута нет» верно и о таблице, которая пуста, и о предикате, не находящем
// ничего. Поэтому рядом стоит положительный контроль: сосед по ТОМУ ЖЕ сервису,
// у которого аннотация есть, в таблице ОБЯЗАН найтись. И печатается объём
// осмотренного: «ноль находок» обязано быть отличимо от «ноль прочитанного».

package middleware

import (
	"strings"
	"testing"
)

func TestBCL1450_LivenessVerbHasNoRestRoute(t *testing.T) {
	const liveness = "kaname.cloud.iam.v1.InternalIAMService/CheckBasicCredentialLive"
	const neighbour = "kaname.cloud.iam.v1.InternalIAMService/ResolveBasicCredential"

	var (
		scanned          int
		sameService      int
		livenessRoutes   []string
		neighbourPresent bool
	)
	for _, r := range generatedRestRoutes {
		scanned++
		if strings.HasPrefix(r.FQN, "kaname.cloud.iam.v1.InternalIAMService/") {
			sameService++
		}
		switch r.FQN {
		case liveness:
			livenessRoutes = append(livenessRoutes, r.Method+" "+r.Template)
		case neighbour:
			neighbourPresent = true
		}
	}

	t.Logf("осмотрено: маршрутов в таблице %d, из них внутреннего сервиса iam %d", scanned, sameService)
	if scanned == 0 {
		t.Fatal("таблица маршрутов пуста — «маршрута нет» здесь означало бы «ничего не прочитано»")
	}
	// Положительный контроль: предикат СПОСОБЕН найти глагол этого сервиса.
	if !neighbourPresent {
		t.Fatalf("сосед %q в таблице не найден — предикат не находит ничего, "+
			"и утверждение ниже вакуумно", neighbour)
	}
	if len(livenessRoutes) != 0 {
		t.Errorf("у вопроса о живости завёлся путь края %v: он отвечается по одному "+
			"идентификатору, без предъявления секрета, и на маршрутизируемой поверхности "+
			"становится оракулом существования удостоверений", livenessRoutes)
	}
}
