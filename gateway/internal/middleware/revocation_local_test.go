// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// revocation_local_test.go — ОТЗЫВ, ЗАПИСАННЫЙ У НАС, ОБЯЗАН ДЕЙСТВОВАТЬ НА
// ПУТИ ЗАПРОСА.
//
// ПРЕДМЕТ (#797). Выход человека записывает отзыв В НАШУ базу: по
// идентификатору удостоверения и отсечкой по времени. Контроль, действующий на
// ВЫДАЧЕ и не действующий на ПРЕДЪЯВЛЕНИИ, отзывом не является — предъявленное
// проходит до истечения срока, и это состояние не сходится само.
//
// ИСТОЧНИК ОДИН. Их было два, и спрашивались оба: сперва наша запись, потом
// чужой поставщик по административному пути его интроспекции. Второй снят
// вместе с самим поставщиком — его токенов край не принимает, и вопрос ему был
// бы утверждением о предмете, которого у него нет. Композиция выродилась в свою
// первую половину, и половина эта — авторитет.
//
// ПОЧЕМУ FAIL-CLOSED. Недоступность источника отзыва не есть «не отозван».
// Ответ «не знаю» на вопрос о безопасности означает отказ — тот же контракт, что
// у проверки доступа, которую край уже делает рядом. Мягкого прохода для ЭТОГО
// источника не было и нет: авторитет наш, и проход означал бы «отзываем и свой
// же отзыв не исполняем».

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

func TestLocalRevocationIsAskedAndStopsTheRequest(t *testing.T) {
	local := &fakeLocalReader{revoked: true}

	c := NewOwnRevocationSource(local)
	_, err := c.Introspect(context.Background(), "jti-1", "raw")

	if !errors.Is(err, ErrTokenInactive) {
		t.Fatalf("удостоверение, отозванное В НАШЕЙ записи, не остановило запрос: err=%v. "+
			"Пока это так, выход пользователя не прекращает доступ — он лишь "+
			"записывает намерение", err)
	}
	if local.asked != 1 {
		t.Fatalf("наш источник спрошен %d раз, ждали 1", local.asked)
	}
}

func TestLiveTokenPassesAndTheAnswerIsWhole(t *testing.T) {
	// ЗАКОННЫЙ БЛИЗНЕЦ: без него проба выше зеленела бы на проверке, которая
	// отвергает всё подряд.
	//
	// Отличие от предмета РОВНО ОДНО — что ответил источник.
	local := &fakeLocalReader{revoked: false}

	c := NewOwnRevocationSource(local)
	res, err := c.Introspect(context.Background(), "jti-2", "raw")
	if err != nil {
		t.Fatalf("не отозванное удостоверение отвергнуто: %v", err)
	}
	if local.asked != 1 {
		t.Fatalf("наш источник спрошен %d раз, ждали 1", local.asked)
	}
	// «Не отозвано» здесь ПОЛНЫЙ ответ, а не половина прежнего: спрашивать
	// больше некого, и вызывающий обязан получить годность, а не пустоту,
	// которую следующий слой прочитает как «неизвестно».
	if !res.Active {
		t.Fatal("источник ответил «не отозвано», а исход не назван годным — " +
			"вызывающий не отличит его от неотвеченного вопроса")
	}
}

func TestUnavailableSourceIsNotAnAnswer(t *testing.T) {
	// FAIL-CLOSED: «не знаю» — не «не отозван».
	boom := errors.New("сосед не ответил")
	local := &fakeLocalReader{err: boom}

	c := NewOwnRevocationSource(local)
	_, err := c.Introspect(context.Background(), "jti-3", "raw")

	if err == nil {
		t.Fatal("недоступность нашего источника отзыва прошла как «удостоверение живо» — " +
			"это открывает контроль ровно тогда, когда он не работает")
	}
	if errors.Is(err, ErrTokenInactive) {
		t.Fatalf("недоступность подана как отзыв: %v. Это разные исходы, и "+
			"вызывающий обязан их различать", err)
	}
	// Признак молчания ТИПИЗИРОВАН, а не выведен из текста: текст пишет тот, кто
	// ошибку породил, и он меняется от версии соседа. Читает признак слой
	// решения (auth_revocation.go), и без него молчание нашего источника попало
	// бы в мягкий проход, объявленный когда-то для ЧУЖОГО.
	if !errors.Is(err, ErrOwnRevocationSourceSilent) {
		t.Fatalf("молчание не несёт своего признака: %v", err)
	}
	// Причина сохранена цепочкой: журнал обязан назвать диагноз, а не только
	// класс исхода.
	if !errors.Is(err, boom) {
		t.Fatalf("причина молчания потеряна: %v", err)
	}
}

func TestReaderWithoutASourceRefuses(t *testing.T) {
	// Конструктор участника ТРЕБУЕТ, но не проверяет. Читатель без источника
	// отвечал бы на вопрос о безопасности, ничего не спросив, и отличить это от
	// исправной работы было бы нечем.
	//
	// Признак — ErrIntrospectionMisconfigured, а не «не ответил»: неполная
	// сборка не лечится повтором.
	c := NewOwnRevocationSource(nil)
	_, err := c.Introspect(context.Background(), "jti-4", "raw")
	if !errors.Is(err, ErrIntrospectionMisconfigured) {
		t.Fatalf("читатель без источника обязан отвечать признаком настройки, получено: %v", err)
	}
}
