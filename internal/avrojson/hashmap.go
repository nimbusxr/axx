package avrojson

import (
	"sort"
	"unicode/utf16"
)

// MapEntry is one entry of an OrderedMap.
type MapEntry struct {
	Key   string
	Value any
}

// OrderedMap is an Avro map value that remembers the order its entries were
// read in. Java reads Avro maps into a java.util.HashMap, whose iteration
// order depends on that insertion order when keys share a hash bucket, so
// Render reproduces Java exactly for an OrderedMap and only approximately for
// a Go map (colliding keys are then ordered by key).
type OrderedMap []MapEntry

// Get returns the value of the last entry named key.
func (m OrderedMap) Get(key string) (any, bool) {
	for i := len(m) - 1; i >= 0; i-- {
		if m[i].Key == key {
			return m[i].Value, true
		}
	}
	return nil, false
}

// javaHashMapOrder returns the indexes of keys (given in insertion order,
// without duplicates) in the order a java.util.HashMap filled with them in
// that order iterates. Avro's GenericDatumReader creates its maps with the
// default capacity (16; measured, whatever the map's size), so the table
// ends at the smallest power of two from 16 up whose 0.75 load threshold
// holds every key; entries iterate by bucket, and in insertion order within
// a bucket (resizes keep that order). utf8 selects Avro's Utf8 key hash
// (Arrays.hashCode of the UTF-8 bytes) instead of String.hashCode.
func javaHashMapOrder(keys []string, utf8 bool) []int {
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	if len(keys) < 2 {
		return idx
	}
	capacity := 16
	for capacity*3/4 < len(keys) {
		capacity <<= 1
	}
	bucket := make([]int, len(keys))
	for i, k := range keys {
		var h int32
		if utf8 {
			h = utf8Hash(k)
		} else {
			h = javaStringHash(k)
		}
		spread := uint32(h) ^ (uint32(h) >> 16)
		bucket[i] = int(spread & uint32(capacity-1))
	}
	sort.SliceStable(idx, func(a, b int) bool { return bucket[idx[a]] < bucket[idx[b]] })
	return idx
}

// utf8Hash is org.apache.avro.util.Utf8.hashCode (Avro 1.12):
// Arrays.hashCode of the UTF-8 bytes, which are signed in Java.
func utf8Hash(s string) int32 {
	h := int32(1)
	for i := 0; i < len(s); i++ {
		h = 31*h + int32(int8(s[i]))
	}
	return h
}

// javaStringHash is java.lang.String.hashCode over UTF-16 code units.
func javaStringHash(s string) int32 {
	var h int32
	for _, u := range utf16.Encode([]rune(s)) {
		h = 31*h + int32(u)
	}
	return h
}

// orderedEntries returns a map value's entries in Java HashMap order. m is a
// Go map (keys are first sorted, see OrderedMap) or an OrderedMap (a repeated
// key keeps its first position and last value, as a HashMap does).
func orderedEntries(v any, utf8 bool) ([]MapEntry, bool) {
	var entries []MapEntry
	switch m := v.(type) {
	case OrderedMap:
		pos := map[string]int{}
		for _, e := range m {
			if i, ok := pos[e.Key]; ok {
				entries[i].Value = e.Value
				continue
			}
			pos[e.Key] = len(entries)
			entries = append(entries, e)
		}
	case map[string]any:
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			entries = append(entries, MapEntry{k, m[k]})
		}
	default:
		return nil, false
	}
	keys := make([]string, len(entries))
	for i, e := range entries {
		keys[i] = e.Key
	}
	out := make([]MapEntry, len(entries))
	for i, j := range javaHashMapOrder(keys, utf8) {
		out[i] = entries[j]
	}
	return out, true
}
