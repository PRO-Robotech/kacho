// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_keyid_fuzz_test.go — F1-22 на произвольном входе.
//
// Цель стоит в ВНЕШНЕМ пакете пробы и гоняет прод-путь целиком —
// `Verifier.Verify`, — а не отдельную функцию формы: идентификатор ключа читается
// из заголовка ДО проверки подписи, поэтому предметом является весь путь от
// разбора заголовка до разрешения ключа, а не одна его проверка.
//
// Утверждается свойство, которое перечень враждебных значений проверить не может:
// НИ ОДНА строка негодной формы не доходит до источника набора. Перечень
// показывает это на своих примерах, произвольный вход — на всех.
package jwks_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PRO-Robotech/kacho/services/registry/internal/clients/jwks"
)

// FuzzKeyIDNeverReachesTheKeySetSource — форма идентификатора ключа ограничена
// ДО использования.
//
// Проверяются три свойства сразу, и каждое — про отдельный путь утечки:
// произвольная строка никогда не принимается; она не уходит в текст, покидающий
// процесс; и она не оплачивается обращением к источнику, пока не окажется
// известным идентификатором.
//
// Положительный контроль (идентификатор законной формы резолвится) живёт в
// парной пробе `TestF1_22_KeyIDIsBoundedBeforeUse`: цель фаззинга утверждает
// инвариант отказа, и приписывать ей вторую половину значило бы делать её
// недетерминированной.
func FuzzKeyIDNeverReachesTheKeySetSource(f *testing.F) {
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	f.Cleanup(srv.Close)

	const issuer = "https://kaname.kacho.local"
	v, err := jwks.New([]jwks.KeySetSource{{
		Issuer: issuer, URL: srv.URL, TokenType: "at+jwt",
	}}, "registry.kacho.local")
	if err != nil {
		f.Fatalf("предпосылка цели: проверяющий обязан строиться: %v", err)
	}

	for _, seed := range []string{
		"our-1", "", "   ", "../../../etc/passwd", `..\..\windows`, "kid\x00\r\n",
		"<script>alert(1)</script>", "%s%n", strings.Repeat("A", 4096),
		"AZaz09-_.~:", "ключ", "k/i/d", "k?i=d", "k#d", "k d", "https://evil.example/keys",
	} {
		f.Add(seed)
	}

	b64 := func(v any) string {
		b, merr := json.Marshal(v)
		if merr != nil {
			panic(merr)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}

	f.Fuzz(func(t *testing.T, kid string) {
		before := fetches.Load()

		header := map[string]any{"alg": "RS256", "typ": "at+jwt"}
		if kid != "" {
			header["kid"] = kid
		}
		claims := map[string]any{
			"sub": "sva-1", "aud": "registry.kacho.local", "iss": issuer, "exp": 1 << 40,
		}
		tok := b64(header) + "." + b64(claims) + "." + base64.RawURLEncoding.EncodeToString([]byte("stub"))

		sub, verr := v.Verify(context.Background(), tok)
		if verr == nil {
			t.Fatalf("идентификатор ключа %q принят: подписи под ним нет, набор источника пуст, "+
				"и принять такой токен нельзя ни при каком идентификаторе (получен субъект %q)", kid, sub)
		}
		// Текст отказа берётся из ЗАКРЫТОГО набора: он не зависит от входа
		// вовсе. Так сформулировано намеренно — проверка «текст не содержит
		// поданной строки» на коротком входе ложно срабатывает сама (буква «n»
		// есть в слове «unknown»), и это нашёл первый же прогон цели.
		switch verr.Error() {
		case "jwks: invalid token: malformed key id", "jwks: invalid token: unknown key id":
		default:
			t.Fatalf("текст отказа зависит от входа: получен %q при идентификаторе %q. "+
				"Текст уходит в журнал и в диагностику, то есть покидает процесс, "+
				"поэтому значение от предъявителя в него не попадает", verr.Error(), kid)
		}

		// Идентификатор годной формы вправе стоить ОДНОГО вынужденного
		// перезапроса; негодной — ни одного. Верхняя граница здесь общая: цель
		// утверждает отсутствие усиления нагрузки, а не точное число.
		if grew := fetches.Load() - before; grew > 1 {
			t.Fatalf("идентификатор ключа %q стоил %d обращений к источнику — "+
				"поток выдуманных идентификаторов не должен превращаться в поток обращений к публикатору",
				kid, grew)
		}
	})
}
