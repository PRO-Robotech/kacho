// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// revocation_local_test.go — ОТЗЫВ, ЗАПИСАННЫЙ У НАС, ОБЯЗАН ДЕЙСТВОВАТЬ НА
// ПУТИ ЗАПРОСА.
//
// ПРЕДМЕТ (#797). Край спрашивает об отзыве ПРОВАЙДЕРА — и делает это на каждом
// запросе, это верно и остаётся. Но выход пользователя записывает отзыв В НАШУ
// базу: по идентификатору удостоверения и отсечкой по времени. Провайдер об
// этой записи не знает и знать не может, поэтому предъявленное удостоверение
// проходит его проверку и после нашего отзыва.
//
// Наблюдение на живом стенде (2026-08-21): сессий входа у провайдера — ноль,
// выданных удостоверений — полсотни. Механизм, которым мы «закрываем вход», не
// имеет предмета: удостоверения выданы машинным потоком и к сессии не привязаны.
//
// РЕШЕНИЕ. Источников отзыва два, и спрашивать надо оба. Наш — первым: он
// дешевле (свой сосед против внешнего провайдера) и отвечает на вопрос, которого
// провайдер не понимает.
//
// ПОЧЕМУ FAIL-CLOSED. Недоступность источника отзыва не есть «не отозван».
// Ответ «не знаю» на вопрос о безопасности означает отказ — тот же контракт, что
// у проверки доступа, которую край уже делает рядом.

import (
	"context"
	"errors"
	"testing"
)

// fakeLocalReader — наш источник отзыва.
type fakeLocalReader struct {
	revoked bool
	err     error
	asked   int
}

func (f *fakeLocalReader) IsSessionRevoked(_ context.Context, _ string) (bool, error) {
	f.asked++
	if f.err != nil {
		return false, f.err
	}
	return f.revoked, nil
}

// fakeProvider — источник провайдера.
type fakeProvider struct {
	err   error
	asked int
}

func (f *fakeProvider) Introspect(_ context.Context, _, _ string) (IntrospectionResult, error) {
	f.asked++
	return IntrospectionResult{}, f.err
}

func TestLocalRevocationIsAskedAndStopsTheRequest(t *testing.T) {
	local := &fakeLocalReader{revoked: true}
	provider := &fakeProvider{}

	c := NewLocalThenProviderRevocation(local, provider)
	_, err := c.Introspect(context.Background(), "jti-1", "raw")

	if !errors.Is(err, ErrTokenInactive) {
		t.Fatalf("удостоверение, отозванное В НАШЕЙ записи, не остановило запрос: err=%v. "+
			"Пока это так, выход пользователя не прекращает доступ — он лишь "+
			"записывает намерение", err)
	}
	if local.asked != 1 {
		t.Fatalf("наш источник спрошен %d раз, ждали 1", local.asked)
	}
	if provider.asked != 0 {
		t.Fatalf("провайдер спрошен %d раз при уже известном отзыве — лишний "+
			"обход к соседу на каждом запросе отозванного", provider.asked)
	}
}

func TestLiveTokenStillGoesToTheProvider(t *testing.T) {
	// ЗАКОННЫЙ БЛИЗНЕЦ: без него проба выше зеленела бы на проверке, которая
	// отвергает всё подряд.
	local := &fakeLocalReader{revoked: false}
	provider := &fakeProvider{}

	c := NewLocalThenProviderRevocation(local, provider)
	if _, err := c.Introspect(context.Background(), "jti-2", "raw"); err != nil {
		t.Fatalf("не отозванное удостоверение отвергнуто: %v", err)
	}
	if provider.asked != 1 {
		t.Fatalf("провайдер спрошен %d раз, ждали 1: наш источник не заменяет "+
			"его, а дополняет — провайдер знает об отзывах, которых не знаем мы",
			provider.asked)
	}
}

func TestUnavailableLocalSourceIsNotAnAnswer(t *testing.T) {
	// FAIL-CLOSED: «не знаю» — не «не отозван».
	boom := errors.New("сосед не ответил")
	local := &fakeLocalReader{err: boom}
	provider := &fakeProvider{}

	c := NewLocalThenProviderRevocation(local, provider)
	_, err := c.Introspect(context.Background(), "jti-3", "raw")

	if err == nil {
		t.Fatal("недоступность нашего источника отзыва прошла как «удостоверение живо» — " +
			"это открывает контроль ровно тогда, когда он не работает")
	}
	if errors.Is(err, ErrTokenInactive) {
		t.Fatalf("недоступность подана как отзыв: %v. Это разные исходы, и "+
			"вызывающий обязан их различать", err)
	}
	if provider.asked != 0 {
		t.Fatalf("провайдер спрошен при неотвечающем своём источнике (%d) — "+
			"вердикт был бы вынесен по половине картины", provider.asked)
	}
}

func TestProviderVerdictSurvivesUntouched(t *testing.T) {
	// Ответ провайдера проходит НАСКВОЗЬ: композиция добавляет источник, а не
	// переписывает чужие исходы.
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"отозван у провайдера", ErrTokenInactive},
		{"провайдер настроен неверно", ErrIntrospectionMisconfigured},
		{"провайдер не ответил", errors.New("таймаут")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewLocalThenProviderRevocation(
				&fakeLocalReader{revoked: false}, &fakeProvider{err: tc.err})
			if _, err := c.Introspect(context.Background(), "jti", "raw"); !errors.Is(err, tc.err) {
				t.Fatalf("исход провайдера подменён: получено %v, ждали %v", err, tc.err)
			}
		})
	}
}
