// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_deploy_authmode_one_address_test.go — посадка безопасности
// объявляется ОДНИМ адресом.
//
// Ручку звали в дереве восемью разными адресами и двумя именами: `authn.mode`,
// `config.authn.mode`, `auth.mode`, `config.mode`, `config.authMode`, `authMode`.
// Ночью первый вопрос оператора — «в какой посадке работает этот кластер», и
// задать его одной командой по одному ключу было НЕЛЬЗЯ: надо знать семь адресов
// плюс восьмой у края. Ужесточить флот одним `--set` тоже нельзя — путей столько
// же, сколько сервисов.
//
// Канон — `authMode` в корне значений сервиса. В профиле зонтика это значит
// `<сабчарт>.authMode`, то есть ровно одна строка на сервис и один grep на весь
// флот.
//
// Гейт судит ОБЪЯВЛЕНИЯ — умолчания чартов и профили, — а не рендер: ключ
// объявляется здесь, оператор грепает здесь, и проверке не нужны ни зависимости
// чарта, ни helm, поэтому она не может пропуститься. Судит УЗЕЛ-КЛЮЧ разобранного
// YAML: имена прежних адресов стоят в этих же файлах в комментариях — в том числе
// в абзаце, объясняющем сам перенос, — и гейт по подстроке краснел бы на
// собственном объяснении.
//
// Чего гейт НЕ проверяет, названо прямо: он не судит СЛОВАРЬ допустимых значений.
// Тот разошёлся отдельно (nlb принимает алиас, которого не принимает никто, и
// отвергает значение, в котором работают соседи), живёт в коде разбора
// конфигурации сервиса и закрывается своим изменением.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// canonicalPostureKey — единственный законный адрес в корне значений сервиса.
const canonicalPostureKey = "authMode"

// legacyPostureTails — прежние адреса, выведенные из объявлений на момент
// переноса. Хвост считается ОТ КОРНЯ ЗНАЧЕНИЙ СЕРВИСА: в файле умолчаний чарта
// это весь путь, в профиле зонтика — путь без имени сабчарта.
var legacyPostureTails = map[string]bool{
	"authn.mode":        true,
	"config.authn.mode": true,
	"auth.mode":         true,
	"config.mode":       true,
	"config.authMode":   true,
}

// isUmbrellaProfile отличает профиль зонтика от файла умолчаний чарта: у первого
// первый сегмент пути — имя сабчарта, у второго путь начинается сразу от корня
// значений сервиса.
func isUmbrellaProfile(rel string) bool {
	return strings.HasPrefix(filepath.ToSlash(rel), "deploy/helm/umbrella/values")
}

// TestSecurityPostureKnobHasOneAddress — во всех объявлениях посадка задаётся
// каноническим адресом.
//
// Проваливается на: прежнем адресе в любом файле значений и на пустом обходе
// (гейт, не нашедший ни одного объявления посадки, не утверждает ничего).
func TestSecurityPostureKnobHasOneAddress(t *testing.T) {
	root := repoRoot(t)
	files := valuesFiles(t, root)

	var canonical, filesRead int
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var doc yaml.Node
		if uerr := yaml.Unmarshal(raw, &doc); uerr != nil {
			t.Fatalf("parse %s: %v", path, uerr)
		}
		filesRead++

		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		profile := isUmbrellaProfile(rel)

		// Оба имени ключа сразу: прежние адреса оканчивались и на `mode`, и на
		// `authMode`, и второе отличается от канона только глубиной.
		var found []string
		found = append(found, findKeyPaths(&doc, "mode", nil)...)
		found = append(found, findKeyPaths(&doc, canonicalPostureKey, nil)...)

		for _, p := range found {
			tail := p
			if profile {
				// В профиле первый сегмент — имя сабчарта; посадка адресуется от
				// корня значений сервиса, а не от корня файла.
				i := strings.Index(p, ".")
				if i < 0 {
					continue // ключ верхнего уровня профиля — не посадка сервиса
				}
				tail = p[i+1:]
			}
			switch {
			case tail == canonicalPostureKey:
				canonical++
			case legacyPostureTails[tail]:
				t.Errorf("%s: посадка объявлена прежним адресом (%s). Вопрос «в какой посадке "+
					"работает этот кластер» по одному ключу на этот сервис не отвечается, и одним "+
					"`--set` флот не ужесточить. Перенести в `%s` в корне значений сервиса; прежний "+
					"адрес шаблон принимает, пока канон не задан, а заданные оба и различающиеся "+
					"дают отказ рендера",
					rel, p, canonicalPostureKey)
			}
		}
	}

	// Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	if filesRead == 0 {
		t.Fatal("прочитано ноль файлов значений — гейт судил бы по пустоте")
	}
	if canonical == 0 {
		t.Fatalf("во всех %d файлах значений не нашлось ни одного объявления `%s` — либо посадка "+
			"перестала объявляться, либо разбор перестал её видеть; и то и другое находка",
			filesRead, canonicalPostureKey)
	}
	t.Logf("перепись: файлов значений %d · канонических объявлений посадки %d", filesRead, canonical)
}
