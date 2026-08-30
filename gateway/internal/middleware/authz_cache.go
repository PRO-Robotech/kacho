// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authz_cache.go — the per-RPC authz decision cache and its cache-key builder.
//
// Extracted from authz.go (which had grown to a 1200+-line god-file mixing
// interceptors, the decision engine and the cache). The eviction/TTL/LRU
// mechanics now live in the shared internal/lrucache primitive; this file only
// carries the authz-specific policy: the cached decision shape, the
// subject-prefix invalidation used by session-revocation pushes, the
// write-after-invalidate epoch guard wiring, and the deterministic cache key.
package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/lrucache"
)

// decisionCacheEntry — форма кешируемого вердикта. Полей у неё нет НАМЕРЕННО:
// кешируется ТОЛЬКО «разрешено», и сам факт живой записи это и означает.
//
// Отдельного поля «разрешено» здесь быть не должно, иначе отказ снова станет
// представимым — а он не имеет права попадать в этот кеш. Срок жизни записи
// объявлен политикой `pkg/authz.RevocationPolicy` как ОКНО ОТЗЫВА, и это
// обоснование целиком про положительный вердикт: сколько продолжает проходить
// тот, у кого право уже отобрали. На отказе то же число не защищает ничего
// (отказ и так fail-closed) и работает второй, необъявленной стороной — как
// ЗАДЕРЖКА ВЫДАЧИ: запрос, проигравший гонку материализации, сам записывал бы
// свой отказ и держал его весь срок уже ПОСЛЕ появления права.
//
// Ровно так же и по той же причине устроен кеш общей библиотеки
// (`pkg/authz.Cache`): «Кешируются ТОЛЬКО allowed=true». Край — последняя
// площадка, которая от этого правила отступала.
type decisionCacheEntry struct{}

// decisionCache — LRU-with-TTL over the shared lrucache primitive, plus the
// authz-specific subject-prefix invalidation. Safe for concurrent use.
type decisionCache struct {
	c *lrucache.Cache[string, decisionCacheEntry]
}

func newDecisionCache(maxSize int, ttl time.Duration, now func() time.Time) *decisionCache {
	return &decisionCache{c: lrucache.New[string, decisionCacheEntry](maxSize, ttl, now)}
}

// allowed сообщает, есть ли живая запись, то есть закеширован ли положительный
// вердикт. Второго исхода у этого кеша нет by construction.
func (c *decisionCache) allowed(key string) bool { _, ok := c.c.Get(key); return ok }

func (c *decisionCache) put(key string) { c.c.Put(key, decisionCacheEntry{}) }

// generation snapshots the invalidation generation for the epoch guard; see
// putIfGen.
func (c *decisionCache) generation() uint64 { return c.c.Generation() }

// putIfGen stores the allow only when the cache generation still equals the
// snapshot the caller captured at get()-miss time. If an
// Invalidate ran in between, the (potentially stale,
// pre-revocation) write is discarded — closing the write-after-invalidate race
// where a Check computed against a pre-revocation grant would otherwise
// re-populate a just-flushed entry and survive for the whole TTL
// (CWE-362 + CWE-613).
func (c *decisionCache) putIfGen(key string, gen uint64) {
	c.c.PutIfGen(key, decisionCacheEntry{}, gen)
}

// Invalidate removes ALL cache entries — used by the LISTEN/NOTIFY
// session_revocations push safety-net. Bumps the generation.
func (c *decisionCache) Invalidate() { c.c.Invalidate() }

// ПООБЪЕКТНОГО СБРОСА ПО СУБЪЕКТУ ЗДЕСЬ НЕТ — снят вместе с портом, который его
// отдавал (задача #1024). Единственным вызывающим была служба, которой iam гасил
// кэш края; направление развёрнуто, и сброс остался один — целиком.
//
// Вместе с ним снята и ПРИСТАВКА КЛЮЧА: ключ несёт субъект открытым текстом
// только затем, чтобы сброс мог отобрать записи по началу строки. Оставить
// приставку значило бы держать форму ради отбора, которого больше нет, — и
// объяснять её комментарием, указывающим на снятое.

// Size returns the number of live cache entries.
func (c *decisionCache) Size() int { return c.c.Len() }

// buildCacheKey — stable cache key over (subject, action, resource,
// principal-binding context). Including `acr`/`mfa_at`/`client_ip`/
// `device_id`/`passkey_aaguid`/`device_attestation`/`amr_claims` ensures
// step-up AND condition-input changes invalidate naturally; excluding
// `current_time`/`jti`/`dpop_jkt`/`auth_time` avoids per-request cache busts
// for fields that are either enforced independently (dpop_jkt/auth_time via
// the DPoP-replay cache and mfa-staleness gate) or intentionally volatile
// per-request (current_time/jti).
func buildCacheKey(subject, action, resourceType, resourceID string, contextMap map[string]any) string {
	// Use a canonical concatenation of the security-affecting context keys
	// so equivalent contexts collide cleanly. We pick a subset to keep keys
	// reasonable in length; full-context-hash would change on harmless
	// fields and obliterate the cache. The subset MUST cover every
	// condition-context input actually consumed by an FGA Condition
	// predicate (mfa_fresh, device_compliant, ...) that is not otherwise
	// independently enforced — see context_extractor.go's reserved-keys
	// doc-comment for the full input list and which ones are exempt.
	parts := []string{subject, action, resourceType, resourceID}
	if contextMap != nil {
		keys := []string{
			"acr_value", "mfa_at", "client_ip", "device_id", "passkey_aaguid",
			"device_attestation", "amr_claims",
		}
		sort.Strings(keys) // deterministic
		for _, k := range keys {
			if v, ok := contextMap[k]; ok {
				parts = append(parts, k+"="+fmt.Sprint(v))
			}
		}
	}
	raw := strings.Join(parts, "|")
	// Compress with sha256 for stable length (the cache map handles
	// collisions naturally — sha256 collision probability is negligible).
	//
	// Ключ — ТОЛЬКО дайджест. Прежде он нёс субъект открытой приставкой, чтобы
	// пообъектный сброс отбирал записи по началу строки; сброса нет, и приставки
	// тоже — форма, пережившая свой отбор, читалась бы как живая возможность.
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
