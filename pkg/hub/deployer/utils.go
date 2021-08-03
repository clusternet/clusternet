package deployer

import (
	"fmt"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/clusternet/clusternet/pkg/known"
)

func FormatFeed(feed appsapi.Feed) string {
	return fmt.Sprintf(" APIVersion: %s, Kind: %s with Name %s in Namespace %s that matches %q", feed.APIVersion, feed.Kind, feed.Name, feed.Namespace, feed.FeedSelector.String())
}

func IsFeedMatches(f appsapi.Feed, manifest *appsapi.Manifest) bool {
	if len(f.Name) > 0 {
		if f.Namespace == manifest.Namespace &&
			f.Name == manifest.Name &&
			f.Kind == manifest.Labels[known.ConfigKindLabel] &&
			f.APIVersion == manifest.Labels[known.ConfigVersionLabel] {
			return true
		} else {
			return false
		}
	} else {
		selector, err := metav1.LabelSelectorAsSelector(f.FeedSelector)
		if err != nil {
			return false
		}
		return selector.Matches(labels.Set(manifest.Labels))
	}
}

func GetManifestApiVersion(manifest *appsapi.Manifest) string {
	gv := schema.GroupVersion{
		Group:   manifest.Labels[known.ConfigGroupLabel],
		Version: manifest.Labels[known.ConfigVersionLabel],
	}
	return gv.String()
}
