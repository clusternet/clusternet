/*
Copyright 2021 The Clusternet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// inspired by k8s.io/kubernetes/pkg/scheduler/internal/queue/queue.go
// because the current clusternet scheduler does not have Node-related events,
// we removed the unschedulable queue and made some simplifications.
// if there are cluster-related events that can trigger the scheduling process later
// the unschedulable queue should be added.

package queue

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/internal/heap"
)

const (
	queueClosed = "scheduling queue is closed"
)

const (
	// DefaultInitialBackoffDuration is the default value for the initial backoff duration
	DefaultInitialBackoffDuration time.Duration = 1 * time.Second
	// DefaultMaxBackoffDuration is the default value for the max backoff duration
	DefaultMaxBackoffDuration time.Duration = 10 * time.Second
)

// SchedulingQueue is an interface for a queue to store subscriptions waiting to be scheduled
type SchedulingQueue interface {

	// Run starts the goroutines managing the queue.
	Run()

	// Pop pop out a subscription scheduling info
	Pop() (*framework.SubscriptionInfo, error)

	// AddUnschedulableIfNotPresent inserts a subscription that cannot be schedule directly into
	// the backoff queue.
	AddUnschedulableIfNotPresent(sInfo *framework.SubscriptionInfo) error

	// Add adds a subscription to the active queu
	Add(sub *appsapi.Subscription) error

	Update(oldSub, newSub *appsapi.Subscription) error

	Delete(sub *appsapi.Subscription) error

	// Close closes the SchedulingQueue so that the goroutine which is
	// waiting to pop items can exit gracefully.
	Close()
}

type PriorityQueue struct {
	// stop is a channel to exit priority queue loop
	stop chan struct{}
	// clock is used to get the current time when the program is running
	clock clock.Clock

	initialBackoffDuration time.Duration
	maxBackoffDuration     time.Duration

	lock sync.RWMutex
	cond sync.Cond

	activeQ  *heap.Heap
	backoffQ *heap.Heap

	// closed indicates that the queue is closed.
	// It is mainly used to let Pop() exit its control loop while waiting for an item.
	closed bool
}

type priorityQueueOptions struct {
	clock                  clock.Clock
	initialBackoffDuration time.Duration
	maxBackoffDuration     time.Duration
}

// Option configures a PriorityQueue
type Option func(*priorityQueueOptions)

// WithClock sets clock for PriorityQueue, the default clock is util.RealClock.
func WithClock(clock clock.Clock) Option {
	return func(o *priorityQueueOptions) {
		o.clock = clock
	}
}

// WithInitialBackoffDuration sets initial backoff duration for PriorityQueue,
func WithInitialBackoffDuration(duration time.Duration) Option {
	return func(o *priorityQueueOptions) {
		o.initialBackoffDuration = duration
	}
}

// WithMaxBackoffDuration sets max backoff duration for PriorityQueue,
func WithMaxBackoffDuration(duration time.Duration) Option {
	return func(o *priorityQueueOptions) {
		o.maxBackoffDuration = duration
	}
}

var defaultPriorityQueueOptions = priorityQueueOptions{
	clock:                  clock.RealClock{},
	initialBackoffDuration: DefaultInitialBackoffDuration,
	maxBackoffDuration:     DefaultMaxBackoffDuration,
}

// Making sure that PriorityQueue implements SchedulingQueue.
var _ SchedulingQueue = &PriorityQueue{}

// NewSchedulingQueue initializes a priority queue as a new scheduling queue.
func NewSchedulingQueue(lessFn framework.LessFunc, opts ...Option) SchedulingQueue {
	return NewPriorityQueue(lessFn, opts...)
}

// NewPriorityQueue creates a PriorityQueue object.
func NewPriorityQueue(
	lessFn framework.LessFunc,
	opts ...Option,
) *PriorityQueue {
	options := defaultPriorityQueueOptions
	for _, opt := range opts {
		opt(&options)
	}

	comp := func(subInfo1, subInfo2 interface{}) bool {
		sInfo1 := subInfo1.(*framework.SubscriptionInfo)
		sInfo2 := subInfo2.(*framework.SubscriptionInfo)
		return lessFn(sInfo1, sInfo2)
	}

	pq := &PriorityQueue{
		clock:                  options.clock,
		stop:                   make(chan struct{}),
		initialBackoffDuration: options.initialBackoffDuration,
		maxBackoffDuration:     options.maxBackoffDuration,
		activeQ:                heap.NewWithRecorder(subInfoKeyFunc, comp),
	}
	pq.cond.L = &pq.lock
	pq.backoffQ = heap.NewWithRecorder(subInfoKeyFunc, pq.subsCompareBackoffCompleted)
	return pq
}

// Run starts the goroutine to pump from backoffQ to activeQ
func (p *PriorityQueue) Run() {
	go wait.Until(p.flushBackoffQCompleted, 1.0*time.Second, p.stop)
}

// Pop removes the head of the active queue and returns it. It blocks if the
// activeQ is empty and waits until a new item is added to the queue. It
// increments scheduling cycle when a subscription is popped.
func (p *PriorityQueue) Pop() (*framework.SubscriptionInfo, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	for p.activeQ.Len() == 0 {
		// When the queue is empty, invocation of Pop() is blocked until new item is enqueued.
		// When Close() is called, the p.closed is set and the condition is broadcast,
		// which causes this loop to continue and return from the Pop().
		if p.closed {
			return nil, fmt.Errorf(queueClosed)
		}
		p.cond.Wait()
	}
	obj, err := p.activeQ.Pop()
	if err != nil {
		return nil, err
	}
	sInfo := obj.(*framework.SubscriptionInfo)
	sInfo.Attempts++
	return sInfo, err
}

// AddUnschedulableIfNotPresent inserts a subscription that cannot be schedule directly into
// the backoff queue.
// TODO: add a subscription that cannot be schedule into a unschedulableSubscriptions queue and
// wait for subscribe-related events to occur, and then trigger the sub to move from
// unschedulableSubscriptions to the backoff queue.
func (p *PriorityQueue) AddUnschedulableIfNotPresent(sInfo *framework.SubscriptionInfo) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	sub := sInfo.Subscription

	if _, exists, _ := p.activeQ.Get(sInfo); exists {
		return fmt.Errorf("subscription %v is already present in the active queue", klog.KObj(sub))
	}
	if _, exists, _ := p.backoffQ.Get(sInfo); exists {
		return fmt.Errorf("subscription %v is already present in the backoff queue", klog.KObj(sub))
	}

	// Refresh the timestamp since the subscription is re-added.
	sInfo.Timestamp = p.clock.Now()

	// move the subscription into backoff queue directly
	if err := p.backoffQ.Add(sInfo); err != nil {
		return fmt.Errorf("error adding subscription %v to the backoff queue: %v", klog.KObj(sub), err)
	}
	return nil
}

// Add adds a subscription to the active queue. It should be called only when a new subscription
// is added so there is no chance the subscription is already in any queues
func (p *PriorityQueue) Add(sub *appsapi.Subscription) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	sInfo := p.newSubscriptionInfo(sub)
	// Delete subscription from backoffQ if it is backing off
	if err := p.backoffQ.Delete(sInfo); err == nil {
		klog.V(5).InfoS("removed subscription from backoff queue due to Add event.", "subscription", klog.KObj(sub))
	}
	if err := p.activeQ.Add(sInfo); err != nil {
		klog.ErrorS(err, "Error adding subscription to the scheduling queue", "subscription", klog.KObj(sub))
		return err
	}
	p.cond.Broadcast()

	return nil
}

// Update update old sub and new sub
func (p *PriorityQueue) Update(oldSub, newSub *appsapi.Subscription) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	if oldSub != nil {
		oldSubInfo := newSubscriptionInfoNoTimestamp(oldSub)
		// If the subscription is already in the active queue, just update it there.
		if oldSubInfo, exists, _ := p.activeQ.Get(oldSubInfo); exists {
			return p.activeQ.Update(updateSubscription(oldSubInfo, newSub))
		}

		// If the subscription is in the backoff queue, update it there.
		if oldSubInfo, exists, _ := p.backoffQ.Get(oldSubInfo); exists {
			if err := p.backoffQ.Delete(oldSubInfo); err != nil {
				return err
			}
			err := p.activeQ.Add(updateSubscription(oldSubInfo, newSub))
			if err == nil {
				p.cond.Broadcast()
			}
			return err
		}
	}
	// If subscription is not in any of the queues, we put it in the active queue.
	err := p.activeQ.Add(p.newSubscriptionInfo(newSub))
	if err == nil {
		p.cond.Broadcast()
	}
	return err
}

// Delete deletes the item from either of the two queues. It assumes the subscription is
// only in one queue.
func (p *PriorityQueue) Delete(sub *appsapi.Subscription) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	err := p.activeQ.Delete(newSubscriptionInfoNoTimestamp(sub))
	if err != nil { // The item was probably not found in the activeQ.
		if berr := p.backoffQ.Delete(newSubscriptionInfoNoTimestamp(sub)); berr != nil {
			return berr
		}
	}
	return nil
}

// Close closes the priority queue.
func (p *PriorityQueue) Close() {
	p.lock.Lock()
	defer p.lock.Unlock()
	close(p.stop)
	p.closed = true
	p.cond.Broadcast()
}

// flushBackoffQCompleted Moves all subscriptions from backoffQ which have completed backoff in to activeQ
func (p *PriorityQueue) flushBackoffQCompleted() {
	p.lock.Lock()
	defer p.lock.Unlock()
	for {
		rawSubInfo := p.backoffQ.Peek()
		if rawSubInfo == nil {
			return
		}
		sub := rawSubInfo.(*framework.SubscriptionInfo).Subscription
		boTime := p.getBackoffTime(rawSubInfo.(*framework.SubscriptionInfo))
		if boTime.After(p.clock.Now()) {
			return
		}
		_, err := p.backoffQ.Pop()
		if err != nil {
			klog.ErrorS(err, "Unable to pop subscription from backoff queue despite backoff completion.",
				"subscription", klog.KObj(sub))
			return
		}
		if aerr := p.activeQ.Add(rawSubInfo); aerr != nil {
			klog.ErrorS(aerr, "Unable to add subscription to active queue.", "subscription", klog.KObj(sub))
			return
		}
		defer p.cond.Broadcast()
	}
}

// getBackoffTime returns the time that subInfo completes backoff
func (p *PriorityQueue) getBackoffTime(subInfo *framework.SubscriptionInfo) time.Time {
	duration := p.calculateBackoffDuration(subInfo)
	backoffTime := subInfo.Timestamp.Add(duration)
	return backoffTime
}

// calculateBackoffDuration is a helper function for calculating the backoffDuration
// based on the number of attempts the subscription has made.
func (p *PriorityQueue) calculateBackoffDuration(subInfo *framework.SubscriptionInfo) time.Duration {
	duration := p.initialBackoffDuration
	for i := 1; i < subInfo.Attempts; i++ {
		duration = duration * 2
		if duration > p.maxBackoffDuration {
			return p.maxBackoffDuration
		}
	}
	return duration
}

// newSubscriptionInfo builds a SubscriptionInfo object.
func (p *PriorityQueue) newSubscriptionInfo(sub *appsapi.Subscription) *framework.SubscriptionInfo {
	now := p.clock.Now()
	return &framework.SubscriptionInfo{
		Subscription:            sub,
		Timestamp:               now,
		InitialAttemptTimestamp: now,
	}
}

// newSubscriptionInfoNoTimestamp builds a SubscriptionInfo object without timestamp.
func newSubscriptionInfoNoTimestamp(sub *appsapi.Subscription) *framework.SubscriptionInfo {
	return &framework.SubscriptionInfo{
		Subscription: sub,
	}
}

func updateSubscription(oldSubInfo interface{}, newSub *appsapi.Subscription) *framework.SubscriptionInfo {
	sInfo := oldSubInfo.(*framework.SubscriptionInfo)
	sInfo.Subscription = newSub
	return sInfo
}

func (p *PriorityQueue) subsCompareBackoffCompleted(subInfo1, subInfo2 interface{}) bool {
	sInfo1 := subInfo1.(*framework.SubscriptionInfo)
	sInfo2 := subInfo2.(*framework.SubscriptionInfo)
	bo1 := p.getBackoffTime(sInfo1)
	bo2 := p.getBackoffTime(sInfo2)
	return bo1.Before(bo2)
}

func subInfoKeyFunc(obj interface{}) (string, error) {
	return cache.MetaNamespaceKeyFunc(obj.(*framework.SubscriptionInfo).Subscription)
}
