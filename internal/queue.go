package internal

type Queue[T any] struct {
	array    []*T
	capacity int
}

func NewQueue[T any](capacity int) *Queue[T] {
	queue := Queue[T]{
		array:    make([]*T, 0),
		capacity: capacity,
	}
	return &queue
}

func (queue *Queue[T]) Enqueue(item *T, force bool) bool {
	if !force && len(queue.array) >= queue.capacity {
		return false
	}
	queue.array = append(queue.array, item)
	return true
}

func (queue *Queue[T]) Dequeue() *T {
	if len(queue.array) == 0 {
		return nil
	}
	item := queue.array[0]
	queue.array = queue.array[1:]
	return item
}

func (queue *Queue[T]) IsFull() bool {
	return len(queue.array) >= queue.capacity
}

func (queue *Queue[T]) IsEmpty() bool {
	return len(queue.array) == 0
}
