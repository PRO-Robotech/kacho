// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// db_credential_probe_covers_every_instance_test.go — ПРОБА КРЕДЕНШЕЛОВ БАЗ
// УТВЕРЖДАЕТ О ВСЕХ БАЗАХ ЗОНТА, А НЕ О ТРЁХ ИЗ ДЕВЯТИ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// `deploy/e2e/0.1/E5-secrets.sh` проверяет, что для каждой базы подчарта bitnami
// существует секрет `<релиз>-pg-<имя>`. Перечень имён был ВЫПИСАН руками и с тех
// пор разошёлся с зонтом в ОБЕ стороны сразу:
//
//	вниз — `resource-manager` упразднён (KAC-124), секрета
//	       `kacho-umbrella-pg-resource-manager` зонт не рендерит ни в одном
//	       профиле: проба утверждала о несуществующем;
//	вверх — баз стало девять, а названо было три: geo, hydra, iam, kratos, nlb,
//	       registry, storage не проверялись вовсе.
//
// Половинное утверждение — худший из двух дефектов: оно ЗЕЛЕНЕЕТ и потому
// читается как покрытие. Снять из перечня одно мёртвое имя и оставить два живых
// значило бы починить видимую половину и оставить невидимую.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ — ПАРНОСТЬ, И ОБЕ ПОЛОВИНЫ НУЖНЫ
//
//	каждое имя пробы объявлено зонтом псевдонимом `pg-<имя>` — иначе проба
//	  утверждает о базе, которой чарт не создаёт;
//	каждый псевдоним `pg-<имя>` зонта назван пробой — иначе база заводится, а
//	  её креденшелы никто не проверяет, и это тоже молчание.
//
// Односторонний предикат ловил бы ровно один из двух сегодняшних дефектов.
//
// ПОЧЕМУ ПЕРЕЧЕНЬ ОСТАЁТСЯ ВЫПИСАННЫМ, А НЕ ВЫВОДИТСЯ В САМОМ СКРИПТЕ: проба
// исполняется на живом стенде и читает кластер, а не дерево; выведи она перечень
// из `Chart.yaml` — сверять стало бы нечего, обе стороны пришли бы из одного
// источника и разошлись бы с ним молча ВМЕСТЕ. Сверку держит дерево, здесь.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА
//
// Условность подчарта (`condition: pg-vpc.enabled`) здесь НЕ судится: проба
// гоняется `make e2e-test` по стенду `make dev-up`, а он поднимается профилем
// `values.dev.yaml`, где включены все девять. Суженный профиль — другой предмет
// и другой владелец (`deploy/scripts/assert-shard-coverage.py`).
package deploy_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// dbCredentialProbe — путь пробы креденшелов относительно `deploy/`.
const dbCredentialProbe = "e2e/0.1/E5-secrets.sh"

// reDBProbeLoop — перечень имён баз в пробе: `for svc in a b c; do`.
//
// Читается ИСПОЛНЯЕМАЯ строка: комментарий, объясняющий перечень, перечнем не
// является.
var reDBProbeLoop = regexp.MustCompile(`^\s*for\s+svc\s+in\s+([^;]+);\s*do\s*$`)

// dbProbeCensus — объём осмотренного.
type dbProbeCensus struct {
	LinesRead int
	Named     int
	Aliases   int
}

func (c dbProbeCensus) String() string {
	return fmt.Sprintf("строк пробы прочитано %d · имён названо пробой %d · "+
		"псевдонимов `pg-*` у зонта %d", c.LinesRead, c.Named, c.Aliases)
}

// dbProbeFinding — расхождение пробы и зонта. Направление названо: односторонний
// предикат поймал бы половину.
type dbProbeFinding struct {
	Alias string
	// Phantom: проба называет базу, которой зонт не объявляет.
	Phantom bool
}

