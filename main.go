package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"

	"golang.org/x/oauth2"

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

		url, _, err := client.Repositories.GetArchiveLink(owner, repo, github.Tarball, &github.RepositoryContentGetOptions{Ref: ref})
		if err != nil {
			return err
		}

		req, err := http.NewRequest("GET", url.String(), nil)
		if err != nil {
			return err
		}

		response, err := oc.Do(req)
		if err != nil {
			return err
		}

		defer response.Body.Close()

		gzipReader, err := gzip.NewReader(response.Body)
		if err != nil {
			return err
		}

		extractTarStream(gzipReader)

		return nil
	}

}

func extractTarStream(r io.Reader) error {
	tr := tar.NewReader(r)
	var header *tar.Header
	var err error
	topLevelDir := ""
	for header, err = tr.Next(); err != nil; header, err = tr.Next() {

		if header.Name == "pax_global_header" {
			_, err = io.Copy(ioutil.Discard, tr)
			if err != nil {
				return err
			}
			continue
		}

		filename := header.Name

		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(filename, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if topLevelDir == "" {
				topLevelDir = filename
			}

		case tar.TypeReg:
			var writer io.WriteCloser
			writer, err = os.Create(filename)

			if err != nil {
				return err
			}

			_, err = io.Copy(writer, tr)

			err = os.Chmod(filename, os.FileMode(header.Mode))

			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			writer.Close()
		}

	}

	if err != nil && err != io.EOF {
		return err
	}

	return nil

}
