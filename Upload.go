package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func HandleUpload(r *bufio.Reader, zipFiles []string) bool {
	GenerateMetaData(r)
	fmt.Println("Initiating swarm upload")

	const chunkSize = 15 * 1024 * 1024 // 15 MB

	// Create a new progress bar container
	p := mpb.New()

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

		totalChunks := (fileSize + chunkSize - 1) / chunkSize // Calculate total number of chunks

		// Create a progress bar for the file
		fileBar := p.AddBar(fileSize,
			mpb.PrependDecorators(
				decor.Name(fmt.Sprintf("Uploading %s: ", filepath.Base(zipFile))),
				decor.CountersKibiByte("% .2f / % .2f"),
			),
			mpb.AppendDecorators(decor.Percentage()),
		)

		metadata := AttachMetaData(filepath.Base(zipFile))
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			fmt.Printf("Error marshalling metadata: %v\n", err)
			return false
		}

		var wg sync.WaitGroup
		errorChan := make(chan error, totalChunks)

		for offset := int64(0); offset < fileSize; offset += chunkSize {
			wg.Add(1)
			go func(offset int64) {
				defer wg.Done()

				chunk := make([]byte, chunkSize)
				n, err := file.ReadAt(chunk, offset)
				if err != nil && err != io.EOF {
					errorChan <- fmt.Errorf("error reading file chunk: %v", err)
					return
				}

				var requestBody bytes.Buffer
				writer := multipart.NewWriter(&requestBody)

				part, err := writer.CreateFormFile("file", filepath.Base(zipFile))
				if err != nil {
					errorChan <- fmt.Errorf("error creating form file: %v", err)
					return
				}

				_, err = part.Write(chunk[:n])
				if err != nil {
					errorChan <- fmt.Errorf("error writing chunk to form: %v", err)
					return
				}

				err = writer.WriteField("metadata", string(metadataJSON))
				if err != nil {
					errorChan <- fmt.Errorf("error writing metadata field: %v", err)
					return
				}

				err = writer.WriteField("totalChunks", fmt.Sprintf("%d", totalChunks))
				if err != nil {
					errorChan <- fmt.Errorf("error writing totalChunks field: %v", err)
					return
				}

				err = writer.WriteField("chunkIndex", fmt.Sprintf("%d", offset/chunkSize))
				if err != nil {
					errorChan <- fmt.Errorf("error writing chunkIndex field: %v", err)
					return
				}

				writer.Close()

				requestURL := fmt.Sprintf("%v/upload/", API_BASE_URL)
				req, err := http.NewRequest("POST", requestURL, &requestBody)
				if err != nil {
					errorChan <- fmt.Errorf("error creating request: %v", err)
					return
				}

				req.Header.Set("Content-Type", writer.FormDataContentType())
				req.Header.Set("Authorization", "Bearer "+Token)

				client := &http.Client{
					Timeout: time.Minute * 30,
				}
				resp, err := client.Do(req)
				if err != nil {
					errorChan <- fmt.Errorf("error sending request: %v", err)
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errorChan <- fmt.Errorf("upload failed for file %s: %s", zipFile, resp.Status)
					return
				}

				fileBar.IncrBy(n)
			}(offset)
		}

		wg.Wait()
		close(errorChan)

		for err := range errorChan {
			if err != nil {
				fmt.Println(err)
				return false
			}
		}

		fmt.Printf("Successfully uploaded file %s\n", zipFile)
	}

	// Wait for all bars to complete
	p.Wait()

	return true
}
