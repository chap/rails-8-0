package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type PostRequestBody struct {
	Path           string `json:"path"`
	RepoURL        string `json:"repoURL"`
	TargetRevision string `json:"targetRevision"`
}

func main() {
	http.HandleFunc("/", handleRequest)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		handleGetRequest(w, r)
	} else if r.Method == http.MethodPost {
		handlePostRequest(w, r)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetRequest(w http.ResponseWriter, r *http.Request) {
	// Remove the leading slash from the URL path
	path := strings.TrimPrefix(r.URL.Path, "/")

	// Split the remaining path by "/"
	parts := strings.Split(path, "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid URL format. Expected: /provider/owner/repo/path", http.StatusBadRequest)
		return
	}

	// Extract the provider, owner, repo, and optional subdirectory path
	provider, owner, repo := parts[0], parts[1], parts[2]
	subPath := "" // default subdirectory path is empty

	// If there are more parts, the remaining part is the path within the repo
	if len(parts) > 3 {
		subPath = strings.Join(parts[3:], "/")
	}

	// Default ref and timeout values
	ref := "main"  // default ref
	timeout := 120 // default timeout

	// Check if ref is provided in the query params
	refParam := r.URL.Query().Get("ref")
	if refParam != "" {
		ref = refParam
	}

	// Check if timeout is provided in the query params
	timeoutParam := r.URL.Query().Get("timeout")
	if timeoutParam != "" {
		var err error
		timeout, err = strconv.Atoi(timeoutParam)
		if err != nil {
			http.Error(w, "Invalid timeout value", http.StatusBadRequest)
			return
		}
	}

	// Construct the repo URL based on the provider
	repoURL := fmt.Sprintf("https://%s/%s/%s", provider, owner, repo)

	// Call the function to process the request with the provided parameters
	processRequest(w, r, repoURL, repo, ref, subPath, timeout)
}

func handlePostRequest(w http.ResponseWriter, r *http.Request) {
	var requestBody PostRequestBody
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if requestBody.RepoURL == "" || requestBody.TargetRevision == "" {
		http.Error(w, "Missing required fields: repoURL and targetRevision", http.StatusBadRequest)
		return
	}

	parts := strings.Split(strings.Trim(requestBody.RepoURL, "/"), "/")
	processRequest(w, r, requestBody.RepoURL, parts[4], requestBody.TargetRevision, requestBody.Path, 20)
}

func processRequest(w http.ResponseWriter, r *http.Request, repoURL, repo, ref, path string, timeout int) {
	tmpDir, err := os.MkdirTemp("", "repo-download-")
	if err != nil {
		http.Error(w, "Failed to create temporary directory", http.StatusInternalServerError)
		log.Printf("Error creating temp directory: %v", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	zipURL := fmt.Sprintf("%s/archive/%s.zip", repoURL, ref)
	zipPath := filepath.Join(tmpDir, "repo.zip")
	if err := downloadFile(zipURL, zipPath); err != nil {
		http.Error(w, "Failed to download repository", http.StatusInternalServerError)
		log.Printf("Error downloading file from %s: %v", zipURL, err)
		return
	}

	extractPath := filepath.Join(tmpDir, "repo")
	if err := unzip(zipPath, extractPath); err != nil {
		http.Error(w, "Failed to extract ZIP file", http.StatusInternalServerError)
		log.Printf("Error extracting zip file %s: %v", zipPath, err)
		return
	}

	if path != "" {
		fullPath := fmt.Sprintf("%s-%s/%s", repo, ref, path)
		extractPath = filepath.Join(extractPath, fullPath)
		if _, err := os.Stat(extractPath); os.IsNotExist(err) {
			http.Error(w, "Specified path does not exist", http.StatusBadRequest)
			log.Printf("Path does not exist: %s", extractPath)
			return
		}
	}

	archiveName := fmt.Sprintf("repo-%d.tar.gz", time.Now().Unix())
	archivePath := filepath.Join(tmpDir, archiveName)
	if err := createTarGz(archivePath, extractPath); err != nil {
		http.Error(w, "Failed to create archive", http.StatusInternalServerError)
		log.Printf("Error creating tar.gz file %s: %v", archivePath, err)
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", archiveName))
	http.ServeFile(w, r, archivePath)
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file: %s", resp.Status)
	}

	file, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	return err
}

func unzip(src, dest string) error {
	zipReader, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zipReader.Close()

	for _, file := range zipReader.File {
		extractPath := filepath.Join(dest, file.Name)
		log.Printf("Extracting file: %s", extractPath)
		if file.FileInfo().IsDir() {
			os.MkdirAll(extractPath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(extractPath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(extractPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return err
		}

		rc, err := file.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func createTarGz(outputPath, sourceDir string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	return filepath.Walk(sourceDir, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}

		header.Name, _ = filepath.Rel(sourceDir, file)
		log.Printf("Adding file to tar.gz: %s", header.Name)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !fi.Mode().IsRegular() {
			return nil
		}

		srcFile, err := os.Open(file)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		_, err = io.Copy(tw, srcFile)
		return err
	})
}
