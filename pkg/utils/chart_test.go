package utils

import (
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
)

type fakeOCIRegistryClient struct {
	tags   []string
	err    error
	gotRef string
}

func (f *fakeOCIRegistryClient) Tags(ref string) ([]string, error) {
	f.gotRef = ref
	return f.tags, f.err
}

func TestFindOCIChartUsesPlainHTTPAndBuildsOCIRef(t *testing.T) {
	originalFactory := newOCIRegistryClient
	defer func() {
		newOCIRegistryClient = originalFactory
	}()

	fakeClient := &fakeOCIRegistryClient{tags: []string{"0.1.0"}}
	var gotPlainHTTP bool
	newOCIRegistryClient = func(plainHTTP bool) (ociRegistryClient, error) {
		gotPlainHTTP = plainHTTP
		return fakeClient, nil
	}

	found, err := FindOCIChart("oci://10.0.208.12:5000/test", "nginx", "0.1.0", true)
	if err != nil {
		t.Fatalf("FindOCIChart returned error: %v", err)
	}
	if !found {
		t.Fatalf("FindOCIChart returned found=false, want true")
	}
	if !gotPlainHTTP {
		t.Fatalf("FindOCIChart did not enable plain HTTP")
	}
	if fakeClient.gotRef != "10.0.208.12:5000/test/nginx" {
		t.Fatalf("FindOCIChart queried ref %q, want %q", fakeClient.gotRef, "10.0.208.12:5000/test/nginx")
	}
}

func TestNewOCIHTTPClientUsesRegistryTimeout(t *testing.T) {
	client := newOCIHTTPClient()

	if client.Timeout != 15*time.Second {
		t.Fatalf("HTTP client timeout = %s, want %s", client.Timeout, 15*time.Second)
	}
}

func TestConfigureChartPathOptionsUsesPlainHTTPForOCI(t *testing.T) {
	client := action.NewInstall(&action.Configuration{})

	fullChartName := configureChartPathOptions(
		client,
		"oci://10.0.208.12:5000/test",
		"user",
		"pass",
		"nginx",
		"0.1.0",
		true,
	)

	if !client.ChartPathOptions.PlainHTTP {
		t.Fatalf("ChartPathOptions.PlainHTTP = false, want true")
	}
	if client.ChartPathOptions.RepoURL != "" {
		t.Fatalf("ChartPathOptions.RepoURL = %q, want empty for OCI", client.ChartPathOptions.RepoURL)
	}
	if client.ChartPathOptions.Username != "user" {
		t.Fatalf("ChartPathOptions.Username = %q, want %q", client.ChartPathOptions.Username, "user")
	}
	if client.ChartPathOptions.Password != "pass" {
		t.Fatalf("ChartPathOptions.Password = %q, want %q", client.ChartPathOptions.Password, "pass")
	}
	if fullChartName != "oci://10.0.208.12:5000/test/nginx" {
		t.Fatalf("configureChartPathOptions returned %q, want %q", fullChartName, "oci://10.0.208.12:5000/test/nginx")
	}
}
