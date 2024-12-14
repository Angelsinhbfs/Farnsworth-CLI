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

	const chunkSize = 25 * 1024 * 1024 // 25 MB

	for _, zipFile := range zipFiles {
		file, err := os.Open(zipFile)
		if err != nil {
			fmt.Printf("Error opening file %s: %v\n", zipFile, err)
			return false
		}
		defer file.Close()

		fileInfo, err := file.Stat()
		if err != nil {
			fmt.Printf("Error getting file info: %v\n", err)
			return false
		}
		fileSize := fileInfo.Size()

		fileBar := progressbar.NewOptions64(fileSize, progressbar.OptionSetDescription(fmt.Sprintf("Uploading %s", filepath.Base(zipFile))))

		metadata := AttachMetaData(filepath.Base(zipFile))
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			fmt.Printf("Error marshalling metadata: %v\n", err)
			return false
		}

		offset := int64(0)
		for offset < fileSize {
			chunk := make([]byte, chunkSize)
			n, err := file.ReadAt(chunk, offset)
			if err != nil && err != io.EOF {
				fmt.Printf("Error reading file chunk: %v\n", err)
				return false
			}

			var requestBody bytes.Buffer
			writer := multipart.NewWriter(&requestBody)

			part, err := writer.CreateFormFile("file", filepath.Base(zipFile))
			if err != nil {
				fmt.Printf("Error creating form file: %v\n", err)
				return false
			}

			_, err = part.Write(chunk[:n])
			if err != nil {
				fmt.Printf("Error writing chunk to form: %v\n", err)
				return false
			}

			err = writer.WriteField("metadata", string(metadataJSON))
			if err != nil {
				fmt.Printf("Error writing metadata field: %v\n", err)
				return false
			}

			writer.Close()

			requestURL := fmt.Sprintf("%v/upload/", API_BASE_URL)
			req, err := http.NewRequest("POST", requestURL, &requestBody)
			if err != nil {
				fmt.Printf("Error creating request: %v\n", err)
				return false
			}

			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.Header.Set("Authorization", "Bearer "+Token)

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Error sending request: %v\n", err)
				return false
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Upload failed for file %s: %s\n", zipFile, resp.Status)
				return false
			}

			fileBar.Add(n)
			offset += int64(n)
		}

		fmt.Printf("Successfully uploaded file %s\n", zipFile)
		overallBar.Add(1)
	}

	return true
}
