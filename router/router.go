package router

import (
	"log/slog"
	"strconv"
	"sync"

	"tgproxy/types"
)

type Router struct {
	inbound     chan types.Message
	subscribers []subscriber
	mu          sync.RWMutex
	log         *slog.Logger
	bufferSize  int
}

type subscriber struct {
	out     chan<- types.Message
	chatIDs []int64
}

func NewRouter(bufferSize int, log *slog.Logger) *Router {
	r := &Router{
		inbound:     make(chan types.Message, bufferSize),
		subscribers: make([]subscriber, 0),
		log:         log,
		bufferSize:  bufferSize,
	}
	go r.run()
	return r
}

func (r *Router) Inbound() chan<- types.Message {
	return r.inbound
}

func (r *Router) Register(chatIDs []int64) chan types.Message {
	out := make(chan types.Message, r.bufferSize)
	r.mu.Lock()
	r.subscribers = append(r.subscribers, subscriber{
		out:     out,
		chatIDs: chatIDs,
	})
	r.mu.Unlock()
	r.log.Debug("registered new subscriber", "chat_ids_count", len(chatIDs), "total_subscribers", len(r.subscribers))
	return out
}

func (r *Router) Unregister(out chan<- types.Message) {
	r.mu.Lock()
	var removed bool
	for i, sub := range r.subscribers {
		if sub.out == out {
			r.subscribers = append(r.subscribers[:i], r.subscribers[i+1:]...)
			removed = true
			break
		}
	}
	r.mu.Unlock()
	r.log.Debug("subscriber unregistered", "removed", removed, "total_subscribers", len(r.subscribers))
}

func contains(ids []int64, peerID string) bool {
	pid, err := strconv.ParseInt(peerID, 10, 64)
	if err != nil {
		return false
	}
	for _, cid := range ids {
		if cid == pid {
			return true
		}
	}
	return false
}

func (r *Router) run() {
	for msg := range r.inbound {
		r.log.Debug("inbound message received", "peer_id", msg.PeerID, "msg_id", msg.ID)
		r.mu.RLock()
		r.log.Debug("routing to subscribers", "count", len(r.subscribers))
		for _, sub := range r.subscribers {
			if len(sub.chatIDs) == 0 || contains(sub.chatIDs, msg.PeerID) {
				select {
				case sub.out <- msg:
				default:
					r.log.Debug("dropped message to blocked subscriber")
					// drop if subscriber is blocked/full
				}
			}
		}
		r.mu.RUnlock()
	}
}

// Close safely shuts down the router by closing the inbound channel.
func (r *Router) Close() {
	r.log.Debug("closing router")
	close(r.inbound)
}
