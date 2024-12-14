package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/schollz/progressbar/v3"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

type ProgressReader struct {
	io.Reader
	bar *progressbar.ProgressBar
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	pr.bar.Add(n)
	return n, err
}

func HandleUpload(r *bufio.Reader, zipFiles []string) bool {
	GenerateMetaData(r)
	overallBar := progressbar.New(len(zipFiles))

	for _, zipFile := range zipFiles {
		// Open the zip file
		file, err := os.Open(zipFile)
		if err != nil {
			fmt.Printf("Error opening file %s: %v\n", zipFile, err)
			return false
		}
		defer file.Close()

		// Get the file size for the progress bar
		fileInfo, err := file.Stat()
		if err != nil {
			fmt.Printf("Error getting file info: %v\n", err)
			return false
		}
		fileSize := fileInfo.Size()

		// Create a progress bar for the file upload
		fileBar := progressbar.NewOptions64(fileSize, progressbar.OptionSetDescription(fmt.Sprintf("Uploading %s", filepath.Base(zipFile))))

		// Create a new buffer to hold the form data
		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)

		// Add the zip file to the form
		part, err := writer.CreateFormFile("file", filepath.Base(zipFile))
		if err != nil {
			fmt.Printf("Error creating form file: %v\n", err)
			return false
		}

		// Use ProgressReader to track upload progress
		progressReader := &ProgressReader{Reader: file, bar: fileBar}
		_, err = io.Copy(part, progressReader)
		if err != nil {
			fmt.Printf("Error copying file data: %v\n", err)
			return false
		}

		// Add metadata to the form
		metadata := AttachMetaData(filepath.Base(zipFile))
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			fmt.Printf("Error marshalling metadata: %v\n", err)
			return false
		}
		err = writer.WriteField("metadata", string(metadataJSON))
		if err != nil {
			fmt.Printf("Error writing metadata field: %v\n", err)
			return false
		}

		// Close the writer to finalize the form data
		writer.Close()

		// Create the HTTP request
		requestURL := fmt.Sprintf("%v/upload/", API_BASE_URL)
		req, err := http.NewRequest("POST", requestURL, &requestBody)
		if err != nil {
			fmt.Printf("Error creating request: %v\n", err)
			return false
		}

		// Set headers
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+Token)

		// Send the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Error sending request: %v\n", err)
			return false
		}
		defer resp.Body.Close()

		// Check the response
		if resp.StatusCode != http.StatusOK {
			fmt.Printf("Upload failed for file %s: %s\n", zipFile, resp.Status)
		} else {

			fmt.Printf("Successfully uploaded file %s\n", zipFile)
		}

		overallBar.Add(1)
	}

	return true
}
