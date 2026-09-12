// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// quotaauthorityprofiles_test.go — перепись профилей развёртывания по ПАРЕ
// «адрес домена величин и удостоверение к нему».
//
// # Предмет: половина пары хуже отсутствия обеих — В ОБЕ СТОРОНЫ
//
// У пары два способа разъехаться, и до 2026-09-12 эта проверка знала один:
//
//  1. адрес объявлен, удостоверения нет. Профиль ВЫГЛЯДИТ настроенным:
//     обращение уходит, сосед отвечает «требуется сертификат клиента», и отказ
//     читается как недоступность соседа при исправном соседе
//     (`security.md` §«Контроль, у которого нет МЕХАНИЗМА исполниться»);
//  2. адрес объявляет ОТСУТСТВИЕ домена величин, а удостоверение объявлено.
//     Обращаться не к кому, поэтому имя для сверки рукопожатия называет ПИРА, К
//     КОТОРОМУ РЕБРО НЕ ИДЁТ: контроль присутствует, провязан, исполняется — и
//     защищает не фактического собеседника.
//
// # ПРЕЖНЯЯ РЕДАКЦИЯ ТРЕБОВАЛА ВТОРОЙ ПОЛОВИНЫ ОШИБКИ, И ЭТО НАЗВАНО ПРЯМО
//
// Она не читала адрес вовсе — она ДОПУСКАЛА, что он задан, и её собственный
// текст отказа это допущение называл: «адрес домена величин у этой службы задан
// умолчанием чарта». Допущение пережило свой предмет: после выноса службы
// величин умолчание каждого из пяти чартов — `not-deployed`, то есть объявление
// ОТСУТСТВИЯ. Проверка требовала удостоверения к соседу, которого нет, —
// то есть предписывала ровно тот класс, который ловит
// `deploy/tests/helm/servername-checked-against-the-peer-test.sh`. Два гейта об
// одном предмете, из которых верен один.
//
// Отсюда несущее правило нынешней редакции: пара судится по ОБЕИМ прочитанным
// половинам, а не по одной прочитанной и одной предположенной.
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
//
// Вторая половина (объявленное отсутствие + удостоверение) режимом НЕ
// смягчается — собеседника нет ни в одном режиме, — но и она судится только в
// боевых блоках: иначе перепись считала бы одни блоки по одному предикату, а
// другие по другому, и число «рассмотрено» перестало бы значить одно.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	corequota "github.com/PRO-Robotech/corelib/quota"
)

// quotaEdgeKey — ключ ребра величин в блоке `mtls.edges` любого чарта.
const quotaEdgeKey = "quotaAuthority"

// quotaPairVerdict — исход пары для ОДНОГО блока (профиль, потребитель).
type quotaPairVerdict int

const (
	// quotaPairWhole — пара полна: либо адрес и удостоверение, либо ни того ни другого.
	quotaPairWhole quotaPairVerdict = iota
	// quotaPairAddressWithoutTransport — адрес объявлен, удостоверения нет.
	quotaPairAddressWithoutTransport
	// quotaPairAbsenceWithTransport — объявлено отсутствие соседа, удостоверение есть.
	quotaPairAbsenceWithTransport
)

// judgeQuotaPair — вердикт о паре по ОБЕИМ её прочитанным половинам.
//
// Чистая функция от двух значений, а не строка внутри обхода: инъекция кормит её
// же, поэтому доказанное на синтетике верно для дерева. Встроенное в обход
// решение приходилось бы воспроизводить в инъекции — второе место об одном
// предмете, которое разойдётся молча.
func judgeQuotaPair(authority string, transportDeclared bool) quotaPairVerdict {
	if strings.TrimSpace(authority) == corequota.NotDeployed {
		if transportDeclared {
			return quotaPairAbsenceWithTransport
		}
		return quotaPairWhole
	}
	if !transportDeclared {
		return quotaPairAddressWithoutTransport
	}
	return quotaPairWhole
}

// quotaProfileSections — секция профиля → каталог чарта, выведенные из
// описателя зависимостей зонтичного чарта.
//
// Выводятся, а не выписываются: прежняя редакция несла список из пяти имён, и
// шестой потребитель разошёлся бы с ним молча. Имя секции и имя каталога службы
// совпадают у четырёх из пяти и НЕ совпадают у службы балансировки — то есть
// совпадение остальных есть совпадение, а не свойство дерева.
func quotaProfileSections(root string) (map[string]string, error) {
	body, err := os.ReadFile(filepath.Join(root, "deploy", "helm", "umbrella", "Chart.yaml"))
	if err != nil {
		return nil, err
	}
	var chart struct {
		Dependencies []struct {
			Name       string `yaml:"name"`
			Alias      string `yaml:"alias"`
			Repository string `yaml:"repository"`
		} `yaml:"dependencies"`
	}
	if uerr := yaml.Unmarshal(body, &chart); uerr != nil {
		return nil, uerr
	}
	out := map[string]string{}
	for _, dep := range chart.Dependencies {
		const prefix = "file://../../../services/"
		if !strings.HasPrefix(dep.Repository, prefix) {
			continue
		}
		dir := strings.TrimSuffix(strings.TrimPrefix(dep.Repository, prefix), "/deploy")
		if dir == "" || strings.Contains(dir, "/") {
			continue
		}
		section := dep.Name
		if dep.Alias != "" {
			section = dep.Alias
		}
		out[section] = dir
	}
	return out, nil
}

