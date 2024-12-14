package main

import (
	"bufio"
	"fmt"
	"strings"
)

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
	Description = GetInputWithPrompt(r, "Enter the description:")
	genreInput := GetInputWithPrompt(r, "Enter the genres (separated by spaces):")
	Genre = strings.Fields(genreInput)
	tagsInput := GetInputWithPrompt(r, "Enter the tags (separated by spaces):")
	Tags = strings.Fields(tagsInput)
	Directory = GetInputWithPrompt(r, "Enter the directory this should be placed in on the server:")
	// Ensure MediaType is either "video" or "audio"
	for {
		MediaType = GetInputWithPrompt(r, "Enter the media type (video/audio):")
		if MediaType == "video" || MediaType == "audio" {
			break
		}
		fmt.Println("Invalid media type. Please enter 'video' or 'audio'.")
	}
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
