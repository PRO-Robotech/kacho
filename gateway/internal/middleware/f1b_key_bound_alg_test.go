// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_key_bound_alg_test.go — Ф1б-01, недостающая ось: алгоритм заголовка равен
// алгоритму, ЗАКРЕПЛЁННОМУ за найденным ключом.
//
// Состав объявлен и содержит `key-bound-algorithm`, но пробы, подающей токен без
// РОВНО ЭТОГО признака, не было: объявление держалось добросовестностью, а гейт
// по дереву судит объявление, а не его правдивость. Здесь признак предъявлен.
//
// # Почему проверка отдельная, а не следствие проверки подписи
//
// Она срабатывает РАНЬШЕ подписи и потому наблюдаема отдельно: заголовок
// алгоритм не выбирает. Если бы выбирал, предъявитель назначал бы, каким
// способом проверять его подпись, — а именно это и есть путаница алгоритмов.
// Проба утверждает, что отказ пришёл ИМЕННО отсюда (sentinel), а не от
// несошедшейся подписи ниже по потоку.
package middleware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// f1bRewriteHeaderAlg переписывает `alg` в заголовке компактного JWS, не трогая
// подпись. Так строится токен, у которого не хватает РОВНО одного признака:
// заголовок называет один алгоритм, ключ набора закреплён за другим.
func f1bRewriteHeaderAlg(t *testing.T, raw, alg string) string {
	t.Helper()
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("не компактный JWS: частей %d", len(parts))
	}
	hdrBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("разбор заголовка: %v", err)
	}
	var hdr map[string]any
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		t.Fatalf("заголовок не JSON: %v", err)
	}
	hdr["alg"] = alg
	out, err := json.Marshal(hdr)
	if err != nil {
		t.Fatalf("сборка заголовка: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(out) + "." + parts[1] + "." + parts[2]
}

func TestF1b01_HeaderDoesNotChooseTheAlgorithmBoundToTheKey(t *testing.T) {
	v, ours, _ := f1bTwoIssuerVerifier(t)
	now := time.Now()

	legit := ours.mint("ours-1", PlatformTokenType,
		f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))

	// Ключ набора закреплён за ES256; заголовок объявляет другой допустимый
	// алгоритм словаря. Отказ обязан прийти ИМЕННО от сверки с ключом.
	for _, claimed := range []string{"RS256", "EdDSA"} {
		forged := f1bRewriteHeaderAlg(t, legit, claimed)
		_, err := v.Verify(context.Background(), forged)
		if err == nil {
			t.Fatalf("заголовок объявил %q при ключе, закреплённом за ES256, и токен принят — "+
				"тогда предъявитель назначает, каким способом проверять его подпись", claimed)
		}
		if !errors.Is(err, ErrAlgMismatch) {
			t.Fatalf("заголовок объявил %q, а отказ пришёл НЕ от сверки с алгоритмом ключа "+
				"(%v) — значит эту ось держит что-то другое, и объявление состава про неё "+
				"сказано ни о чём", claimed, err)
		}
	}

	// Положительный контроль: тот же токен с НЕТРОНУТЫМ заголовком принимается.
	// Без него отрицания зеленели бы на проверяющем, отвергающем всякий
	// переписанный заголовок по любой причине.
	if _, err := v.Verify(context.Background(), legit); err != nil {
		t.Fatalf("законный токен с совпадающим алгоритмом отвергнут: %v", err)
	}

	// И вторая половина того же признака: алгоритм ВНЕ закрытого словаря
	// отвергается ДО разрешения ключа — обращения к источнику не происходит.
	before := ours.Requests
	if _, err := v.Verify(context.Background(), f1bRewriteHeaderAlg(t, legit, "HS256")); err == nil {
		t.Fatalf("алгоритм вне закрытого словаря принят")
	}
	if ours.Requests != before {
		t.Fatalf("алгоритм вне словаря стоил обращения к источнику набора — отбор происходит "+
			"ПОСЛЕ разрешения ключа, а обязан ДО (обращений было %d, стало %d)",
			before, ours.Requests)
	}
}
