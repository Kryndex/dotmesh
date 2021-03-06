package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotmesh-io/citools"
)

/*

Take a look at docs/dev-commands.md to see how to run these tests.

*/

func TestTeardownFinished(t *testing.T) {
	citools.TeardownFinishedTestRuns()
}

func TestDefaultDot(t *testing.T) {
	// Test default dot select on a totally fresh cluster
	citools.TeardownFinishedTestRuns()

	f := citools.Federation{citools.NewCluster(1)}

	citools.StartTiming()
	err := f.Start(t)
	defer citools.TestMarkForCleanup(f)
	if err != nil {
		t.Error(err)
	}
	node1 := f[0].GetNode(0).Container

	// These test MUST BE RUN ON A CLUSTER WITH NO DOTS.
	// Ensure that any other test in this suite deletes all its dots at the end.

	t.Run("DefaultDotSwitch", func(t *testing.T) {
		fsname := citools.UniqName()

		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/HELLO")

		// This would fail if we didn't pick up a default dot when there's only one
		citools.RunOnNode(t, node1, "dm commit -m 'Commit without selecting a dot first'")

		// Clean up
		citools.RunOnNode(t, node1, "dm dot delete -f "+fsname)
	})

	// Embarrassingly, the combination of the above and below tests
	// together test that Configuration.DeleteStateForVolume correctly
	// cleans up the dot name from DefaultDotSwitch sothat
	// NoDefaultDotError runs successfully (otherwise, volume_1) is
	// still selected so we have a default, even if it doesn't exist.

	// We *could* write an explicit test for this case, but it's
	// probably not worth it as it *is* tested here.

	// But only due to the interaction between two tests. So I'm
	// documenting this nastiness.

	t.Run("NoDefaultDotError", func(t *testing.T) {
		fsname1 := citools.UniqName()
		fsname2 := citools.UniqName()

		citools.RunOnNode(t, node1, citools.DockerRun(fsname1)+" touch /foo/HELLO")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname2)+" touch /foo/HELLO")

		// This should fail if we didn't pick up a default dot when there's only one
		st := citools.OutputFromRunOnNode(t, node1, "if dm commit -m 'Commit without selecting a dot first'; then false; else true; fi")

		if !strings.Contains(st, "No current dot is selected") {
			t.Error(fmt.Sprintf("We didn't get an error when a default dot couldn't be found: %+v", st))
		}

		// Clean up
		citools.RunOnNode(t, node1, "dm dot delete -f "+fsname1)
		citools.RunOnNode(t, node1, "dm dot delete -f "+fsname2)
	})
}

