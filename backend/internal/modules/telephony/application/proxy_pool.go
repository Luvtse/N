package application

import (
	"sync"

	telephonydomain "nidaw-backend/internal/modules/telephony/domain"
)

// ProxyPool is a thread-safe inventory of proxy numbers available for booking.
// Numbers come from configuration (purchased Twilio numbers or DID ranges on a
// self-hosted gateway). The pool enforces one active session per number, which
// is what makes the masking sound: no two rides ever share a proxy number at
// the same time.
type ProxyPool struct {
	mu      sync.Mutex
	free    []string
	booked  map[string]bool
	validity map[string]bool // dedupe guard against duplicate config entries
}

// NewProxyPool validates every candidate number up front (E.164) and refuses
// to start with an empty pool — an empty pool means masked calling cannot be
// offered honestly, and silent fallback to real numbers is forbidden by plan
// §2.2 ("Never return the other party's real number").
func NewProxyPool(numbers []string) (*ProxyPool, error) {
	p := &ProxyPool{
		booked:   make(map[string]bool),
		validity: make(map[string]bool),
	}
	for _, raw := range numbers {
		n, err := telephonydomain.NormalizeE164(raw)
		if err != nil {
			return nil, err
		}
		if p.validity[n] {
			continue // dedupe; config files sometimes repeat numbers
		}
		p.validity[n] = true
		p.free = append(p.free, n)
	}
	if len(p.free) == 0 {
		return nil, telephonydomain.ErrNoProxyNumber
	}
	return p, nil
}

// Acquire books the next free number. Returns ErrNoProxyNumber when the pool
// is exhausted so callers surface "Calling unavailable" instead of queueing
// silently or reusing a booked number.
func (p *ProxyPool) Acquire() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.free) == 0 {
		return "", telephonydomain.ErrNoProxyNumber
	}
	n := p.free[0]
	p.free = p.free[1:]
	p.booked[n] = true
	return n, nil
}

// Release returns a number to the front of the free list so recycled numbers
// are handed out again before fresh inventory — this keeps small pools stable
// and makes the recycle path deterministic for callers and tests. Releasing an
// unbooked number (double teardown, sweep racing an explicit cancel) is a
// harmless no-op.
func (p *ProxyPool) Release(number string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.booked[number] {
		return
	}
	delete(p.booked, number)
	p.free = append([]string{number}, p.free...)
}

// Available reports the free count; used for health/metrics.
func (p *ProxyPool) Available() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.free)
}
