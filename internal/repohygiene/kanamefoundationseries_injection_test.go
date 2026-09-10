// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Инъекция разделения форм «имя ряда витрины ≠ клеймо токена» — В ОБЕ СТОРОНЫ И
// ПО КАЖДОЙ ОСИ (#2523).
//
// Осей две, и каждая — отдельный способ ошибиться молча:
//
//  1. КЛАССИФИКАЦИЯ. Токен, объявленный рядом витрины, уходит на границу; тот же
//     токен без собранного перечня остаётся клеймом — дельта миров ОДИН факт,
//     перечень. И законный близнец: настоящее клеймо токена остаётся клеймом ДАЖЕ
//     при собранном перечне, иначе вычитание смело бы полосу целиком.
//  2. ОРАКУЛ. Ряд собирается из ПОЗИЦИИ (`Name:` в объявлении измерителя), а не
//     из подстроки. Без этой оси оракул вычел бы имена схем и таблиц — они носят
//     ту же приставку — и замаскировал бы остаток, ради которого ведомость и
//     заведена.
package repohygiene

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// laneOfOneHit — полоса, которой достался единственный токен корпуса.
func laneOfOneHit(t *testing.T, line string, world residueWorld) string {
	t.Helper()
	hits, _, census, err := FindKanameNameResidue(
		map[string][]byte{"services/iam/docs/content/advanced/observability.mdx": []byte(line)},
		nil, nil, world)
	require.NoError(t, err)
	require.NotZerof(t, census.FilesRead, "обход обязан состояться")
	require.Lenf(t, hits, 1, "мир обязан нести РОВНО одно вхождение: иначе дельта не один факт")
	return hits[0].Lane
}

// seriesWorld — перечень, в котором ряд фундамента ОБЪЯВЛЕН.
func seriesWorld() residueWorld {
	return residueWorld{FoundationSeries: map[string]bool{
		"kacho_grpc_server_handled_total":    true,
		"kacho_grpc_server_handling_seconds": true,
		"kacho_outbox_poisoned_count":        true,
	}}
}

func TestFoundationSeriesInjection_DeclaredSeriesLeavesTheClaimLane(t *testing.T) {
	t.Parallel()
	const line = "| `kacho_grpc_server_handled_total` | counter | доля неуспешных ответов |\n"
	require.Equalf(t, borderFoundationSeries, laneOfOneHit(t, line, seriesWorld()),
		"имя ряда, ОБЪЯВЛЕННОЕ деревом, полосе клейм не принадлежит")
}

func TestFoundationSeriesInjection_WithoutTheRosterItStaysAClaim(t *testing.T) {
	t.Parallel()
	const line = "| `kacho_grpc_server_handled_total` | counter | доля неуспешных ответов |\n"
	require.Equalf(t, laneClaimAssertion, laneOfOneHit(t, line, residueWorld{}),
		"без собранного перечня вычитания нет — и это ровно тот мир, в котором "+
			"полоса клейм получала чужой предмет")
}

func TestFoundationSeriesInjection_ARealClaimStaysAClaim(t *testing.T) {
	t.Parallel()
	const line = "Токен несёт клеймо `kacho_principal_type`, и оно межрепозиторное.\n"
	require.Equalf(t, laneClaimAssertion, laneOfOneHit(t, line, seriesWorld()),
		"ЗАКОННЫЙ БЛИЗНЕЦ: настоящее клеймо обязано остаться клеймом даже при "+
			"собранном перечне — иначе вычитание смело бы полосу целиком")
}

func TestFoundationSeriesInjection_HistogramSuffixIsTheSameSeries(t *testing.T) {
	t.Parallel()
	const line = "Запрос берёт `kacho_grpc_server_handling_seconds_count` за пять минут.\n"
	require.Equalf(t, borderFoundationSeries, laneOfOneHit(t, line, seriesWorld()),
		"в объявлении стоит базовое имя, а дежурный спрашивает ПРОИЗВОДНОЕ: "+
			"хвост гистограммы обязан узнаваться")
}

