// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// metric_series_names_test.go — ВТОРОЕ значение приставки продукта: имя ряда витрины.
//
// # Зачем это здесь
//
// Соседняя проба (`retired_type_names_test.go`) судит `kaname_<...>` на страницах как имя
// типа провайдера и сама предупреждала: «у семейства службы доступа такой неоднозначности
// сегодня нет — предикат назван ниже, и он же обязан покраснеть, когда она появится».
// Она появилась: опубликованная страница наблюдаемости называет ряды витрины, и они носят
// ту же приставку — `kaname_authz_check_duration_seconds`, `kaname_lro_inflight`,
// `kaname_db_pool_idle_conns`. Проба покраснела ровно как задумана.
//
// Исход выбран тот, который она же и предписывает: предикат узнавания ПЕРЕЕЗЖАЕТ вместе с
// неоднозначностью, а не снимается. Здесь собирается второе значение приставки, и сосед
// вычитает его из своих кандидатов.
//
// # Почему НЕ список исключений и НЕ послабление по странице
//
// Список прощённых токенов пришлось бы вести руками, и он пережил бы свой предмет. Прощение
// целой страницы («на этой странице типов не бывает») маскирует и будущий фантом на ней.
// Здесь дозволение СТРУКТУРНОЕ: токен не судится как тип ровно тогда, когда дерево
// ОБЪЯВЛЯЕТ его рядом витрины.
//
// # Откуда берутся имена — из двух мест, и оба обязательны
//
//  1. объявления в каталоге наблюдаемости службы: литерал `"kaname_x"` и склейка
//     `Namespace + "_x"`. Обход сужен до этого каталога намеренно: тот же литерал,
//     встреченный где угодно в дереве, мог бы оказаться снятым именем типа, и широкий
//     обход маскировал бы фантом, ради которого сосед и написан;
//  2. имена, которые фундамент СОБИРАЕТ ИЗ ЧАСТЕЙ (`BuildFQName`): разбором текста они не
//     восстановимы — приставка приходит доводом. Их отдаёт сам коллектор тем же
//     объявлением, которым отдаёт их реестру.
//
// # Контроль в обе стороны — рядом, а не «когда-нибудь»
//
// Набор обязан быть непуст (иначе вычитание ничего не делает и сосед краснеет по-старому) и
// обязан НЕ ПЕРЕСЕКАТЬСЯ с реестром типов провайдера (иначе он маскировал бы действующий
// тип). Оба утверждения проверяются пробой ниже.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"

	"github.com/prometheus/client_golang/prometheus"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/treecorpus"
)

// metricsNamespace — приставка имён рядов службы доступа. Та же строка, которой продукт
// называет себя; совпадение с приставкой типов провайдера и есть предмет этого файла.
const metricsNamespace = "kaname"

// observabilityDeclarationDir — каталог, объявляющий ряды витрины службы, относительно
// корня дерева. Координата, а не признак: предмет ровно один, и переезд обязан покраснеть
// здесь, а не завести слепую зону молча.
const observabilityDeclarationDir = "services/iam/internal/observability"

var (
	// reSeriesLiteral — имя ряда, записанное литералом целиком.
	reSeriesLiteral = regexp.MustCompile(`"(` + metricsNamespace + `_[a-z0-9_]+)"`)
	// reSeriesJoined — имя ряда, склеенное с объявленной приставкой.
	reSeriesJoined = regexp.MustCompile(`Namespace\s*\+\s*"(_[a-z0-9_]+)"`)
)

// productMetricSeriesNames — имена рядов витрины, ОБЪЯВЛЕННЫЕ деревом.
//
// Второе возвращаемое — объём осмотренного: файлов прочитано. Ноль означает, что обход не
// состоялся, и «рядов не нашлось» тогда значит «не искали».
func productMetricSeriesNames(root string) (map[string]bool, int, error) {
	out := map[string]bool{}
	filesRead := 0

	// Состав берётся у ИНДЕКСА репозитория, а не обходом диска: под каталогами служб на
	// всякой машине, где поднимали стенд или собирали фронтенд, лежит игнорируемое —
	// распаковки чартов, сборочные каталоги, отчёты прогонов, — и обход диска читал бы их
	// наравне с деревом.
	files, err := treecorpus.Under(root)
	if err != nil {
		return nil, 0, err
	}
	prefix := filepath.Join(root, filepath.FromSlash(observabilityDeclarationDir)) + string(filepath.Separator)
	for _, path := range files {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		body, rerr := os.ReadFile(path) // #nosec G304 -- путь пришёл из индекса репозитория
		if rerr != nil {
			return nil, filesRead, rerr
		}
		filesRead++
		s := string(body)
		for _, m := range reSeriesLiteral.FindAllStringSubmatch(s, -1) {
			out[m[1]] = true
		}
		for _, m := range reSeriesJoined.FindAllStringSubmatch(s, -1) {
			out[metricsNamespace+m[1]] = true
		}
	}

	// Имена, собираемые фундаментом из частей. Пул нулевой намеренно: объявления коллектор
	// отдаёт независимо от того, настроен ли пул.
	descs := make(chan *prometheus.Desc, 64)
	go func() {
		coredb.NewPoolStatsCollector(metricsNamespace, "primary", nil).Describe(descs)
		close(descs)
	}()
	reFQ := regexp.MustCompile(`\b` + metricsNamespace + `_[a-z0-9_]+\b`)
	for d := range descs {
		if m := reFQ.FindString(d.String()); m != "" {
			out[m] = true
		}
	}
	return out, filesRead, nil
}

