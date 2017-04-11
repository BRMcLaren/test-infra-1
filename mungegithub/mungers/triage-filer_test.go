/*
Copyright 2017 The Kubernetes Authors.

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

package mungers

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/github"
	"k8s.io/test-infra/mungegithub/mungers/mungerutil"
	"k8s.io/test-infra/mungegithub/mungers/testowner"
)

var (
	// json1issue2job2test is a small example of the JSON format that loadClusters reads.
	// It includes all of the different types of formatting that is accepted. Namely both types
	// of buildnum to row index mappings.
	json1issue2job2test []byte
	// buildTimes is a map containing the build times of builds found in the json1issue2job2test JSON data.
	buildTimes map[int]int64
	// sampleOwnerCSV is a small sample test owners csv file that contains both real test owner
	// data and owner/SIG info for a fake test in json1issue2job2test.
	sampleOwnerCSV []byte
	// latestBuildTime is the end time of the sliding window for these tests.
	latestBuildTime int64
)

func init() {
	latestBuildTime = int64(947462400) // Jan 10, 2000
	hourSecs := int64(60 * 60)
	dailySecs := hourSecs * 24
	buildTimes = map[int]int64{
		41:  latestBuildTime - (dailySecs * 10),           // before window start
		42:  latestBuildTime + hourSecs - (dailySecs * 5), // just inside window start
		43:  latestBuildTime + hourSecs - (dailySecs * 4),
		52:  latestBuildTime + hourSecs - (dailySecs * 2),
		142: latestBuildTime - dailySecs, // a day before window end
		144: latestBuildTime - hourSecs,  // an hour before window end
	}

	json1issue2job2test = []byte(
		`{
		"builds":
		{
			"cols":
			{
				"started":
				[
					` + strconv.FormatInt(buildTimes[41], 10) + `,
					` + strconv.FormatInt(buildTimes[42], 10) + `,
					` + strconv.FormatInt(buildTimes[43], 10) + `,
					10000000,
					10000000,
					10000000,
					10000000,
					10000000,
					10000000,
					10000000,
					10000000,
					` + strconv.FormatInt(buildTimes[52], 10) + `,
					` + strconv.FormatInt(buildTimes[142], 10) + `,
					10000000,
					` + strconv.FormatInt(buildTimes[144], 10) + `
				]
			},
			"jobs":
			{
				"jobname1": [41, 12, 0],
				"jobname2": {"142": 12, "144": 14},
				"pr:jobname3": {"200": 13}
			},
			"job_paths":
			{
				"jobname1": "path//to/jobname1",
				"jobname2": "path//to/jobname2",
				"pr:jobname3": "path//to/pr:jobname3"
			}
		},
		"clustered":
		[
			{
				"id": "key_hash",
				"key": "key_text",
				"tests": 
				[
					{
						"jobs":
						[
							{
								"builds": [42, 43, 52],
								"name": "jobname1"
							},
							{
								"builds": [144],
								"name": "jobname2"
							}
						],
						"name": "testname1"
					},
					{
						"jobs":
						[
							{
								"builds": [41, 42, 43],
								"name": "jobname1"
							},
							{
								"builds": [200],
								"name": "pr:jobname3"
							}
						],
						"name": "testname2"
					}
				],
				"text":	"issue_name"
			}
		]
	}`)

	sampleOwnerCSV = []byte(
		`name,owner,auto-assigned,sig
DEFAULT,rmmh/spxtr/ixdy/apelisse/fejta,0,
Sysctls should support sysctls,Random-Liu,1,node
Sysctls should support unsafe sysctls which are actually whitelisted,deads2k,1,node
testname1,cjwagner,1,sigarea
ThirdParty resources Simple Third Party creating/deleting thirdparty objects works,luxas,1,api-machinery
Upgrade cluster upgrade should maintain a functioning cluster,luxas,1,cluster-lifecycle
Upgrade master upgrade should maintain a functioning cluster,xiang90,1,cluster-lifecycle
Upgrade node upgrade should maintain a functioning cluster,zmerlynn,1,cluster-lifecycle
Variable Expansion should allow composing env vars into new env vars,derekwaynecarr,0,node
Variable Expansion should allow substituting values in a container's args,dchen1107,1,node
Variable Expansion should allow substituting values in a container's command,mml,1,node
Volume Disk Format verify disk format type - eagerzeroedthick is honored for dynamically provisioned pv using storageclass,piosz,1,`)
}

// NewTestTriageFiler creates a new TriageFiler that isn't connected to an IssueCreator so that
// it can be used for testing.
func NewTestTriageFiler() *TriageFiler {
	return &TriageFiler{
		topClustersCount: 3,
		windowDays:       5, // This is the important value for testing.
		creator:          &IssueCreator{},
	}
}

func TestTFParserSimple(t *testing.T) {
	f := NewTestTriageFiler()
	issues, err := f.loadClusters(json1issue2job2test)
	if err != nil {
		t.Fatalf("Error parsing triage data: %v\n", err)
	}

	if len(issues) != 1 {
		t.Error("Expected 1 issue, got ", len(issues))
	}
	if issues[0].Text != "issue_name" {
		t.Error("Expected Text='issue_name', got ", issues[0].Text)
	}
	if issues[0].Id != "key_hash" {
		t.Error("Expected Id='key_hash', got ", issues[0].Id)
	}
	// Note that 5 builds failed in json, but one is outside the time window.
	if issues[0].totalBuilds != 4 {
		t.Error("Expected totalBuilds failed = 4, got ", issues[0].totalBuilds)
	}
	// Note that 3 jobs failed in json, but one is a PR job and should be ignored.
	if issues[0].totalJobs != 2 || len(issues[0].jobs) != 2 {
		t.Error("Expected totalJobs failed = 2, got ", issues[0].totalJobs)
	}
	if issues[0].totalTests != 2 || len(issues[0].Tests) != 2 {
		t.Error("Expected totalTests failed = 2, got ", issues[0].totalTests)
	}
	if f.data.Builds.JobPaths["jobname1"] != "path//to/jobname1" ||
		f.data.Builds.JobPaths["jobname2"] != "path//to/jobname2" {
		t.Error("Invalid jobpath. got jobname1: ", f.data.Builds.JobPaths["jobname1"],
			" and jobname2: ", f.data.Builds.JobPaths["jobname2"])
	}

	checkBuildStart(t, f, "jobname1", 42, buildTimes[42])
	checkBuildStart(t, f, "jobname1", 52, buildTimes[52])
	checkBuildStart(t, f, "jobname2", 144, buildTimes[144])

	checkCluster(issues[0], t)
}

func checkBuildStart(t *testing.T, f *TriageFiler, jobName string, build int, expected int64) {
	row, err := f.data.Builds.Jobs[jobName].rowForBuild(build)
	if err != nil {
		t.Errorf("Failed to look up row index for %s:%d", jobName, build)
	}
	actual := f.data.Builds.Cols.Started[row]
	if actual != expected {
		t.Errorf("Expected build start time for build %s:%d to be %d, got %d.", jobName, build, expected, actual)
	}
}

// checkCluster checks that the properties that should be true for all clusters hold for this cluster
func checkCluster(clust *Cluster, t *testing.T) {
	if !checkTopFailingsSorted(clust) {
		t.Errorf("Top tests or jobs is improperly sorted for cluster: %s\n", clust.Id)
	}
	if clust.totalJobs != len(clust.jobs) {
		t.Errorf("Total job count is invalid for cluster: %s\n", clust.Id)
	}
	if clust.totalTests != len(clust.Tests) {
		t.Errorf("Total test count is invalid for cluster: %s\n", clust.Id)
	}
	title := clust.Title()
	body := clust.Body(nil)
	id := clust.ID()
	if len(title) <= 0 {
		t.Errorf("Title of cluster: %s is empty!", clust.Id)
	}
	if len(body) <= 0 {
		t.Errorf("Body of cluster: %s is empty!", clust.Id)
	}
	if len(id) <= 0 {
		t.Errorf("ID of cluster: %s is empty!", clust.Id)
	}
	if !strings.Contains(body, id) {
		t.Errorf("The body text for cluster: %s does not contain its ID!\n", clust.Id)
	}
	//ensure that 'kind/flake' is among the label set
	found := false
	for _, label := range clust.Labels() {
		if label == "kind/flake" {
			found = true
		} else {
			if label == "" {
				t.Errorf("Cluster: %s has an empty label!\n", clust.Id)
			}
		}
	}
	if !found {
		t.Errorf("The cluster: %s does not have the label 'kind/flake'!", clust.Id)
	}
}

// TestTFValidateRealClusters fetches fresh cluster data and checks that the clusters parsed from it
// are valid and can be sorted properly by topClusters.
func TestTFValidateRealClusters(t *testing.T) {
	f := NewTestTriageFiler()
	raw, err := mungerutil.ReadHTTP(clusterDataURL)
	if err != nil {
		t.Fatal("Failed to fetch file at url '" + clusterDataURL + "' errmsg: " + err.Error())
	}
	clusters, err := f.loadClusters(raw)
	if err != nil {
		t.Fatalf("Failed to load clusters: %v", err)
	}
	for _, clust := range clusters {
		checkCluster(clust, t)
	}

	sorted := topClusters(clusters, len(clusters))
	for i := 1; i < len(clusters); i++ {
		if sorted[i-1].totalBuilds < sorted[i].totalBuilds {
			t.Errorf("Top Clusters were improperly sorted. '%s' should come before '%s'\n", sorted[i].Id, sorted[i-1].Id)
		}
	}
}

func TestTFOwnersAndSIGs(t *testing.T) {
	// This test is really more of a test of OwnerMapper and the IssueCreator's 'TestsSIGs' and 'TestsOwners'
	// methods, but it fits well here because it can use real testnames from the clustered failure data.
	sigregexp := regexp.MustCompile("sig/.*")

	f := NewTestTriageFiler()
	raw, err := mungerutil.ReadHTTP(clusterDataURL)
	if err != nil {
		t.Fatal("Failed to fetch file at url '" + clusterDataURL + "' errmsg: " + err.Error())
	}
	f.creator.owners, err = testowner.NewOwnerListFromCsv(bytes.NewReader(sampleOwnerCSV))
	f.creator.maxSIGCount = 3
	f.creator.maxAssignees = 3
	if err != nil {
		t.Fatalf("Failed to create a new OwnersList.  errmsg: %v", err)
	}

	clusters, err := f.loadClusters(raw)
	if err != nil {
		t.Fatalf("Failed to load clusters: %v", err)
	}
	for _, clust := range clusters {
		owners := clust.Owners()
		labels := clust.Labels()
		if len(owners) > f.creator.maxAssignees {
			t.Errorf("Cluster: %s has too many assignees: %v\n", clust.Id, owners)
		}
		for _, owner := range owners {
			if owner == "" {
				t.Errorf("Cluster: %s has at least one empty string owner!\n", clust.Id)
				break
			}
		}
		// Check that the 'kind/flake' label still exists when sig labels are used.
		sigsCount := 0
		foundKind := false
		for _, label := range labels {
			if label == "kind/flake" {
				foundKind = true
			} else {
				if sigregexp.MatchString(label) {
					sigsCount++
				}
			}
		}
		if !foundKind {
			t.Errorf("The cluster: %s does not have the label 'kind/flake'!", clust.Id)
		}
		if sigsCount > f.creator.maxSIGCount {
			t.Errorf("The cluster: %s has too many 'sig/.*' labels!\n", clust.Id)
		}
	}

	// Check that the usernames and sig areas are as expected (no stay commas or anything like that).
	clusters, err = f.loadClusters(json1issue2job2test)
	if err != nil {
		t.Fatalf("Failed to load clusters: %v", err)
	}
	foundSIG := false
	for _, label := range clusters[0].Labels() {
		if label == "sig/sigarea" {
			foundSIG = true
			break
		}
	}
	if !foundSIG {
		t.Errorf("Failed to get the SIG for cluster: %s\n", clusters[0].Id)
	}
	foundUser := false
	for _, user := range clusters[0].Owners() {
		if user == "cjwagner" {
			foundUser = true
			break
		}
	}
	if !foundUser {
		t.Errorf("Failed to get the owner for cluster: %s\n", clusters[0].Id)
	}
}

// TestTFPrevCloseInWindow checks that Cluster issues will abort issue creation by returning an empty
// body if there is a recently closed issue for the cluster.
func TestTFPrevCloseInWindow(t *testing.T) {
	f := NewTestTriageFiler()
	clusters, err := f.loadClusters(json1issue2job2test)
	if err != nil || len(clusters) == 0 {
		t.Fatalf("Error parsing triage data: %v\n", err)
	}
	clust := clusters[0]

	lastWeek := time.Unix(latestBuildTime, 0).AddDate(0, 0, -7)
	yesterday := time.Unix(latestBuildTime, 0).AddDate(0, 0, -1)
	five := 5
	// Only need to populate the Issue.ClosedAt and Issue.Number field of the MungeObject.
	prevIssues := []*github.Issue{&github.Issue{ClosedAt: &yesterday, Number: &five}}
	if clust.Body(prevIssues) != "" {
		t.Errorf("Cluster returned an issue body when there was a recently closed issue for the cluster.")
	}

	prevIssues = []*github.Issue{&github.Issue{ClosedAt: &lastWeek, Number: &five}}
	if clust.Body(prevIssues) == "" {
		t.Errorf("Cluster returned an empty issue body when it should have returned a valid body.")
	}
}

func checkTopFailingsSorted(issue *Cluster) bool {
	return checkTopJobsFailedSorted(issue) && checkTopTestsFailedSorted(issue)
}

func checkTopJobsFailedSorted(issue *Cluster) bool {
	topJobs := issue.topJobsFailed(len(issue.jobs))
	for i := 1; i < len(topJobs); i++ {
		if len(topJobs[i-1].Builds) < len(topJobs[i].Builds) {
			return false
		}
	}
	return true
}

func checkTopTestsFailedSorted(issue *Cluster) bool {
	topTests := issue.topTestsFailed(len(issue.Tests))
	for i := 1; i < len(topTests); i++ {
		if len(topTests[i-1].Jobs) < len(topTests[i].Jobs) {
			return false
		}
	}
	return true
}
