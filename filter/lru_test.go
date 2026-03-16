package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRU(t *testing.T) {
	t.Run("basic operations", func(t *testing.T) {
		c := newLRU[string, int](10)

		c.Put("a", 1)
		c.Put("b", 2)
		c.Put("c", 3)

		v, ok := c.Get("a")
		require.True(t, ok)
		assert.Equal(t, 1, v)

		v, ok = c.Get("b")
		require.True(t, ok)
		assert.Equal(t, 2, v)

		v, ok = c.Get("c")
		require.True(t, ok)
		assert.Equal(t, 3, v)

		_, ok = c.Get("d")
		assert.False(t, ok)

		assert.Equal(t, 3, c.Len())
	})

	t.Run("eviction", func(t *testing.T) {
		c := newLRU[string, int](3)

		c.Put("a", 1)
		c.Put("b", 2)
		c.Put("c", 3)
		assert.Equal(t, 3, c.Len())

		// Adding 4th should evict "a" (oldest)
		c.Put("d", 4)
		assert.Equal(t, 3, c.Len())

		_, ok := c.Get("a")
		assert.False(t, ok, "a should have been evicted")

		v, ok := c.Get("b")
		require.True(t, ok)
		assert.Equal(t, 2, v)

		v, ok = c.Get("d")
		require.True(t, ok)
		assert.Equal(t, 4, v)
	})

	t.Run("access refreshes LRU position", func(t *testing.T) {
		c := newLRU[string, int](3)

		c.Put("a", 1)
		c.Put("b", 2)
		c.Put("c", 3)

		// Access "a" to refresh it
		c.Get("a")

		// Now "b" is the oldest — adding "d" should evict "b"
		c.Put("d", 4)

		_, ok := c.Get("b")
		assert.False(t, ok, "b should have been evicted (oldest non-accessed)")

		v, ok := c.Get("a")
		require.True(t, ok, "a should still exist (was accessed)")
		assert.Equal(t, 1, v)
	})

	t.Run("update existing key", func(t *testing.T) {
		c := newLRU[string, int](3)

		c.Put("a", 1)
		c.Put("a", 42)

		assert.Equal(t, 1, c.Len())

		v, ok := c.Get("a")
		require.True(t, ok)
		assert.Equal(t, 42, v)
	})

	t.Run("Each visits all entries", func(t *testing.T) {
		c := newLRU[string, int](10)

		c.Put("a", 1)
		c.Put("b", 2)
		c.Put("c", 3)

		collected := make(map[string]int)
		c.Each(func(k string, v int) {
			collected[k] = v
		})

		assert.Equal(t, map[string]int{"a": 1, "b": 2, "c": 3}, collected)
	})
}
