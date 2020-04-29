// Copyright 2019 The Meshery Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/layer5io/meshery/models"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/asaskevich/govalidator"
	"github.com/spf13/cobra"
)

var (
	testURL            = ""
	testName           = ""
	testMesh           = ""
	qps                = ""
	concurrentRequests = ""
	testDuration       = ""
	loadGenerator      = ""
	filePath           = ""
	tokenPath          = ""
)

var perfDetails = `
Performance Testing & Benchmarking using Meshery CLI.

Usage:
  mesheryctl perf --[flags]

Available Flags for Performance Command:
  name[string]                  (optional) A short descriptor to serve as reference for this test. If not provided, a random name will be generate.
  url[string]                   (required) URL endpoint to send requests.
  duration[string]              (required) Length of time to perform test (e.g 30s, 15m, 1hr). See standard notation https://golang.org/pkg/time/#ParseDuration
  load-generator[string]        (optional) Name of load generator to be used to perform test (default: "fortio")
  mesh[string]              	(optional) Name of the service mesh to be tested (default: "None")
  provider[string]            	(required) Choice of Provider (default: "Meshery")
  concurrent-requests[string]   (required) Number of parallel requests to be sent (default: "1")
  qps[string]                   (required) Queries per second (default: "0")
  file[string]			        (optional) file containing SMPS-compatible test configuration. See https://github.com/layer5io/service-mesh-performance-specification
  help                          Help for perf subcommand

url, duration, concurrent-requests, and qps can be considered optional flags if specified through an SMPS compatible yaml file using --file

Example usage of perf subcommand :

 mesheryctl perf --name "a quick stress test" --url http://192.168.1.15/productpage --qps 300 --concurrent-requests 2 --duration 30s --token "provider=Meshery"
`

const tokenName = "token"
const providerName = "meshery-provider"

var seededRand = rand.New(
	rand.NewSource(time.Now().UnixNano()))

