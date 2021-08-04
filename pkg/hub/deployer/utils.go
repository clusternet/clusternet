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

package deployer

import (
	"fmt"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

func FormatFeed(feed appsapi.Feed) string {
	return fmt.Sprintf(" APIVersion: %s, Kind: %s with Name %s in Namespace %s that matches %q", feed.APIVersion, feed.Kind, feed.Name, feed.Namespace, feed.FeedSelector.String())
}
