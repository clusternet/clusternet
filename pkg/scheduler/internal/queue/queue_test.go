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

package queue

import (
	"errors"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
)

var lowPriority, midPriority, highPriority = int32(0), int32(100), int32(1000)

var lowPrioritySub = appsapi.Subscription{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "lps",
		Namespace: "ns1",
		UID:       "lpsns1",
	},
	Spec: appsapi.SubscriptionSpec{
		Priority: &lowPriority,
	},
}

var midPrioritySub = appsapi.Subscription{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "mps",
		Namespace: "ns1",
		UID:       "mpsns1",
	},
	Spec: appsapi.SubscriptionSpec{
		Priority: &midPriority,
	},
}

var highPrioritySub = appsapi.Subscription{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "hps",
		Namespace: "ns1",
		UID:       "hpsns1",
	},
	Spec: appsapi.SubscriptionSpec{
		Priority: &highPriority,
	},
}

func createAndRunPriorityQueue() *PriorityQueue {
	q := NewPriorityQueue(framework.Less)
	q.Run()
	return q
}

func TestPriorityQueue_Add(t *testing.T) {
	q := createAndRunPriorityQueue()
	if err := q.Add(&lowPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	if err := q.Add(&midPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	if err := q.Add(&highPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	if s, err := q.Pop(); err != nil || s.Subscription != &highPrioritySub {
		t.Errorf("Expected: %v after Pop, but got: %v", highPrioritySub.Name, s.Subscription.Name)
	}
	if s, err := q.Pop(); err != nil || s.Subscription != &midPrioritySub {
		t.Errorf("Expected: %v after Pop, but got: %v", midPrioritySub.Name, s.Subscription.Name)
	}
	if s, err := q.Pop(); err != nil || s.Subscription != &lowPrioritySub {
		t.Errorf("Expected: %v after Pop, but got: %v", lowPrioritySub.Name, s.Subscription.Name)
	}
}

func TestPriorityQueue_AddUnschedulableIfNotPresent(t *testing.T) {
	q := createAndRunPriorityQueue()
	if err := q.AddUnschedulableIfNotPresent(newSubscriptionInfoNoTimestamp(&highPrioritySub)); err != nil {
		t.Errorf("add unschedulable sub failed: %v", err)
	}
	if err := q.Add(&lowPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	if s, err := q.Pop(); err != nil || s.Subscription != &lowPrioritySub {
		t.Errorf("Expected: %v after Pop, but got: %v", lowPrioritySub.Name, s.Subscription.Name)
	}
	if s, err := q.Pop(); err != nil || s.Subscription != &highPrioritySub {
		t.Errorf("Expected: %v after Pop, but got: %v", highPrioritySub.Name, s.Subscription.Name)
	}
}

func TestPriorityQueue_Close(t *testing.T) {
	q := createAndRunPriorityQueue()
	q.Close()

	closeErr := fmt.Errorf(queueClosed)

	_, err := q.Pop()
	if errors.Is(err, closeErr) {
		t.Errorf("Expected: %v after close, but got: %v", closeErr, err)
	}
}

func TestPriorityQueue_Pop(t *testing.T) {
	q := createAndRunPriorityQueue()
	if err := q.Add(&lowPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	for i := 0; i < 3; i++ {
		s, err := q.Pop()
		if err != nil || s.Subscription != &lowPrioritySub {
			t.Errorf("Expected: %v after Pop, but got: %v", lowPrioritySub.Name, s.Subscription.Name)
		}
		if s.Attempts != i+1 {
			t.Errorf("Expected: attempt %d, but get %d", i+1, s.Attempts)
		}
		if aerr := q.AddUnschedulableIfNotPresent(s); aerr != nil {
			t.Errorf("add failed: %v", aerr)
		}
	}
}

func TestPriorityQueue_Delete(t *testing.T) {
	q := createAndRunPriorityQueue()
	if err := q.Add(&lowPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	if err := q.Add(&midPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	if err := q.Add(&highPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	if err := q.Delete(&midPrioritySub); err != nil {
		t.Errorf("delete failed: %v", err)
	}
	if s, err := q.Pop(); err != nil || s.Subscription != &highPrioritySub {
		t.Errorf("Expected: %v after Pop, but got: %v", highPrioritySub.Name, s.Subscription.Name)
	}
	if s, err := q.Pop(); err != nil || s.Subscription != &lowPrioritySub {
		t.Errorf("Expected: %v after Pop, but got: %v", lowPrioritySub.Name, s.Subscription.Name)
	}
}

func TestPriorityQueue_Update(t *testing.T) {
	q := createAndRunPriorityQueue()
	if err := q.Add(&lowPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	if err := q.Add(&midPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	if err := q.Add(&highPrioritySub); err != nil {
		t.Errorf("add failed: %v", err)
	}
	newLow := lowPrioritySub.DeepCopy()
	newLowPriority := int32(10000)
	newLow.Spec.Priority = &newLowPriority
	if err := q.Update(&lowPrioritySub, newLow); err != nil {
		t.Errorf("update failed: %v", err)
	}
	if s, err := q.Pop(); err != nil || s.Subscription != newLow {
		t.Errorf("Expected: %v after Pop, but got: %v", newLow.Name, s.Subscription.Name)
	}
	if s, err := q.Pop(); err != nil || s.Subscription != &highPrioritySub {
		t.Errorf("Expected: %v after Pop, but got: %v", highPrioritySub.Name, s.Subscription.Name)
	}
	if s, err := q.Pop(); err != nil || s.Subscription != &midPrioritySub {
		t.Errorf("Expected: %v after Pop, but got: %v", midPrioritySub.Name, s.Subscription.Name)
	}
}
