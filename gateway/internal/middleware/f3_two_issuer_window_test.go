// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

// TestF3_TwoIssuerWindow — переход без разрыва: край принимает подписи ОБОИХ
// издателей, пока живы уже выданные токены (#899, Ф3 отказа от внешнего сервера).
//
// # Что здесь утверждается — и почему всеми тремя ветками сразу
//
// Приём двух подписей — не «мягче», а ШИРЕ ровно на одну объявленную полосу.
// Проба, показавшая только приём обоих, не отличила бы это от «принимаем всё»:
// подделка обязана отвергаться той же настройкой, что принимает законное.
//
// Поэтому три ветки идут вместе:
//
//	наш издатель      → принимается;
//	прежний издатель  → принимается;
//	подпись чужим ключом от имени нашего издателя → ОТВЕРГАЕТСЯ.
//
// Третья — не «негатив ради полноты»: именно она отличает окно перехода от
// снятой проверки. Без неё две первые зеленели бы на проверяющем, который не
// проверяет подпись вовсе.
func TestF3_TwoIssuerWindow(t *testing.T) {
	ours, legacy := newF1bKeySet(t), newF1bKeySet(t)
	ours.addEC("ours-1")
	legacy.addRSA("legacy-1")

	// Третий набор НИКОМУ не объявлен: им подписывается подделка.
	rogue := newF1bKeySet(t)
	rogue.addEC("ours-1") // тот же идентификатор ключа — и это намеренно

	v, err := NewJWTVerifier(JWTVerifierConfig{
		Issuers: []IssuerKeySet{
			{Issuer: f1bPlatformIssuer, KeySetURL: ours.URL(),
				TokenTypes: []string{PlatformTokenType}},
			{Issuer: f1bLegacyIssuer, KeySetURL: legacy.URL(),
				TokenTypes: []string{LegacyTokenType}, TolerateAbsentTokenType: true},
		},
		ExpectedAudience: f1bAudience,
	})
	if err != nil {
		t.Fatalf("построение проверяющего: %v", err)
	}

	now := time.Now()
	ctx := context.Background()

	t.Run("наш издатель принимается", func(t *testing.T) {
		tok := ours.mint("ours-1", PlatformTokenType,
			f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))
		if _, verr := v.Verify(ctx, tok); verr != nil {
			t.Fatalf("токен нашей чеканки отвергнут: %v", verr)
		}
	})

	t.Run("прежний издатель принимается", func(t *testing.T) {
		tok := legacy.mint("legacy-1", LegacyTokenType,
			f1bClaims(f1bLegacyIssuer, f1bAudience, "usr-1", now, time.Minute))
		if _, verr := v.Verify(ctx, tok); verr != nil {
			t.Fatalf("токен прежнего издателя отвергнут — переход стал бы разрывом: %v", verr)
		}
	})

	t.Run("подделка от имени нашего издателя отвергается", func(t *testing.T) {
		// Идентификатор ключа СОВПАДАЕТ с законным, издатель тоже — расходится
		// только материал подписи. Так выглядит попытка воспользоваться тем, что
		// полос стало две: если проверяющий берёт набор ключей не по издателю, а
		// «любой из объявленных», подделка пройдёт.
		tok := rogue.mint("ours-1", PlatformTokenType,
			f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))
		if _, verr := v.Verify(ctx, tok); verr == nil {
			t.Fatal("подпись чужим ключом принята — окно перехода превратилось в " +
				"отсутствие проверки")
		}
	})

	t.Run("издатель вне перечня отвергается", func(t *testing.T) {
		// Окно шире ровно на ОБЪЯВЛЕННЫЕ полосы. Третий издатель, которого никто
		// не объявлял, не становится законным оттого, что полос две.
		tok := ours.mint("ours-1", PlatformTokenType,
			f1bClaims("https://nobody.example.invalid", f1bAudience, "usr-1", now, time.Minute))
		if _, verr := v.Verify(ctx, tok); verr == nil {
			t.Fatal("издатель без объявленной записи принят")
		}
	})
}

// TestF3_WindowLengthIsANumber — длительность окна названа ЧИСЛОМ, а не «на
// всякий случай» (#899, п.1 предиката готовности).
//
// # Почему это проба, а не запись в документе
//
// Окно перехода обязано покрывать жизнь уже выданных токенов: пока хоть один
// живой токен прежней подписи существует, снятие её приёма — разрыв для того,
// кто его держит. Значит нижняя граница окна выводится из ОБЪЯВЛЕННЫХ величин,
// а не назначается.
//
// Здесь та же арифметика, что у срока удержания снятого ключа: максимальный
// срок жизни токена плюс потолок кэша набора ключей у потребителя. Обе величины
// уже объявлены политикой — окно не заводит третьей.
func TestF3_WindowLengthIsANumber(t *testing.T) {
	window := tokenpolicy.KeyRemovalGrace

	if window < tokenpolicy.MaxTokenTTL+tokenpolicy.ConsumerKeySetCacheCeiling {
		t.Fatalf("окно перехода (%v) короче жизни выданного токена плюс кэша ключей "+
			"(%v + %v): снятие приёма прежней подписи оборвало бы живой токен",
			window, tokenpolicy.MaxTokenTTL, tokenpolicy.ConsumerKeySetCacheCeiling)
	}

	// Обратная сторона: окно не бесконечно. Приём двух подписей — состояние
	// перехода, а не устройство продукта; названная величина и есть то, что
	// делает его конечным.
	if window > 24*time.Hour {
		t.Fatalf("окно перехода (%v) больше суток — это уже не переход, а вторая "+
			"постоянная полоса приёма", window)
	}

	t.Logf("окно перехода: %v (срок жизни токена %v + потолок кэша ключей %v + запас %v)",
		window, tokenpolicy.MaxTokenTTL, tokenpolicy.ConsumerKeySetCacheCeiling,
		tokenpolicy.RemovalSlack)
}
