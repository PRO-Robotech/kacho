// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// docs_example_carrier_pair_test.go — ОПУБЛИКОВАННЫЙ ПРИМЕР НАСТРОЙКИ ПОДНИМАЕТ
// КРАЙ, а не отвергается его же стражем.
//
// # Класс, а не опечатка
//
// Страница установки несёт единственный пример новой ручки, и оператор
// копирует именно его. Пример, объявляющий пару, которую страж отвергает,
// хуже отсутствующего: отсутствующий заставляет прочитать текст, а этот
// обещает работающий стенд и даёт неподнимающийся — причём отказ называет
// ручку, которую оператор ВЗЯЛ ИЗ ДОКУМЕНТАЦИИ.
//
// Так и случилось: в одном блоке стояли посадка `external` и множество
// `own,external`, то есть ровно та пара, которую соседняя проба утверждает как
// ОТКАЗ, — и объяснял это словами комментарий двумя строками выше.
//
// # Почему гейт судит ОБЪЯВЛЕНИЯ, а не отрисованный стенд
//
// Предмет здесь — то, что ЧИТАЕТ ОПЕРАТОР. Отрисовка чарта отвечает на другой
// вопрос (что получится из профиля), и зелёная отрисовка ничего не сказала бы
// о странице. Гейт берёт значения из самого документа и спрашивает у них ровно
// то, что спросит процесс при старте, — ТЕМ ЖЕ читателем настройки
// (`config.Load`) и ТЕМ ЖЕ стражем.
//
// # Граница названа
//
// Судится ПАРА «посадка × множество читателей» и её адреса. Прочие ручки
// примера этот гейт не судит: у них свои стражи и свои пробы, и утверждать о
// них здесь значило бы завести второе правило об одном предмете.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// docsConfigurationPage — страница с примером, относительно каталога пакета.
const docsConfigurationPage = "../../docs/content/install/configuration.mdx"

// docsExampleBlockOpener — открывающая последовательность блока примера.
const docsExampleBlockOpener = "{dedent`"

// docsEnvLine — строка примера вида `KACHO_...: значение`. Значение берётся до
// конца строки и очищается от кавычек: так его читает и оператор.
var docsEnvLine = regexp.MustCompile(`(?m)^\s*(KACHO_[A-Z0-9_]+)\s*:\s*(.*)$`)

// docsExampleEnv — пары «ручка → значение», объявленные примером.
func docsExampleEnv(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(docsConfigurationPage))
	if err != nil {
		t.Fatalf("страница настройки не прочитана: %v", err)
	}
	// ПРЕДПОСЫЛКА ГЕЙТА, проверенная, а не подразумеваемая: пример на странице
	// ОДИН. Второй блок объявил бы те же ручки другими значениями, карта взяла
	// бы последнее, и гейт судил бы смесь, которой нет ни на одном стенде —
	// молча. Появится второй пример — эту функцию обязано пересмотреть то же
	// изменение, которое его добавит.
	if n := strings.Count(string(raw), docsExampleBlockOpener); n != 1 {
		t.Fatalf("блоков примера на странице %d, ожидался 1: при нескольких блоках значения "+
			"сливаются в одну карту, и гейт судит пару, которой нет ни в одном примере", n)
	}

	out := map[string]string{}
	for _, m := range docsEnvLine.FindAllStringSubmatch(string(raw), -1) {
		value := strings.TrimSpace(m[2])
		value = strings.Trim(value, `"`)
		out[m[1]] = value
	}
	if len(out) == 0 {
		t.Fatalf("в %s не найдено ни одной ручки — гейт судил бы о непрочитанном: "+
			"форма примера изменилась, и предикат перестал опознавать свой предмет", docsConfigurationPage)
	}
	return out
}

func TestDocsExample_TheCarrierPairItPublishesPassesTheStartupGuard(t *testing.T) {
	env := docsExampleEnv(t)

	// Пара обязана быть ОБЪЯВЛЕНА примером: ручка, у которой нет примера, —
	// не предмет этого гейта, но и молчание здесь было бы ложным зелёным.
	for _, knob := range []string{config.IdentityProviderKnob, config.SessionCarriersKnob} {
		if _, ok := env[knob]; !ok {
			t.Fatalf("пример не объявляет %s — единственный образец новой ручки пропал со страницы, "+
				"и гейт остался бы зелёным, ничего не проверив", knob)
		}
	}

	// Ручки, которые судит гейт, обнуляются ПЕРЕД тем, как задать объявленные
	// примером: величина, приехавшая из окружения прогона и примером не
	// названная, сделала бы вердикт вердиктом о чужом значении.
	for _, knob := range []string{
		config.IdentityProviderKnob,
		config.SessionCarriersKnob,
		config.SessionCarrierWindowOpenedAtKnob,
		"KACHO_API_GATEWAY_KRATOS_PUBLIC_URL",
	} {
		t.Setenv(knob, "")
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("настройка примера не загружается тем же читателем, что у процесса: %v", err)
	}
	posture, err := cfg.ResolvedIdentityProvider()
	if err != nil {
		t.Fatalf("посадка примера не разбирается: %v", err)
	}
	carriers, err := cfg.ResolvedSessionCarriers()
	if err != nil {
		t.Fatalf("множество читателей примера не разбирается: %v", err)
	}
	windowOpenedAt, err := cfg.ResolvedSessionCarrierWindowOpenedAt()
	if err != nil {
		t.Fatalf("момент открытия окна из примера не разбирается: %v", err)
	}
	if err := validateSessionCarrierConfig(SessionCarrierConfig{
		Posture: posture, Carriers: carriers, ProviderURL: cfg.KratosPublicURL,
		WindowOpenedAt: windowOpenedAt,
	}); err != nil {
		t.Fatalf("опубликованный пример ОТВЕРГАЕТСЯ стражем старта: %v\n\n"+
			"Оператор, скопировавший единственный образец новой ручки, получит неподнимающийся "+
			"стенд, и отказ назовёт ему ручку, взятую из документации.", err)
	}
	t.Logf("перепись: ручек прочитано из примера %d · судится пара «посадка × множество»: %s=%q · %s=%q · адрес чужой стороны %q",
		len(env), config.IdentityProviderKnob, posture.String(),
		config.SessionCarriersKnob, carriers.String(), cfg.KratosPublicURL)
}