// chartQuotaAuthority — умолчание адреса домена величин в чарте службы.
//
// Читается ИЗ ЧАРТА, а не предполагается: предположение о нём и было предметом
// прежней редакции. Пустое значение — «не прочитано», и это исход, отличный от
// обоих законных: судить по нему нельзя ни в какую сторону.
func chartQuotaAuthority(root, dir string) (string, error) {
	body, err := os.ReadFile(filepath.Join(root, "services", dir, "deploy", "values.yaml"))
	if err != nil {
		return "", err
	}
	var doc map[string]any
	if uerr := yaml.Unmarshal(body, &doc); uerr != nil {
		return "", uerr
	}
	quota, ok := doc["quota"].(map[string]any)
	if !ok {
		return "", nil
	}
	value, _ := quota["authority"].(string)
	return value, nil
}

// profileQuotaAuthority — переопределение адреса профилем, если оно есть.
func profileQuotaAuthority(doc map[string]any, section string) (string, bool) {
	block, ok := doc[section].(map[string]any)
	if !ok {
		return "", false
	}
	quota, ok := block["quota"].(map[string]any)
	if !ok {
		return "", false
	}
	value, ok := quota["authority"].(string)
	return value, ok
}

func TestQuotaAuthorityProfilesDeclareBothHalvesOfThePair(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	umbrella := filepath.Join(root, "deploy", "helm", "umbrella")

	sections, serr := quotaProfileSections(root)
	require.NoError(t, serr)
	consumers, cerr := quotaConsumers(root)
	require.NoError(t, cerr)
	require.NotEmpty(t, consumers, "обход беспредметен: потребителей ребра величин не найдено")

	// Секции профиля, чей чарт — потребитель ребра величин. Пересечение двух
	// выведенных перечней, а не третий выписанный.
	carriesQuota := map[string]bool{}
	for _, dir := range consumers {
		carriesQuota[dir] = true
	}
	defaults := map[string]string{}
	var watched []string
	for section, dir := range sections {
		if !carriesQuota[dir] {
			continue
		}
		value, derr := chartQuotaAuthority(root, dir)
		require.NoError(t, derr)
		require.NotEmpty(t, value,
			"чарт службы %s не объявляет умолчания адреса домена величин — "+
				"пару судить нечем, и это «не прочитано», а не «пара полна»", dir)
		defaults[section] = value
		watched = append(watched, section)
	}
	sort.Strings(watched)
	require.Len(t, watched, len(consumers),
		"секций профиля и потребителей ребра разное число: описатель зависимостей "+
			"зонтичного чарта и каталог служб разошлись, и часть потребителей не судилась бы")

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

	var examined, whole, withAddress, withAbsence int
	for _, name := range profiles {
		body, rerr := os.ReadFile(filepath.Join(umbrella, name))
		require.NoError(t, rerr)

		var doc map[string]any
		require.NoError(t, yaml.Unmarshal(body, &doc), "профиль %s", name)

		for _, section := range watched {
			edges, ok := quotaEdgesOf(doc, section)
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

			authority := defaults[section]
			if override, has := profileQuotaAuthority(doc, section); has {
				authority = override
			}
			if strings.TrimSpace(authority) == corequota.NotDeployed {
				withAbsence++
			} else {
				withAddress++
			}

			declared, _ := edges[quotaEdgeKey].(bool)
			switch judgeQuotaPair(authority, declared) {
			case quotaPairWhole:
				whole++
			case quotaPairAddressWithoutTransport:
				t.Errorf("профиль %s, служба %s: адрес домена величин объявлен (%q), "+
					"а %s не объявлено. Посадка получает АДРЕС БЕЗ УДОСТОВЕРЕНИЯ: "+
					"обращение уходит, сосед отвечает «требуется сертификат клиента», "+
					"и страж старта отказывает в пуске",
					name, section, authority, quotaEdgeKey)
			case quotaPairAbsenceWithTransport:
				t.Errorf("профиль %s, служба %s: адрес объявляет, что домена величин НЕТ "+
					"(%q), а %s = true. Обращаться не к кому, поэтому имя для сверки "+
					"рукопожатия называет ПИРА, К КОТОРОМУ РЕБРО НЕ ИДЁТ — и страж старта "+
					"отказывает в пуске",
					name, section, authority, quotaEdgeKey)
			}
		}
	}

	t.Logf("перепись: профилей прочитано %d, потребителей под надзором %d, "+
		"боевых блоков ребра величин %d (адрес объявлен %d, объявлено отсутствие %d), "+
		"пара полна у %d", len(profiles), len(watched), examined, withAddress, withAbsence, whole)
	require.Positive(t, examined,
		"ни один профиль не включает клиентских рёбер — судить нечего, "+
			"и зелёный здесь означал бы «не прочитано», а не «чисто»")
	require.Equal(t, examined, whole)
}

