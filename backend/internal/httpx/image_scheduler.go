package httpx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const imageHedgeIdle = 2 * time.Second

// Three image requests can start their primary before any hedge can consume
// the fourth physical connection. A lane is acquired before the global slot.
// This deliberately trades a fourth simultaneous primary for fair admission
// of the three cold images and a bounded escape from stalled peers.
type imageCandidateLanes struct {
	primary   chan struct{}
	secondary chan struct{}
}

func newImageCandidateLanes() *imageCandidateLanes {
	return &imageCandidateLanes{primary: make(chan struct{}, 3), secondary: make(chan struct{}, 1)}
}

var bufferedImageLanes = newImageCandidateLanes()

var errImageCandidateYielded = errors.New("image candidate yielded after inactivity")

type imagePeer struct {
	address  net.IPAddr
	attempts int
}

type imageAttempt struct {
	peer      *imagePeer
	number    int
	ctx       context.Context
	cancel    context.CancelFunc
	finished  chan struct{}
	promoted  chan struct{}
	activity  chan<- struct{}
	lanes     *imageCandidateLanes
	secondary atomic.Bool
	holdsLane atomic.Bool
	yielded   bool // Owned only by the scheduler, distinct from caller cancel/EOF.
	mu        sync.Mutex
	started   bool
	completed bool
	last      time.Time
}

func (a *imageAttempt) progress() {
	a.mu.Lock()
	a.started = true
	a.last = time.Now()
	a.mu.Unlock()
	select {
	case a.activity <- struct{}{}:
	default:
	}
}

func (a *imageAttempt) idleDeadline() (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last.Add(imageHedgeIdle), a.started && !a.completed
}

func (a *imageAttempt) releaseSecondary() {
	if a.holdsLane.Swap(false) {
		<-a.lanes.secondary
	}
}

func (a *imageAttempt) promote() {
	a.secondary.Store(false)
	close(a.promoted)
	a.releaseSecondary()
}

type imageCandidateResult struct {
	attempt  *imageAttempt
	response *http.Response
	err      error
}

// The primary is never discarded merely for inactivity. A first secondary may
// yield to an untried peer, then resume once. EOF recovery and scheduler resumes
// share the same two-attempt/IP budget. A final attempt retains the original
// remaining deadline, so a legitimate quiet alternate is not repeatedly cut.
func (t *bufferedGETTransport) roundTripImages(req *http.Request, addresses []net.IPAddr) (*http.Response, error) {
	lanes := t.imageLanes
	if lanes == nil {
		lanes = bufferedImageLanes
	}
	select {
	case lanes.primary <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	ctx, cancel := context.WithCancel(req.Context())
	var attempts sync.WaitGroup
	defer func() {
		cancel()
		attempts.Wait()
		<-lanes.primary
	}()
	initial := make([]*imagePeer, len(addresses))
	for index, address := range addresses {
		initial[index] = &imagePeer{address: address}
	}
	var retries []*imagePeer
	next := func() *imagePeer {
		if len(initial) != 0 {
			peer := initial[0]
			initial = initial[1:]
			return peer
		}
		if len(retries) != 0 {
			peer := retries[0]
			retries = retries[1:]
			return peer
		}
		return nil
	}
	pending := func() bool { return len(initial) != 0 || len(retries) != 0 }
	activity := make(chan struct{}, 1)
	results := make(chan imageCandidateResult, 2*len(addresses))
	launch := func(peer *imagePeer, secondary bool) *imageAttempt {
		peer.attempts++
		attemptCtx, stop := context.WithCancel(ctx)
		a := &imageAttempt{peer: peer, number: peer.attempts, ctx: attemptCtx, cancel: stop, finished: make(chan struct{}), promoted: make(chan struct{}), activity: activity, lanes: lanes}
		a.secondary.Store(secondary)
		attempts.Add(1)
		go func() {
			defer attempts.Done()
			defer close(a.finished)
			response, err := t.runImageAttempt(req, a)
			a.mu.Lock()
			a.completed = true
			a.mu.Unlock()
			results <- imageCandidateResult{attempt: a, response: response, err: err}
		}()
		return a
	}
	var primary, secondary *imageAttempt
	var firstResponse *http.Response
	var failures []error
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		if primary == nil && secondary != nil && !secondary.yielded {
			secondary.promote()
			primary, secondary = secondary, nil
		}
		if primary == nil && secondary == nil {
			if peer := next(); peer != nil {
				primary = launch(peer, false)
			} else {
				break
			}
		}
		timer.Stop()
		var timerC <-chan time.Time
		var watched *imageAttempt
		if pending() {
			if secondary == nil {
				watched = primary
			} else if !secondary.yielded && secondary.number == 1 {
				watched = secondary
			}
		}
		if watched != nil {
			if deadline, started := watched.idleDeadline(); started {
				timer.Reset(max(time.Duration(0), time.Until(deadline)))
				timerC = timer.C
			}
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-activity:
		case <-timerC:
			// Progress and timer delivery may race; recheck the latest clock.
			deadline, started := watched.idleDeadline()
			if !started || time.Now().Before(deadline) {
				continue
			}
			if secondary == nil {
				secondary = launch(next(), true)
			} else {
				secondary.yielded = true
				secondary.cancel()
			}
		case result := <-results:
			a := result.attempt
			<-a.finished // Socket close and both permit releases precede rotation.
			a.cancel()
			if a == primary {
				primary = nil
			}
			if a == secondary {
				secondary = nil
			}
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			if result.err == nil {
				result.response.Request = req
				if bufferedGETWinner(result.response) {
					return result.response, nil
				}
				if firstResponse == nil {
					firstResponse = result.response
				}
				continue
			}
			if a.yielded && errors.Is(result.err, context.Canceled) {
				failures = append(failures, errImageCandidateYielded)
				if a.peer.attempts == 1 {
					retries = append(retries, a.peer)
				}
				continue
			}
			failures = append(failures, result.err)
			if a.peer.attempts == 1 && transientPreBodyFailure(result.err) {
				retries = append(retries, a.peer)
			}
		}
	}
	if firstResponse != nil {
		return firstResponse, nil
	}
	return nil, fmt.Errorf("all public addresses failed: %w", errors.Join(failures...))
}

func (t *bufferedGETTransport) runImageAttempt(req *http.Request, a *imageAttempt) (*http.Response, error) {
	if a.secondary.Load() {
		select {
		case a.lanes.secondary <- struct{}{}:
			a.holdsLane.Store(true)
			if !a.secondary.Load() {
				a.releaseSecondary()
			}
		case <-a.promoted:
		case <-a.ctx.Done():
			return nil, a.ctx.Err()
		}
	}
	defer a.releaseSecondary()
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-a.ctx.Done():
		return nil, a.ctx.Err()
	}
	if err := a.ctx.Err(); err != nil {
		return nil, err
	}
	a.progress()
	return t.fetchCandidate(req.Clone(a.ctx), a.peer.address, a.progress)
}

// Track actual connection I/O, including CONNECT and TLS, rather than only
// body reads. The same wrapper is used by direct, HTTP and SOCKS transports.
type imageProgressConn struct {
	net.Conn
	progress func()
}

func (c *imageProgressConn) Read(buffer []byte) (int, error) {
	n, err := c.Conn.Read(buffer)
	if n > 0 {
		c.progress()
	}
	return n, err
}

func (c *imageProgressConn) Write(buffer []byte) (int, error) {
	n, err := c.Conn.Write(buffer)
	if n > 0 {
		c.progress()
	}
	return n, err
}
