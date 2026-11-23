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

var errTODO = errors.New("executor: not implemented yet (Part II, Step 4)")

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
	e := &Executor{state: state}
	// TODO 3: Initialize both channels, with capacity QueueLen for the queue.
	return e
}

// Submit enqueues cmd and waits for its result.
func (e *Executor) Submit(ctx context.Context, cmd *auctionv1.Command) (*auctionv1.Result, error) {
	r := newRequest(cmd)
	if err := e.enqueue(ctx, r); err != nil {
		return nil, err
	}
	return e.await(ctx, r)
}

func (e *Executor) enqueue(ctx context.Context, r request) error {
	// TODO 4a: Send to the queue or return on context cancellation or shutdown.
	return errTODO
}

func (e *Executor) await(ctx context.Context, r request) (*auctionv1.Result, error) {
	// TODO 4b: Wait for the reply, context cancellation, or shutdown.
	return nil, errTODO
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
