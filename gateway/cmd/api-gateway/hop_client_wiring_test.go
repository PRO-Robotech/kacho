// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// admin_hop_wiring_test.go — КЛИЕНТ ХОПА ОБЯЗАН ДОЕХАТЬ ДО СВОЕГО ПОТРЕБИТЕЛЯ,
// А СТРАЖ СТАРТА — УВИДЕТЬ ВЕЛИЧИНУ, КОТОРУЮ СУДИТ.
//
// Возможность, которую никто не зовёт, — дефект, с которого началась эта
// работа: кеш интроспекции ВСЕГДА принимал HTTPClient, а композиционный корень
// его не заполнял — и хоп, несущий живое удостоверение, шёл на клиенте, которому
// якорь доверия задать было нечем; ни одна проба этого не видела, потому что
// поле существовало и компилировалось.
//
// main() из пробы не исполним (он дозванивается до бэкендов и занимает порты),
// поэтому провязка утверждается ТАМ, ГДЕ ОНА ЖИВЁТ — в исходнике корня. Чтение
// исходника слабее исполнения и применяется намеренно ровно к тому свойству,
// которого «оно собирается» показать не может: что построенный клиент ДОЕЗЖАЕТ
// до потребителя.
//
// ХОПОВ БЫЛО ТРИ, ОСТАЛОСЬ ДВА. Третий — к административному API чужого
// поставщика — снят вместе с ним: он нёс снятие сессии входа на его стороне и
// интроспекцию его токенов, и оба потребителя исчезли. Случаи о нём сняты
// ВМЕСТЕ с предметом, а не переписаны на соседний хоп «чтобы сохранились»:
// каждый оставшийся случай называет СВОЕГО потребителя, и их ровно столько,
// сколько потребителей в дереве.
//
// Поведение самих клиентов (проверяет ту связку, отвергает чужую, отказывает в
// старте на негодной) исполняется по-настоящему в `jwks_hop_client_test.go` и
// `platform_revocation_client_cert_test.go`.
//
// Помощник `compositionRoot` объявлен здесь и читается соседями
// (`own_lane_revocation_authority_test.go`): координата корня — одна, и вторая
// её копия разъехалась бы с первой молча.

// compositionRoot — исходник композиционного корня. Координата объявлена ОДИН
// раз: соседние пробы читают тот же корень, и вторая копия пути разъехалась бы
// с первой молча.
func compositionRoot(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("main.go")
	require.NoError(t, err, "composition root must be readable")
	return string(b)
}

// ─── Хоп за ключами верификации ─────────────────────────────────────────────

func TestCompositionRoot_BuildsTheKeySetHopClient(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`newJWKSHopClient\(\s*\n?\s*cfg\.TokenKeySetCAFile`),
		src,
		"композиционный корень обязан строить клиент хопа за ключами из настроенного "+
			"якоря доверия; без этого "+keySetHopCAEnv+" — ручка, не меняющая ничего")
}

func TestCompositionRoot_FeedsTheKeySetClientToTheVerifier(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`JWTVerifierConfig\{(?s:.*?)HTTPClient:\s*jwksHopClient`),
		src,
		"проверяющему обязан быть передан клиент хопа за ключами. По этому хопу едет "+
			"материал, которым край проверяет ПОДПИСЬ каждого предъявителя; оставленный "+
			"пустым, он молча откатывается на клиент с системными корнями, которому "+
			"внутренний центр задать нечем")
}

func TestCompositionRoot_RefusesToStartOnAnUnusableKeySetAnchor(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`if jwksCAErr != nil \{\s*\n(?s:.*?)os\.Exit\(1\)`),
		src,
		"негодный якорь доверия обязан останавливать процесс в композиционном корне. "+
			"Продолжить значило бы оставить оператора в уверенности, что хоп проверяется "+
			"по внутреннему центру, тогда как он не проверяется")
}

// ─── Хоп к НАШЕМУ авторитету отзыва ─────────────────────────────────────────

func TestCompositionRoot_BuildsThePlatformRevocationHopClient(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`newPlatformRevocationHopClient\(\s*\n?\s*cfg\.PlatformTokenRevocationCAFile`),
		src,
		"композиционный корень обязан строить клиент хопа к нашему авторитету отзыва из "+
			"настроенного якоря доверия: по этому хопу едет ПРЕДЪЯВЛЕННЫЙ токен")
}

func TestCompositionRoot_FeedsThePlatformClientToTheIntrospectionHop(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`IntrospectionCacheConfig\{(?s:.*?)HTTPClient:\s*platformHopClient`),
		src,
		"кешу интроспекции обязан быть передан клиент хопа к нашему авторитету. Это тот "+
			"хоп, который несёт живое удостоверение вызывающего на каждом промахе кеша; "+
			"оставленный пустым, он молча идёт на клиенте с системными корнями")
}

func TestCompositionRoot_RefusesToStartOnAnUnusablePlatformAnchor(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`if phErr != nil \{\s*\n\s*log\.Fatalf`),
		src,
		"негодный якорь или половинная клиентская пара обязаны останавливать процесс в корне")
}

// ЗДЕСЬ СТОЯЛ СЛУЧАЙ «страж старта обязан увидеть cfg.HydraAdminCAFile».
//
// Он стоил выкатки и потому объяснён здесь дословно: страж решал «хоп по https,
// значит якорь ОБЯЗАН быть закреплён», читая поле, которое корень не заполнял, —
// и в производственном окружении заключал «якоря нет» ПРИ ЛЮБОЙ настройке.
// Чарт задавал ручку, секрет был смонтирован, файл лежал, envconfig его
// разобрал — а процесс отказывался стартовать, называя ровно ту ручку, которая
// была задана.
//
// Класс от снятия поставщика никуда не делся и держится НА ЖИВОЙ ОСИ:
// `TestCompositionRoot_ShowsOurRevocationAuthorityToTheGuard` требует, чтобы
// корень подал стражу все четыре величины нашего авторитета, а
// `TestCompositionRoot_ShowsNoForeignProviderAxisToTheGuard` — чтобы он не
// подавал снятых (оба — `own_lane_revocation_authority_test.go`).
