// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Инъекция анализатора координат в причинах надгробия — обе стороны по каждой оси.
//
// Дерево строится тем же харнессом, что у соседнего гейта (`retiredTinyTree`):
// два сервиса в одном файле стабов, у каждого свой метод. Этого достаточно,
// чтобы близнец отличался от дефекта ТОЛЬКО по существу: имена сервисов
// пересекаются, имена методов пересекаются, файл один и тот же.

func reasonTinyOptions(root string, retired ...RetiredRPC) RetiredReasonOptions {
	return RetiredReasonOptions{Root: root, APIRoot: "pkg/api", Retired: retired}
}

// TestRetiredReasonCoordinates_CatchesDeadCoordinate — ДЕФЕКТ: причина называет
// координату, которой в дереве нет.
//
// Близость подобрана нарочно: метод `Pong` в дереве ЕСТЬ, но у другого сервиса, а
// сервис `AlphaService` в дереве ЕСТЬ, но без такого метода. Гейт, сверяющий одно
// лишь имя метода либо одно лишь имя сервиса, здесь промолчит.
func TestRetiredReasonCoordinates_CatchesDeadCoordinate(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, []string{})
	dead := RetiredRPC{
		FQN:    "kacho.cloud.demo.v1.GammaService/Old",
		Reason: "живым остаётся kacho.cloud.demo.v1.AlphaService/Pong — иди туда",
	}

	findings, census, err := AuditRetiredReasonCoordinates(reasonTinyOptions(root, dead), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("мёртвая координата не поймана: находок %d, координат прочитано %d",
			len(findings), census.Mentions)
	}
	if !strings.Contains(findings[0].String(), "AlphaService/Pong") {
		t.Errorf("находка не называет координату: %s", findings[0].String())
	}
	if !strings.Contains(findings[0].String(), "GammaService/Old") {
		t.Errorf("находка не называет запись, чья это причина: %s", findings[0].String())
	}
}

// TestRetiredReasonCoordinates_SilentOnLiveCoordinate — ЗАКОННЫЙ БЛИЗНЕЦ: та же
// форма фразы, но координата резолвится.
func TestRetiredReasonCoordinates_SilentOnLiveCoordinate(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, []string{})
	alive := RetiredRPC{
		FQN:    "kacho.cloud.demo.v1.GammaService/Old",
		Reason: "живым остаётся kacho.cloud.demo.v1.AlphaService/Ping — иди туда",
	}

	findings, census, err := AuditRetiredReasonCoordinates(reasonTinyOptions(root, alive), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("законная координата вызвала находку — гейт ловит форму, а не существо: %s",
			findings[0].String())
	}
	// Премиса самого контроля: молчание получено на прочитанном, а не на пустом.
	if census.Mentions != 1 || census.Resolved != 1 {
		t.Fatalf("координат прочитано %d, разрешено %d — контроль молчал вхолостую",
			census.Mentions, census.Resolved)
	}
}

// TestRetiredReasonCoordinates_ShortNameResolves — сокращённое имя сервиса
// (без пакета) обязано резолвиться: причины пишут и так, и так.
func TestRetiredReasonCoordinates_ShortNameResolves(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, []string{})
	short := RetiredRPC{FQN: "kacho.cloud.demo.v1.GammaService/Old", Reason: "живой путь — AlphaService/Ping"}

	findings, census, err := AuditRetiredReasonCoordinates(reasonTinyOptions(root, short), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 || census.Resolved != 1 {
		t.Errorf("сокращённое имя не разрешилось: находок %d, разрешено %d", len(findings), census.Resolved)
	}
}

// TestRetiredReasonCoordinates_SuffixIsBySegment — контроль на способ сравнения:
// сопоставление идёт по СЕГМЕНТУ, а не по подстроке. Иначе `AlphaService`
// разрешался бы хвостом чужого имени.
func TestRetiredReasonCoordinates_SuffixIsBySegment(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, []string{})
	substr := RetiredRPC{FQN: "kacho.cloud.demo.v1.GammaService/Old", Reason: "живой путь — SuperAlphaService/Ping"}

	findings, _, err := AuditRetiredReasonCoordinates(reasonTinyOptions(root, substr), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 {
		t.Errorf("подстрочное совпадение принято за разрешение координаты: находок %d", len(findings))
	}
}

// TestRetiredReasonCoordinates_NoCoordinateIsTheGoal — причина БЕЗ координат
// законна и предпочтительна: ноль упоминаний — цель гейта, а не его поломка.
func TestRetiredReasonCoordinates_NoCoordinateIsTheGoal(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, []string{})
	plain := RetiredRPC{
		FQN:    "kacho.cloud.demo.v1.GammaService/Old",
		Reason: "предмета больше нет; чем выражается замысел теперь — сказано в задаче",
	}

	findings, census, err := AuditRetiredReasonCoordinates(reasonTinyOptions(root, plain), nil)
	if err != nil {
		t.Fatalf("причина без координат обязана проходить, а не ронять прогон: %v", err)
	}
	if len(findings) != 0 || census.Mentions != 0 {
		t.Errorf("находок %d, координат %d — ожидались нули на причине без координат",
			len(findings), census.Mentions)
	}
	// Обход при этом НЕ пуст: премиса проверена, а не подразумевается.
	if census.DeclaredMethods == 0 {
		t.Fatal("методов прочитано ноль — молчание получено на пустом обходе")
	}
}

// TestRetiredReasonCoordinates_EmptyLedgerIsAnError — пустая перепись: гейту
// нечего читать, и «ноль находок» тут не вердикт.
func TestRetiredReasonCoordinates_EmptyLedgerIsAnError(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, []string{})
	if _, _, err := AuditRetiredReasonCoordinates(reasonTinyOptions(root), nil); err == nil {
		t.Fatal("пустая перепись прошла как «ноль находок» — гейт инертен и об этом не сообщает")
	}
}

// TestRetiredReasonCoordinates_EmptyStubsIsAnError — вторая половина той же
// премисы: не прочитано ни одного метода ⇒ «координата мертва» было бы получено
// даром для ЛЮБОЙ координаты.
func TestRetiredReasonCoordinates_EmptyStubsIsAnError(t *testing.T) {
	root := t.TempDir()
	dead := RetiredRPC{FQN: "kacho.cloud.demo.v1.GammaService/Old", Reason: "живой путь — AlphaService/Ping"}
	if _, _, err := AuditRetiredReasonCoordinates(reasonTinyOptions(root, dead), nil); err == nil {
		t.Fatal("пустые стабы прошли как «координата мертва» — вердикт получен даром")
	}
}
