package deployer

import (
	"fmt"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

func FormatFeed(feed appsapi.Feed) string {
	return fmt.Sprintf(" APIVersion: %s, Kind: %s with Name %s in Namespace %s that matches %q", feed.APIVersion, feed.Kind, feed.Name, feed.Namespace, feed.FeedSelector.String())
}
