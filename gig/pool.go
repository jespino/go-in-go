package gig

import "errors"

// Module is anything backed by an Instance: *Instance itself, or the Module
// type of generated bindings.
type Module interface {
	GigInstance() *Instance
}

// Pool spreads calls over several instances of a module. An instance runs
// one call at a time; a pool of n runs up to n calls in parallel. Instances
// are independent (separate heaps and globals), so a pool suits stateless
// functions. An instance that died (panic, fault) is replaced by a new one
// the next time it's picked.
//
//	p, err := gig.NewPool(runtime.NumCPU(), func() (*greeter.Module, error) {
//		return greeter.Load("greeter.gig", host, nil)
//	})
//	err = p.Do(func(m *greeter.Module) error { s, err = m.Greet("gopher"); return err })
type Pool[M Module] struct {
	load func() (M, error)
	free   chan M
	loaded int // instances in the pool, idle or busy
}

// NewPool loads size instances with load.
func NewPool[M Module](size int, load func() (M, error)) (*Pool[M], error) {
	if size < 1 {
		return nil, errors.New("gig: pool size must be at least 1")
	}
	p := &Pool[M]{load: load, free: make(chan M, size)}
	for i := 0; i < size; i++ {
		m, err := load()
		if err != nil {
			p.Close()
			return nil, err
		}
		p.free <- m
		p.loaded++
	}
	return p, nil
}

// Do runs f with an idle instance, waiting for one if all are busy.
func (p *Pool[M]) Do(f func(M) error) error {
	m := <-p.free
	if m.GigInstance().Err() != nil {
		fresh, err := p.load()
		if err != nil {
			p.free <- m // try again next time
			return err
		}
		m.GigInstance().Close()
		m = fresh
	}
	defer func() { p.free <- m }()
	return f(m)
}

// Close closes every instance, waiting for running calls to finish.
func (p *Pool[M]) Close() {
	for ; p.loaded > 0; p.loaded-- {
		m := <-p.free
		m.GigInstance().Close()
	}
}
