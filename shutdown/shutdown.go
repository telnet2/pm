// Package shutdown provides convenient interface for working with os.Signal.
//
// Multiple hooks can be applied,
// they will be called simultaneously on app shutdown.
package shutdown

import (
	"math/rand"
	"os"
	"os/signal"
	"sync"
)

// DefaultShutdown is a default instance.
var DefaultShutdown = New()

// Shutdown is an instance of shutdown handler.
type Shutdown struct {
	count int32
	hooks map[string]func(os.Signal)
	mutex *sync.Mutex
	final func()
}

// New creates a new Shutdown instance.
func New() *Shutdown {
	return &Shutdown{
		hooks: map[string]func(os.Signal){},
		mutex: &sync.Mutex{},
		count: 0,
	}
}

// Add adds a shutdown hook
// and returns hook identificator (key).
func Add(fn func()) string {
	return DefaultShutdown.Add(fn)
}

// AddWithKey adds a shutdown hook
// with provided identificator (key).
func AddWithKey(key string, fn func()) {
	DefaultShutdown.AddWithKey(key, fn)
}

// AddWithParam adds a shutdown hook with signal parameter
// and returns hook identificator (key).
func AddWithParam(fn func(os.Signal)) string {
	return DefaultShutdown.AddWithParam(fn)
}

// AddWithKeyWithParam adds a shutdown hook with signal parameter
// with provided identificator (key).
func AddWithKeyWithParam(key string, fn func(os.Signal)) {
	DefaultShutdown.AddWithKeyWithParam(key, fn)
}

// Hooks returns a copy of current hooks.
func Hooks() map[string]func(os.Signal) {
	return DefaultShutdown.Hooks()
}

// Listen waits for provided OS signals.
// It will wait for any signal if no signals provided.
func Listen(signals ...os.Signal) {
	DefaultShutdown.Listen(signals...)
}

// ListenAsync waits for provided OS signals.
// It runs its hooks in separate go-routines.
// It will wait for any signal if no signals provided.
func ListenAsync(signals ...os.Signal) {
	DefaultShutdown.ListenAsync(signals...)
}

// RepeatListen waits for provided OS signals.
// It will wait for any signal if no signals provided.
// After processing the signal, it will keep listening.
// This is useful when a signal would shutdown the process indirectly and
// may need additional interrupt for shutdown.
func RepeatListen(signals ...os.Signal) {
	DefaultShutdown.RepeatListen(signals...)
}

// RepeatListen waits for provided OS signals.
// It will wait for any signal if no signals provided.
// After processing the signal, it will keep listening.
// This is useful when a signal would shutdown the process indirectly and
// may need additional interrupt for shutdown.
func ListenUpToCount(n int32, signals ...os.Signal) {
	DefaultShutdown.ListenUpToCount(n, signals...)
}

// Remove cancels hook by identificator (key).
func Remove(key string) {
	DefaultShutdown.Remove(key)
}

// Reset cancels all hooks.
func Reset() {
	DefaultShutdown.Reset()
}

func Finally(fn func()) {
	DefaultShutdown.final = fn
}

// Add adds a shutdown hook
// and returns hook identificator (key).
func (s *Shutdown) Add(fn func()) string {
	return s.AddWithParam(func(os.Signal) {
		fn()
	})
}

// AddWithKey adds a shutdown hook
// with provided identificator (key).
func (s *Shutdown) AddWithKey(key string, fn func()) {
	s.AddWithKeyWithParam(key, func(os.Signal) {
		fn()
	})
}

// AddWithParam adds a shutdown hook with signal parameter
// and returns hook identificator (key).
func (s *Shutdown) AddWithParam(fn func(os.Signal)) string {
	key := randomKey()
	s.AddWithKeyWithParam(key, fn)
	return key
}

// AddWithKeyWithParam adds a shutdown hook with signal parameter
// with provided identificator (key).
func (s *Shutdown) AddWithKeyWithParam(key string, fn func(os.Signal)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.hooks[key] = fn
}

// Hooks returns a copy of current hooks.
func (s *Shutdown) Hooks() map[string]func(os.Signal) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	fns := map[string]func(os.Signal){}
	for key, cb := range s.hooks {
		fns[key] = cb
	}
	return fns
}

// Listen waits for provided OS signals.
// It will wait for any signal if no signals provided.
func (s *Shutdown) ListenAsync(signals ...os.Signal) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)
	sig := <-ch
	var wg sync.WaitGroup
	for _, fn := range s.Hooks() {
		wg.Add(1)
		go func(sig os.Signal, fn func(os.Signal)) {
			defer wg.Done()
			fn(sig)
		}(sig, fn)
	}
	wg.Wait()

	// handle the final hook
	if s.final != nil {
		s.final()
	}
}

// Listen waits for provided OS signals.
// It will wait for any signal if no signals provided.
func (s *Shutdown) Listen(signals ...os.Signal) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)
	sig := <-ch
	for _, fn := range s.Hooks() {
		fn(sig)
	}

	// handle the final hook
	if s.final != nil {
		s.final()
	}
}

// Listen waits for provided OS signals.
// It will wait for any signal if no signals provided.
func (s *Shutdown) RepeatListen(signals ...os.Signal) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)
	for sig := range ch {
		var wg sync.WaitGroup
		for _, fn := range s.Hooks() {
			wg.Add(1)
			go func(sig os.Signal, fn func(os.Signal)) {
				defer wg.Done()
				fn(sig)
			}(sig, fn)
		}
		wg.Wait()

		// handle the final hook
		if s.final != nil {
			s.final()
		}
	}
}

// ListenUpToCount waits for provided OS signals.
// It works similar to RepeatListen()
// but if the signal is received more than n times, the process will exit without calling shutdown hooks.
func (s *Shutdown) ListenUpToCount(n int32, signals ...os.Signal) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)
	for sig := range ch {
		s.count++
		if s.count > n {
			// if signal is repeated more than n times, exit the process.
			os.Exit(0)
		}

		for _, fn := range s.Hooks() {
			fn(sig)
		}

		// handle the final hook
		if s.final != nil {
			s.final()
		}
	}
}

// Remove cancels hook by identificator (key).
func (s *Shutdown) Remove(key string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.hooks, key)
}

// Reset cancels all hooks.
func (s *Shutdown) Reset() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for key := range s.hooks {
		delete(s.hooks, key)
	}
}

// randomKey generates a random identificator (key) for hook.
//
// Do not use this identificator for purposes other then to remove a hook
// as long as it's not fairly random without seed.
func randomKey() string {
	runes := []rune("0123456789abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, 16)
	for i := range b {
		b[i] = runes[rand.Intn(len(runes))]
	}
	return string(b)
}
