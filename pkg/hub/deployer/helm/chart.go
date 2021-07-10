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

package helm

import (
	"fmt"
	"reflect"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

var (
	settings = cli.New()
)

// LocateHelmChart will looks for a chart from repository and load it.
func LocateHelmChart(chartRepo, chartName, chartVersion string) (*chart.Chart, error) {
	client := action.NewInstall(nil)
	client.ChartPathOptions.RepoURL = chartRepo
	client.ChartPathOptions.Version = chartVersion

	cp, err := client.ChartPathOptions.LocateChart(chartName, settings)
	if err != nil {
		return nil, err
	}

	klog.V(5).Infof("chart %s/%s:%s locates at: %s", chartRepo, chartName, chartVersion, cp)

	// Check chart dependencies to make sure all are present in /charts
	chartRequested, err := loader.Load(cp)
	if err != nil {
		return nil, err
	}

	if err := CheckIfInstallable(chartRequested); err != nil {
		return nil, err
	}

	return chartRequested, nil
}

// CheckIfInstallable validates if a chart can be installed
// only application chart type is installable
func CheckIfInstallable(chart *chart.Chart) error {
	switch chart.Metadata.Type {
	case "", "application":
		return nil
	}
	return fmt.Errorf("chart %s is %s, which is not installable", chart.Name(), chart.Metadata.Type)
}

func InstallRelease(cfg *action.Configuration, hr *appsapi.HelmRelease,
	chart *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
	client := action.NewInstall(cfg)
	client.ReleaseName = hr.Name
	client.CreateNamespace = true
	client.Namespace = hr.Spec.TargetNamespace

	return client.Run(chart, vals)
}

func UpgradeRelease(cfg *action.Configuration, hr *appsapi.HelmRelease,
	chart *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
	klog.V(5).Infof("Upgrading HelmRelease %s", klog.KObj(hr))
	client := action.NewUpgrade(cfg)
	client.Namespace = hr.Spec.TargetNamespace
	return client.Run(hr.Name, chart, vals)
}

func UninstallRelease(cfg *action.Configuration, hr *appsapi.HelmRelease) error {
	client := action.NewUninstall(cfg)
	_, err := client.Run(hr.Name)
	if err != nil {
		if strings.Contains(err.Error(), "Release not loaded") {
			return nil
		}
		return err
	}
	return nil
}

func ReleaseNeedsUpgrade(rel *release.Release, hr *appsapi.HelmRelease, chart *chart.Chart, vals map[string]interface{}) bool {
	if rel.Name != hr.Name {
		return true
	}
	if rel.Namespace != hr.Spec.TargetNamespace {
		return true
	}

	if rel.Chart.Metadata.Name != hr.Spec.Chart {
		return true
	}
	if rel.Chart.Metadata.Version != hr.Spec.ChartVersion {
		return true
	}

	if !reflect.DeepEqual(rel.Config, vals) {
		return true
	}

	return false
}

func UpdateRepo(repoURL string) error {
	klog.V(4).Infof("updating helm repo %s", repoURL)

	entry := repo.Entry{
		URL:                   repoURL,
		InsecureSkipTLSverify: true,
	}
	cr, err := repo.NewChartRepository(&entry, getter.All(settings))
	if err != nil {
		return err
	}

	if _, err := cr.DownloadIndexFile(); err != nil {
		return err
	}

	klog.V(5).Infof("successfully got an repository update for %s", repoURL)
	return nil
}
