package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/cli/browser"
	"github.com/cli/go-gh"
	"github.com/dustin/go-humanize"
)

type Run struct {
	Conclusion string `json:"conclusion"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Url        string `json:"url"`
	RunId      int    `json:"databaseId"`
	HeadSha    string `json:"headSha"`
	Event      string `json:"event"`
}

type Artifact struct {
	Name    string `json:"name"`
	Size    uint64 `json:"size_in_bytes"`
	Expired bool   `json:"expired"`
	RunId   int    `json:"run_id"`
	Run     Run
}

func ghServePath() (string, error) {
	ghServeDir := filepath.Join(".cache", "gh-serve")
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	gitRootDir := strings.TrimSpace(string(output))
	return filepath.Join(gitRootDir, ghServeDir), nil
}

func getNameWithOwner() (string, error) {
	args := []string{
		"repo", "view",
		"--json", "nameWithOwner",
		"-q", ".nameWithOwner",
	}
	output, _, err := gh.Exec(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output.String()), nil
}

func getLatestCommit() (string, string, bool, error) {

	// If the current branch has an open PR, use the latest commit to that PR
	args := []string{
		"pr", "view",
		"--json", "commits,headRefName,closed",
		"-q", "select(.closed==false)|.commits[-1].oid,.headRefName",
	}
	output, _, err := gh.Exec(args...)
	if err == nil && strings.TrimSpace(output.String()) != "" {
		list := strings.Split(strings.TrimSpace(output.String()), "\n")
		sha := list[0]
		branch := list[1]
		isPr := true
		return branch, sha, isPr, nil
	}

	// Otherwise, use the latest commit on the upstream branch with the same name
	// TODO: Not guaranteed to be the same branch
	cmd := exec.Command("git", "branch", "--show-current")
	current_branch, err := cmd.Output()
	if err != nil {
		return "", "", false, err
	}
	branch := strings.TrimSpace(string(current_branch))

	nameWithOwner, err := getNameWithOwner()
	if err != nil {
		return "", "", false, err
	}

	args = []string{
		"api", "repos/" + nameWithOwner + "/branches/" + branch,
		"-q", ".commit.sha",
	}
	output, _, err = gh.Exec(args...)
	if err != nil {
		return "", "", false, err
	}
	sha := strings.TrimSpace(output.String())
	isPr := false
	return branch, sha, isPr, nil
}

func getArtifact(branch string, sha string, isPr bool) (Artifact, error) {

	filter := `map(select(.headSha=="` + sha + `"))`
	if isPr { // Prefer runs triggered by a PR
		filter += `|(
            if any(.event=="pull_request")
            then map(select(.event=="pull_request"))
            else . end
        )`
	} else { // Exclude runs triggered by a PR
		filter += `|map(select(.event!="pull_request"))`
	}

	// Get a list of runs for the sha
	args := []string{
		"run", "list", "-b", branch,
		"--json", "conclusion,name,status,url,databaseId,headSha,event",
		"-q", filter,
	}
	output, _, err := gh.Exec(args...)
	if err != nil {
		return Artifact{}, err
	}
	workflowRuns := []Run{}
	err = json.Unmarshal([]byte(output.String()), &workflowRuns)
	if err != nil {
		return Artifact{}, err
	}
	runIds := make([]string, len(workflowRuns))
	for i, m := range workflowRuns {
		runIds[i] = strconv.Itoa(m.RunId)
	}

	// Get a list of artifacts for the runs
	nameWithOwner, err := getNameWithOwner()
	if err != nil {
		return Artifact{}, err
	}
	allArtifacts := []Artifact{}
	for i, runId := range runIds {
		args := []string{
			"api", "repos/" + nameWithOwner + "/actions/runs/" + runId + "/artifacts",
			"-q", `
                .artifacts|map(
                    {name, size_in_bytes, expired, run_id: .workflow_run.id}
                )|map(select(.expired==false))
            `,
		}
		output, _, err := gh.Exec(args...)
		if err == nil && strings.TrimSpace(output.String()) != "" {
			var artifactList []Artifact
			err = json.Unmarshal([]byte(output.String()), &artifactList)
			if err != nil {
				return Artifact{}, err
			}
			for j := range artifactList {
				artifactList[j].Run = workflowRuns[i]
			}
			allArtifacts = append(allArtifacts, artifactList...)
		}
	}
	if len(allArtifacts) == 0 {
		return Artifact{}, fmt.Errorf("No artifacts found for branch %s and sha %s", branch, sha)
	}

	// Select the artifact to serve
	var artifact Artifact
	if len(allArtifacts) == 1 {
		artifact = allArtifacts[0]
	} else {
		names := make([]string, len(allArtifacts))
		for i, m := range allArtifacts {
			names[i] = m.Name
		}
		var qs = &survey.Select{
			Message: "Choose an artifact:",
			Options: names,
			Description: func(value string, index int) string {
				return getArtifactInfo(allArtifacts[index])
			},
		}
		answerIndex := 0
		err := survey.AskOne(qs, &answerIndex)
		if err != nil {
			return Artifact{}, err
		}
		artifact = allArtifacts[answerIndex]
	}
	return artifact, nil
}

func getArtifactInfo(artifact Artifact) string {
	workflowRun := artifact.Run
	artifactSize := humanize.Bytes(artifact.Size)
	var status string
	if workflowRun.Status == "completed" {
		if workflowRun.Conclusion == "success" {
			status = "✅ " + workflowRun.Conclusion
		} else {
			status = "❌ " + workflowRun.Conclusion
		}
	} else {
		status = "🟡 " + strings.Replace(workflowRun.Status, "_", " ", -1)
	}
	return fmt.Sprintf("%s [%s] (%s) [%s] %s",
		workflowRun.Name, workflowRun.Event, status, artifactSize, workflowRun.Url,
	)
}

func getCommitInfo(branch string, sha string, isPr bool) (string, error) {
	// Get the branch info
	info := fmt.Sprintf("Serving artifact for branch `%s` (%s)", branch, sha)

	// Get the PR info
	if isPr {
		args := []string{
			"pr", "view",
			"--json", "url,author,title,number",
			"-t", "{{.title}} #{{.number}} by {{.author.login}} ({{.url}})",
		}
		output, _, err := gh.Exec(args...)
		if err != nil {
			return "", err
		}
		info += "\nPR: " + strings.TrimSpace(output.String())
	}

	// Get the commit message, author and date
	nameWithOwner, err := getNameWithOwner()
	if err != nil {
		return "", err
	}
	args := []string{
		"api", "repos/" + nameWithOwner + "/commits/" + sha,
		"-q", `
            . as $base | .commit |
            if has("message") then .message |= split("\n")[0] else . end |
            .message,.author.name,.author.date,$base.html_url
        `,
	}
	output, _, err := gh.Exec(args...)
	if err != nil {
		return "", err
	}
	commitInfo := strings.Split(strings.TrimSpace(output.String()), "\n")
	commitMessage := commitInfo[0]
	commitAuthor := commitInfo[1]
	commitDate := commitInfo[2]
	commitUrl := commitInfo[3]
	date, err := time.Parse(time.RFC3339, commitDate)
	if err != nil {
		return "", err
	}
	commitDate = date.Format("Mon Jan 2 15:04:05 2006 -0700")
	info += fmt.Sprintf("\nCommit: %s by %s on %s (%s)", commitMessage, commitAuthor, commitDate, commitUrl)

	return info, nil
}

func download(artifact Artifact, downloadDir string) error {
	if _, err := os.Stat(downloadDir); os.IsNotExist(err) {
	} else {
		// downloadDir exists
		if noCache {
			err := os.RemoveAll(downloadDir)
			if err != nil {
				return err
			}
		} else { // Use cache
			return nil
		}
	}
	args := []string{
		"run", "download", strconv.Itoa(artifact.RunId),
		"-n", artifact.Name,
		"-D", downloadDir,
	}
	_, _, err := gh.Exec(args...)
	return err
}

func getIndexFile(dir string) (string, error) {
	var names []string = []string{
		"index.html",
		"index.htm",
		".html",
		".htm",
	}
	for _, name := range names {
		found, err := findFile(dir, name)
		if err != nil {
			return "", err
		}
		if found != "" {
			return filepath.Rel(dir, found)
		}
	}
	return "", nil
}

func findFile(dirPath string, fileName string) (string, error) {
	var filePath string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(info.Name(), fileName) {
			filePath = path
			return nil
		}
		return nil
	})

	if err != nil {
		return "", err
	}
	return filePath, nil
}

var (
	port      string
	noBrowser bool
	noCache   bool
)

func init() {
	flag.StringVar(&port, "port", "8080", "Port to serve on")
	flag.BoolVar(&noBrowser, "no-browser", false, "Don't open browser")
	flag.BoolVar(&noCache, "no-cache", false, "Don't use artifact cache")
	flag.Parse()
}

func main() {

	root, err := ghServePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	branch, sha, isPr, err := getLatestCommit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	commitInfo, err := getCommitInfo(branch, sha, isPr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(commitInfo)

	artifact, err := getArtifact(branch, sha, isPr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Artifact: %s - %s\n", artifact.Name, getArtifactInfo(artifact))

	runId := strconv.Itoa(artifact.RunId)
	downloadDir := filepath.Join(root, runId, artifact.Name)
	err = download(artifact, downloadDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	indexFile, err := getIndexFile(downloadDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	url := "http://localhost:" + port + "/" + indexFile
	fmt.Println(url)
	if !noBrowser {
		err = browser.OpenURL(url)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
	http.Handle("/", http.FileServer(http.Dir(downloadDir)))
	log.Printf("Serving %s on HTTP port: %s\n", downloadDir, port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
