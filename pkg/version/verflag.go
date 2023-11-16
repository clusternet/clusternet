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

package version

import (
	"fmt"
	"os"

	flag "github.com/spf13/pflag"
	apimachineryversion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/pkg/version"
	"sigs.k8s.io/yaml"

	"github.com/clusternet/clusternet/pkg/utils"
)

var (
	versionFlag *string
)

const (
	versionFlagName = "version"
)

type Version struct {
	ProgramName              string `json:"programName"`
	apimachineryversion.Info `json:",inline"`
}

func AddVersionFlag(fs *flag.FlagSet) {
	versionFlag = fs.String(versionFlagName, "", "Print version with format and quit; Available options are 'yaml', 'json' and 'short'")
	fs.Lookup(versionFlagName).NoOptDefVal = "short"
}

func PrintAndExitIfRequested(programName string) error {
	curVersion := Version{
		ProgramName: programName,
		Info:        version.Get(),
	}

	switch *versionFlag {
	case "":
		return nil
	case "short":
		fmt.Printf("%s version: %s\n", programName, curVersion.GitVersion)
	case "yaml":
		y, err := yaml.Marshal(&curVersion)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", string(y))
	case "json":
		y, err := utils.MarshalIndent(&curVersion, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", string(y))
	default:
		return fmt.Errorf("invalid output format %q", *versionFlag)
	}

	os.Exit(0)
	return nil
}