func TestFoundationSeriesInjection_FullNameIsAskedBeforeTrimming(t *testing.T) {
	t.Parallel()
	// `kacho_outbox_poisoned_count` — счётчик, объявленный ЦЕЛИКОМ; базового
	// `kacho_outbox_poisoned` не существует. Обратный порядок вопросов ошибся бы
	// на нём, не найдя базового имени.
	const line = "Счётчик `kacho_outbox_poisoned_count` растёт на отравленной строке.\n"
	require.Equal(t, borderFoundationSeries, laneOfOneHit(t, line, seriesWorld()))
}

func TestFoundationSeriesOracle_NamePositionIsCollected(t *testing.T) {
	t.Parallel()
	series, filesRead := FoundationSeriesFromCorpus(map[string][]byte{
		"pkg/grpcsrv/latency.go": []byte("package grpcsrv\n\n" +
			"var o = prometheus.CounterOpts{\n\tName: \"kacho_grpc_server_handled_total\",\n}\n"),
	})
	require.Equal(t, 1, filesRead)
	require.True(t, series["kacho_grpc_server_handled_total"], "литерал в позиции имени ряда — ряд")
}

func TestFoundationSeriesOracle_SchemaNameIsNotASeries(t *testing.T) {
	t.Parallel()
	series, filesRead := FoundationSeriesFromCorpus(map[string][]byte{
		"pkg/db/schema.go": []byte("package db\n\n" +
			"const schema = \"kacho_vpc\"\n" +
			"var table = \"kacho_iam_subjects\"\n"),
	})
	require.Equal(t, 1, filesRead)
	require.Emptyf(t, series, "имя схемы и имя таблицы носят ТУ ЖЕ форму; оракул по "+
		"подстроке вычел бы их из своих полос и замаскировал бы остаток")
}

func TestFoundationSeriesOracle_TestFileIsNotADeclaration(t *testing.T) {
	t.Parallel()
	series, filesRead := FoundationSeriesFromCorpus(map[string][]byte{
		"pkg/grpcsrv/latency_test.go": []byte("package grpcsrv\n\n" +
			"var o = prometheus.CounterOpts{\n\tName: \"kacho_made_up_row\",\n}\n"),
	})
	require.Zerof(t, filesRead, "фикстура пробы ряда не объявляет: приняв её за "+
		"объявление, оракул вычел бы выдуманное имя")
	require.Empty(t, series)
}

func TestFoundationSeriesOracle_CommentIsNotADeclaration(t *testing.T) {
	t.Parallel()
	series, filesRead := FoundationSeriesFromCorpus(map[string][]byte{
		"pkg/grpcsrv/doc.go": []byte("package grpcsrv\n\n" +
			"// Измеритель эмитирует Name: \"kacho_prose_only_row\".\n"),
	})
	require.Equal(t, 1, filesRead)
	require.Emptyf(t, series, "комментарий ничего не объявляет: оракул, читающий прозу, "+
		"собрал бы собственное объяснение")
}

func TestFoundationSeriesOracle_EmptyCorpusReadsNothing(t *testing.T) {
	t.Parallel()
	series, filesRead := FoundationSeriesFromCorpus(map[string][]byte{})
	require.Zerof(t, filesRead, "пустой корпус обязан давать НОЛЬ прочитанного — именно "+
		"на это число держатель и роняет прогон")
	require.Empty(t, series)
}

func TestFoundationSeriesPredicate_EmptyRosterSubtractsNothing(t *testing.T) {
	t.Parallel()
	require.Falsef(t, isFoundationMetricSeries(nil, "kacho_grpc_server_handled_total"),
		"пустой перечень означает «не собирали»: вычитание тогда не делается вовсе, "+
			"и молчание не выдаётся за «рядов нет»")
}