func TestSingleNode(t *testing.T) {
	// single node tests
	citools.TeardownFinishedTestRuns()

	f := citools.Federation{citools.NewCluster(1)}

	citools.StartTiming()
	err := f.Start(t)
	defer citools.TestMarkForCleanup(f)
	if err != nil {
		t.Error(err)
	}
	node1 := f[0].GetNode(0).Container

	// Sub-tests, to reuse common setup code.
	t.Run("Init", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, "dm init "+fsname)
		resp := citools.OutputFromRunOnNode(t, node1, "dm list")
		if !strings.Contains(resp, fsname) {
			t.Error("unable to find volume name in ouput")
		}
	})

	t.Run("InitDuplicate", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, "dm init "+fsname)

		resp := citools.OutputFromRunOnNode(t, node1, "if dm init "+fsname+"; then false; else true; fi ")
		if !strings.Contains(resp, fmt.Sprintf("Error: %s exists already", fsname)) {
			t.Error("Didn't get an error when attempting to re-create the volume")
		}

		resp = citools.OutputFromRunOnNode(t, node1, "dm list")

		if !strings.Contains(resp, fsname) {
			t.Error("unable to find volume name in ouput")
		}
	})

	t.Run("InitCrashSafety", func(t *testing.T) {
		fsname := citools.UniqName()

		_, err := citools.DoSetDebugFlag(f[0].GetNode(0).IP, "admin", f[0].GetNode(0).ApiKey, "PartialFailCreateFilesystem", "true")
		if err != nil {
			t.Error(err)
		}

		resp := citools.OutputFromRunOnNode(t, node1, "if dm init "+fsname+"; then false; else true; fi ")

		if !strings.Contains(resp, "Injected fault") {
			t.Error("Couldn't inject fault into CreateFilesystem")
		}

		_, err = citools.DoSetDebugFlag(f[0].GetNode(0).IP, "admin", f[0].GetNode(0).ApiKey, "PartialFailCreateFilesystem", "false")
		if err != nil {
			t.Error(err)
		}

		// Now try again, and check it recovers and creates the volume
		citools.RunOnNode(t, node1, "dm init "+fsname)

		resp = citools.OutputFromRunOnNode(t, node1, "dm list")

		if !strings.Contains(resp, fsname) {
			t.Error("unable to find volume name in ouput")
		}
	})

	t.Run("Version", func(t *testing.T) {
		var parsedResponse []string
		var validVersion = regexp.MustCompile(`[A-Za-z-.0-9]+`)
		var validRemote = regexp.MustCompile(`^Current remote: `)

		serverResponse := citools.OutputFromRunOnNode(t, node1, "dm version")

		lines := strings.Split(serverResponse, "\n")

		remoteInfo := lines[0]
		versionInfo := lines[1:]

		if !validRemote.MatchString(remoteInfo) {
			t.Errorf("unable to find current remote in version string: %v", remoteInfo)
		}

		for _, versionBit := range versionInfo {
			parsedResponse = append(parsedResponse, strings.Fields(strings.TrimSpace(versionBit))...)
		}

		if (parsedResponse[0] != "Client:") || parsedResponse[1] != "Version:" {
			t.Errorf("unable to find all parts of Client version in ouput: %v %v", parsedResponse[0], parsedResponse[1])
		}
		if !validVersion.MatchString(parsedResponse[2]) {
			t.Errorf("unable to find all client version params in ouput: %v", serverResponse)
		}
		if parsedResponse[3] != "Server:" || parsedResponse[4] != "Version:" {
			t.Errorf("unable to find all version params in ouput: %v %v", parsedResponse[3], parsedResponse[4])
		}

		if !validVersion.MatchString(parsedResponse[5]) {
			t.Errorf("unable to find valid server version in ouput: %v", parsedResponse[5])
		}

		if citools.Contains(versionInfo, "uninitialized") {
			t.Errorf("Version was uninitialized: %v", versionInfo)
		}
	})

	t.Run("Commit", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/X")
		citools.RunOnNode(t, node1, "dm switch "+fsname)
		citools.RunOnNode(t, node1, "dm commit -m 'hello'")
		resp := citools.OutputFromRunOnNode(t, node1, "dm log")
		if !strings.Contains(resp, "hello") {
			t.Error("unable to find commit message in log output")
		}
	})

	t.Run("Branch", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/X")
		citools.RunOnNode(t, node1, "dm switch "+fsname)
		citools.RunOnNode(t, node1, "dm commit -m 'hello'")
		citools.RunOnNode(t, node1, "dm checkout -b branch1")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/Y")
		citools.RunOnNode(t, node1, "dm commit -m 'there'")
		resp := citools.OutputFromRunOnNode(t, node1, "dm log")
		if !strings.Contains(resp, "there") {
			t.Error("unable to find commit message in log output")
		}
		citools.RunOnNode(t, node1, "dm checkout master")
		resp = citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname)+" ls /foo/")
		if strings.Contains(resp, "Y") {
			t.Error("failed to switch filesystem")
		}
		citools.RunOnNode(t, node1, "dm checkout branch1")
		resp = citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname)+" ls /foo/")
		if !strings.Contains(resp, "Y") {
			t.Error("failed to switch filesystem")
		}
	})

	t.Run("Reset", func(t *testing.T) {
		fsname := citools.UniqName()
		// Run a container in the background so that we can observe it get
		// restarted.
		citools.RunOnNode(t, node1,
			citools.DockerRun(fsname, "-d --name sleeper")+" sleep 100",
		)
		initialStart := citools.OutputFromRunOnNode(t, node1,
			"docker inspect sleeper |jq .[0].State.StartedAt",
		)

		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/X")
		citools.RunOnNode(t, node1, "dm switch "+fsname)
		citools.RunOnNode(t, node1, "dm commit -m 'hello'")
		resp := citools.OutputFromRunOnNode(t, node1, "dm log")
		if !strings.Contains(resp, "hello") {
			t.Error("unable to find commit message in log output")
		}
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/Y")
		citools.RunOnNode(t, node1, "dm commit -m 'again'")
		resp = citools.OutputFromRunOnNode(t, node1, "dm log")
		if !strings.Contains(resp, "again") {
			t.Error("unable to find commit message in log output")
		}
		citools.RunOnNode(t, node1, "dm reset --hard HEAD^")
		resp = citools.OutputFromRunOnNode(t, node1, "dm log")
		if strings.Contains(resp, "again") {
			t.Error("found 'again' in dm log when i shouldn't have")
		}
		// check filesystem got rolled back
		resp = citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname)+" ls /foo/")
		if strings.Contains(resp, "Y") {
			t.Error("failed to roll back filesystem")
		}
		newStart := citools.OutputFromRunOnNode(t, node1,
			"docker inspect sleeper |jq .[0].State.StartedAt",
		)
		if initialStart == newStart {
			t.Errorf("container was not restarted during rollback (initialStart %v == newStart %v)", strings.TrimSpace(initialStart), strings.TrimSpace(newStart))
		}

	})

	t.Run("RunningContainersListed", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname, "-d --name tester")+" sleep 100")
		err := citools.TryUntilSucceeds(func() error {
			resp := citools.OutputFromRunOnNode(t, node1, "dm list")
			if !strings.Contains(resp, "tester") {
				return fmt.Errorf("container running not listed")
			}
			return nil
		}, "listing containers")
		if err != nil {
			t.Error(err)
		}
	})

	// TODO test AllDotsAndBranches
	t.Run("AllDotsAndBranches", func(t *testing.T) {
		resp := citools.OutputFromRunOnNode(t, node1, "dm debug AllDotsAndBranches")
		fmt.Printf("AllDotsAndBranches response: %v\n", resp)
	})

	// Exercise the import functionality which should already exists in Docker
	t.Run("ImportDockerImage", func(t *testing.T) {
		fsname := citools.UniqName()
		resp := citools.OutputFromRunOnNode(t, node1,
			// Mount the volume at /etc in the container. Docker should copy the
			// contents of /etc in the image over the top of the new blank volume.
			citools.DockerRun(fsname, "--name import-test", "busybox", "/etc")+" cat /etc/passwd",
		)
		// "root" normally shows up in /etc/passwd
		if !strings.Contains(resp, "root") {
			t.Error("unable to find 'root' in expected output")
		}
		// If we reuse the volume, we should find the contents of /etc
		// imprinted therein.
		resp = citools.OutputFromRunOnNode(t, node1,
			citools.DockerRun(fsname, "--name import-test-2", "busybox", "/foo")+" cat /foo/passwd",
		)
		// "root" normally shows up in /etc/passwd
		if !strings.Contains(resp, "root") {
			t.Error("unable to find 'root' in expected output")
		}
	})
	// XXX This test doesn't fail on Docker 1.12.6, which is used
	// by dind, but it does fail without using
	// `fs.StringWithoutAdmin()` in docker.go due to manual testing
	// on docker 17.06.2-ce. Need to improve the test suite to use
	// a variety of versions of docker in dind environments.
	t.Run("RunningContainerTwice", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/HELLO")
		st := citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname)+" ls /foo/HELLO")
		if !strings.Contains(st, "HELLO") {
			t.Errorf("Data did not persist between two instanciations of the same volume on the same host: %v", st)
		}
	})

	t.Run("BranchPinning", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/HELLO-ORIGINAL")
		citools.RunOnNode(t, node1, "dm switch "+fsname)
		citools.RunOnNode(t, node1, "dm commit -m original")

		citools.RunOnNode(t, node1, "dm checkout -b branch1")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/HELLO-BRANCH1")
		citools.RunOnNode(t, node1, "dm commit -m branch1commit1")

		citools.RunOnNode(t, node1, "dm checkout master")
		citools.RunOnNode(t, node1, "dm checkout -b branch2")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/HELLO-BRANCH2")
		citools.RunOnNode(t, node1, "dm commit -m branch2commit1")

		citools.RunOnNode(t, node1, "dm checkout master")
		citools.RunOnNode(t, node1, "dm checkout -b branch3")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/HELLO-BRANCH3")
		citools.RunOnNode(t, node1, "dm commit -m branch3commit1")

		st := citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname+"@branch1")+" ls /foo")
		if st != "HELLO-BRANCH1\nHELLO-ORIGINAL\n" {
			t.Errorf("Wrong content in branch 1: '%s'", st)
		}

		st = citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname+"@branch2")+" ls /foo")
		if st != "HELLO-BRANCH2\nHELLO-ORIGINAL\n" {
			t.Errorf("Wrong content in branch 2: '%s'", st)
		}

		st = citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname+"@branch3")+" ls /foo")
		if st != "HELLO-BRANCH3\nHELLO-ORIGINAL\n" {
			t.Errorf("Wrong content in branch 3: '%s'", st)
		}
	})

	t.Run("Subdots", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname+".frogs")+" touch /foo/HELLO-FROGS")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname+".eat")+" touch /foo/HELLO-EAT")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname+".flies")+" touch /foo/HELLO-FLIES")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname+".__root__")+" touch /foo/HELLO-ROOT")
		st := citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname+".__root__")+" find /foo -type f | sort")
		if st != "/foo/HELLO-ROOT\n/foo/eat/HELLO-EAT\n/foo/flies/HELLO-FLIES\n/foo/frogs/HELLO-FROGS\n" {
			t.Errorf("Subdots didn't work out: %s", st)
		}
	})

	t.Run("DefaultSubdot", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/HELLO-DEFAULT")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname+".__root__")+" touch /foo/HELLO-ROOT")
		st := citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname+".__root__")+" find /foo -type f | sort")
		if st != "/foo/HELLO-ROOT\n/foo/__default__/HELLO-DEFAULT\n" {
			t.Errorf("Subdots didn't work out: %s", st)
		}
	})

	t.Run("ConcurrentSubdots", func(t *testing.T) {
		fsname := citools.UniqName()

		citools.RunOnNode(t, node1, citools.DockerRunDetached(fsname+".frogs")+" sh -c 'touch /foo/HELLO-FROGS; sleep 30'")
		citools.RunOnNode(t, node1, citools.DockerRunDetached(fsname+".eat")+" sh -c 'touch /foo/HELLO-EAT; sleep 30'")
		citools.RunOnNode(t, node1, citools.DockerRunDetached(fsname+".flies")+" sh -c 'touch /foo/HELLO-FLIES; sleep 30'")
		citools.RunOnNode(t, node1, citools.DockerRunDetached(fsname+".__root__")+" sh -c 'touch /foo/HELLO-ROOT; sleep 30'")
		// Let everything get started
		time.Sleep(5)

		st := citools.OutputFromRunOnNode(t, node1, "dm list")
		matched, err := regexp.MatchString("/[a-z]+_[a-z]+,/[a-z]+_[a-z]+,/[a-z]+_[a-z]+,/[a-z]+_[a-z]+", st)
		if err != nil {
			t.Error(err)
		}
		if !matched {
			t.Errorf("Couldn't find four containers attached to the dot: %+v", st)
		}

		// Check combined state
		st = citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname+".__root__")+" find /foo -type f | sort")
		if st != "/foo/HELLO-ROOT\n/foo/eat/HELLO-EAT\n/foo/flies/HELLO-FLIES\n/foo/frogs/HELLO-FROGS\n" {
			t.Errorf("Subdots didn't work out: %s", st)
		}

		// Check commits and branches work
		citools.RunOnNode(t, node1, "dm switch "+fsname)
		citools.RunOnNode(t, node1, "dm commit -m pod-commit")
		citools.RunOnNode(t, node1, "dm checkout -b branch") // Restarts the containers
		citools.RunOnNode(t, node1, citools.DockerRun(fsname+"@branch.again")+" touch /foo/HELLO-AGAIN")
		citools.RunOnNode(t, node1, "dm commit -m branch-commit")

		// Check branch state
		st = citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname+"@branch.__root__")+" find /foo -type f | sort")

		if st != "/foo/HELLO-ROOT\n/foo/again/HELLO-AGAIN\n/foo/eat/HELLO-EAT\n/foo/flies/HELLO-FLIES\n/foo/frogs/HELLO-FROGS\n" {
			t.Errorf("Subdots didn't work out on branch: %s", st)
		}

		// Check master state
		citools.RunOnNode(t, node1, "dm checkout master") // Restarts the containers
		st = citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname+".__root__")+" find /foo -type f | sort")

		if st != "/foo/HELLO-ROOT\n/foo/eat/HELLO-EAT\n/foo/flies/HELLO-FLIES\n/foo/frogs/HELLO-FROGS\n" {
			t.Errorf("Subdots didn't work out back on master: %s", st)
		}

		// Check containers all got restarted
		st = citools.OutputFromRunOnNode(t, node1, "docker ps | grep 'touch /foo' | wc -l")
		if st != "4\n" {
			t.Errorf("Subdot containers didn't get restarted")
			citools.RunOnNode(t, node1, "docker ps")
		}
	})

	t.Run("SubdotSwitch", func(t *testing.T) {
		fsname := citools.UniqName()

		citools.RunOnNode(t, node1, "dm init "+fsname)
		citools.RunOnNode(t, node1, "dm switch "+fsname)
		citools.RunOnNode(t, node1, "dm commit -m 'initial empty commit'")

		// Set up branch A
		citools.RunOnNode(t, node1, "dm checkout -b branch_A")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname+"@branch_A.frogs")+" sh -c 'echo A_FROGS > /foo/HELLO'")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname+"@branch_A")+" sh -c 'echo A_DEFAULT > /foo/HELLO'")
		citools.RunOnNode(t, node1, "dm commit -m 'branch A commit'")
		time.Sleep(5)

		// Set up branch B
		citools.RunOnNode(t, node1, "dm checkout master")
		citools.RunOnNode(t, node1, "dm checkout -b branch_B")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname+"@branch_B.frogs")+" sh -c 'echo B_FROGS > /foo/HELLO'")
		citools.RunOnNode(t, node1, citools.DockerRun(fsname+"@branch_B")+" sh -c 'echo B_DEFAULT > /foo/HELLO'")
		citools.RunOnNode(t, node1, "dm commit -m 'branch B commit'")

		// Switch back to master
		citools.RunOnNode(t, node1, "dm checkout master")

		// Start up a long sleep container on each, that we can docker exec into
		frogsContainer := fsname + "_frogs"
		defaultContainer := fsname + "_default"
		citools.RunOnNode(t, node1, citools.DockerRunDetached(fsname+".frogs", "--name "+frogsContainer)+" sh -c 'sleep 30'")
		citools.RunOnNode(t, node1, citools.DockerRunDetached(fsname, "--name "+defaultContainer)+" sh -c 'sleep 30'")

		// Check returns branch B content
		citools.RunOnNode(t, node1, "dm checkout branch_B") // Restarts container
		st := citools.OutputFromRunOnNode(t, node1, "docker exec "+frogsContainer+" cat /foo/HELLO")
		if st != "B_FROGS\n" {
			t.Errorf("Expected B_FROGS, got %+v", st)
		}
		st = citools.OutputFromRunOnNode(t, node1, "docker exec "+defaultContainer+" cat /foo/HELLO")

		if st != "B_DEFAULT\n" {
			t.Errorf("Expected B_DEFAULT, got %+v", st)
		}

		// Check returns branch A content
		citools.RunOnNode(t, node1, "dm checkout branch_A") // Restarts container
		st = citools.OutputFromRunOnNode(t, node1, "docker exec "+frogsContainer+" cat /foo/HELLO")
		if st != "A_FROGS\n" {
			t.Errorf("Expected A_FROGS, got %+v", st)
		}
		st = citools.OutputFromRunOnNode(t, node1, "docker exec "+defaultContainer+" cat /foo/HELLO")
		if st != "A_DEFAULT\n" {
			t.Errorf("Expected A_DEFAULT, got %+v", st)
		}
	})

	t.Run("ApiKeys", func(t *testing.T) {
		apiKey := f[0].GetNode(0).ApiKey
		password := f[0].GetNode(0).Password

		var resp struct {
			ApiKey string
		}

		err := citools.DoRPC(f[0].GetNode(0).IP, "admin", apiKey,
			"DotmeshRPC.GetApiKey",
			struct {
			}{},
			&resp)
		if err != nil {
			t.Error(err)
		}
		if resp.ApiKey != apiKey {
			t.Errorf("Got API key %v, expected %v", resp.ApiKey, apiKey)
		}

		err = citools.DoRPC(f[0].GetNode(0).IP, "admin", apiKey,
			"DotmeshRPC.ResetApiKey",
			struct {
			}{},
			&resp)
		if err == nil {
			t.Errorf("Was able to reset API key without a password")
		}

		err = citools.DoRPC(f[0].GetNode(0).IP, "admin", password,
			"DotmeshRPC.ResetApiKey",
			struct {
			}{},
			&resp)
		if err != nil {
			t.Error(err)
		}
		if resp.ApiKey == apiKey {
			t.Errorf("Got API key %v, expected a new one!", resp.ApiKey, apiKey)
		}

		var user struct {
			Id          string
			Name        string
			Email       string
			EmailHash   string
			CustomerId  string
			CurrentPlan string
		}

		fmt.Printf("About to expect failure...\n")
		// Use old API key, expect failure
		err = citools.DoRPC(f[0].GetNode(0).IP, "admin", apiKey,
			"DotmeshRPC.CurrentUser",
			struct {
			}{},
			&resp)
		if err == nil {
			t.Errorf("Successfully used old API key")
		}

		fmt.Printf("About to expect success...\n")
		// Use new API key, expect success
		err = citools.DoRPC(f[0].GetNode(0).IP, "admin", resp.ApiKey,
			"DotmeshRPC.CurrentUser",
			struct {
			}{},
			&user)
		if err != nil {
			t.Error(err)
		}

		// UGLY HACK: This test must be LAST in the suite, as it leaves
		// the API key out of synch with what's in .dotmesh/config and
		// f[0].GetNode(0).ApiKey

		// FIXME: Update GetNode(0).ApiKey and on-disk remote API key so
		// later tests don't fail! We can do a "citools.RunOnNode(t, node1, sed -i
		// s/old/new/ /root/.dotmesh/config)" but we can't mutate
		// GetNode(0).ApiKey from here.
	})

}

