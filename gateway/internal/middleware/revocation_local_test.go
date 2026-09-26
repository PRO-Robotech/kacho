// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// revocation_local_test.go — ОТЗЫВ, ЗАПИСАННЫЙ У НАС, ОБЯЗАН ДЕЙСТВОВАТЬ НА
// ПУТИ ЗАПРОСА.
//
// ПРЕДМЕТ (#797). Выход пользователя записывает отзыв В НАШУ базу: по
// идентификатору удостоверения и отсечкой по времени. Прежде эту запись
// спрашивали ПЕРЕД прежним поставщиком; поставщик снят (#2734), и запись —
// единственный источник полосы записи отзыва.
//
// ПОЧЕМУ FAIL-CLOSED. Недоступность источника отзыва не есть «не отозван».
// Ответ «не знаю» на вопрос о безопасности означает отказ — тот же контракт, что
// у проверки доступа, которую край уже делает рядом.

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeLocalReader — наша запись отзыва.
type fakeLocalReader struct {
	revoked  bool
	err      error
	asked    int
	deadline time.Duration
}

func (f *fakeLocalReader) IsSessionRevoked(ctx context.Context, _ string) (bool, error) {
	f.asked++
	if dl, ok := ctx.Deadline(); ok {
		f.deadline = time.Until(dl)
	}
	if f.err != nil {
		return false, f.err
	}
	return f.revoked, nil
}

func TestOwnRevocationSource_RevokedInOurRecordStopsTheRequest(t *testing.T) {
	local := &fakeLocalReader{revoked: true}
	_, err := NewOwnRevocationSource(local).Introspect(context.Background(), "jti-1", "raw")
	if !errors.Is(err, ErrTokenInactive) {
		t.Fatalf("удостоверение, отозванное В НАШЕЙ записи, не остановило запрос: err=%v. "+
			"Пока это так, выход пользователя не прекращает доступ — он лишь "+
			"записывает намерение", err)
	}
	if local.asked != 1 {
		t.Fatalf("наша запись спрошена %d раз, ждали 1", local.asked)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ: без него проба выше зеленела бы на проверке, которая
// отвергает всё подряд.
func TestOwnRevocationSource_LiveTokenIsAPositiveAnswer(t *testing.T) {
	res, err := NewOwnRevocationSource(&fakeLocalReader{}).Introspect(context.Background(), "jti-2", "raw")
	if err != nil || !res.Active {
		t.Fatalf("не отозванное удостоверение не признано живым: res=%+v err=%v", res, err)
	}
}

// FAIL-CLOSED: «не знаю» — не «не отозван», и не «отозван» тоже: признак
// молчания ТИПИЗИРОВАН, чтобы слой решения различал его без чтения текста.
func TestOwnRevocationSource_SilenceIsTypedAndIsNotAnAnswer(t *testing.T) {
	boom := errors.New("сосед не ответил")
	_, err := NewOwnRevocationSource(&fakeLocalReader{err: boom}).Introspect(context.Background(), "jti-3", "raw")
	if err == nil {
		t.Fatal("недоступность нашей записи прошла как «удостоверение живо» — это открывает " +
			"контроль ровно тогда, когда он не работает")
	}
	if errors.Is(err, ErrTokenInactive) {
		t.Fatalf("недоступность подана как отзыв: %v — разные исходы лечатся противоположно", err)
	}
	if !errors.Is(err, ErrOwnRevocationSourceSilent) || !errors.Is(err, boom) {
		t.Fatalf("молчание не несёт своего признака и причины: %v", err)
	}
}

// Сборка без источника — настройка, а не заминка: признак неверной настройки.
func TestOwnRevocationSource_WithoutASourceIsMisconfigured(t *testing.T) {
	_, err := NewOwnRevocationSource(nil).Introspect(context.Background(), "jti-4", "raw")
	if !errors.Is(err, ErrIntrospectionMisconfigured) {
		t.Fatalf("читатель без источника обязан отвечать признаком неверной настройки, получено %v", err)
	}
}

// Свой бюджет на вызове соседа: сырой контекст запроса пределом не является.
func TestOwnRevocationSource_AsksWithinItsOwnBudget(t *testing.T) {
	local := &fakeLocalReader{}
	if _, err := NewOwnRevocationSource(local).Introspect(context.Background(), "jti-5", "raw"); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if local.deadline <= 0 || local.deadline > OwnRevocationCallBudget {
		t.Fatalf("вопрос задан без своего предела: осталось %v при бюджете %v",
			local.deadline, OwnRevocationCallBudget)
	}
}
