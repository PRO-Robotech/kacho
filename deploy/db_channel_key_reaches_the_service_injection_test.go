// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// db_channel_key_reaches_the_service_injection_test.go — доказательство того,
// что соседняя проверка СПОСОБНА упасть, и что она молчит на законном близнеце.
//
// Без него пустой список находок на дереве неотличим от предиката, который не
// умеет находить ничего: обе стороны печатают ноль. Каждая ось вносится
// ОТДЕЛЬНО и меняет РОВНО ОДИН факт против своего положительного близнеца —
// иначе красное могло бы приходить от соседней оси, и доказательством оно бы
// не было.
package deploy_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// parseSyntheticConfigPackage — синтетический пакет настроек в том виде, в
// каком его читает проверка дерева: разобранными файлами, а не текстом.
//
// Разбор, а не подстрока: сборщик опознаётся по литералу в ТЕЛЕ функции и по
// узлу-селектору, и проверка, судящая текст, приняла бы за чтение поля его
// упоминание в комментарии.
func parseSyntheticConfigPackage(t *testing.T, src string) []*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synthetic.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("разобрать синтетику: %v", err)
	}
	return []*ast.File{f}
}

// devProdChannelBlock — синтетический блок настроек той же формы, что в дереве:
// ключ канала подставлен из ручки, рядом стоят комментарий про ту же ручку и
// ключ, подставленный из другой.
const devProdChannelBlock = `  config.yaml: |
    logger:
      level: "INFO"

    repository:
      postgres:
        url: {{ printf "postgres://%s@%s:%v/%s" .Values.db.user .Values.db.host .Values.db.port .Values.db.name | quote }}
        # Что sslmode обязан быть НЕ открытым на боевом стеке, держит соседняя проверка.
        ssl-mode: {{ required "sslMode не задан" .Values.config.repository.postgres.sslMode | quote }}
        password-from-env: KANAME_DB_PASSWORD

    authn:
      mode: "production-strict"
`

func channelKeysOf(t *testing.T, block string) []configKeyRef {
	t.Helper()
	lines, base, ok := configBlockLines(block)
	if !ok {
		t.Fatalf("блок `config.yaml: |` не найден в синтетике — фикстура не описывает " +
			"того мира, ради которого заведена")
	}
	return dbChannelKeys(configKeyRefs(lines, base))
}

// TestDBChannelKeyPredicate_FindsTheKeyAndItsPath — положительный контроль
// распознавателя: ключ канала найден, и найден ВМЕСТЕ С ПУТЁМ.
//
// Путь — половина предмета: проверка резолвит его по тегам структуры, поэтому
// распознаватель, вернувший верный ключ с потерянным путём, дал бы ложное
// красное на исправном дереве.
func TestDBChannelKeyPredicate_FindsTheKeyAndItsPath(t *testing.T) {
	keys := channelKeysOf(t, devProdChannelBlock)
	if len(keys) != 1 {
		t.Fatalf("ключей канала распознано %d, ожидался 1: %v", len(keys), keys)
	}
	if got := keys[0].dotted(); got != "repository.postgres.ssl-mode" {
		t.Errorf("путь ключа канала прочитан как %q, ожидался repository.postgres.ssl-mode", got)
	}
}

// TestDBChannelKeyPredicate_IgnoresProseAndForeignKnobs — законные близнецы той
// же формы: молчание.
//
// Оба реальны в дереве. Комментарий про ту же ручку стоит вплотную к ключу (его
// объяснение), а соседние ключи подставлены из других ручек. Распознаватель,
// судящий по слову, а не по подстановке, красил бы оба.
func TestDBChannelKeyPredicate_IgnoresProseAndForeignKnobs(t *testing.T) {
	block := `  config.yaml: |
    repository:
      postgres:
        # ssl-mode обязан быть объявлен явно: пустое значение дало бы открытый текст.
        url: {{ .Values.config.repository.postgres.url | quote }}
        max-conns: {{ .Values.config.repository.postgres.maxConns }}
`
	if keys := channelKeysOf(t, block); len(keys) != 0 {
		t.Errorf("распознаватель покраснел на законной конструкции — проза про ручку и "+
			"соседние ключи ключом канала не являются: %v", keys)
	}
}