func checkDeletionWorked(t *testing.T, fsname string, delay time.Duration, node1 string, node2 string) {
	// We return after the first failure, as there's little point
	// continuing (it just makes it hard to scroll back to the point of
	// initial failure).
	fmt.Printf("Sleeping for %d seconds. See comments in acceptance_test.go for why.\n", delay/time.Second)
	time.Sleep(delay)

	st := citools.OutputFromRunOnNode(t, node1, "dm list")
	if strings.Contains(st, fsname) {
		t.Error(fmt.Sprintf("The volume is still in 'dm list' on node1 (after %d seconds)", delay/time.Second))
		return
	}

	st = citools.OutputFromRunOnNode(t, node2, "dm list")
	if strings.Contains(st, fsname) {
		t.Error(fmt.Sprintf("The volume is still in 'dm list' on node2 (after %d seconds)", delay/time.Second))
		return
	}

	st = citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname)+" cat /foo/HELLO || true")
	if strings.Contains(st, "WORLD") {
		t.Error(fmt.Sprintf("The volume name wasn't reusable %d seconds after delete on node 1...", delay/time.Second))
		return
	}

	st = citools.OutputFromRunOnNode(t, node2, citools.DockerRun(fsname)+" cat /foo/HELLO || true")
	if strings.Contains(st, "WORLD") {
		t.Error(fmt.Sprintf("The volume name wasn't reusable %d seconds after delete on node 2...", delay/time.Second))
		return
	}

	st = citools.OutputFromRunOnNode(t, node1, "dm list")
	if !strings.Contains(st, fsname) {
		t.Error(fmt.Sprintf("The re-use of the deleted volume name failed in 'dm list' on node1 (after %d seconds)", delay/time.Second))
		return
	}

	st = citools.OutputFromRunOnNode(t, node2, "dm list")
	if !strings.Contains(st, fsname) {
		t.Error(fmt.Sprintf("The re-use of the deleted volume name failed in 'dm list' on node2 (after %d seconds)", delay/time.Second))
		return
	}
}

