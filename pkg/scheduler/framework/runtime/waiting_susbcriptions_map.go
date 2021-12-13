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

// This file was copied from k8s.io/kubernetes/pkg/scheduler/framework/runtime/waiting_pods_map.go and modified.

package runtime

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
)

// waitingSubscriptionsMap a thread-safe map used to maintain subscriptions waiting in the permit phase.
type waitingSubscriptionsMap struct {
	subs map[types.UID]*waitingSubscription
	mu   sync.RWMutex
}

// newWaitingSubscriptionsMap returns a new waitingSubscriptionsMap.
func newWaitingSubscriptionsMap() *waitingSubscriptionsMap {
	return &waitingSubscriptionsMap{
		subs: make(map[types.UID]*waitingSubscription),
	}
}

// add a new WaitingSubscription to the map.
func (m *waitingSubscriptionsMap) add(wp *waitingSubscription) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[wp.GetSubscription().UID] = wp
}

// remove a WaitingSubscription from the map.
func (m *waitingSubscriptionsMap) remove(uid types.UID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subs, uid)
}

// get a WaitingSubscription from the map.
func (m *waitingSubscriptionsMap) get(uid types.UID) *waitingSubscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subs[uid]
}

// iterate acquires a read lock and iterates over the WaitingSubscriptions map.
func (m *waitingSubscriptionsMap) iterate(callback func(subscription framework.WaitingSubscription)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, v := range m.subs {
		callback(v)
	}
}

// waitingSubscription represents a subscription waiting in the permit phase.
type waitingSubscription struct {
	sub            *appsapi.Subscription
	pendingPlugins map[string]*time.Timer
	s              chan *framework.Status
	mu             sync.RWMutex
}

var _ framework.WaitingSubscription = &waitingSubscription{}

// newWaitingSubscription returns a new waitingSubscription instance.
func newWaitingSubscription(sub *appsapi.Subscription, pluginsMaxWaitTime map[string]time.Duration) *waitingSubscription {
	wp := &waitingSubscription{
		sub: sub,
		// Allow() and Reject() calls are non-blocking. This property is guaranteed
		// by using non-blocking send to this channel. This channel has a buffer of size 1
		// to ensure that non-blocking send will not be ignored - possible situation when
		// receiving from this channel happens after non-blocking send.
		s: make(chan *framework.Status, 1),
	}

	wp.pendingPlugins = make(map[string]*time.Timer, len(pluginsMaxWaitTime))
	// The time.AfterFunc calls wp.Reject which iterates through pendingPlugins map. Acquire the
	// lock here so that time.AfterFunc can only execute after newWaitingSubscription finishes.
	wp.mu.Lock()
	defer wp.mu.Unlock()
	for k, v := range pluginsMaxWaitTime {
		plugin, waitTime := k, v
		wp.pendingPlugins[plugin] = time.AfterFunc(waitTime, func() {
			msg := fmt.Sprintf("rejected due to timeout after waiting %v at plugin %v",
				waitTime, plugin)
			wp.Reject(plugin, msg)
		})
	}

	return wp
}

// GetSubscription returns a reference to the waiting subscription.
func (w *waitingSubscription) GetSubscription() *appsapi.Subscription {
	return w.sub
}

// GetPendingPlugins returns a list of pending permit plugin's name.
func (w *waitingSubscription) GetPendingPlugins() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	plugins := make([]string, 0, len(w.pendingPlugins))
	for p := range w.pendingPlugins {
		plugins = append(plugins, p)
	}

	return plugins
}

// Allow declares the waiting subscription is allowed to be scheduled by plugin pluginName.
// If this is the last remaining plugin to allow, then a success signal is delivered
// to unblock the subscription.
func (w *waitingSubscription) Allow(pluginName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if timer, exist := w.pendingPlugins[pluginName]; exist {
		timer.Stop()
		delete(w.pendingPlugins, pluginName)
	}

	// Only signal success status after all plugins have allowed
	if len(w.pendingPlugins) != 0 {
		return
	}

	// The select clause works as a non-blocking send.
	// If there is no receiver, it's a no-op (default case).
	select {
	case w.s <- framework.NewStatus(framework.Success, ""):
	default:
	}
}

// Reject declares the waiting subscription unschedulable.
func (w *waitingSubscription) Reject(pluginName, msg string) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, timer := range w.pendingPlugins {
		timer.Stop()
	}

	// The select clause works as a non-blocking send.
	// If there is no receiver, it's a no-op (default case).
	select {
	case w.s <- framework.NewStatus(framework.Unschedulable, msg).WithFailedPlugin(pluginName):
	default:
	}
}
