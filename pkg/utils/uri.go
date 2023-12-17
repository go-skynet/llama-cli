package utils

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	githubURI = "github:"
)

func GetURI(url string, f func(url string, i []byte) error) error {
	if strings.HasPrefix(url, githubURI) {
		parts := strings.Split(url, ":")
		repoParts := strings.Split(parts[1], "@")
		branch := "main"

		if len(repoParts) > 1 {
			branch = repoParts[1]
		}

		repoPath := strings.Split(repoParts[0], "/")
		org := repoPath[0]
		project := repoPath[1]
		projectPath := strings.Join(repoPath[2:], "/")

		url = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", org, project, branch, projectPath)
	}

	if strings.HasPrefix(url, "file://") {
		rawURL := strings.TrimPrefix(url, "file://")
		// checks if the file is symbolic, and resolve if so - otherwise, this function returns the path unmodified.
		resolvedFile, err := filepath.EvalSymlinks(rawURL)
		if err != nil {
			return err
		}
		// Read the response body
		body, err := os.ReadFile(resolvedFile)
		if err != nil {
			return err
		}

		// Unmarshal YAML data into a struct
		return f(url, body)
	}

	// Send a GET request to the URL
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// Read the response body
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	// Unmarshal YAML data into a struct
	return f(url, body)
}

func DownloadFile(url string, filepath string) error {
	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)

	return err
}

// MD5 of a string
func MD5(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}
