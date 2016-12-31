package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"

	"golang.org/x/oauth2"

	"github.com/google/go-github/github"
	"gopkg.in/urfave/cli.v2"
)

func main() {
	app := cli.App{}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "owner",
			Usage:   "Owner of the repositry",
			EnvVars: []string{"OWNER"},
		},
		&cli.StringFlag{
			Name:    "repo",
			Usage:   "Name of the github repo",
			EnvVars: []string{"REPO"},
		},
		&cli.StringFlag{
			Name:    "token",
			Usage:   "OAUTH2 token use when authenticating",
			EnvVars: []string{"TOKEN"},
		},
	}

	app.Commands = []*cli.Command{
		&cli.Command{
			Name: "poll-repo",
			Action: func(c *cli.Context) error {
				owner := c.String("owner")
				repo := c.String("repo")
				token := c.String("token")

				ts := oauth2.StaticTokenSource(
					&oauth2.Token{AccessToken: token},
				)

				oc := oauth2.NewClient(oauth2.NoContext, ts)
				client := github.NewClient(oc)

				toCheck := []string{}

				commits, _, err := client.Repositories.ListCommits(owner, repo, &github.CommitsListOptions{
					ListOptions: github.ListOptions{
						PerPage: 10,
					},
					SHA: "master",
				})
				if err != nil {
					return err
				}
				for _, c := range commits {
					sha := *c.SHA
					statuses, _, err := client.Repositories.ListStatuses(owner, repo, sha, &github.ListOptions{PerPage: 200})
					if err != nil {
						return err
					}

					found := false
					for _, s := range statuses {
						if *s.Context == "wheeltapper" {
							found = true
							break
						}
					}
					if found {
						continue
					}
					toCheck = append(toCheck, sha)
				}

				for _, sha := range toCheck {
					checkRef(client, oc, owner, repo, sha)
				}

				return nil
			},
		},
		&cli.Command{
			Name: "check-ref",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "ref",
					Usage:   "Reference to check",
					EnvVars: []string{"REF"},
				},
			},
			Action: func(c *cli.Context) error {
				owner := c.String("owner")
				repo := c.String("repo")
				ref := c.String("ref")
				token := c.String("token")

				ts := oauth2.StaticTokenSource(
					&oauth2.Token{AccessToken: token},
				)

				oc := oauth2.NewClient(oauth2.NoContext, ts)
				client := github.NewClient(oc)
				return checkRef(client, oc, owner, repo, ref)
			},
		},
	}

	app.Run(os.Args)
}

func checkRef(client *github.Client, oc *http.Client, owner, repo, ref string) error {
	sha, _, err := client.Repositories.GetCommitSHA1(owner, repo, ref, "")
	if err != nil {
		return err
	}

	projectDir, err := downloadRef(client, oc, owner, repo, sha)
	if err != nil {
		return err
	}

	err = runCI(projectDir)

	if err != nil {
		client.Repositories.CreateStatus(owner, repo, sha, &github.RepoStatus{
			State:     github.String("failure"),
			TargetURL: github.String("https://www.netice9.com"),
			Context:   github.String("wheeltapper"),
		})
		return err
	}

	_, _, err = client.Repositories.CreateStatus(owner, repo, sha, &github.RepoStatus{
		State:     github.String("success"),
		TargetURL: github.String("https://www.netice9.com"),
		Context:   github.String("wheeltapper"),
	})

	return err

}

func runCI(projectDir string) error {
	cmd := exec.Command("docker-compose", "-f", "docker-compose-ci.yml", "run", "test")
	cmd.Dir = projectDir
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func downloadRef(client *github.Client, oc *http.Client, owner, repo, ref string) (string, error) {
	url, _, err := client.Repositories.GetArchiveLink(owner, repo, github.Tarball, &github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return "", err
	}

	response, err := oc.Do(req)
	if err != nil {
		return "", err
	}

	defer response.Body.Close()

	gzipReader, err := gzip.NewReader(response.Body)
	if err != nil {
		return "", err
	}

	return extractTarStream(gzipReader)

}

func extractTarStream(r io.Reader) (string, error) {
	tr := tar.NewReader(r)
	var header *tar.Header
	var err error
	topLevelDir := ""
	for header, err = tr.Next(); err == nil; header, err = tr.Next() {

		if header.Name == "pax_global_header" {
			_, err = io.Copy(ioutil.Discard, tr)
			if err != nil {
				return "", err
			}
			continue
		}

		filename := header.Name
		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(filename, os.FileMode(header.Mode))
			if err != nil {
				return "", err
			}
			if topLevelDir == "" {
				topLevelDir = filename
			}

		case tar.TypeReg:
			var writer io.WriteCloser
			writer, err = os.Create(filename)

			if err != nil {
				return "", err
			}

			_, err = io.Copy(writer, tr)
			if err != nil {
				return "", err
			}

			err = os.Chmod(filename, os.FileMode(header.Mode))

			if err != nil {
				return "", err
			}

			writer.Close()
		}

	}

	if err != nil && err != io.EOF {
		return "", err
	}

	return topLevelDir, nil

}
