// Package executor runs auction commands in queue order on a single goroutine.
package executor

import (
	"context"
	"errors"

	auctionv1 "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
	"github.com/orimintz21/distributed_systems_project/internal/sm"
)

// QueueLen is the maximum number of commands that can wait in the queue.
const QueueLen = 1024

// ErrStopped is returned by Submit once Run has returned.
var ErrStopped = errors.New("executor stopped")

type request struct {
	cmd   *auctionv1.Command
	reply chan *auctionv1.Result
}

func newRequest(cmd *auctionv1.Command) request {
	// One reply can be sent even if the caller has stopped waiting.
	return request{cmd: cmd, reply: make(chan *auctionv1.Result, 1)}
}

// Executor queues commands for Run, which owns the state.
type Executor struct {
	state   *auctionv1.State
	queue   chan request
	stopped chan struct{}
}

// New returns an executor over the given state.
func New(state *auctionv1.State) *Executor {
	return &Executor{
		state:   state,
		queue:   make(chan request, QueueLen),
		stopped: make(chan struct{}),
	}
}

// Submit enqueues cmd and waits for its result.
// Canceling the wait after enqueueing leaves the command in the queue.
func (e *Executor) Submit(ctx context.Context, cmd *auctionv1.Command) (*auctionv1.Result, error) {
	r := newRequest(cmd)
	if err := e.enqueue(ctx, r); err != nil {
		return nil, err
	}
	return e.await(ctx, r)
}

func (e *Executor) enqueue(ctx context.Context, r request) error {
	// Check cancellation and shutdown before competing for an available queue slot.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.stopped:
		return ErrStopped
	default:
	}

	select {
	case e.queue <- r:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.stopped:
		return ErrStopped
	}
}

func (e *Executor) await(ctx context.Context, r request) (*auctionv1.Result, error) {
	select {
	case result := <-r.reply:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.stopped:
		return nil, ErrStopped
	}
}

// Run executes commands until ctx ends. Call it once per executor.
func (e *Executor) Run(ctx context.Context) {
	// TODO 5a: Close the stopped channel when Run returns.
	for {
		r, ok := e.next(ctx)
		if !ok {
			return
		}
		e.execute(r)
	}
}

func (e *Executor) next(ctx context.Context) (r request, ok bool) {

	// TODO 5b: Receive the next request or return false when ctx ends.
	<-ctx.Done()
	return request{}, false
}

func (e *Executor) execute(r request) {
	res := sm.Execute(e.state, r.cmd)
	// TODO 5c: Send res on the request's buffered reply channel.
	_ = res
}