// TestJudgeQuotaPairFailsInBothDirections — доказательство того, что вердикт о
// паре СПОСОБЕН отвергнуть, и отвергает не всё подряд.
//
// Обе стороны в одной пробе намеренно: проверка, знавшая одну сторону, и была
// предметом переписи выше — «зелено» у неё означало «вторую сторону не смотрю».
func TestJudgeQuotaPairFailsInBothDirections(t *testing.T) {
	t.Parallel()
	const addr = "kaname-internal.kacho.svc:9091"

	require.Equal(t, quotaPairAddressWithoutTransport, judgeQuotaPair(addr, false),
		"адрес без удостоверения — половина пары")
	require.Equal(t, quotaPairWhole, judgeQuotaPair(addr, true),
		"близнец: адрес с удостоверением — законная боевая посадка")

	require.Equal(t, quotaPairAbsenceWithTransport, judgeQuotaPair(corequota.NotDeployed, true),
		"объявленное отсутствие с удостоверением — половина пары с другой стороны")
	require.Equal(t, quotaPairWhole, judgeQuotaPair(corequota.NotDeployed, false),
		"близнец: объявленное отсутствие без удостоверения — законная посадка")

	require.Equal(t, quotaPairWhole, judgeQuotaPair("  "+corequota.NotDeployed+" ", false),
		"пробелы вокруг написания не выводят посадку из-под предиката")
	require.Equal(t, quotaPairAbsenceWithTransport, judgeQuotaPair(corequota.NotDeployed+"\n", true),
		"и не выводят её из-под отказа")
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

// TestQuotaAuthorityChartsHaveNoDefault — чарт потребителя НЕ подставляет
// умолчания за оператора: объявление проходит через `required`.
//
// # Почему это отдельная проверка, а не следствие стража старта
//
// Страж отказывает ПРОЦЕССУ, то есть уже на поднятом стенде и после выкатки.
// Чарт отказывает РЕНДЕРУ — на машине оператора, до того как что-либо
// применено. Полосы разные, и умолчание, вписанное в шаблон (`| default
// "kaname-internal:9091"`), сняло бы вторую целиком: оператор получал бы адрес,
// которого не выбирал, а страж видел бы его заданным и молчал.
//
// # Что судится и чего эта проверка НЕ видит
//
// Судится объявление шаблона, а не исход рендера: рендер требует helm, которого
// у короткого прогона нет. Значит проверка ловит подставленное умолчание и не
// ловит шаблон, синтаксически верный, но рендерящийся во что-то иное. Вторую
// половину закрывает рендер на прогоне посадки; здесь она названа, а не скрыта.
func TestQuotaAuthorityChartsHaveNoDefault(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	// Каталоги чартов ВЫВОДЯТСЯ из тех же потребителей, что и перепись выше:
	// выписанный список разошёлся бы с деревом при шестом потребителе.
	consumers, err := quotaConsumers(root)
	require.NoError(t, err)
	require.NotEmpty(t, consumers, "обход беспредметен: потребителей ребра величин не найдено")

	var templatesRead, carrying int
	for _, svc := range consumers {
		dir := filepath.Join(root, "services", svc, "deploy", "templates")
		var found bool
		werr := rootedWalk(dir,
			func(rel string) bool { return filepath.Ext(rel) == ".yaml" },
			func(abs string, body []byte) error {
				text := string(body)
				if !strings.Contains(text, ".Values.quota.authority") {
					return nil
				}
				templatesRead++
				for _, line := range strings.Split(text, "\n") {
					if !strings.Contains(line, ".Values.quota.authority") {
						continue
					}
					if !strings.Contains(line, "required ") {
						t.Errorf("%s: объявление домена величин проходит без `required`: %s\n"+
							"Умолчание в шаблоне подставило бы за оператора выбор между "+
							"«потолки действуют» и «потолков нет», и страж старта увидел бы "+
							"значение заданным — то есть промолчал бы", abs, strings.TrimSpace(line))
						continue
					}
					found = true
				}
				return nil
			})
		require.NoError(t, werr)
		if found {
			carrying++
			continue
		}
		t.Errorf("чарт службы %s не проводит объявление домена величин ни одним шаблоном: "+
			"ручка есть у процесса и недостижима из профиля", svc)
	}

	t.Logf("перепись: потребителей %d, из них проводят объявление %d (шаблонов с ним %d)",
		len(consumers), carrying, templatesRead)
}
