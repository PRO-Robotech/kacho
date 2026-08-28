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
// Запрет объявлен один, а исполняется семью разными способами.
//
// ЧТО НАЙДЕНО ЗАВЕДЕНИЕМ ГЕЙТА, И ЭТО НЕ ОПЕЧАТКА. geo объявлял `DBSSLMode` с
// умолчанием `disable`, `AuthMode`, обе пары mTLS — каждую с комментарием про
// боевой режим, — и не проверял НИ ОДНУ. Самоотчёт о посадке (`bootPosture`) и
// разбор режима были, но самоотчёт сообщает, а не отказывает: процесс с
// `sslmode=disable` в боевом режиме стартовал и рапортовал об этом. Страж
// заведён тем же изменением, что и гейт; число в переписи — ЕГО вывод на
// сегодняшнем дереве, а не переписанное здесь утверждение.
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
// ГРАНИЦА ОХВАТА НАЗВАНА ЧИСЛОМ, А НЕ СЛОВОМ. Перепись печатает ДВЕ величины —
// сколько сервисов осмотрено и сколько из них объявляют посадочные ручки, —
// потому что одно число скрывает ровно тот случай, ради которого гейт заведён:
// «ноль находок» становится неотличимо от «ноль прочитанного».
//
// Здесь стояло «объявляют посадочные ручки: 4 из семи; у iam, nlb и vpc их
// РОВНО НОЛЬ». Это было верно для распознавателя, знавшего ОДНУ форму записи
// (`envconfig`), и перестало быть верным, когда он научился второй
// (`mapstructure`): три сервиса читают настройки через `viper`/`koanf`, и их
// ручки не отсутствовали — они были ВНЕ НАБЛЮДЕНИЯ. Форма, о которой
// распознаватель не знает, не даёт ни красного, ни зелёного; она молчит
// (`testing.md` §«Гейт на класс», п.7). Число не переписывается сюда впредь:
// его печатает прогон.
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

	// ТРИ ОСИ СВЯЗАНЫ ОДНИМ ПАКЕТОМ, А НЕ СЧИТАЮТСЯ ПОРОЗНЬ. `Config.Validate()`
	// в дереве объявляют и мигратор, и доменные пакеты: у iam таких объявлений
	// три, у nlb и vpc по два. Гейт, берущий «первого попавшегося стража»,
	// записывал бы каталог произвольно и требовал свидетеля не там — а зелёное
	// держалось бы совпадением. Поэтому сервис считается защищённым, только если
	// НАЙДЁТСЯ ОДИН пакет, в котором сходятся все три: объявлены посадочные
	// ручки · объявлен страж · есть проба, доказывающая отказ.
	type svc struct {
		knobs    []string        // объявленные посадочные ручки (для переписи)
		calledAt string          // где страж ПОЗВАН загрузочным путём
		knobDirs map[string]bool // пакеты, объявляющие посадочные ручки
		guards   map[string]bool // пакеты, объявляющие стража
		proofs   map[string]bool // пакеты со свидетелем отказа
		anyGuard string          // любое объявление стража — для внятной находки
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
			found[name] = &svc{
				knobDirs: map[string]bool{},
				guards:   map[string]bool{},
				proofs:   map[string]bool{},
			}
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
				found[name].knobDirs[filepath.Dir(rel)] = true
			}
		}
		if !isTest && postureGuardRe.MatchString(src) {
			found[name].guards[filepath.Dir(rel)] = true
			if found[name].anyGuard == "" {
				found[name].anyGuard = rel
			}
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
		// Свидетель обязан жить В ТОМ ЖЕ ПАКЕТЕ, что и страж. Это структурное
		// требование, а не текстовое: проба в доменном пакете судит доменную
		// проверку, и засчитывать её за доказательство посадки — то же самое, что
		// принимать чужой отказ за свой. Граница слова выше закрывает совпадение
		// по имени переменной; общий пакет — совпадение по смыслу.
		if isTest && postureGuardCallRe.MatchString(src) && postureRefusalProofRe.MatchString(src) {
			found[name].proofs[filepath.Dir(rel)] = true
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
		// Пакет, в котором сходятся ручки и страж, — единственный, чья проба
		// является свидетельством ПОСАДКИ, а не доменной проверки.
		guarded, guardedUnproven := false, false
		for d := range s.knobDirs {
			if !s.guards[d] {
				continue
			}
			if s.proofs[d] {
				guarded = true
				break
			}
			guardedUnproven = true
		}
		if guarded && s.calledAt != "" {
			withGuard++
			continue
		}
		if guardedUnproven && s.calledAt != "" {
			findings = append(findings, n+" — страж объявлен и позван, но НИ ОДНА проба не "+
				"доказывает, что он отвергает: `Validate() error { return nil }` прошёл бы этот "+
				"гейт насквозь")
			continue
		}
		if (guarded || guardedUnproven) && s.calledAt == "" {
			findings = append(findings, n+" — страж ОБЪЯВЛЕН ("+s.anyGuard+
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
	// Граница слова обязательна: без неё образец совпадал с `hc.Validate()`,
	// `svc.Validate()`, `loc.Validate()`, `rec.Validate()` — то есть с доменными
	// проверками, к посадке отношения не имеющими. Ось доказательства при этом
	// «держалась» у пяти сервисов из семи ЛОЖНЫМИ свидетелями: снятие настоящей
	// пробы nlb гейт не заметил.
	postureGuardCallRe = regexp.MustCompile(`\b(?:cfg|c|conf)\.Validate\(\)`)
	// Свидетель отказа: проба ждёт ошибки от стража.
	postureRefusalProofRe = regexp.MustCompile(`err == nil \{|require\.Error\(|assert\.Error\(`)
)
