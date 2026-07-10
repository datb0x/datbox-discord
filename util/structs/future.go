package structs

type Future[R any] struct {
	channel chan bool
	result  R
	err     error
}

func NewFuture[R any](function func() (R, error)) *Future[R] {
	future := Future[R]{
		channel: make(chan bool),
	}

	go func() {
		result, err := function()
		if err != nil {
			future.err = err
			future.channel <- false
		} else {
			future.result = result
			future.channel <- true
		}
	}()

	return &future
}

func (future *Future[R]) Await() (*R, error) {
	ok := <-future.channel
	if ok {
		return &future.result, nil
	} else {
		return nil, future.err
	}
}
