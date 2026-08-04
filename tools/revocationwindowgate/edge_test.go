// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edge_test.go — самопроверка двух предикатов, которыми перепись научилась
// видеть КРАЙ (gateway/): единицы измерения по имени ручки и локальные
// конструкторы кеша вердиктов.
//
// Оба предиката заведены потому, что край держал полноценное окно отзыва и не
// попадал ни в одну проверку: обход начинался с каталога `services/`, а
// конструктор искался по имени `authz.NewCache`. Край лежит в `gateway/` и
// строит свой `newDecisionCache`, поэтому был вне того, что перепись могла
// выразить, — и молчание про него читалось как «чисто».
//
// Каждая проверка идёт парой: настоящий вход из дерева ⇒ находка; законный
// близнец ТОЙ ЖЕ формы ⇒ молчание. Без второй половины предикат ловит форму, а
// не существо.
package revocationwindowgate_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/tools/revocationwindowgate"
)

// ────────────────────────────────────────────────────────────────────────────
// Единица измерения берётся из ИМЕНИ ручки
// ────────────────────────────────────────────────────────────────────────────

// srcEdgeKnob — форма, в которой край объявляет своё окно: целое число секунд
// в envconfig-теге. Именно её прежний разбор прочитать не мог.
const srcEdgeKnob = `package config

type AuthZConfig struct {
	AuthZCacheTTLSeconds int ` + "`" + `envconfig:"KACHO_API_GATEWAY_AUTHZ_CACHE_TTL_SECONDS" default:"5"` + "`" + `
}
`

// srcCredentialKnob — законный близнец: ТА ЖЕ форма, тот же суффикс `_SECONDS`,
// то же значение, соседнее поле той же структуры. Отличается только тем, ЧТО
// кеширует — учётные данные, а не вердикт. Их отзыв идёт другой полосой и
// обязан быть немедленным; сложив их в одну перепись, мы подтолкнули бы
// уменьшать окно грантов ради задачи, которой оно не решает.
const srcCredentialKnob = `package config

type AuthZConfig struct {
	IntrospectionCacheTTLSeconds int ` + "`" + `envconfig:"KACHO_INTROSPECTION_CACHE_TTL_SECONDS" default:"5"` + "`" + `
}
`

func TestScanFile_ReadsTheEdgeWindowInSeconds(t *testing.T) {
	rep := &revocationwindowgate.Report{}
	if err := revocationwindowgate.ScanFile(rep, "api-gateway", "gateway/internal/config/config.go", srcEdgeKnob); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(rep.Sites) != 1 {
		t.Fatalf("настоящее объявление края дало %d площадок; ожидалась 1. "+
			"Разбор, отказавшийся прочитать объявление, не сообщает о пробеле — он молчит, "+
			"и это неотличимо от отсутствия площадки", len(rep.Sites))
	}
	// 5 секунд, а НЕ 5 наносекунд: единица взята из имени ручки, не угадана по цифрам.
	if got := rep.Sites[0].Window.String(); got != "5s" {
		t.Errorf("окно прочитано как %s, ожидалось 5s — единица обязана браться из суффикса имени ручки", got)
	}
	if rep.SitesMatched != 1 {
		t.Errorf("осмотрено площадок = %d, ожидалась 1", rep.SitesMatched)
	}
}

func TestScanFile_SilentOnTheCredentialKnobOfTheSameShape(t *testing.T) {
	rep := &revocationwindowgate.Report{}
	if err := revocationwindowgate.ScanFile(rep, "api-gateway", "gateway/internal/config/config.go", srcCredentialKnob); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(rep.Sites) != 0 {
		t.Errorf("кеш УЧЁТНЫХ ДАННЫХ той же формы дал %d находок — предикат ловит форму "+
			"объявления, а не то, что кеш хранит: %+v", len(rep.Sites), rep.Sites)
	}
	if rep.FilesParsed != 1 {
		t.Errorf("файл не разобран (FilesParsed=%d) — молчание значит «не читал», а не «чисто»", rep.FilesParsed)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Конструктор кеша вердиктов — не одно имя, а НАБОР
// ────────────────────────────────────────────────────────────────────────────

// srcLocalVerdictCache — форма края: собственный конструктор кеша решений над
// общим LRU. Корелибовский `authz.NewCache` здесь не зовётся ни разу, поэтому
// перепись по одному этому имени отвечала «не строит» на процесс, который
// держал окно.
const srcLocalVerdictCache = `package middleware

func newAuthzMiddleware(cfg Config) *AuthzMiddleware {
	return &AuthzMiddleware{
		cache: newDecisionCache(cfg.CacheMaxEntries, cfg.CacheTTL, cfg.Now),
	}
}
`

// srcCredentialLruCache — законный близнец: тот же пакет, тот же примитив LRU,
// тот же вид вызова. Хранит учётные данные, а не вердикт, поэтому окна отзыва
// гранта не создаёт и в переписи ему делать нечего.
const srcCredentialLruCache = `package middleware

func newIntrospectionCache(cfg Config) *IntrospectionCache {
	return &IntrospectionCache{
		cache: lrucache.New[string, IntrospectionResult](cfg.MaxEntries, cfg.TTL, cfg.Now),
	}
}
`

func TestScanConstructors_FindsTheLocalVerdictCache(t *testing.T) {
	found, err := revocationwindowgate.ScanConstructors("gateway/internal/middleware/authz.go", srcLocalVerdictCache)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if !found {
		t.Fatalf("край строит кеш вердиктов собственным конструктором, а перепись ответила «не строит».\n"+
			"Вопрос «строит ли процесс кеш вердиктов» силён тем, что не нуждается в словаре ручек — "+
			"но заданный как «зовёт ли authz.NewCache», он сам становится словарём, только из "+
			"конструкторов. Распознаваемый набор: %v", revocationwindowgate.LocalVerdictCacheCtors())
	}
}

func TestScanConstructors_SilentOnTheCredentialCache(t *testing.T) {
	found, err := revocationwindowgate.ScanConstructors("gateway/internal/middleware/introspection_cache.go", srcCredentialLruCache)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if found {
		t.Error("кеш учётных данных над тем же примитивом LRU засчитан кешем вердиктов — " +
			"предикат ловит примитив, а не то, что в нём лежит; тогда в перепись окон отзыва " +
			"поедут процессы, у которых окна отзыва гранта нет")
	}
}

// TestLocalVerdictCacheCtors_IsNotEmpty — предпосылка набора.
//
// Пустой набор локальных конструкторов делает обе проверки выше тождественно
// молчаливыми, и молчание снова стало бы читаться как «чисто».
func TestLocalVerdictCacheCtors_IsNotEmpty(t *testing.T) {
	if got := revocationwindowgate.LocalVerdictCacheCtors(); len(got) == 0 {
		t.Fatal("набор локальных конструкторов кеша вердиктов пуст — перепись по конструкторам " +
			"вернулась к одному корелибовскому имени, и процесс со своим кешем снова невидим")
	}
}
