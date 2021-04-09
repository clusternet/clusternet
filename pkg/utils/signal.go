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

package utils

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/klog/v2"
)

var gracefulStopCh = make(chan os.Signal, 2)

// GracefulStopWithContext registered for SIGTERM and SIGINT with a cancel context returned.
// When one of these signals get caught, the returned context's channel will be closed.
// If a second signal is caught, then the program is terminated with exit code 1.
func GracefulStopWithContext() context.Context {
	signal.Notify(gracefulStopCh, syscall.SIGTERM, syscall.SIGINT)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		// waiting for os signal to stop the program
		oscall := <-gracefulStopCh
		klog.Warningf("shutting down, caused by %s", oscall)
		cancel()
		<-gracefulStopCh
		os.Exit(1)
	}()

	return ctx
}
