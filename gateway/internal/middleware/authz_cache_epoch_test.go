// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authz_cache_epoch_test.go — race tests for the decision-cache
// write-after-invalidate guard (epoch/generation counter).
//
// Threat: a request whose Check() was computed against the OLD (pre-revocation)
// grant can execute its put() AFTER an Invalidate() has already run and found
// nothing to remove — re-populating a stale allow=true entry that then services
// requests for the whole TTL, defeating the near-immediate revocation SLA
// (CWE-362 + CWE-613).
//
// The fix is a monotonically-increasing generation counter bumped on every
// Invalidate; a request captures the generation at get()-miss time and its put()
// is dropped when the generation has since moved.
//
// СБРОСОВ ЗДЕСЬ ОДИН, а не два: пообъектный по субъекту снят вместе с портом,
// которым его звали извне (задача #1024). Предмет пробы от этого не изменился —
// он про ЭПОХУ, а не про то, чем её двигают, — поэтому проба переписана на
// оставшийся сброс, а не снята.
package middleware

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDecisionCache_PutIfGen_DroppedAfterInvalidate — deterministic proof that
// a put() carrying a pre-invalidation generation snapshot is dropped rather
// than re-populating a just-flushed entry.
func TestDecisionCache_PutIfGen_DroppedAfterInvalidate(t *testing.T) {
	c := newDecisionCache(100, 5*time.Second, time.Now)

	// A request begins: capture the generation at get()-miss time.
	gen := c.generation()
	ok := c.allowed("user:x|act|project|p1|")
	require.False(t, ok, "cold cache must miss")

	// Meanwhile a revocation flushes the subject (bumps the generation).
	c.Invalidate()

	// The in-flight request now tries to cache its stale allow=true. It must be
	// dropped because the generation moved since the snapshot.
	c.putIfGen("user:x|act|project|p1|", gen)

	ok = c.allowed("user:x|act|project|p1|")
	assert.False(t, ok,
		"stale allow written after Invalidate must NOT survive (epoch guard)")
}

// TestDecisionCache_PutIfGen_KeptWhenNoInvalidate — the common path: no
// invalidation between snapshot and put → the entry is stored normally.
func TestDecisionCache_PutIfGen_KeptWhenNoInvalidate(t *testing.T) {
	c := newDecisionCache(100, 5*time.Second, time.Now)
	gen := c.generation()
	ok := c.allowed("user:x|act|project|p1|")
	require.False(t, ok)
	c.putIfGen("user:x|act|project|p1|", gen)
	ok = c.allowed("user:x|act|project|p1|")
	require.True(t, ok, "put with unchanged generation must be stored")
}

// TestDecisionCache_PutIfGen_DroppedAfterFullFlush — the whole-cache flush path
// (Invalidate) must also bump the generation and drop concurrent stale writes.
func TestDecisionCache_PutIfGen_DroppedAfterFullFlush(t *testing.T) {
	c := newDecisionCache(100, 5*time.Second, time.Now)
	gen := c.generation()
	c.Invalidate()
	c.putIfGen("user:y|act|project|p1|", gen)
	ok := c.allowed("user:y|act|project|p1|")
	assert.False(t, ok, "stale allow after full flush must be dropped")
}

// TestDecisionCache_RevocationWinsUnderConcurrency — the contested path under
// -race: many goroutines interleave Check-driven putIfGen(allow) with
// Invalidate flushes. After the final flush no stale allow may remain
// for the revoked subject.
func TestDecisionCache_RevocationWinsUnderConcurrency(t *testing.T) {
	c := newDecisionCache(10000, 5*time.Second, time.Now)
	const subject = "user:victim"
	const key = "user:victim|iam.projects.get|project|prj1|"

	var wg sync.WaitGroup

	// Writers: simulate in-flight Checks that read allow=true and race to put.
	// A bounded iteration count (not a wall-clock window) makes termination
	// deterministic while still forcing writer/invalidator interleaving under
	// -race — no time.Sleep to make the contention timing-dependent.
	const writerIters = 4000
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writerIters; j++ {
				gen := c.generation()
				if ok := c.allowed(key); !ok {
					c.putIfGen(key, gen)
				}
			}
		}()
	}

	// Invalidators: revocation pushes.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				c.Invalidate()
			}
		}()
	}

	wg.Wait()

	// Final revocation — nothing computed before this instant may survive it.
	finalGen := c.generation()
	c.Invalidate()
	// A late writer that snapshotted finalGen-era state and tries to write now
	// must be dropped.
	c.putIfGen(key, finalGen)
	ok := c.allowed(key)
	assert.False(t, ok,
		"after the final revocation no stale allow may survive for the revoked subject")
}