// TestDBChannelKeyPredicate_SurvivesRenamingTheSettingsKey — ДЕФЕКТ, ради
// которого проверка заведена: ключ файла настроек переименован, а служба
// читает прежний.
//
// Одно-фактность: от положительного близнеца этот мир отличается ровно именем
// ЛИСТОВОГО ключа. Распознаватель обязан найти его по-прежнему (иначе
// переименование выводило бы ключ из-под наблюдения — то есть проверка ловила
// бы форму, а не предмет), а резолв обязан оборваться и назвать сегмент.
func TestDBChannelKeyPredicate_SurvivesRenamingTheSettingsKey(t *testing.T) {
	renamed := strings.Replace(devProdChannelBlock, "ssl-mode:", "sslmode:", 1)
	keys := channelKeysOf(t, renamed)
	if len(keys) != 1 {
		t.Fatalf("переименованный ключ канала не распознан (%d) — проверка ослепла бы "+
			"ровно на том дефекте, ради которого заведена", len(keys))
	}
	if got := keys[0].dotted(); got != "repository.postgres.sslmode" {
		t.Fatalf("путь переименованного ключа прочитан как %q", got)
	}

	idx := devProdTagIndex()
	if _, depth, ok := resolveTagPath(idx, "Config", keys[0].path); ok {
		t.Error("переименованный ключ объявлен резолвящимся — випер отбросил бы его молча, " +
			"а проверка отчиталась бы зелёным")
	} else if depth != 2 {
		t.Errorf("резолв оборвался на глубине %d, ожидалась 2 (сегмент листа) — координата "+
			"находки называла бы читателю не тот сегмент", depth)
	}
}

// devProdTagIndex — теги той же формы, что у службы в дереве.
func devProdTagIndex() tagIndex {
	return tagIndex{
		"Config": {
			"repository": {name: "Repository", typeName: "RepositoryConfig"},
			"authn":      {name: "AuthN", typeName: "AuthNConfig"},
		},
		"RepositoryConfig": {
			"postgres": {name: "Postgres", typeName: "PostgresConfig"},
		},
		"PostgresConfig": {
			"url":      {name: "URL", typeName: "string"},
			"ssl-mode": {name: "SSLMode", typeName: "string"},
		},
	}
}

// TestResolveTagPath_PositiveAndDegenerate — положительный контроль резолва и
// вырожденный вход.
func TestResolveTagPath_PositiveAndDegenerate(t *testing.T) {
	idx := devProdTagIndex()
	fld, depth, ok := resolveTagPath(idx, "Config", []string{"repository", "postgres", "ssl-mode"})
	if !ok {
		t.Fatalf("законная цепочка тегов объявлена неразрешимой (глубина %d) — проверка "+
			"краснела бы на исправном дереве и была бы снята первой же правкой", depth)
	}
	if fld.name != "SSLMode" {
		t.Errorf("резолв дал поле %q, ожидалось SSLMode", fld.name)
	}

	// Вырожденный вход: пустой индекс НЕ имеет права выглядеть «всё в порядке» —
	// иначе служба, чью структуру не удалось прочитать, проходила бы молча.
	if _, _, ok := resolveTagPath(tagIndex{}, "Config", []string{"repository"}); ok {
		t.Error("пустой индекс тегов объявлен разрешающим путь — проверка судила бы " +
			"структуру, которой не прочитала")
	}

	// Обрыв на СЕРЕДИНЕ пути (переименована секция, а не лист) обязан называть
	// свой сегмент, а не лист: иначе координата находки посылает читателя не туда.
	if _, depth, ok := resolveTagPath(idx, "Config", []string{"repository", "pg", "ssl-mode"}); ok {
		t.Error("путь через несуществующую секцию объявлен разрешимым")
	} else if depth != 1 {
		t.Errorf("обрыв на середине дал глубину %d, ожидалась 1", depth)
	}
}

