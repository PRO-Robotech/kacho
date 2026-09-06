// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// quotaauthorityprofiles_test.go — перепись профилей развёртывания по ПАРЕ
// «адрес домена величин и удостоверение к нему».
//
// # Предмет: половина пары хуже отсутствия обеих
//
// Профиль, объявивший ребро величин адресом и не объявивший удостоверения,
// ВЫГЛЯДИТ настроенным: обращение уходит, сосед отвечает «требуется сертификат
// клиента», и отказ читается как недоступность соседа при исправном соседе
// (`security.md` §«Контроль, у которого нет МЕХАНИЗМА исполниться»). На боевой
// посадке страж старта такую посадку отвергает — то есть цена ошибки не «тихая
// деградация», а неподнимаемая служба.
//
// # Единица счёта названа: ПАРА (профиль, потребитель)
//
// Профилей у продукта несколько, потребителей ребра пять, и «сколько профилей»
// без второй половины не отвечает ни на что: один профиль объявляет ребро пяти
// разным службам, и забыть можно у одной.
//
// # Предикат применимости — «профиль считает эту службу боевой»
//
// Требование транспорта у ребра величин ТО ЖЕ, что у остальных рёбер службы
// (боевой режим), поэтому судятся только те блоки, где профиль включил хоть
// одно клиентское ребро. Судить блоки, где не включено ни одного, значило бы
// требовать от локальной посадки строгости, которой не требует ни одно другое
// ребро, — и первым следствием стал бы неподнимаемый стенд.

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// quotaEdgeKey — ключ ребра величин в блоке `mtls.edges` любого чарта.
const quotaEdgeKey = "quotaAuthority"

// quotaProfileConsumers — потребители ребра, чьи секции ищутся в профилях
// зонтичного чарта. Имена секций — имена ЧАРТОВ, а не доменов: у службы
// балансировки они не совпадают, и совпадение остальных четырёх — совпадение, а
// не свойство дерева.
var quotaProfileConsumers = []string{"vpc", "compute", "storage", "registry", "kacho-nlb"}

func TestQuotaAuthorityProfilesDeclareBothHalvesOfThePair(t *testing.T) {
	root := repoRoot(t)
	umbrella := filepath.Join(root, "deploy", "helm", "umbrella")

	entries, err := os.ReadDir(umbrella)
	require.NoError(t, err)

	var profiles []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && filepath.Ext(name) == ".yaml" &&
			len(name) > len("values.") && name[:len("values.")] == "values." {
			profiles = append(profiles, name)
		}
	}
	sort.Strings(profiles)
	require.NotEmpty(t, profiles,
		"обход беспредметен: профилей зонтичного чарта не найдено ни одного — "+
			"«ноль находок» здесь неотличимо от «ноль прочитанного»")

	var examined, carrying int
	for _, name := range profiles {
		body, rerr := os.ReadFile(filepath.Join(umbrella, name))
		require.NoError(t, rerr)

		var doc map[string]any
		require.NoError(t, yaml.Unmarshal(body, &doc), "профиль %s", name)

		for _, svc := range quotaProfileConsumers {
			edges, ok := quotaEdgesOf(doc, svc)
			if !ok {
				continue
			}
			if !anyEdgeEnabled(edges) {
				// Профиль не считает эту службу боевой: клиентских рёбер не
				// включено ни одного, значит удостоверения не требует ни одно
				// ребро, включая это.
				continue
			}
			examined++
			enabled, _ := edges[quotaEdgeKey].(bool)
			if enabled {
				carrying++
				continue
			}
			t.Errorf("профиль %s, служба %s: включены клиентские рёбра, но %s не объявлено. "+
				"Адрес домена величин у этой службы задан умолчанием чарта, поэтому посадка "+
				"получает АДРЕС БЕЗ УДОСТОВЕРЕНИЯ: обращение уходит, сосед отвечает "+
				"«требуется сертификат клиента», и страж старта отказывает в пуске",
				name, svc, quotaEdgeKey)
		}
	}

	t.Logf("перепись: профилей прочитано %d, боевых блоков ребра величин %d, "+
		"из них объявляют удостоверение %d", len(profiles), examined, carrying)
	require.Positive(t, examined,
		"ни один профиль не включает клиентских рёбер — судить нечего, "+
			"и зелёный здесь означал бы «не прочитано», а не «чисто»")
	require.Equal(t, examined, carrying)
}

// quotaEdgesOf достаёт блок `mtls.edges` секции службы.
func quotaEdgesOf(doc map[string]any, service string) (map[string]any, bool) {
	section, ok := doc[service].(map[string]any)
	if !ok {
		return nil, false
	}
	mtls, ok := section["mtls"].(map[string]any)
	if !ok {
		return nil, false
	}
	edges, ok := mtls["edges"].(map[string]any)
	return edges, ok
}

// anyEdgeEnabled — включено ли профилем хоть одно клиентское ребро этой службы.
func anyEdgeEnabled(edges map[string]any) bool {
	for key, v := range edges {
		if key == quotaEdgeKey {
			continue
		}
		if on, ok := v.(bool); ok && on {
			return true
		}
	}
	return false
}