func TestDeletionSimple(t *testing.T) {
	citools.TeardownFinishedTestRuns()

	clusterEnv := make(map[string]string)
	clusterEnv["FILESYSTEM_METADATA_TIMEOUT"] = "5"

	// Our cluster gets a metadata timeout of 5s
	f := citools.Federation{citools.NewClusterWithEnv(2, clusterEnv)}

	citools.StartTiming()
	err := f.Start(t)
	defer citools.TestMarkForCleanup(f)
	if err != nil {
		t.Error(err)
	}
	citools.LogTiming("setup")

	node1 := f[0].GetNode(0).Container
	node2 := f[0].GetNode(1).Container

	t.Run("DeleteNonexistantFails", func(t *testing.T) {
		fsname := citools.UniqName()

		st := citools.OutputFromRunOnNode(t, node1, "if dm dot delete -f "+fsname+"; then false; else true; fi")

		if !strings.Contains(st, "No such filesystem") {
			t.Error(fmt.Sprintf("Deleting a nonexistant volume didn't fail"))
		}
	})

	t.Run("DeleteInUseFails", func(t *testing.T) {
		fsname := citools.UniqName()
		go func() {
			citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" sh -c 'echo WORLD > /foo/HELLO; sleep 10'")
		}()

		// Give time for the container to start
		time.Sleep(5 * time.Second)

		// Delete, while the container is running! Which should fail!
		st := citools.OutputFromRunOnNode(t, node1, "if dm dot delete -f "+fsname+"; then false; else true; fi")
		if !strings.Contains(st, "cannot delete the volume") {
			t.Error(fmt.Sprintf("The presence of a running container failed to suppress volume deletion"))
		}
	})

	t.Run("DeleteInstantly", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" sh -c 'echo WORLD > /foo/HELLO'")
		citools.RunOnNode(t, node1, "dm dot delete -f "+fsname)

		st := citools.OutputFromRunOnNode(t, node1, "dm list")
		if strings.Contains(st, fsname) {
			t.Error(fmt.Sprintf("The volume is still in 'dm list' on node1 (immediately after deletion)"))
			return
		}

		st = citools.OutputFromRunOnNode(t, node1, citools.DockerRun(fsname)+" cat /foo/HELLO || true")
		if strings.Contains(st, "WORLD") {
			t.Error(fmt.Sprintf("The volume name wasn't immediately reusable after deletion on node 1..."))
			return
		}

		/*
				         We don't try and guarantee immediate deletion on other nodes.
				         So the following may or may not fail, we can't test for it.

				   		st = citools.OutputFromRunOnNode(t, node2, citools.DockerRun(fsname)+" cat /foo/HELLO || true")

				   		if strings.Contains(st, "WORLD") {
				   			t.Error(fmt.Sprintf("The volume didn't get deleted on node 2..."))
			          		return
				   		}
		*/

	})

	t.Run("DeleteQuickly", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" sh -c 'echo WORLD > /foo/HELLO'")
		citools.RunOnNode(t, node1, "dm dot delete -f "+fsname)

		// Ensure the initial delete has happened, but the metadata is
		// still draining. This is less than half the metadata timeout
		// configured above; the system should be in the process of
		// cleaning up after the volume, but it should be fine to reuse
		// the name by now.

		checkDeletionWorked(t, fsname, 2*time.Second, node1, node2)
	})

	t.Run("DeleteSlowly", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" sh -c 'echo WORLD > /foo/HELLO'")
		citools.RunOnNode(t, node1, "dm dot delete -f "+fsname)

		// Ensure the delete has happened completely This is twice the
		// metadata timeout configured above, so all traces of the
		// volume should be gone and we get to see the result of the
		// "cleanupDeletedFilesystems" logic (has it ruined the
		// cluster?)

		checkDeletionWorked(t, fsname, 10*time.Second, node1, node2)
	})
}

func setupBranchesForDeletion(t *testing.T, fsname string, node1 string, node2 string) {
	// Set up some branches:
	//
	// Master -> branch1 -> branch2
	//   |
	//   \-> branch3

	citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" sh -c 'echo WORLD > /foo/HELLO'")
	citools.RunOnNode(t, node1, "dm switch "+fsname)
	citools.RunOnNode(t, node1, "dm commit -m 'On master'")

	citools.RunOnNode(t, node1, "dm checkout -b branch1")
	citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" sh -c 'echo WORLD > /foo/GOODBYE'")
	citools.RunOnNode(t, node1, "dm commit -m 'On branch1'")

	citools.RunOnNode(t, node1, "dm checkout -b branch2")
	citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" sh -c 'echo WORLD > /foo/GOODBYE_CRUEL'")
	citools.RunOnNode(t, node1, "dm commit -m 'On branch2'")

	citools.RunOnNode(t, node1, "dm checkout master")
	citools.RunOnNode(t, node1, "dm checkout -b branch3")
	citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" sh -c 'echo WORLD > /foo/HELLO_CRUEL'")
	citools.RunOnNode(t, node1, "dm commit -m 'On branch3'")
}

func TestDeletionComplex(t *testing.T) {
	citools.TeardownFinishedTestRuns()

	clusterEnv := make(map[string]string)
	clusterEnv["FILESYSTEM_METADATA_TIMEOUT"] = "5"

	// Our cluster gets a metadata timeout of 5s
	f := citools.Federation{citools.NewClusterWithEnv(2, clusterEnv)}

	citools.StartTiming()
	err := f.Start(t)
	defer citools.TestMarkForCleanup(f)
	if err != nil {
		t.Error(err)
	}
	citools.LogTiming("setup")

	node1 := f[0].GetNode(0).Container
	node2 := f[0].GetNode(1).Container

	t.Run("DeleteBranchesQuickly", func(t *testing.T) {
		fsname := citools.UniqName()
		setupBranchesForDeletion(t, fsname, node1, node2)

		// Now kill the lot, right?
		citools.RunOnNode(t, node1, "dm dot delete -f "+fsname)

		// Test after two seconds, the state where the registry is cleared out but
		// the metadata remains.
		checkDeletionWorked(t, fsname, 2*time.Second, node1, node2)
	})

	t.Run("DeleteBranchesSlowly", func(t *testing.T) {

		fsname := citools.UniqName()
		setupBranchesForDeletion(t, fsname, node1, node2)

		// Now kill the lot, right?
		citools.RunOnNode(t, node1, "dm dot delete -f "+fsname)

		// Test after tens econds, when all the metadata should be cleared out.
		checkDeletionWorked(t, fsname, 10*time.Second, node1, node2)
	})
}

func TestTwoNodesSameCluster(t *testing.T) {
	citools.TeardownFinishedTestRuns()

	f := citools.Federation{citools.NewCluster(2)}

	citools.StartTiming()
	err := f.Start(t)
	defer citools.TestMarkForCleanup(f)
	if err != nil {
		t.Error(err)
	}
	citools.LogTiming("setup")

	node1 := f[0].GetNode(0).Container
	node2 := f[0].GetNode(1).Container

	t.Run("Move", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" sh -c 'echo WORLD > /foo/HELLO'")
		st := citools.OutputFromRunOnNode(t, node2, citools.DockerRun(fsname)+" cat /foo/HELLO")

		if !strings.Contains(st, "WORLD") {
			t.Error(fmt.Sprintf("Unable to find world in transported data capsule, got '%s'", st))
		}
	})
}

