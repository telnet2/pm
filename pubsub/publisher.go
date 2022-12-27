package pubsub // import "github.com/telnet2/pm/pubsub"
//go:generate genny -in=$GOFILE -out=gen-$GOFILE gen "Elem=string"

import (
	"sync"
	"time"

	"github.com/cheekybits/genny/generic"
)

type Elem generic.Type

var wgElemPool = sync.Pool{New: func() interface{} { return new(sync.WaitGroup) }}

// NewElemPublisher creates a new pub/sub publisher to broadcast messages.
// The duration is used as the send timeout as to not block the publisher publishing
// messages to other clients if one client is slow or unresponsive.
// The buffer is used when creating new channels for subscribers.
func NewElemPublisher(publishTimeout time.Duration, buffer int) *ElemPublisher {
	return &ElemPublisher{
		buffer:      buffer,
		timeout:     publishTimeout,
		subscribers: make(map[ElemSubscriber]ElemTopicFunc),
	}
}

type ElemSubscriber chan Elem
type ElemTopicFunc func(v Elem) bool

// ElemPublisher is basic pub/sub structure. Allows to send events and subscribe
// to them. Can be safely used from multiple goroutines.
type ElemPublisher struct {
	m           sync.RWMutex
	buffer      int
	timeout     time.Duration
	subscribers map[ElemSubscriber]ElemTopicFunc
}

// Len returns the number of subscribers for the publisher
func (p *ElemPublisher) Len() int {
	p.m.RLock()
	i := len(p.subscribers)
	p.m.RUnlock()
	return i
}

// Subscribe adds a new subscriber to the publisher returning the channel.
func (p *ElemPublisher) Subscribe() chan Elem {
	return p.SubscribeTopic(nil)
}

// SubscribeTopic adds a new subscriber that filters messages sent by a topic.
func (p *ElemPublisher) SubscribeTopic(topic ElemTopicFunc) chan Elem {
	ch := make(chan Elem, p.buffer)
	p.m.Lock()
	p.subscribers[ch] = topic
	p.m.Unlock()
	return ch
}

// SubscribeTopicWithBuffer adds a new subscriber that filters messages sent by a topic.
// The returned channel has a buffer of the specified size.
func (p *ElemPublisher) SubscribeTopicWithBuffer(topic ElemTopicFunc, buffer int) chan Elem {
	ch := make(chan Elem, buffer)
	p.m.Lock()
	p.subscribers[ch] = topic
	p.m.Unlock()
	return ch
}

// Evict removes the specified subscriber from receiving any more messages.
func (p *ElemPublisher) Evict(sub chan Elem) {
	p.m.Lock()
	_, exists := p.subscribers[sub]
	if exists {
		delete(p.subscribers, sub)
		close(sub)
	}
	p.m.Unlock()
}

// Publish sends the data in v to all subscribers currently registered with the publisher.
func (p *ElemPublisher) Publish(v Elem) {
	p.m.RLock()
	if len(p.subscribers) == 0 {
		p.m.RUnlock()
		return
	}

	wg := wgElemPool.Get().(*sync.WaitGroup)
	for sub, topic := range p.subscribers {
		wg.Add(1)
		go p.sendTopic(sub, topic, v, wg)
	}
	wg.Wait()
	wgElemPool.Put(wg)
	p.m.RUnlock()
}

// Close closes the channels to all subscribers registered with the publisher.
func (p *ElemPublisher) Close() {
	p.m.Lock()
	for sub := range p.subscribers {
		delete(p.subscribers, sub)
		close(sub)
	}
	p.m.Unlock()
}

func (p *ElemPublisher) sendTopic(sub ElemSubscriber, topic ElemTopicFunc, v Elem, wg *sync.WaitGroup) {
	defer wg.Done()
	if topic != nil && !topic(v) {
		return
	}

	// send under a select as to not block if the receiver is unavailable
	if p.timeout > 0 {
		timeout := time.NewTimer(p.timeout)
		defer timeout.Stop()

		select {
		case sub <- v:
		case <-timeout.C:
		}
		return
	}

	select {
	case sub <- v:
	default:
	}
}
