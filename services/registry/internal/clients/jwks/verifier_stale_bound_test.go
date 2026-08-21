// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verifier_stale_bound_test.go — обслуживание по протухшему кэшу обязано быть
// ОГРАНИЧЕНО во времени. Троттл повторной загрузки существует против наплыва
// неизвестных идентификаторов ключа и допускает короткую отсрочку на транзиентный
// сбой; но окно троттла возобновляется на каждой попытке, поэтому «отдаём из кэша,
// пока окно активно» без абсолютной границы означает: при постоянно недоступном
// источнике отозванный или ротированный ключ принимается ВЕЧНО.
package jwks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Источник ключей недоступен НАВСЕГДА. Верификация обязана перестать принимать
// подпись протухшим ключом — и больше её не принимать, сколько бы окон троттла ни
// прошло. До фикса отказ давала только та единственная проба, что попадала на
// истёкшее окно, а все остальные обслуживались из протухшего кэша.
func TestJWKS_Verify_PermanentlyDownSource_StopsAcceptingStaleKey(t *testing.T) {
	js := newJWKSServer(t, "kid-rsa") // Cache-Control: max-age=300
	v := newTestVerifier(t, js.srv.URL, testAud, testHydraIss)

	clock := time.Now()
	v.now = func() time.Time { return clock }

	// Токен подписан ключом, который сейчас в JWKS; срок действия — сутки, поэтому
	// единственное, что может его отвергнуть, — недоверие к ключу.
	tok := js.mintRS256(t, "kid-rsa", hydraClaims("cid-ci", clock.Add(24*time.Hour)))
	sub, err := v.Verify(context.Background(), tok)
	require.NoError(t, err, "пока источник жив, токен валиден")
	require.Equal(t, "cid-ci", sub)

	// Источник ложится навсегда (ключ отозван/ротирован — подтвердить его больше
	// нечем). Прогоняем время далеко за TTL и идём окнами троттла.
	js.srv.Close()
	clock = clock.Add(10 * time.Minute) // TTL (5 мин) заведомо истёк

	const windows = 12 // ~2 минуты по 10с — окно троттла возобновляется каждый раз
	accepted := 0
	for i := 0; i < windows; i++ {
		for _, step := range []time.Duration{0, 3 * time.Second, 6 * time.Second} {
			at := clock.Add(step)
			v.now = func() time.Time { return at }
			if _, verr := v.Verify(context.Background(), tok); verr == nil {
				accepted++
			}
		}
		clock = clock.Add(11 * time.Second) // следующее окно троттла
	}
	require.Zerof(t, accepted,
		"подпись протухшим ключом принята %d раз(а) за %d окон троттла при недоступном "+
			"источнике: окно возобновляется бесконечно, значит отозванный ключ валиден без границы",
		accepted, windows)
}

// Транзиентный сбой всё ещё прощается: одна неудачная попытка обновления не должна
// на ровном месте отвергать валидные токены — короткая отсрочка сразу за TTL
// остаётся. Граница ограничивает отсрочку, а не отменяет её.
func TestJWKS_Verify_TransientBlip_StillServesWithinGrace(t *testing.T) {
	js := newJWKSServer(t, "kid-rsa")
	v := newTestVerifier(t, js.srv.URL, testAud, testHydraIss)

	clock := time.Now()
	v.now = func() time.Time { return clock }

	tok := js.mintRS256(t, "kid-rsa", hydraClaims("cid-ci", clock.Add(24*time.Hour)))
	_, err := v.Verify(context.Background(), tok)
	require.NoError(t, err)

	// TTL истёк на секунду, источник моргнул: попытка обновления падает и занимает
	// слот троттла.
	clock = clock.Add(5*time.Minute + time.Second)
	js.srv.Close()
	_, err = v.Verify(context.Background(), tok)
	require.Error(t, err, "проба, инициировавшая обновление, отвечает отказом")

	// Следующая проба в пределах отсрочки обслуживается из кэша — иначе один сетевой
	// сбой превращается в полный отказ авторизации для всех валидных токенов.
	clock = clock.Add(2 * time.Second)
	sub, err := v.Verify(context.Background(), tok)
	require.NoError(t, err, "в пределах отсрочки известный ключ обслуживается из кэша")
	require.Equal(t, "cid-ci", sub)
}