func TestTwoSingleNodeClusters(t *testing.T) {
	citools.TeardownFinishedTestRuns()

	f := citools.Federation{
		citools.NewCluster(1), // cluster_0_node_0
		citools.NewCluster(1), // cluster_1_node_0
	}
	citools.StartTiming()
	err := f.Start(t)
	defer citools.TestMarkForCleanup(f)
	if err != nil {
		t.Error(err)
	}
	node1 := f[0].GetNode(0).Container
	node2 := f[1].GetNode(0).Container

	t.Run("PushCommitBranchExtantBase", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node2, citools.DockerRun(fsname)+" touch /foo/X")
		citools.RunOnNode(t, node2, "dm switch "+fsname)
		citools.RunOnNode(t, node2, "dm commit -m 'hello'")
		citools.RunOnNode(t, node2, "dm push cluster_0")

		citools.RunOnNode(t, node1, "dm switch "+fsname)
		resp := citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "hello") {
			t.Error("unable to find commit message remote's log output")
		}
		// test incremental push
		citools.RunOnNode(t, node2, "dm commit -m 'again'")
		citools.RunOnNode(t, node2, "dm push cluster_0")

		resp = citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "again") {
			t.Error("unable to find commit message remote's log output")
		}
		// test pushing branch with extant base
		citools.RunOnNode(t, node2, "dm checkout -b newbranch")
		citools.RunOnNode(t, node2, "dm commit -m 'branchy'")
		citools.RunOnNode(t, node2, "dm push cluster_0")

		citools.RunOnNode(t, node1, "dm checkout newbranch")
		resp = citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "branchy") {
			t.Error("unable to find commit message remote's log output")
		}
	})
	t.Run("PushCommitBranchNoExtantBase", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node2, citools.DockerRun(fsname)+" touch /foo/X")
		// test pushing branch with no base on remote
		citools.RunOnNode(t, node2, "dm switch "+fsname)
		citools.RunOnNode(t, node2, "dm commit -m 'master'")
		citools.RunOnNode(t, node2, "dm checkout -b newbranch")
		citools.RunOnNode(t, node2, "dm commit -m 'branchy'")
		citools.RunOnNode(t, node2, "dm checkout -b newbranch2")
		citools.RunOnNode(t, node2, "dm commit -m 'branchy2'")
		citools.RunOnNode(t, node2, "dm checkout -b newbranch3")
		citools.RunOnNode(t, node2, "dm commit -m 'branchy3'")
		citools.RunOnNode(t, node2, "dm push cluster_0")

		citools.RunOnNode(t, node1, "dm switch "+fsname)
		citools.RunOnNode(t, node1, "dm checkout newbranch3")
		resp := citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "branchy3") {
			t.Error("unable to find commit message remote's log output")
		}
	})
	t.Run("DirtyDetected", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node2, citools.DockerRun(fsname)+" touch /foo/X")
		citools.RunOnNode(t, node2, "dm switch "+fsname)
		citools.RunOnNode(t, node2, "dm commit -m 'hello'")
		citools.RunOnNode(t, node2, "dm push cluster_0")

		citools.RunOnNode(t, node1, "dm switch "+fsname)
		resp := citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "hello") {
			t.Error("unable to find commit message remote's log output")
		}
		// now dirty the filesystem on node1 w/1MB before it can be received into
		citools.RunOnNode(t, node1, citools.DockerRun(""+fsname+"")+" dd if=/dev/urandom of=/foo/Y bs=1024 count=1024")

		for i := 0; i < 10; i++ {
			dirty, err := strconv.Atoi(strings.TrimSpace(
				citools.OutputFromRunOnNode(t, node1, "dm list -H |grep "+fsname+" |cut -f 7"),
			))
			if err != nil {
				t.Error(err)
			}
			if dirty > 0 {
				break
			}
			fmt.Printf("Not dirty yet, waiting...\n")
			time.Sleep(time.Duration(i) * time.Second)
		}

		// test incremental push
		citools.RunOnNode(t, node2, "dm commit -m 'again'")
		result := citools.OutputFromRunOnNode(t, node2, "dm push cluster_0 || true") // an error code is ok

		if !strings.Contains(result, "uncommitted") {
			t.Error("pushing didn't fail when there were known uncommited changes on the peer")
		}
	})
	t.Run("DirtyImmediate", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node2, citools.DockerRun(fsname)+" touch /foo/X")
		citools.RunOnNode(t, node2, "dm switch "+fsname)
		citools.RunOnNode(t, node2, "dm commit -m 'hello'")
		citools.RunOnNode(t, node2, "dm push cluster_0")

		citools.RunOnNode(t, node1, "dm switch "+fsname)
		resp := citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "hello") {
			t.Error("unable to find commit message remote's log output")
		}
		// now dirty the filesystem on node1 w/1MB before it can be received into
		citools.RunOnNode(t, node1, citools.DockerRun(""+fsname+"")+" dd if=/dev/urandom of=/foo/Y bs=1024 count=1024")

		// test incremental push
		citools.RunOnNode(t, node2, "dm commit -m 'again'")
		result := citools.OutputFromRunOnNode(t, node2, "dm push cluster_0 || true") // an error code is ok

		if !strings.Contains(result, "has been modified") {
			t.Error(
				"pushing didn't fail when there were known uncommited changes on the peer",
			)
		}
	})
	t.Run("Diverged", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node2, citools.DockerRun(fsname)+" touch /foo/X")
		citools.RunOnNode(t, node2, "dm switch "+fsname)
		citools.RunOnNode(t, node2, "dm commit -m 'hello'")
		citools.RunOnNode(t, node2, "dm push cluster_0")

		citools.RunOnNode(t, node1, "dm switch "+fsname)
		resp := citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "hello") {
			t.Error("unable to find commit message remote's log output")
		}
		// now make a commit that will diverge the filesystems
		citools.RunOnNode(t, node1, "dm commit -m 'node1 commit'")

		// test incremental push
		citools.RunOnNode(t, node2, "dm commit -m 'node2 commit'")
		result := citools.OutputFromRunOnNode(t, node2, "dm push cluster_0 || true") // an error code is ok

		if !strings.Contains(result, "diverged") && !strings.Contains(result, "hello") {
			t.Error(
				"pushing didn't fail when there was a divergence",
			)
		}
	})
	t.Run("ResetAfterPushThenPushMySQL", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node2, citools.DockerRun(
			fsname, "-d -e MYSQL_ROOT_PASSWORD=secret", "mysql:5.7.17", "/var/lib/mysql",
		))
		time.Sleep(10 * time.Second)
		citools.RunOnNode(t, node2, "dm switch "+fsname)
		citools.RunOnNode(t, node2, "dm commit -m 'hello'")
		citools.RunOnNode(t, node2, "dm push cluster_0")

		citools.RunOnNode(t, node1, "dm switch "+fsname)
		resp := citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "hello") {
			t.Error("unable to find commit message remote's log output")
		}
		// now make a commit that will diverge the filesystems
		citools.RunOnNode(t, node1, "dm commit -m 'node1 commit'")

		// test resetting a commit made on a pushed volume
		citools.RunOnNode(t, node2, "dm commit -m 'node2 commit'")
		citools.RunOnNode(t, node1, "dm reset --hard HEAD^")
		resp = citools.OutputFromRunOnNode(t, node1, "dm log")
		if strings.Contains(resp, "node1 commit") {
			t.Error("found 'node1 commit' in dm log when i shouldn't have")
		}
		citools.RunOnNode(t, node2, "dm push cluster_0")
		resp = citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "node2 commit") {
			t.Error("'node2 commit' didn't make it over to node1 after reset-and-push")
		}
	})
	t.Run("PushToAuthorizedUser", func(t *testing.T) {
		// TODO
		// create a user on the second cluster. on the first cluster, push a
		// volume that user's account.
	})
	t.Run("NoPushToUnauthorizedUser", func(t *testing.T) {
		// TODO
		// a user can't push to a volume they're not authorized to push to.
	})
	t.Run("PushToCollaboratorVolume", func(t *testing.T) {
		// TODO
		// after adding another user as a collaborator, it's possible to push
		// to their volume.
	})
	t.Run("Clone", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, node2, citools.DockerRun(fsname)+" touch /foo/X")
		citools.RunOnNode(t, node2, "dm switch "+fsname)
		citools.RunOnNode(t, node2, "dm commit -m 'hello'")

		// XXX 'dm clone' currently tries to pull the named filesystem into the
		// _current active filesystem name_. instead, it should pull it into a
		// new filesystem with the same name. if the same named filesystem
		// already exists, it should error (and instruct the user to 'dm switch
		// foo; dm pull foo' instead).
		citools.RunOnNode(t, node1, "dm clone cluster_1 "+fsname)
		citools.RunOnNode(t, node1, "dm switch "+fsname)
		resp := citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "hello") {
			// TODO fix this failure by sending prelude in intercluster case also
			t.Error("unable to find commit message remote's log output")
		}
		// test incremental pull
		citools.RunOnNode(t, node2, "dm commit -m 'again'")
		citools.RunOnNode(t, node1, "dm pull cluster_1 "+fsname)

		resp = citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "again") {
			t.Error("unable to find commit message remote's log output")
		}
		// test pulling branch with extant base
		citools.RunOnNode(t, node2, "dm checkout -b newbranch")
		citools.RunOnNode(t, node2, "dm commit -m 'branchy'")
		citools.RunOnNode(t, node1, "dm pull cluster_1 "+fsname+" newbranch")

		citools.RunOnNode(t, node1, "dm checkout newbranch")
		resp = citools.OutputFromRunOnNode(t, node1, "dm log")

		if !strings.Contains(resp, "branchy") {
			t.Error("unable to find commit message remote's log output")
		}
	})

	t.Run("Bug74MissingMetadata", func(t *testing.T) {
		fsname := citools.UniqName()

		// Commit 1
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/X")
		citools.RunOnNode(t, node1, "dm switch "+fsname)
		citools.RunOnNode(t, node1, "dm commit -m 'hello'")

		// Commit 2
		citools.RunOnNode(t, node1, citools.DockerRun(fsname)+" touch /foo/Y")
		citools.RunOnNode(t, node1, "dm commit -m 'again'")

		// Ahhh, push it! https://www.youtube.com/watch?v=vCadcBR95oU
		citools.RunOnNode(t, node1, "dm push cluster_1 "+fsname)

		// What do we get on node2?
		citools.RunOnNode(t, node2, "dm switch "+fsname)
		resp := citools.OutputFromRunOnNode(t, node2, "dm log")

		if !strings.Contains(resp, "hello") ||
			!strings.Contains(resp, "again") {
			t.Error("Some history went missing (if it's bug#74 again, probably the 'hello')")
		}
	})

	t.Run("VolumeNameValidityChecking", func(t *testing.T) {
		// 1) dm init
		resp := citools.OutputFromRunOnNode(t, node1, "if dm init @; then false; else true; fi ")
		if !strings.Contains(resp, "Invalid dot name") {
			t.Error("Didn't get an error when attempting to dm init an invalid volume name: %s", resp)
		}

		// 2) pull/clone it
		fsname := citools.UniqName()

		citools.RunOnNode(t, node2, citools.DockerRun(fsname)+" touch /foo/X")
		citools.RunOnNode(t, node2, "dm switch "+fsname)
		citools.RunOnNode(t, node2, "dm commit -m 'hello'")

		resp = citools.OutputFromRunOnNode(t, node1, "if dm clone cluster_0 "+fsname+" --local-name @; then false; else true; fi ")

		if !strings.Contains(resp, "Invalid dot name") {
			t.Error("Didn't get an error when attempting to dm clone to an invalid volume name: %s", resp)
		}

		resp = citools.OutputFromRunOnNode(t, node1, "if dm pull cluster_0 @ --remote-name "+fsname+"; then false; else true; fi ")
		if !strings.Contains(resp, "Invalid dot name") {
			t.Error("Didn't get an error when attempting to dm pull to an invalid volume name: %s", resp)
		}

		// 3) push it
		resp = citools.OutputFromRunOnNode(t, node2, "if dm push cluster_0 "+fsname+" --remote-name @; then false; else true; fi ")
		if !strings.Contains(resp, "Invalid dot name") {
			t.Error("Didn't get an error when attempting to dm push to an invalid volume name: %s", resp)
		}
	})
}

