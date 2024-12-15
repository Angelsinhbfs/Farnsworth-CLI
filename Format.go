package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"
)

const metadataFileName = "metadata.txt"

var metadataPath string

type MediaIndexEntry struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Genre       []string `json:"genre"`
	Tags        []string `json:"tags"`
	Directory   string   `json:"directory"`
	Location    string   `json:"location"`
	MediaType   string   `json:"mediaType"`
}

var Description string
var Genre []string
var Tags []string
var Directory string
var MediaType string

func GenerateMetaData(r *bufio.Reader) {
	metadataPath = path.Join(cwd, metadataFileName)
	// Check for existing metadata file
	existingMetadata := loadMetadataFromFile()
	fmt.Printf("existing metadata found! Leave answer blank to reuse value\ndescription: %v\ngenres: %v\ntags: %v\ndirectory: %v\n",
		existingMetadata.Description,
		strings.Join(existingMetadata.Genre, " "),
		strings.Join(existingMetadata.Tags, " "),
		existingMetadata.Directory)
	Description = GetInputWithPrompt(r, "Enter the description:", existingMetadata.Description)
	genreInput := GetInputWithPrompt(r, "Enter the genres (separated by spaces):", strings.Join(existingMetadata.Genre, " "))
	Genre = strings.Fields(genreInput)
	tagsInput := GetInputWithPrompt(r, "Enter the tags (separated by spaces):", strings.Join(existingMetadata.Tags, " "))
	Tags = strings.Fields(tagsInput)
	Directory = GetInputWithPrompt(r, "Enter the directory this should be placed in on the server:", existingMetadata.Directory)
	// Ensure MediaType is either "video" or "audio"
	for {
		MediaType = GetInputWithPrompt(r, "Enter the media type (video/audio):", existingMetadata.MediaType)
		if MediaType == "video" || MediaType == "audio" {
			break
		}
		fmt.Println("Invalid media type. Please enter 'video' or 'audio'.")
	}

	// Save metadata to file
	saveMetadataToFile(MediaIndexEntry{
		Description: Description,
		Genre:       Genre,
		Tags:        Tags,
		Directory:   Directory,
		MediaType:   MediaType,
	})
}

func AttachMetaData(filename string) MediaIndexEntry {
	return MediaIndexEntry{
		Title:       filename,
		Description: Description,
		Genre:       Genre,
		Tags:        Tags,
		Directory:   Directory,
		MediaType:   MediaType,
	}
}

func loadMetadataFromFile() MediaIndexEntry {
	var metadata MediaIndexEntry
	file, err := os.Open(metadataPath)
	if err != nil {
		// If the file doesn't exist, return an empty metadata entry
		return metadata
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		log.Printf("Error reading metadata file: %v", err)
		return metadata
	}

	err = json.Unmarshal(data, &metadata)
	if err != nil {
		log.Printf("Error parsing metadata file: %v", err)
	}

	return metadata
}

func saveMetadataToFile(metadata MediaIndexEntry) {
	data, err := json.Marshal(metadata)
	if err != nil {
		log.Printf("Error encoding metadata: %v", err)
		return
	}

	err = os.WriteFile(metadataPath, data, 0644)
	if err != nil {
		log.Printf("Error writing metadata file: %v", err)
	}
}