// StringWithCharset generates a random string with a given length
func StringWithCharset(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz" + "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func AddAuthDetails(req *http.Request, filepath string) error {
	file, err := ioutil.ReadFile(filepath)
	if err != nil {
		log.Errorf("File read failed : %v", err.Error())
		return err
	}
	var tokenObj map[string]string
	if err := json.Unmarshal(file, &tokenObj); err != nil {
		log.Errorf("Token file invalid : %v", err.Error())
		return err
	}
	req.AddCookie(&http.Cookie{
		Name:     tokenName,
		Value:    tokenObj[tokenName],
		HttpOnly: true,
	})
	req.AddCookie(&http.Cookie{
		Name:     providerName,
		Value:    tokenObj[providerName],
		HttpOnly: true,
	})
	return nil
}

func UpdateAuthDetails(filepath string) error {
	req, err := http.NewRequest("GET", mesheryAuthToken, bytes.NewBuffer([]byte("")))
	if err != nil {
		return err
	}
	if err := AddAuthDetails(req, filepath); err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	defer SafeClose(resp.Body)

	if err != nil {
		return err
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(filepath, data, os.ModePerm); err != nil {
		return err
	}
	return nil
}

// perfCmd represents the Performance command
var perfCmd = &cobra.Command{
	Use:   "perf",
	Short: "Performance Testing",
	Long:  `Performance Testing & Benchmarking using Meshery CLI.`,
	Run: func(cmd *cobra.Command, args []string) {
		//Check prerequisite
		preReqCheck()

		// if len(args) == 0 {
		// log.Print(perfDetails)
		// return
		// }
		if filePath != "" {
			var t models.PerformanceSpec
			err := yaml.Unmarshal([]byte(filePath), &t)

			if err != nil {
				log.Errorf("Error: Invalid yaml file.\n%v", err)
			}
			if testDuration == "" {
				testDuration = fmt.Sprintf("%fs", t.EndTime.Sub(t.StartTime).Seconds())
			}
			if testURL == "" {
				testURL = t.EndpointURL
			}
			if concurrentRequests == "" {
				concurrentRequests = fmt.Sprintf("%d", t.Client.Connections)
			}
			if qps == "" {
				qps = fmt.Sprintf("%f", t.Client.Rps)
			}
		}

		if len(testName) <= 0 {
			log.Print("Test Name not provided")
			testName = StringWithCharset(8)
			log.Print("Using random test name: ", testName)
		}

		postData := ""

		startTime := time.Now()
		duration, err := time.ParseDuration(testDuration)
		if err != nil {
			log.Fatal("Error: Test duration invalid")
			return
		}

		endTime := startTime.Add(duration)

		postData = postData + "start_time: " + startTime.Format(time.RFC3339)
		postData = postData + "\nend_time: " + endTime.Format(time.RFC3339)

		if len(testURL) > 0 {
			postData = postData + "\nendpoint_url: " + testURL
		} else {
			log.Fatal("\nError: Please enter a test URL")
			return
		}

		// Method to check if the entered Test URL is valid or not
		var validURL bool = govalidator.IsURL(testURL)

		if !validURL {
			log.Fatal("\nError: Please enter a valid test URL")
			return
		}

		postData = postData + "\nclient:"
		postData = postData + "\n connections: " + concurrentRequests
		postData = postData + "\n rps: " + qps

		req, err := http.NewRequest("POST", mesheryURL, bytes.NewBuffer([]byte(postData)))
		if err != nil {
			log.Print("\nError in building the request")
			log.Fatal("Error Message:\n", err)
			return
		}

		if err := AddAuthDetails(req, tokenPath); err != nil {
			log.Printf("Error Authorizing request : %v", err.Error())
			return
		}

		q := req.URL.Query()
		q.Add("name", testName)
		q.Add("loadGenerator", loadGenerator)
		if len(testMesh) > 0 {
			q.Add("mesh", testMesh)
		}
		req.URL.RawQuery = q.Encode()

		client := &http.Client{}
		resp, err := client.Do(req)

		if err != nil {
			log.Print("\nFailed to make request to URL:", testURL)
			log.Fatal("Error Message:\n", err)
			return
		}
		log.Print("Initiating Performance test ...")
		log.Printf(resp.Status)

		defer SafeClose(resp.Body)
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Erro reading body: %v", err.Error())
			return
		}
		log.Print(string(data))

		if err := UpdateAuthDetails(tokenPath); err != nil {
			log.Printf("Error updating token : %v", err.Error())
			return
		}

		log.Print("Test Completed Successfully!")
	},
}

func init() {
	perfCmd.Flags().StringVar(&testURL, "url", "", "(required) Endpoint URL to test")
	perfCmd.Flags().StringVar(&testName, "name", "", "(optional) Name of the Test")
	perfCmd.Flags().StringVar(&testMesh, "mesh", "", "(optional) Name of the Service Mesh")
	perfCmd.Flags().StringVar(&qps, "qps", "0", "(optional) Queries per second")
	perfCmd.Flags().StringVar(&concurrentRequests, "concurrent-requests", "1", "(required) Number of Parallel Requests")
	perfCmd.Flags().StringVar(&testDuration, "duration", "30s", "(optional) Length of test (e.g. 10s, 5m, 2h). For more, see https://golang.org/pkg/time/#ParseDuration")
	perfCmd.Flags().StringVar(&tokenPath, "token", authConfigFile, "(optional) Path to meshery auth config")
	perfCmd.Flags().StringVar(&loadGenerator, "load-generator", "fortio", "(optional) Load-Generator to be used (fortio/wrk2)")
	perfCmd.Flags().StringVar(&filePath, "file", "", "(optional) file containing SMPS-compatible test configuration. For more, see https://github.com/layer5io/service-mesh-performance-specification")
	rootCmd.AddCommand(perfCmd)
}