func TestThreeSingleNodeClusters(t *testing.T) {
	citools.TeardownFinishedTestRuns()

	f := citools.Federation{
		citools.NewCluster(1), // cluster_0_node_0 - common
		citools.NewCluster(1), // cluster_1_node_0 - alice
		citools.NewCluster(1), // cluster_2_node_0 - bob
	}
	citools.StartTiming()
	err := f.Start(t)
	defer citools.TestMarkForCleanup(f)
	if err != nil {
		t.Error(err)
	}
	commonNode := f[0].GetNode(0)
	aliceNode := f[1].GetNode(0)
	bobNode := f[2].GetNode(0)

	bobKey := "bob is great"
	aliceKey := "alice is great"

	// Create users bob and alice on the common node
	err = citools.RegisterUser(commonNode, "bob", "bob@bob.com", bobKey)
	if err != nil {
		t.Error(err)
	}

	err = citools.RegisterUser(commonNode, "alice", "alice@bob.com", aliceKey)
	if err != nil {
		t.Error(err)
	}

	t.Run("TwoUsersSameNamedVolume", func(t *testing.T) {

		// bob and alice both push to the common node
		citools.RunOnNode(t, aliceNode.Container, citools.DockerRun("apples")+" touch /foo/alice")
		citools.RunOnNode(t, aliceNode.Container, "dm switch apples")
		citools.RunOnNode(t, aliceNode.Container, "dm commit -m'Alice commits'")
		citools.RunOnNode(t, aliceNode.Container, "dm push cluster_0 apples --remote-name alice/apples")

		citools.RunOnNode(t, bobNode.Container, citools.DockerRun("apples")+" touch /foo/bob")
		citools.RunOnNode(t, bobNode.Container, "dm switch apples")
		citools.RunOnNode(t, bobNode.Container, "dm commit -m'Bob commits'")
		citools.RunOnNode(t, bobNode.Container, "dm push cluster_0 apples --remote-name bob/apples")

		// bob and alice both clone from the common node
		citools.RunOnNode(t, aliceNode.Container, "dm clone cluster_0 bob/apples --local-name bob-apples")
		citools.RunOnNode(t, bobNode.Container, "dm clone cluster_0 alice/apples --local-name alice-apples")

		// Check they get the right volumes
		resp := citools.OutputFromRunOnNode(t, commonNode.Container, "dm list -H | cut -f 1 | grep apples")
		if resp != "alice/apples\nbob/apples\n" {
			t.Error("Didn't find alice/apples and bob/apples on common node")
		}

		resp = citools.OutputFromRunOnNode(t, aliceNode.Container, "dm list -H | cut -f 1 | grep apples")
		if resp != "apples\nbob-apples\n" {
			t.Error("Didn't find apples and bob-apples on alice's node")
		}

		resp = citools.OutputFromRunOnNode(t, bobNode.Container, "dm list -H | cut -f 1 | grep apples")
		if resp != "alice-apples\napples\n" {
			t.Error("Didn't find apples and alice-apples on bob's node")
		}

		// Check the volumes actually have the contents they should
		resp = citools.OutputFromRunOnNode(t, aliceNode.Container, citools.DockerRun("bob-apples")+" ls /foo/")
		if !strings.Contains(resp, "bob") {
			t.Error("Filesystem bob-apples had the wrong content")
		}

		resp = citools.OutputFromRunOnNode(t, bobNode.Container, citools.DockerRun("alice-apples")+" ls /foo/")
		if !strings.Contains(resp, "alice") {
			t.Error("Filesystem alice-apples had the wrong content")
		}

		// bob commits again
		citools.RunOnNode(t, bobNode.Container, citools.DockerRun("apples")+" touch /foo/bob2")
		citools.RunOnNode(t, bobNode.Container, "dm switch apples")
		citools.RunOnNode(t, bobNode.Container, "dm commit -m'Bob commits again'")
		citools.RunOnNode(t, bobNode.Container, "dm push cluster_0 apples --remote-name bob/apples")

		// alice pulls it
		citools.RunOnNode(t, aliceNode.Container, "dm pull cluster_0 bob-apples --remote-name bob/apples")

		// Check we got the change
		resp = citools.OutputFromRunOnNode(t, aliceNode.Container, citools.DockerRun("bob-apples")+" ls /foo/")
		if !strings.Contains(resp, "bob2") {
			t.Error("Filesystem bob-apples had the wrong content")
		}
	})

	t.Run("ShareBranches", func(t *testing.T) {
		// Alice pushes
		citools.RunOnNode(t, aliceNode.Container, citools.DockerRun("cress")+" touch /foo/alice")
		citools.RunOnNode(t, aliceNode.Container, "dm switch cress")
		citools.RunOnNode(t, aliceNode.Container, "dm commit -m'Alice commits'")
		citools.RunOnNode(t, aliceNode.Container, "dm push cluster_0 cress --remote-name alice/cress")

		// Generate branch and push
		citools.RunOnNode(t, aliceNode.Container, "dm checkout -b mustard")
		citools.RunOnNode(t, aliceNode.Container, citools.DockerRun("cress")+" touch /foo/mustard")
		citools.RunOnNode(t, aliceNode.Container, "dm commit -m'Alice commits mustard'")
		citools.RunOnNode(t, aliceNode.Container, "dm push cluster_0 cress mustard --remote-name alice/cress")

		/*
		   COMMON
		   testpool-1508755395558066569-0-node-0/dmfs/61f356f0-39a4-4e0e-6286-e04d25744344                                         19K  9.63G    19K  legacy
		   testpool-1508755395558066569-0-node-0/dmfs/61f356f0-39a4-4e0e-6286-e04d25744344@b8b3c196-2caa-4562-6995-51df3b4bc494      0      -    19K  -

		   ALICE
		   testpool-1508755395558066569-1-node-0/dmfs/05652ba4-acb8-4349-4711-956bd0c88c8c                                          9K  9.63G    19K  legacy
		   testpool-1508755395558066569-1-node-0/dmfs/61f356f0-39a4-4e0e-6286-e04d25744344                                         19K  9.63G    19K  legacy
		   testpool-1508755395558066569-1-node-0/dmfs/61f356f0-39a4-4e0e-6286-e04d25744344@b8b3c196-2caa-4562-6995-51df3b4bc494      0      -    19K  -

		   BOB
		   testpool-1508755395558066569-2-node-0                                                                                  142K  9.63G    19K  /dotmesh-test-pools/testpool-1508755395558066569-2-node-0/mnt
		   testpool-1508755395558066569-2-node-0/dmfs                                                                              38K  9.63G    19K  legacy
		   testpool-1508755395558066569-2-node-0/dmfs/05652ba4-acb8-4349-4711-956bd0c88c8c                                         19K  9.63G    19K  legacy
		   testpool-1508755395558066569-2-node-0/dmfs/05652ba4-acb8-4349-4711-956bd0c88c8c@b8b3c196-2caa-4562-6995-51df3b4bc494      0      -    19K  -
		*/
		// Bob clones the branch
		citools.RunOnNode(t, bobNode.Container, "dm clone cluster_0 alice/cress mustard --local-name cress")
		citools.RunOnNode(t, bobNode.Container, "dm switch cress")
		citools.RunOnNode(t, bobNode.Container, "dm checkout mustard")

		// Check we got both changes
		// TODO: had to pin the branch here, seems like `dm switch V; dm
		// checkout B; docker run C ... -v V:/...` doesn't result in V@B being
		// mounted into C. this is probably surprising behaviour.
		resp := citools.OutputFromRunOnNode(t, bobNode.Container, citools.DockerRun("cress@mustard")+" ls /foo/")
		if !strings.Contains(resp, "alice") {
			t.Error("We didn't get the master branch")
		}
		if !strings.Contains(resp, "mustard") {
			t.Error("We didn't get the mustard branch")
		}
	})

	t.Run("DefaultRemoteNamespace", func(t *testing.T) {
		// Alice pushes to the common node with no explicit remote volume, should default to alice/pears
		citools.RunOnNode(t, aliceNode.Container, citools.DockerRun("pears")+" touch /foo/alice")
		citools.RunOnNode(t, aliceNode.Container, "echo '"+aliceKey+"' | dm remote add common_pears alice@"+commonNode.IP)
		citools.RunOnNode(t, aliceNode.Container, "dm switch pears")
		citools.RunOnNode(t, aliceNode.Container, "dm commit -m'Alice commits'")
		citools.RunOnNode(t, aliceNode.Container, "dm push common_pears") // local pears becomes alice/pears

		// Check it gets there
		resp := citools.OutputFromRunOnNode(t, commonNode.Container, "dm list -H | cut -f 1 | sort")
		if !strings.Contains(resp, "alice/pears") {
			t.Error("Didn't find alice/pears on the common node")
		}
	})

	t.Run("DefaultRemoteVolume", func(t *testing.T) {
		// Alice pushes to the common node with no explicit remote volume, should default to alice/pears
		citools.RunOnNode(t, aliceNode.Container, citools.DockerRun("bananas")+" touch /foo/alice")
		citools.RunOnNode(t, aliceNode.Container, "echo '"+aliceKey+"' | dm remote add common_bananas alice@"+commonNode.IP)
		citools.RunOnNode(t, aliceNode.Container, "dm switch bananas")
		citools.RunOnNode(t, aliceNode.Container, "dm commit -m'Alice commits'")
		citools.RunOnNode(t, aliceNode.Container, "dm push common_bananas bananas")

		// Check the remote branch got recorded
		resp := citools.OutputFromRunOnNode(t, aliceNode.Container, "dm dot show -H bananas | grep defaultUpstreamDot")
		if resp != "defaultUpstreamDot\tcommon_bananas\talice/bananas\n" {
			t.Error("alice/bananas is not the default remote for bananas on common_bananas")
		}

		// Add Bob as a collaborator
		err := citools.DoAddCollaborator(commonNode.IP, "alice", aliceKey, "alice", "bananas", "bob")
		if err != nil {
			t.Error(err)
		}

		// Clone it back as bob
		citools.RunOnNode(t, bobNode.Container, "echo '"+bobKey+"' | dm remote add common_bananas bob@"+commonNode.IP)
		// Clone should save admin/bananas@common => alice/bananas
		citools.RunOnNode(t, bobNode.Container, "dm clone common_bananas alice/bananas --local-name bananas")
		citools.RunOnNode(t, bobNode.Container, "dm switch bananas")

		// Check it did so
		resp = citools.OutputFromRunOnNode(t, bobNode.Container, "dm dot show -H bananas | grep defaultUpstreamDot")
		if resp != "defaultUpstreamDot\tcommon_bananas\talice/bananas\n" {
			t.Error("alice/bananas is not the default remote for bananas on common_bananas")
		}

		// And then do a pull, not specifying the remote or local volume
		// There is no bob/bananas, so this will fail if the default remote volume is not saved.
		citools.RunOnNode(t, bobNode.Container, "dm pull common_bananas") // local = bananas as we switched, remote = alice/banas from saved default

		// Now push back
		citools.RunOnNode(t, bobNode.Container, citools.DockerRun("bananas")+" touch /foo/bob")
		citools.RunOnNode(t, bobNode.Container, "dm commit -m'Bob commits'")
		citools.RunOnNode(t, bobNode.Container, "dm push common_bananas") // local = bananas as we switched, remote = alice/banas from saved default
	})

	t.Run("DefaultRemoteNamespaceOverride", func(t *testing.T) {
		// Alice pushes to the common node with no explicit remote volume, should default to alice/kiwis
		citools.RunOnNode(t, aliceNode.Container, citools.DockerRun("kiwis")+" touch /foo/alice")
		citools.RunOnNode(t, aliceNode.Container, "echo '"+aliceKey+"' | dm remote add common_kiwis alice@"+commonNode.IP)
		citools.RunOnNode(t, aliceNode.Container, "dm switch kiwis")
		citools.RunOnNode(t, aliceNode.Container, "dm commit -m'Alice commits'")
		citools.RunOnNode(t, aliceNode.Container, "dm push common_kiwis") // local kiwis becomes alice/kiwis

		// Check the remote branch got recorded
		resp := citools.OutputFromRunOnNode(t, aliceNode.Container, "dm dot show -H kiwis | grep defaultUpstreamDot")
		if resp != "defaultUpstreamDot\tcommon_kiwis\talice/kiwis\n" {
			t.Error("alice/kiwis is not the default remote for kiwis on common_kiwis")
		}

		// Manually override it (the remote repo doesn't need to exist)
		citools.RunOnNode(t, aliceNode.Container, "dm dot set-upstream common_kiwis bob/kiwis")

		// Check the remote branch got changed
		resp = citools.OutputFromRunOnNode(t, aliceNode.Container, "dm dot show -H kiwis | grep defaultUpstreamDot")
		if resp != "defaultUpstreamDot\tcommon_kiwis\tbob/kiwis\n" {
			t.Error("bob/kiwis is not the default remote for kiwis on common_kiwis, looks like the set-upstream failed")
		}
	})

	t.Run("DeleteNotMineFails", func(t *testing.T) {
		fsname := citools.UniqName()

		// Alice pushes to the common node with no explicit remote volume, should default to alice/fsname
		citools.RunOnNode(t, aliceNode.Container, citools.DockerRun(fsname)+" touch /foo/alice")
		citools.RunOnNode(t, aliceNode.Container, "echo '"+aliceKey+"' | dm remote add common_"+fsname+" alice@"+commonNode.IP)
		citools.RunOnNode(t, aliceNode.Container, "dm switch "+fsname)
		citools.RunOnNode(t, aliceNode.Container, "dm commit -m'Alice commits'")
		citools.RunOnNode(t, aliceNode.Container, "dm push common_"+fsname) // local fsname becomes alice/fsname

		// Bob tries to delete it
		citools.RunOnNode(t, bobNode.Container, "echo '"+bobKey+"' | dm remote add common_"+fsname+" bob@"+commonNode.IP)
		citools.RunOnNode(t, bobNode.Container, "dm remote switch common_"+fsname)
		// We expect failure, so reverse the sense
		resp := citools.OutputFromRunOnNode(t, bobNode.Container, "if dm dot delete -f alice/"+fsname+"; then false; else true; fi")
		if !strings.Contains(resp, "You are not the owner") {
			t.Error("bob was able to delete alices' volumes")
		}
	})

	t.Run("NamespaceAuthorisationNonexistant", func(t *testing.T) {
		citools.RunOnNode(t, aliceNode.Container, citools.DockerRun("grapes")+" touch /foo/alice")
		citools.RunOnNode(t, aliceNode.Container, "echo '"+aliceKey+"' | dm remote add common_grapes alice@"+commonNode.IP)
		citools.RunOnNode(t, aliceNode.Container, "dm switch grapes")
		citools.RunOnNode(t, aliceNode.Container, "dm commit -m'Alice commits'")

		// Let's try and put things in a nonexistant namespace
		// Likewise, This SHOULD fail, so we reverse the sense of the return code.
		citools.RunOnNode(t, aliceNode.Container, "if dm push common_grapes --remote-name nonexistant/grapes; then exit 1; else exit 0; fi")

		// Check it doesn't get there
		resp := citools.OutputFromRunOnNode(t, commonNode.Container, "dm list -H | cut -f 1 | sort")
		if strings.Contains(resp, "nonexistant/grapes") {
			t.Error("Found nonexistant/grapes on the common node - but alice shouldn't have been able to create that!")
		}
	})

	t.Run("NamespaceAuthorisation", func(t *testing.T) {
		citools.RunOnNode(t, aliceNode.Container, citools.DockerRun("passionfruit")+" touch /foo/alice")
		citools.RunOnNode(t, aliceNode.Container, "echo '"+aliceKey+"' | dm remote add common_passionfruit alice@"+commonNode.IP)
		citools.RunOnNode(t, aliceNode.Container, "dm switch passionfruit")
		citools.RunOnNode(t, aliceNode.Container, "dm commit -m'Alice commits'")

		// Let's try and put things in bob's namespace.
		// This SHOULD fail, so we reverse the sense of the return code.
		citools.RunOnNode(t, aliceNode.Container, "if dm push common_passionfruit --remote-name bob/passionfruit; then exit 1; else exit 0; fi")

		// Check it doesn't get there
		resp := citools.OutputFromRunOnNode(t, commonNode.Container, "dm list -H | cut -f 1 | sort")
		if strings.Contains(resp, "bob/passionfruit") {
			t.Error("Found bob/passionfruit on the common node - but alice shouldn't have been able to create that!")
		}
	})

	t.Run("NamespaceAuthorisationAdmin", func(t *testing.T) {
		citools.RunOnNode(t, aliceNode.Container, citools.DockerRun("prune")+" touch /foo/alice")
		citools.RunOnNode(t, aliceNode.Container, "dm switch prune")
		citools.RunOnNode(t, aliceNode.Container, "dm commit -m'Alice commits'")

		// Let's try and put things in bob's namespace, but using the cluster_0 remote which is logged in as admin
		// This should work, because we're admin, even though it's bob's namespace
		citools.RunOnNode(t, aliceNode.Container, "dm push cluster_0 --remote-name bob/prune")

		// Check it got there
		resp := citools.OutputFromRunOnNode(t, commonNode.Container, "dm list -H | cut -f 1 | sort")
		if !strings.Contains(resp, "bob/prune") {
			t.Error("Didn't find bob/prune on the common node - but alice should have been able to create that using her admin account!")
		}
	})

	// on alice's machine
	// ------------------
	// dm init foo
	// dm commit -m "initial"
	// dm checkout -b branch_a
	// dm commit -m "A commit"
	// dm push hub foo branch_a
	// <at this point, hub correctly shows master, its commit, branch_a, its commit>

	// over on bob's machine:
	// ----------------------
	// dm clone hub alice/foo branch_a
	// dm switch foo
	// dm checkout master
	// dm log <-- MYSTERIOUSLY EMPTY
	// dm checkout branch_a <-- works
	// dm commit -m "New commit from bob"
	// dm push hub [foo branch_a] <-- fails saying it can't find the commit with id of "initial" commit (I think)
	// ---- BUT! -----
	// if bob started by:
	// dm clone hub alice/foo [master]
	// dm switch foo
	// dm pull hub foo branch_a
	// ... then everything works!

	/*
		t.Run("Issue226", func(t *testing.T) {
			fsname := citools.UniqName()
			citools.RunOnNode(t, aliceNode.Container, "echo '"+aliceKey+"' | dm remote add common_"+fsname+" alice@"+commonNode.IP)

			citools.RunOnNode(t, aliceNode.Container, "dm init "+fsname)
			citools.RunOnNode(t, aliceNode.Container, "dm switch "+fsname)
			citools.RunOnNode(t, aliceNode.Container, "dm commit -m initial")
			citools.RunOnNode(t, aliceNode.Container, "dm checkout -b branch_a")
			citools.RunOnNode(t, aliceNode.Container, "dm commit -m 'commit on a'")
			citools.RunOnNode(t, aliceNode.Container, "dm push common_"+fsname+" "+fsname+" branch_a")

			citools.RunOnNode(t, bobNode.Container, "echo '"+bobKey+"' | dm remote add common_"+fsname+" bob@"+commonNode.IP)
			citools.RunOnNode(t, bobNode.Container, "dm clone common_"+fsname+" alice/"+fsname+" branch_a")
			citools.RunOnNode(t, bobNode.Container, "dm switch "+fsname)
			citools.RunOnNode(t, bobNode.Container, "dm checkout master")
			st := citools.OutputFromRunOnNode(t, bobNode.Container, "dm log")
			fmt.Printf("Master log, should include 'initial': %+v\n", st)
		})
	*/
}

