package model

import (
	"hash/maphash"
	"iter"
	"maps"
)

const shardCount = 256

var shardSeed = maphash.MakeSeed()

// shards is a map split by key hash, so a patched copy of a World shares
// every shard the patch does not write. a shard is cloned on its first
// write after a copy and never written while shared.
type shards[V any] struct {
	m     [shardCount]map[string]V
	owned [shardCount]bool
}

func shardOf(k string) int { return int(maphash.String(shardSeed, k) % shardCount) }

func (s *shards[V]) get(k string) V { return s.m[shardOf(k)][k] }

func (s *shards[V]) lookup(k string) (V, bool) {
	v, ok := s.m[shardOf(k)][k]
	return v, ok
}

func (s *shards[V]) set(k string, v V) {
	i := shardOf(k)
	s.own(i)
	s.m[i][k] = v
}

func (s *shards[V]) del(k string) {
	i := shardOf(k)
	if _, ok := s.m[i][k]; !ok {
		return
	}
	s.own(i)
	delete(s.m[i], k)
}

func (s *shards[V]) own(i int) {
	if s.owned[i] {
		return
	}
	if s.m[i] = maps.Clone(s.m[i]); s.m[i] == nil {
		s.m[i] = map[string]V{}
	}
	s.owned[i] = true
}

// copy shares every shard with s until it is written
func (s *shards[V]) copy() shards[V] { return shards[V]{m: s.m} }

func (s *shards[V]) all() iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		for _, m := range s.m {
			for k, v := range m {
				if !yield(k, v) {
					return
				}
			}
		}
	}
}
