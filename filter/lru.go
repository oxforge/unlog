package filter

type lruNode[K comparable, V any] struct {
	key        K
	val        V
	prev, next *lruNode[K, V]
}

type lru[K comparable, V any] struct {
	capacity int
	items    map[K]*lruNode[K, V]
	head     *lruNode[K, V] // most recent
	tail     *lruNode[K, V] // least recent
}

func newLRU[K comparable, V any](capacity int) *lru[K, V] {
	return &lru[K, V]{
		capacity: capacity,
		items:    make(map[K]*lruNode[K, V], capacity),
	}
}

func (c *lru[K, V]) Get(key K) (V, bool) {
	node, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.moveToFront(node)
	return node.val, true
}

func (c *lru[K, V]) Put(key K, val V) {
	if node, ok := c.items[key]; ok {
		node.val = val
		c.moveToFront(node)
		return
	}

	node := &lruNode[K, V]{key: key, val: val}
	c.items[key] = node
	c.pushFront(node)

	if len(c.items) > c.capacity {
		c.evict()
	}
}

func (c *lru[K, V]) Len() int {
	return len(c.items)
}

func (c *lru[K, V]) Each(fn func(K, V)) {
	for node := c.head; node != nil; node = node.next {
		fn(node.key, node.val)
	}
}

func (c *lru[K, V]) pushFront(node *lruNode[K, V]) {
	node.prev = nil
	node.next = c.head
	if c.head != nil {
		c.head.prev = node
	}
	c.head = node
	if c.tail == nil {
		c.tail = node
	}
}

func (c *lru[K, V]) moveToFront(node *lruNode[K, V]) {
	if c.head == node {
		return
	}
	c.detach(node)
	c.pushFront(node)
}

func (c *lru[K, V]) detach(node *lruNode[K, V]) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
	node.prev = nil
	node.next = nil
}

func (c *lru[K, V]) evict() {
	if c.tail == nil {
		return
	}
	node := c.tail
	c.detach(node)
	delete(c.items, node.key)
}