// derivedSeriesSuffixes — хвосты, которые Prometheus дописывает к ряду гистограммы. В
// объявлении их нет: объявлено базовое имя, а запрос дежурного берёт производное.
var derivedSeriesSuffixes = []string{"_bucket", "_sum", "_count"}

// isProductMetricSeries — объявлен ли токен рядом витрины.
//
// ПОЛНОЕ имя спрашивается ПЕРВЫМ, и только затем — имя без хвоста гистограммы. Обратный
// порядок ошибается на настоящем ряде, чьё имя оканчивается так же: `kaname_outbox_poisoned_count`
// — счётчик, а не производное от несуществующего `kaname_outbox_poisoned`.
func isProductMetricSeries(series map[string]bool, token string) bool {
	if series[token] {
		return true
	}
	for _, suffix := range derivedSeriesSuffixes {
		if strings.HasSuffix(token, suffix) && series[strings.TrimSuffix(token, suffix)] {
			return true
		}
	}
	return false
}

// TestMetricSeriesNamesAreDistinctFromProviderTypes — предпосылка вычитания, в обе стороны.
//
// Набор непуст (иначе сосед судил бы ряды витрины как типы, а вычитание было бы вакуумным)
// и не пересекается с реестром типов (иначе он маскировал бы действующий тип — то есть
// ровно ту находку, ради которой сосед написан).
func TestMetricSeriesNamesAreDistinctFromProviderTypes(t *testing.T) {
	root := repoTreeRoot(t)

	// КАТАЛОГ ОБЪЯВЛЕНИЙ ЖИВЁТ У СЛУЖБЫ, А СЛУЖБА ВЫНЕСЕНА.
	//
	// Имена рядов витрины объявляет сама служба доступа, и после её выноса
	// отдельным репозиторием (задача #1111) каталога объявлений в этом дереве
	// нет. Требовать непустого обхода значило бы требовать координату, которой
	// не существует: проба краснела бы на верно исполненном разрезе.
	//
	// Утверждение при этом НЕ ослаблено до «как получится»: пока каталог здесь,
	// обе стороны проверяются как прежде. Нет каталога — проверять нечего, и
	// это ПЕЧАТАЕТСЯ, а не проглатывается зелёным: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного». Возврат исходников возвращает и замер.
	if !productnaming.SourcesInThisTree("iam") {
		t.Logf("каталог объявлений %s в этом дереве отсутствует: исходники службы "+
			"вынесены отдельным репозиторием. Имена рядов витрины здесь НЕ СОБИРАЮТСЯ, "+
			"и непересечение их с реестром типов провайдера НЕ ИЗМЕРЯЕТСЯ — молчание "+
			"по этой оси не означает «пересечений нет»", observabilityDeclarationDir)
		return
	}

	series, filesRead, err := productMetricSeriesNames(root)
	if err != nil {
		t.Fatalf("сбор имён рядов: %v", err)
	}
	if filesRead == 0 {
		t.Fatalf("обход каталога %s не прочитал ни одного файла — «рядов нет» означало бы "+
			"«не искали», и вычитание стало бы вакуумным", observabilityDeclarationDir)
	}
	if len(series) == 0 {
		t.Fatal("рядов витрины не собрано ни одного — вычитание ничего не делает")
	}

	known := registeredProviderTypeNames(t)
	if len(known) == 0 {
		t.Fatal("реестр провайдера пуст — сверять не с чем")
	}

	var clash []string
	for name := range series {
		if known[name] {
			clash = append(clash, name)
		}
	}
	sort.Strings(clash)
	for _, name := range clash {
		t.Errorf("%s — И ряд витрины, И действующий тип провайдера.\n"+
			"Вычитание рядов из кандидатов замаскировало бы этот тип: страница, назвавшая "+
			"его с опечаткой, прошла бы молча. Одно из двух имён обязано смениться, и "+
			"решает это человек, а не проба.", name)
	}

	t.Logf("осмотрено: файлов объявления %d, рядов витрины %d, типов в реестре %d, "+
		"пересечение %d", filesRead, len(series), len(known), len(clash))

	_ = context.Background
}