func (f dbProbeFinding) String() string {
	if f.Phantom {
		return fmt.Sprintf("%s: проба %s требует секрет `kacho-umbrella-%s`, а зонт не "+
			"объявляет такого псевдонима — проба утверждает о несуществующем и оттого "+
			"выглядит проверкой. Снимите имя тем же изменением, которым снята база",
			f.Alias, dbCredentialProbe, f.Alias)
	}
	return fmt.Sprintf("%s: зонт объявляет базу псевдонимом, а проба %s её не называет — "+
		"утверждение половинное: зелёное означает «проверены НЕ все креденшелы». "+
		"Допишите имя тем же изменением, которым заведена база",
		f.Alias, dbCredentialProbe)
}

// readDBProbeAliases — псевдонимы, о которых утверждает проба. Имя `x` в цикле
// даёт секрет `<релиз>-pg-x`, то есть псевдоним `pg-x`.
func readDBProbeAliases(path string) ([]string, int, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer fh.Close()

	var out []string
	lines := 0
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		lines++
		text := sc.Text()
		if strings.HasPrefix(strings.TrimLeft(text, " \t"), "#") {
			continue
		}
		m := reDBProbeLoop.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		for _, w := range strings.Fields(m[1]) {
			out = append(out, "pg-"+w)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, lines, err
	}
	sort.Strings(out)
	return out, lines, nil
}

// rePgAlias — псевдоним базы у зависимости зонта.
var rePgAlias = regexp.MustCompile(`^\s+alias:\s+"?(pg-[A-Za-z0-9._-]+)"?\s*$`)

// umbrellaPgAliases — базы, которые объявляет зонт.
func umbrellaPgAliases(umbrella string) ([]string, error) {
	fh, err := os.Open(filepath.Join(umbrella, "Chart.yaml"))
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	var out []string
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		text := sc.Text()
		if strings.HasPrefix(strings.TrimLeft(text, " \t"), "#") {
			continue
		}
		if m := rePgAlias.FindStringSubmatch(text); m != nil {
			out = append(out, m[1])
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// judgeDBCredentialProbe — парность в обе стороны.
func judgeDBCredentialProbe(named, aliases []string, lines int) ([]dbProbeFinding, dbProbeCensus) {
	census := dbProbeCensus{LinesRead: lines, Named: len(named), Aliases: len(aliases)}

	inProbe := map[string]bool{}
	for _, n := range named {
		inProbe[n] = true
	}
	inChart := map[string]bool{}
	for _, a := range aliases {
		inChart[a] = true
	}

	var findings []dbProbeFinding
	for _, n := range named {
		if !inChart[n] {
			findings = append(findings, dbProbeFinding{Alias: n, Phantom: true})
		}
	}
	for _, a := range aliases {
		if !inProbe[a] {
			findings = append(findings, dbProbeFinding{Alias: a})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Phantom != findings[j].Phantom {
			return findings[i].Phantom
		}
		return findings[i].Alias < findings[j].Alias
	})
	return findings, census
}

// TestDBCredentialProbeCoversEveryInstance — проба креденшелов и зонт называют
// одни и те же базы.
//
// Прогон одной командой:
//
//	go test ./deploy/ -run TestDBCredentialProbeCoversEveryInstance -count=1 -v
func TestDBCredentialProbeCoversEveryInstance(t *testing.T) {
	t.Parallel()

	named, lines, err := readDBProbeAliases(dbCredentialProbe)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: проба %s не прочитана: %v", dbCredentialProbe, err)
	}
	aliases, err := umbrellaPgAliases(filepath.Join("helm", "umbrella"))
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: объявления зонта не прочитаны: %v", err)
	}

	findings, census := judgeDBCredentialProbe(named, aliases, lines)
	t.Logf("перепись: %s", census)

	if census.Named == 0 {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: проба %s не называет НИ ОДНОЙ базы "+
			"(строк прочитано %d) — предикат перестал видеть свой предмет, и «находок "+
			"ноль» означало бы «ноль прочитанного»", dbCredentialProbe, census.LinesRead)
	}
	if census.Aliases == 0 {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: зонт не объявляет ни одного псевдонима "+
			"`pg-*` — сверять не с чем (имён у пробы %d)", census.Named)
	}

	for _, f := range findings {
		t.Error(f)
	}
	if len(findings) == 0 {
		t.Logf("проба и зонт сходятся поимённо: баз %d", census.Aliases)
	}
}
