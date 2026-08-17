// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// Владелец, который СПИСЫВАЕТ, обязан отвечать на ЧТЕНИЕ — и его снимок обязан
// догонять авторитет.
//
// ПРЕДМЕТ (задача `PRO-Robotech/kacho#412`). Предел, который ограничивает и
// которого не видно, для арендатора неотличим от сбоя платформы: он узнаёт о
// квоте только когда упирается в неё отказом. На день заведения гейта списывали
// пять доменов, отвечал на чтение один.
//
// ПОЧЕМУ ДВА УСЛОВИЯ, А НЕ ОДНО. Чтение без синхронизатора хуже отсутствия
// чтения: строка снимка заводится материализацией ОДИН раз и дальше живёт со
// своей величиной вечно, потому что промаха больше не случается, а запись идёт
// `ON CONFLICT DO NOTHING`. Показать такую строку арендатору значит громко
// назвать величину, которая никогда не догонит ту, что назначил администратор.
// Поэтому «отвечает на чтение» здесь означает пару: поверхность И курсор дельты.
//
// ПОЧЕМУ СВЕРКА ИДЁТ В ОБЕ СТОРОНЫ. Домен, который отвечает на чтение и не
// списывает, — тоже находка, и она тише: его ответ описывает потолок, под
// который ничего не считается, то есть число, которое не наполнится никогда.
func TestEveryQuotaChargingOwnerAnswersTheTenantRead(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)

	// Каталог контракта у домена `nlb` называется `loadbalancer` — так его назвал
	// сам контракт, и переименовать его нельзя (сломалась бы форма на проводе).
	//
	// Соответствие объявлено ЗДЕСЬ и явно, а не выведено из совпадения имён:
	// вывод по совпадению молча пропустил бы ровно этот домен, и гейт отчитался
	// бы «ноль находок», не осмотрев пятую часть предмета.
	protoDirOf := map[string]string{"nlb": "loadbalancer"}

	sqlFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".sql")
	require.NoError(t, err, "перечень миграций берётся у индекса дерева, а не обходом диска")

	charging := map[string]string{} // домен → миграция, где найдено списание
	cursor := map[string]string{}   // домен → миграция, где заведён курсор дельты

	chargeRe := regexp.MustCompile(`kacho_quota_count\(`)
	cursorRe := regexp.MustCompile(`quota_sync_cursor`)

	migrationsSeen := 0
	for _, path := range sqlFiles {
		if !strings.Contains(path, "/internal/migrations/") {
			continue
		}
		raw, rerr := os.ReadFile(path)
		require.NoError(t, rerr, "чтение %s", path)
		migrationsSeen++
		body := string(raw)

		rel := path
		if i := strings.Index(path, "/services/"); i >= 0 {
			rel = path[i+1:]
		}
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			continue
		}
		domain := parts[1]

		if chargeRe.MatchString(body) {
			charging[domain] = rel
		}
		if cursorRe.MatchString(body) {
			cursor[domain] = rel
		}
	}

	// Кто отвечает на чтение: домен, чей контракт объявляет `QuotaService`.
	protoFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, "proto"), ".proto")
	require.NoError(t, err, "перечень контрактов берётся у индекса дерева")

	answering := map[string]string{} // каталог контракта → файл
	serviceRe := regexp.MustCompile(`(?m)^service QuotaService\b`)
	protosSeen := 0
	for _, path := range protoFiles {
		raw, rerr := os.ReadFile(path)
		require.NoError(t, rerr, "чтение %s", path)
		protosSeen++
		if !serviceRe.MatchString(string(raw)) {
			continue
		}
		rel := path
		if i := strings.Index(path, "/proto/"); i >= 0 {
			rel = path[i+1:]
		}
		// proto/kacho/cloud/<каталог>/v1/…
		seg := strings.Split(rel, "/")
		if len(seg) > 3 {
			answering[seg[3]] = rel
		}
	}

	require.NotZero(t, migrationsSeen,
		"гейт не прочитал НИ ОДНОЙ миграции — он объявил бы «ноль находок», ничего не осмотрев")
	require.NotZero(t, protosSeen,
		"гейт не прочитал НИ ОДНОГО контракта — «ноль находок» было бы «ноль прочитанного»")
	require.NotEmpty(t, charging,
		"гейт не нашёл ни одного домена со списанием: либо имя триггера сменилось, "+
			"либо предикат перестал его ловить. Осмотрено миграций: %d", migrationsSeen)
	require.NotEmpty(t, answering,
		"гейт не нашёл НИ ОДНОГО объявления `service QuotaService`: предикат перестал "+
			"ловить предмет, и молчание такого гейта неотличимо от согласия. Осмотрено контрактов: %d",
		protosSeen)

	protoDir := func(domain string) string {
		if d, ok := protoDirOf[domain]; ok {
			return d
		}
		return domain
	}

	var findings []string
	for domain, where := range charging {
		if _, ok := answering[protoDir(domain)]; !ok {
			findings = append(findings, domain+
				" — списывает квоту ("+where+"), но арендатору её не показывает: "+
				"нет `service QuotaService` в proto/kacho/cloud/"+protoDir(domain)+"/v1/")
		}
		if _, ok := cursor[domain]; !ok {
			findings = append(findings, domain+
				" — списывает квоту ("+where+"), но снимок величины не догоняет авторитет: "+
				"нет таблицы `quota_sync_cursor` в его миграциях")
		}
	}

	// Зеркальная форма: ответ про потолок, под который никто не считает.
	chargingProtoDirs := map[string]bool{}
	for domain := range charging {
		chargingProtoDirs[protoDir(domain)] = true
	}
	for dir, where := range answering {
		if !chargingProtoDirs[dir] {
			findings = append(findings, dir+
				" — отвечает на чтение квот ("+where+"), но не списывает ни одного вида: "+
				"ответ называет потребление, которое не наполнится никогда")
		}
	}
	sort.Strings(findings)

	t.Logf("перепись: миграций осмотрено %d, контрактов %d; списывают %d (%s); "+
		"отвечают на чтение %d (%s); догоняют величину %d (%s)",
		migrationsSeen, protosSeen,
		len(charging), joinKeys(charging),
		len(answering), joinKeys(answering),
		len(cursor), joinKeys(cursor))

	require.Empty(t, findings,
		"предел, который ограничивает и которого не видно, неотличим для арендатора от сбоя "+
			"платформы — он узнаёт о квоте только упершись в неё отказом:\n%s",
		strings.Join(findings, "\n"))
}
