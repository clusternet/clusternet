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

package template

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
)

const (
	defaultSize = 5
)

type FixupFunc func(runtime.Object) runtime.Object

type WatchWrapper struct {
	// watch and report changes
	watcher watch.Interface
	// used to correct the object before we send it to the serializer
	fixup FixupFunc

	ctx context.Context

	targetGVK schema.GroupVersionKind

	result chan watch.Event
	sync.Mutex
}

func (w *WatchWrapper) Stop() {
	w.Lock()
	defer w.Unlock()

	w.watcher.Stop()
}

func (w *WatchWrapper) ResultChan() <-chan watch.Event {
	return w.result
}

func (w *WatchWrapper) Run() {
	w.Lock()
	defer w.Unlock()

	ch := w.watcher.ResultChan()
	done := w.ctx.Done()

	for {
		select {
		case <-done:
			return
		case event, ok := <-ch:
			if !ok {
				// End of results.
				return
			}

			if w.fixup == nil {
				w.result <- event
				continue
			}

			obj := event.Object
			switch event.Type {
			case watch.Bookmark:
				obj = w.transformBookMarkEvent(obj)
			case watch.Error:
				klog.Warningf("got a watch error: %v", event)
			default:
				obj = w.fixup(event.Object)

			}
			w.result <- watch.Event{
				Type:   event.Type,
				Object: obj,
			}
		}
	}
}

func (w *WatchWrapper) transformBookMarkEvent(object runtime.Object) runtime.Object {
	result := &unstructured.Unstructured{}
	objMeta, err := meta.Accessor(object)
	if err != nil {
		klog.Errorf("got a transform error: %v", err)
		return object
	}
	result.SetResourceVersion(objMeta.GetResourceVersion())
	result.SetGroupVersionKind(w.targetGVK)
	result.SetAnnotations(map[string]string{
		"k8s.io/initial-events-end": "true",
	})

	return result
}

func NewWatchWrapper(ctx context.Context, watcher watch.Interface, fixup FixupFunc, targetGVK schema.GroupVersionKind, size int) *WatchWrapper {
	return &WatchWrapper{
		ctx:       ctx,
		watcher:   watcher,
		fixup:     fixup,
		targetGVK: targetGVK,
		result:    make(chan watch.Event, size),
	}
}

var _ watch.Interface = &WatchWrapper{}
