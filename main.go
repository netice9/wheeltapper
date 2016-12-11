package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"

	"github.com/docker/libcompose/docker"
	"github.com/docker/libcompose/docker/ctx"
	"github.com/docker/libcompose/project"
	"github.com/docker/libcompose/project/events"
	"github.com/docker/libcompose/project/options"
	"github.com/google/go-github/github"
	"gopkg.in/urfave/cli.v1"
)

func main() {
	app := cli.NewApp()

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "owner",
			Usage:  "Owner of the repositry",
			EnvVar: "OWNER",
		},
		cli.StringFlag{
			Name:   "repo",
			Usage:  "Name of the github repo",
			EnvVar: "REPO",
		},
		cli.StringFlag{
			Name:   "ref",
			Usage:  "Reference to check",
			EnvVar: "REF",
		},
		cli.StringFlag{
			Name:   "token",
			Usage:  "OAUTH2 token use when authenticating",
			EnvVar: "TOKEN",
		},
	}

	app.Action = func(c *cli.Context) error {
		owner := c.String("owner")
		repo := c.String("repo")
		ref := c.String("ref")
		token := c.String("token")

		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)

		oc := oauth2.NewClient(oauth2.NoContext, ts)
		client := github.NewClient(oc)

		projectDir, err := downloadRef(client, oc, owner, repo, ref)
		if err != nil {
			return err
		}

		err = runCI(projectDir)

		if err != nil {
			return err
		}

		return nil
	}

	app.Run(os.Args)
}

func runCI(projectDir string) error {
	proj, err := docker.NewProject(&ctx.Context{
		Context: project.Context{
			ComposeFiles: []string{fmt.Sprintf("%s/docker-compose-ci.yml", projectDir)},
			ProjectName:  projectDir,
		},
	}, nil)

	cfg, _ := proj.GetServiceConfig("ci")

	ch := make(chan events.Event, 10)

	proj.AddListener(ch)
	exitCode, err := proj.Run(context.Background(), "ci", cfg.Command, options.Run{Detached: false})
	if err != nil {
		return err
	}

	log.Println("ExitCode", exitCode)

	for e := range ch {
		log.Println(e)
		if e.EventType == events.ServiceRun && e.ServiceName == "ci" {
			break
		}
	}

	err = proj.Stop(context.Background(), 10)
	if err != nil {
		return err
	}

	err = proj.Delete(context.Background(), options.Delete{RemoveVolume: true, RemoveRunning: true})
	if err != nil {
		return err
	}
	return nil
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
		log.Println("filename", filename)
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
			log.Println("writing", filename)
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