// TestPopulationDictionaryRecognisesTheRenamedPartAndNotAStranger — коуплинг
// отбора популяции со словарём имён продукта.
//
// Ось названа отдельно, потому что именно она сломалась молча: пока отбор шёл
// литералом приставки, часть, назвавшая себя своим именем, выпадала из
// популяции — и «ноль находок» о ней не означало ничего.
func TestPopulationDictionaryRecognisesTheRenamedPartAndNotAStranger(t *testing.T) {
	if dir, mine := productnaming.ServiceDir("kaname"); !mine || dir != "iam" {
		t.Errorf("словарь имён продукта не признаёт часть, назвавшую себя своим именем: "+
			"ServiceDir(%q) = (%q, %v) — отбор популяции снова ослеп бы на ней", "kaname", dir, mine)
	}
	if dir, mine := productnaming.ServiceDir("opa"); mine {
		t.Errorf("словарь имён продукта признал ЧУЖОЙ процесс своим: ServiceDir(%q) = (%q, %v) — "+
			"проверка требовала бы разбора его настроек нашей структурой", "opa", dir, mine)
	}
}

// TestDSNComposerReads_BothDirectionsAndAbsence — три исхода оси «сборщик
// строки читает это поле», и третий не сливается с первыми двумя.
func TestDSNComposerReads_BothDirectionsAndAbsence(t *testing.T) {
	reads := parseSyntheticConfigPackage(t, `package config

func (c Config) composeDSN(raw string) string {
	mode := c.Repository.Postgres.SSLMode
	return raw + "?sslmode=" + mode
}
`)
	if ok, found := dsnComposerReads(reads, "SSLMode"); !found || !ok {
		t.Errorf("сборщик, читающий поле, объявлен не читающим (reads=%v, found=%v) — "+
			"проверка краснела бы на исправном коде", ok, found)
	}

	// ДЕФЕКТ: одно-фактное отличие — сборщик читает СОСЕДНЕЕ поле.
	wrong := parseSyntheticConfigPackage(t, `package config

func (c Config) composeDSN(raw string) string {
	mode := c.Repository.Postgres.SlaveURL
	return raw + "?sslmode=" + mode
}
`)
	if ok, found := dsnComposerReads(wrong, "SSLMode"); !found || ok {
		t.Errorf("сборщик, читающий другое поле, объявлен читающим нужное (reads=%v, found=%v) — "+
			"величина жила бы в структуре и не попадала в строку подключения", ok, found)
	}

	// ТРЕТИЙ ИСХОД: сборщика в пакете нет вовсе. Это НЕ «поле не читается» —
	// ось просто не судится, и перепись обязана назвать её отдельным числом.
	none := parseSyntheticConfigPackage(t, `package config

func (c Config) DSN() string { return c.Repository.Postgres.URL }
`)
	if ok, found := dsnComposerReads(none, "SSLMode"); found || ok {
		t.Errorf("отсутствие сборщика прочитано как вердикт (reads=%v, found=%v) — "+
			"«не судилось» слилось бы с «судилось и прошло»", ok, found)
	}
}

// TestConfigBlockLines_AbsentBlockIsNotAnEmptyOne — блока настроек нет вовсе:
// это отказ вызывающего, а не «ноль ключей».
func TestConfigBlockLines_AbsentBlockIsNotAnEmptyOne(t *testing.T) {
	if _, _, ok := configBlockLines("apiVersion: v1\nkind: ConfigMap\ndata:\n  other.yaml: |\n    a: b\n"); ok {
		t.Error("манифест без блока `config.yaml: |` объявлен несущим его — шаблон, " +
			"переставший рендерить настройки, читался бы как шаблон без находок")
	}
	if _, _, ok := configBlockLines(devProdChannelBlock); !ok {
		t.Error("блок синтетики не распознан — фикстура не описывает того мира, ради " +
			"которого заведена")
	}
}
