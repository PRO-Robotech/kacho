// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// Сервис, ОБЪЯВЛЯЮЩИЙ посадочные ручки, обязан нести стража, который на них
// смотрит.
//
// ПРЕДМЕТ — ban #16 дословно: «Каждый сервис ОБЯЗАН нести production boot-guard
// (`Config.Validate()` fail-closed → refuse-to-start при insecure config)».
// Запрет объявлен один, а исполняется семью разными способами, и один из семи —
// нулевой.
//
// ЧТО ИМЕННО НАЙДЕНО, И ЭТО НЕ ОПЕЧАТКА. geo объявляет `DBSSLMode` с умолчанием
// `disable`, `AuthMode`, обе пары mTLS — каждую с комментарием про боевой
// режим, — и не проверяет НИ ОДНУ. Есть самоотчёт о посадке (`bootPosture`) и
// разбор режима, но самоотчёт сообщает, а не отказывает: процесс с
// `sslmode=disable` в боевом режиме стартует и рапортует об этом.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ОБЗОР. Отсутствие стража ничем не проявляется: сервис
// собирается, поднимается, отвечает и печатает красивый самоотчёт. Заметить
// можно только сравнением с соседями — то есть ровно тем действием, которое
// никто не совершает, добавляя восьмой сервис.
//
// ЧЕГО ГЕЙТ НЕ СУДИТ. Он не требует одинакового НАБОРА проверяемых осей: у
// шести стражей наборы разные (48…967 строк), и сведение их к одному — предмет
// отдельный, со своей приёмкой. Здесь требуется одно: у объявленных посадочных
// ручек есть читатель, который на небезопасном значении ОТКАЗЫВАЕТ.
//
// ГРАНИЦА ОХВАТА НАЗВАНА, И ОНА ЧЕСТНАЯ. Перепись печатает «объявляют посадочные
// ручки: 4» из семи сервисов, и это не слепота распознавателя: у iam, nlb и vpc
// таких ручек через `envconfig` РОВНО НОЛЬ — они читают настройки иначе
// (`viper`/`koanf`, `os.Getenv`). То есть предмета у этого гейта там нет, а не
// спрятан. Сведение трёх механизмов чтения к одному — предмет крупнее, со своей
// приёмкой; пока он не сделан, гейт судит тех, у кого предмет есть, и говорит об
// этом числом.
//
// ТРЕТЬЯ ФОРМА, КОТОРУЮ ГЕЙТ НЕ ЛОВИТ, И ПОЧЕМУ ЭТО НЕ ДЫРА. Страж может быть
// позван, а результат проигнорирован — `_ = cfg.Validate()` либо вызов без
// проверки ошибки. Формы в дереве нет (предикат:
// `git grep -nE '_ = (cfg|c|conf)\.Validate\(\)' -- services/ ':!*_test.go'` →
// пусто), и естественной она не является: так не пишут ни по невнимательности,
// ни при копировании соседа — это требует намеренного действия. Заводить под неё
// распознаватель значило бы ловить противника вместо случайности.
func TestServiceDeclaringPostureKnobsHasABootGuard(t *testing.T) {
	root := repoRoot(t)

	type svc struct {
		knobs    []string // объявленные посадочные ручки
		guardAt  string   // где ОБЪЯВЛЕН отказ старта
		calledAt string   // где он ПОЗВАН загрузочным путём
		provenAt string   // где ДОКАЗАНО, что он отвергает
	}
	found := map[string]*svc{}

	// Обход включает ПРОБЫ: `trackedGoFiles` их отсекает, и ось доказательства
	// на нём давала ноль у всех семи — то есть требование, которое нельзя было
	// выполнить. Свидетель отказа живёт именно в `_test.go`, поэтому обход берёт
	// состав дерева напрямую.
	all, terr := treecorpus.Under(filepath.Join(root, "services"))
	if terr != nil {
		t.Fatalf("состав дерева: %v", terr)
	}
	for _, abs := range all {
		if !strings.HasSuffix(abs, ".go") {
			continue
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			t.Fatalf("относительный путь %s: %v", abs, err)
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			continue
		}
		name := parts[1]
		if found[name] == nil {
			found[name] = &svc{}
		}
		b, err := os.ReadFile(abs) // #nosec G304 -- путь из индекса git этого модуля
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		src := string(b)

		isTest := strings.HasSuffix(rel, "_test.go")
		if !isTest {
			for _, m := range postureKnobRe.FindAllStringSubmatch(src, -1) {
				k := m[1]
				if k == "" {
					k = m[2]
				}
				found[name].knobs = append(found[name].knobs, k)
			}
		}
		if !isTest && found[name].guardAt == "" && postureGuardRe.MatchString(src) {
			found[name].guardAt = rel
		}
		// ВЫЗОВ стража, а не только его объявление. Гейт, довольный объявлением,
		// удостоверяет стража, который никогда не исполняется, — ровно та форма
		// без содержания, которую он заведён ловить. Найдено на себе: у geo
		// `Validate` был написан и НЕ ПОЗВАН ни одним загрузочным путём.
		if !isTest && found[name].calledAt == "" && postureGuardCallRe.MatchString(src) {
			found[name].calledAt = rel
		}
		// ПРОБА, ДОКАЗЫВАЮЩАЯ ОТКАЗ. Без неё гейт удостоверяет форму: страж
		// `func (c Config) Validate() error { return nil }` при целом вызове
		// проходил — то есть гейт зеленел на ровно той заглушке, ради которой
		// заводился. Найдено внешним аудитом, воспроизведено подстановкой.
		//
		// Судить исполняемое напрямую гейт не может — он читает, а не запускает.
		// Поэтому он требует СВИДЕТЕЛЯ: пробу, которая подаёт небезопасное
		// значение и ждёт ошибки. Заглушка такую пробу не переживёт by
		// construction, и подделать её нельзя, не написав настоящей проверки.
		if found[name].provenAt == "" && isTest &&
			postureGuardCallRe.MatchString(src) && postureRefusalProofRe.MatchString(src) {
			found[name].provenAt = rel
		}
	}

	var names []string
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)

	var findings []string
	withKnobs, withGuard := 0, 0
	for _, n := range names {
		s := found[n]
		if len(s.knobs) == 0 {
			continue
		}
		withKnobs++
		if s.guardAt != "" && s.calledAt != "" && s.provenAt != "" {
			withGuard++
			continue
		}
		if s.guardAt != "" && s.calledAt != "" && s.provenAt == "" {
			findings = append(findings, n+" — страж объявлен и позван, но НИ ОДНА проба не "+
				"доказывает, что он отвергает: `Validate() error { return nil }` прошёл бы этот "+
				"гейт насквозь")
			continue
		}
		if s.guardAt != "" && s.calledAt == "" {
			findings = append(findings, n+" — страж ОБЪЯВЛЕН ("+s.guardAt+
				"), но не позван ни одним загрузочным путём: он не исполняется никогда")
			continue
		}
		sort.Strings(s.knobs)
		uniq := s.knobs[:0]
		seen := map[string]bool{}
		for _, k := range s.knobs {
			if !seen[k] {
				seen[k] = true
				uniq = append(uniq, k)
			}
		}
		findings = append(findings, n+" — объявляет посадочные ручки ("+strings.Join(uniq, ", ")+
			"), а стража, который на них смотрит, не имеет")
	}

	// Объём осмотренного: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного», а обе величины — друг от друга.
	t.Logf("осмотрено сервисов: %d; объявляют посадочные ручки: %d; из них несут стража: %d",
		len(names), withKnobs, withGuard)

	if withKnobs == 0 {
		t.Fatal("предпосылка гейта не выполняется: ни один сервис не объявляет посадочных " +
			"ручек — либо имена ручек сменились, либо распознаватель ослеп; «ноль находок» " +
			"в таком дереве не утверждает ничего")
	}

	if len(findings) > 0 {
		t.Fatalf("ban #16 требует production boot-guard от КАЖДОГО сервиса, "+
			"а объявленные ручки остались без читателя:\n  %s\n\n"+
			"Самоотчёт о посадке стражем НЕ является: он сообщает, а не отказывает. "+
			"Процесс с `sslmode=disable` в боевом режиме стартует и честно об этом "+
			"рапортует.", strings.Join(findings, "\n  "))
	}
}

