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

	"github.com/PRO-Robotech/kacho/pkg/platformmodules"
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

	// Соответствий здесь ДВА, и они РАЗНОГО РОДА — сливать их в один литерал
	// значило бы объявить общим то, что общим не является.
	//
	// Первое — «служба ↔ модуль каталога»: у домена `nlb` каталог контракта
	// называется `loadbalancer`, так его назвал сам контракт, и переименовать
	// его нельзя (сломалась бы форма на проводе). Оно ОБЩЕЕ для платформы и
	// берётся у словаря имён модулей (pkg/platformmodules).
	//
	// Второе — частное и к словарю не относится: у домена `iam` служба чтения
	// величин объявлена в пакете ОБЩЕЙ формы ответа (`quota`), потому что та
	// форма уже зависит от `iam.v1`, и объявление службы внутри `iam.v1`
	// замкнуло бы пакеты друг на друга — это отвергает `buf lint`. Собственные
	// контракты iam при этом лежат в `proto/kacho/cloud/iam`, поэтому в словарь
	// эта запись не годится: она верна только для контрактов величин.
	protoDirOf := platformmodules.AliasesByService()
	protoDirOf["iam"] = "quota"

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
	// Имён у службы чтения два, и второе — не синоним: `IdentityQuotaService`
	// отвечает о носителе, который не является ни проектом, ни аккаунтом, поэтому
	// у него другая форма запроса (полей нет вовсе). Искать только первое имя
	// значило бы объявить находкой домен, который читать как раз ДАЁТ.
	serviceRe := regexp.MustCompile(`(?m)^service (Quota|IdentityQuota)Service\b`)
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

	// Владелец ВЕЛИЧИН — тот, чей контракт объявляет службу их выдачи. Он
	// определяется ЗАМЕРОМ, а не именем: гейт, знающий его по написанию,
	// продолжил бы освобождать `iam` и после того, как авторитет переехал бы
	// в другой домен, — то есть освобождал бы того, кому освобождение больше не
	// причитается.
	limitOwners := map[string]string{}
	ownerRe := regexp.MustCompile(`(?m)^service InternalLimitService\b`)
	for _, path := range protoFiles {
		raw, rerr := os.ReadFile(path)
		require.NoError(t, rerr, "чтение %s", path)
		if !ownerRe.MatchString(string(raw)) {
			continue
		}
		rel := path
		if i := strings.Index(path, "/proto/"); i >= 0 {
			rel = path[i+1:]
		}
		if seg := strings.Split(rel, "/"); len(seg) > 3 {
			limitOwners[seg[3]] = rel
		}
	}
	require.Lenf(t, limitOwners, 1,
		"владельцев величин найдено %d, а их обязан быть ровно один: освобождение "+
			"от догоняющего опирается на то, что авторитет и снимок лежат в ОДНОЙ базе, "+
			"и на двух владельцах эта предпосылка неверна", len(limitOwners))

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
			// Владельцу ВЕЛИЧИН догоняющий не нужен, и это не послабление, а
			// отсутствие предмета: авторитет лежит в той же базе, что снимок, и
			// списание обновляет снимок тем же оператором. Дельты к самому себе не
			// существует.
			//
			// ПРЕДПОСЫЛКА ОСВОБОЖДЕНИЯ ПРОВЕРЯЕТСЯ, а не принимается на слово: файл,
			// определяющий списание, обязан читать таблицу величин ЭТОГО ЖЕ домена.
			// Замени владелец живое чтение на снимок из дельты — освобождение
			// перестанет иметь основание, и гейт скажет об этом здесь.
			// Владелец ищется по ОБОИМ его именам: службу выдачи величин он
			// объявляет в СВОЁМ пакете, а службу чтения — возможно, в чужом (у
			// `iam` это ровно так). Спросить только одно имя значило бы не найти
			// владельца ровно там, где два имени и разошлись.
			_, ownerBySvc := limitOwners[domain]
			_, ownerByProto := limitOwners[protoDir(domain)]
			if ownerBySvc || ownerByProto {
				if !localAuthorityReadByCharger(t, root, domain) {
					findings = append(findings, domain+
						" — владелец величин, но его списание НЕ читает таблицу величин "+
						"своей базы: освобождение от догоняющего лишилось предпосылки, "+
						"и снимок теперь может отставать так же, как у прочих")
				}
				continue
			}
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

// localAuthorityReadByCharger — читает ли механизм списания этого домена таблицу
// величин ЕГО ЖЕ базы.
//
// Это ПРЕДПОСЫЛКА освобождения владельца величин от догоняющего, и проверяется
// она, а не принимается на слово. Освобождение защитимо ровно потому, что
// авторитет и снимок лежат в одной базе и обновляются одним оператором; замени
// живое чтение на снимок из дельты — предпосылка исчезнет, а освобождение
// осталось бы, и снимок отставал бы молча.
//
// Предмет — файл, ОПРЕДЕЛЯЮЩИЙ списание (`kacho_quota_count`), а не любой файл
// домена: величину читают многие, а вопрос ровно один — читает ли её тот, кто
// принимает решение о месте.
func localAuthorityReadByCharger(t *testing.T, root, domain string) bool {
	t.Helper()

	files, err := treecorpus.UnderWithSuffix(
		filepath.Join(root, "services", domain, "internal", "migrations"), ".sql")
	require.NoError(t, err, "перечень миграций домена %s берётся у индекса дерева", domain)

	chargeRe := regexp.MustCompile(`kacho_quota_count\(`)
	// Таблица величин у владельца ровно одна и называется `limits` в его схеме.
	// Имя схемы в предикат не зашивается: оно частность владельца, а предмет —
	// «читает СВОЮ таблицу величин».
	authorityRe := regexp.MustCompile(`\blimits\b`)

	seen := 0
	found := false
	for _, path := range files {
		raw, rerr := os.ReadFile(path)
		require.NoError(t, rerr, "чтение %s", path)
		body := string(raw)
		if !chargeRe.MatchString(body) {
			continue
		}
		seen++
		if authorityRe.MatchString(body) {
			found = true
		}
	}
	require.NotZerof(t, seen,
		"у домена %s не нашлось ни одного файла, определяющего списание, — предпосылка "+
			"этой проверки сломана, и её молчание ничего не доказывает", domain)
	return found
}
