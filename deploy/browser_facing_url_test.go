// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// browser_facing_url_test.go — адрес, который исполняет БРАУЗЕР, задаётся
// относительным путём, а не абсолютным URL.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Часть настроек уходит не в межсервисный вызов, а в редирект, который выполняет
// браузер пользователя. Абсолютный адрес в такой настройке фиксирует хост, порт
// И СХЕМУ — то есть заявляет, по какому адресу открыта консоль. Заявление это
// верно ровно для одного способа доступа и молча ложно для всех остальных.
//
// Наблюдалось 2026-08-11 на поднятом стенде. `kratosBrowserUrl` был
// `http://localhost:28080/.ory/kratos/public`, и на собственном хосте консоли
// `/registration` отвечал `303` на `localhost:28080`, где такого ingress нет:
// РЕГИСТРАЦИЯ УПИРАЛАСЬ В `404 nginx`. Соседний профиль боевой площадки нёс тот
// же ключ с её хостом и схемой `http://` — то есть при переводе площадки на
// https редиректы входа продолжали бы уводить на открытый канал.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОТНОСИТЕЛЬНЫЙ ВЕРЕН ПО ПОСТРОЕНИЮ
//
// Всё, с чем говорит консоль, проксируется ЧЕРЕЗ НЕЁ (same-origin), и сам Kratos
// рядом сконфигурирован относительно — `base_url: /.ory/kratos/public/`. Из всей
// связки абсолютным был один этот ключ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЧИТАЕТ ГЕЙТ
//
// Только ОБЪЯВЛЕНИЯ — умолчание чарта и каждый профиль, — поэтому он не требует
// ни чартов, ни кластера и не умеет пропуститься. Ловится форма значения, а не
// конкретный хост: запрет на `localhost` пропустил бы `http://kacho.in-cloud.io`,
// который ломается ровно так же, просто в другом развёртывании.
package deploy_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// browserFacingKeys — ключи, чьё значение исполняет браузер.
//
// Перечень выписан, и это осознанно: «браузерный» — свойство СМЫСЛА ключа, из
// формы значения оно не выводится. Цена закрыта проверкой ниже: ключ, которого
// в дереве не осталось, роняет пробу, поэтому перечень не переживёт свой предмет.
var browserFacingKeys = []string{"kratosBrowserUrl"}

// browserFacingURLDeclarations — где эти ключи объявлены: умолчание чарта и все
// профили умбреллы. Выводится обходом, а не списком файлов.
func browserFacingURLDeclarations(t *testing.T) map[string][]string {
	t.Helper()
	roots := []string{
		filepath.Join(umbrellaDir),
		filepath.Join(umbrellaDir, "charts", "kratos-selfservice-ui"),
	}
	out := map[string][]string{}
	for _, dir := range roots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("каталог %s не читается (%v) — предпосылка гейта исчезла", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			// #nosec G304 -- путь собран обходом фиксированных каталогов дерева.
			raw, rerr := os.ReadFile(p)
			if rerr != nil {
				t.Fatalf("%s не читается: %v", p, rerr)
			}
			for i, line := range strings.Split(string(raw), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "#") {
					continue // объяснение, а не объявление
				}
				for _, key := range browserFacingKeys {
					if !strings.HasPrefix(trimmed, key+":") {
						continue
					}
					val := strings.TrimSpace(strings.TrimPrefix(trimmed, key+":"))
					val = strings.Trim(val, `"'`)
					out[key] = append(out[key], p+":"+itoa(i+1)+" = "+val)
				}
			}
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestBrowserFacingURLsAreRelative(t *testing.T) {
	found := browserFacingURLDeclarations(t)

	var keys []string
	total := 0
	for k, v := range found {
		keys = append(keys, k)
		total += len(v)
	}
	sort.Strings(keys)
	t.Logf("осмотрено: браузерных ключей в перечне %d, объявлений в дереве %d %v",
		len(browserFacingKeys), total, keys)

	for _, key := range browserFacingKeys {
		decls := found[key]
		if len(decls) == 0 {
			t.Errorf("ключ %q не объявлен в дереве ни разу — перечню больше нечего проверять, "+
				"и он остался бы верным при любом значении. Снимите ключ из перечня вместе с его "+
				"предметом", key)
			continue
		}
		for _, d := range decls {
			val := d[strings.LastIndex(d, " = ")+3:]
			if strings.Contains(val, "://") {
				t.Errorf("%s — абсолютный адрес.\n"+
					"Это значение исполняет БРАУЗЕР: абсолютная форма фиксирует хост, порт и схему, "+
					"то есть заявляет, по какому адресу открыта консоль. Заявление верно для одного "+
					"способа доступа и молча ложно для остальных — на собственном хосте консоли "+
					"регистрация упиралась в 404, а зафиксированная схема `http://` пережила бы "+
					"перевод площадки на https. Пишите относительный путь: всё, с чем говорит "+
					"консоль, проксируется через неё же", d)
			}
		}
	}
}