var (
	// Посадочная ручка — та, чьё значение решает БЕЗОПАСНОСТЬ старта, а не
	// поведение домена. Перечень закрыт намеренно: широкий предикат («любая
	// envconfig-ручка») сделал бы находкой каждый сервисный параметр.
	postureKnobRe = regexp.MustCompile(
		`envconfig:"((?:[A-Z_]*)(?:AUTH_MODE|DB_SSLMODE|TRUSTED_FORWARDER_SANS|TRUST_ANY_FORWARDER))"` +
			`|mapstructure:"([a-z-]*(?:auth-?mode|ssl-?mode|trusted-?forwarder[a-z-]*|trust-any-forwarder))"`)
	// Страж — отказ, привязанный к посадке. Судится ВЫЗОВ отказа, а не наличие
	// файла с именем `validate.go`: у compute страж делегирует в общий
	// `grpcsrv.ForwarderGate`, у storage собирает список проблем сам, и оба
	// законны.
	postureGuardRe = regexp.MustCompile(`(?m)^func \(c Config\) Validate\(\) error|ForwarderGate\{|refuses insecure config`)
	// Вызов стража из загрузочного пути. Судится вызов, а не имя файла: у одних
	// он стоит в `main`, у других в `serve`.
	postureGuardCallRe = regexp.MustCompile(`(?:cfg|c|conf)\.Validate\(\)`)
	// Свидетель отказа: проба ждёт ошибки от стража.
	postureRefusalProofRe = regexp.MustCompile(`err == nil \{|require\.Error\(|assert\.Error\(`)
)