func TestKubernetes(t *testing.T) {
	citools.TeardownFinishedTestRuns()

	f := citools.Federation{citools.NewKubernetes(3)}

	citools.StartTiming()
	err := f.Start(t)
	defer citools.TestMarkForCleanup(f)
	if err != nil {
		t.Error(err)
	}
	node1 := f[0].GetNode(0)

	t.Run("FlexVolume", func(t *testing.T) {

		// dm list should succeed in connecting to the dotmesh cluster
		citools.RunOnNode(t, node1.Container, "dm list")

		// init a dotmesh volume and put some data in it
		citools.RunOnNode(t, node1.Container,
			"docker run --rm -i -v apples:/foo --volume-driver dm "+
				"busybox sh -c \"echo 'apples' > /foo/on-the-tree\"",
		)

		// create a PV referencing the data
		citools.KubectlApply(t, node1.Container, `
kind: PersistentVolume
apiVersion: v1
metadata:
  name: admin-apples
  labels:
    apples: tree
spec:
  storageClassName: manual
  capacity:
    storage: 1Gi
  accessModes:
    - ReadWriteOnce
  flexVolume:
    driver: dotmesh.io/dm
    options:
      namespace: admin
      name: apples
`)
		// run a pod with a PVC which lists the data (web server)
		// check that the output of querying the pod is that we can see
		// that the apples are on the tree
		citools.KubectlApply(t, node1.Container, `
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: admin-apples-pvc
spec:
  storageClassName: manual
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  selector:
    matchLabels:
      apples: tree
`)

		citools.KubectlApply(t, node1.Container, `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: apple-deployment
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: apple-server
    spec:
      volumes:
      - name: apple-storage
        persistentVolumeClaim:
         claimName: admin-apples-pvc
      containers:
      - name: apple-server
        image: nginx:1.12.1
        volumeMounts:
        - mountPath: "/usr/share/nginx/html"
          name: apple-storage
`)

		citools.KubectlApply(t, node1.Container, `
apiVersion: v1
kind: Service
metadata:
   name: apple-service
spec:
   type: NodePort
   selector:
       app: apple-server
   ports:
     - port: 80
       nodePort: 30003
`)

		err = citools.TryUntilSucceeds(func() error {
			resp, err := http.Get(fmt.Sprintf("http://%s:30003/on-the-tree", node1.IP))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			if !strings.Contains(string(body), "apples") {
				return fmt.Errorf("No apples on the tree, got this instead: %v", string(body))
			}
			return nil
		}, "finding apples on the tree")
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("DynamicProvisioning", func(t *testing.T) {
		// dm list should succeed in connecting to the dotmesh cluster
		citools.RunOnNode(t, node1.Container, "dm list")

		// Ok, now we have the plumbing set up, try creating a PVC and see if it gets a PV dynamically provisioned
		citools.KubectlApply(t, node1.Container, `
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: admin-grapes-pvc
  annotations:
    # Also available: dotmeshNamespace (defaults to the one from the storage class)
    dotmeshNamespace: k8s
    dotmeshName: dynamic-grapes
    dotmeshSubdot: static-html
spec:
  storageClassName: dotmesh
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
`)

		err = citools.TryUntilSucceeds(func() error {
			result := citools.OutputFromRunOnNode(t, node1.Container, "kubectl get pv")
			// We really want a line like:
			// "pvc-85b6beb0-bb1f-11e7-8633-0242ff9ba756   1Gi        RWO           Delete          Bound     default/admin-grapes-pvc   dotmesh                 15s"
			if !strings.Contains(result, "default/admin-grapes-pvc") {
				return fmt.Errorf("grapes PV didn't get created")
			}
			return nil
		}, "finding the grapes PV")
		if err != nil {
			t.Error(err)
		}

		// Now let's see if a container can see it, and put content there that a k8s container can pick up
		citools.RunOnNode(t, node1.Container,
			"docker run --rm -i -v k8s/dynamic-grapes.static-html:/foo --volume-driver dm "+
				"busybox sh -c \"echo 'grapes' > /foo/on-the-vine\"",
		)

		citools.KubectlApply(t, node1.Container, `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: grape-deployment
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: grape-server
    spec:
      volumes:
      - name: grape-storage
        persistentVolumeClaim:
         claimName: admin-grapes-pvc
      containers:
      - name: grape-server
        image: nginx:1.12.1
        volumeMounts:
        - mountPath: "/usr/share/nginx/html"
          name: grape-storage
`)

		citools.KubectlApply(t, node1.Container, `
apiVersion: v1
kind: Service
metadata:
   name: grape-service
spec:
   type: NodePort
   selector:
       app: grape-server
   ports:
     - port: 80
       nodePort: 30050
`)

		err = citools.TryUntilSucceeds(func() error {
			resp, err := http.Get(fmt.Sprintf("http://%s:30050/on-the-vine", node1.IP))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			if !strings.Contains(string(body), "grapes") {
				return fmt.Errorf("No grapes on the vine, got this instead: %v", string(body))
			}
			return nil
		}, "finding grapes on the vine")
		if err != nil {
			t.Error(err)
		}
	})

}

func TestStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress tests in short mode.")
	}

	citools.TeardownFinishedTestRuns()

	// Tests in this suite should not assume how many nodes we have in
	// the cluster, and iterate over f[0].GetNodes, so that we can
	// scale it to the test hardware we have. Might even pick up the
	// cluster size from an env variable.
	f := citools.Federation{
		citools.NewCluster(5),
	}
	citools.StartTiming()
	err := f.Start(t)
	defer citools.TestMarkForCleanup(f)
	if err != nil {
		t.Error(err)
	}
	commonNode := f[0].GetNode(0)

	t.Run("HandoverStressTest", func(t *testing.T) {
		fsname := citools.UniqName()
		citools.RunOnNode(t, commonNode.Container, citools.DockerRun(fsname)+" sh -c 'echo STUFF > /foo/whatever'")

		for iteration := 0; iteration <= 10; iteration++ {
			for nid, node := range f[0].GetNodes() {
				runId := fmt.Sprintf("%d/%d", iteration, nid)
				st := citools.OutputFromRunOnNode(t, node.Container, citools.DockerRun(fsname)+" sh -c 'echo "+runId+"; cat /foo/whatever'")
				if !strings.Contains(st, "STUFF") {
					t.Error(fmt.Sprintf("We didn't see the STUFF we expected"))
				}
			}
		}
	})
}
