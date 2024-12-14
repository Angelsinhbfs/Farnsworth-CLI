package main

import (
	"archive/zip"
	"bufio"
	"fmt"
	"github.com/schollz/progressbar/v3"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

var cwd string
var UseHardwareAccel bool

func HandleTranscoding(r *bufio.Reader) ([]string, bool) {
	cwd, _ = os.Getwd()
	fmt.Print(PrintHeader())
	fmt.Printf("%v:>", cwd)
	// Navigate to source directory
	for HandleInput(GatherInput(r)) {
		fmt.Printf("%v:>", cwd)
	}
	var zipFiles []string
	// Directory has been found. Get files
	files, err := os.ReadDir(cwd)
	if checkError(err) {
		var wg sync.WaitGroup
		totalFiles := len(files)
		overallBar := progressbar.New(totalFiles)

		for _, file := range files {
			if !file.IsDir() {
				inputFile := path.Join(cwd, file.Name())
				fn := file.Name()
				outputDir := path.Join(cwd, strings.TrimSuffix(fn, filepath.Ext(fn)))
				err := os.MkdirAll(outputDir, os.ModePerm)
				if err != nil {
					log.Fatal(err)
				}
				outputFile := path.Join(outputDir, "output.m3u8")

				fileBar := progressbar.NewOptions(2, progressbar.OptionSetDescription(fmt.Sprintf("Processing %s", fn)))

				if UseMulti {
					wg.Add(1)
					go func(inputFile, outputFile string) {
						defer wg.Done()
						err := TranscodeToHLS(inputFile, outputFile)
						if err != nil {
							log.Printf("Error transcoding file %s: %v", path.Base(inputFile), err)
						}
						fileBar.Add(1) // Update progress bar for transcoding

						zipFileName := outputDir + ".zip"
						err = ZipDirectory(outputDir, outputDir+".zip")
						if err != nil {
							log.Printf("Error zipping directory %s: %v", outputDir, err)
						} else {
							zipFiles = append(zipFiles, zipFileName)
						}
						fileBar.Add(1) // Update progress bar for zipping
						// Delete the output directory after zipping
						err = os.RemoveAll(outputDir)
						if err != nil {
							log.Printf("Error deleting directory %s: %v", outputDir, err)
						}

						overallBar.Add(1) // Update overall progress bar
					}(inputFile, outputFile)
				} else {
					err := TranscodeToHLS(inputFile, outputFile)
					if err != nil {
						log.Printf("Error transcoding file %s: %v", file.Name(), err)
					}
					fileBar.Add(1) // Update progress bar for transcoding

					zipFileName := outputDir + ".zip"
					err = ZipDirectory(outputDir, outputDir+".zip")
					if err != nil {
						log.Printf("Error zipping directory %s: %v", outputDir, err)
					} else {
						zipFiles = append(zipFiles, zipFileName)
					}
					fileBar.Add(1) // Update progress bar for zipping
					// Delete the output directory after zipping
					err = os.RemoveAll(outputDir)
					if err != nil {
						log.Printf("Error deleting directory %s: %v", outputDir, err)
					}

					overallBar.Add(1) // Update overall progress bar
				}
			} else {
				overallBar.Add(1)
			}
		}
		wg.Wait() // Wait for all goroutines to finish
		// Confirm or edit zip file names
		for i, zipFile := range zipFiles {
			newName := ConfirmOrEditZipName(r, zipFile)
			if newName != zipFile {
				err := os.Rename(zipFile, newName)
				if err != nil {
					log.Printf("Error renaming file %s to %s: %v", zipFile, newName, err)
				} else {
					zipFiles[i] = newName // Update the slice with the new name
				}
			}
		}
	}
	return zipFiles, true
}

func ConfirmOrEditZipName(reader *bufio.Reader, fullPath string) string {
	// Extract the base name of the file
	defaultName := filepath.Base(fullPath)
	defaultName = strings.TrimSuffix(defaultName, filepath.Ext(defaultName))

	fmt.Printf("Suggested zip file name: %s\n", defaultName)
	fmt.Print("Press Enter to confirm or type a new name: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return fullPath // Return the original full path if no change
	}
	// Construct the new full path with the edited file name
	newFullPath := filepath.Join(filepath.Dir(fullPath), input)
	return newFullPath
}

func TranscodeToHLS(inputFile, outputFile string) error {
	// Determine available GPU encoding
	encoder := getAvailableEncoder()

	var cmd *exec.Cmd
	//todo: get this actually working
	if encoder == "h264_nvenc" {
		// Use NVIDIA NVENC
		cmd = exec.Command("ffmpeg", "-i", inputFile, "-c:v", "h264_nvenc", "-c:a", "aac", "-strict", "experimental", "-start_number", "0", "-hls_time", "10", "-hls_list_size", "0", "-f", "hls", outputFile)
	} else if encoder == "h264_amf" {
		// Use AMD AMF
		cmd = exec.Command("ffmpeg", "-i", inputFile, "-c:v", "h264_amf", "-c:a", "aac", "-strict", "experimental", "-start_number", "0", "-hls_time", "10", "-hls_list_size", "0", "-f", "hls", outputFile)
	} else {
		// Fallback to CPU encoding
		cmd = exec.Command("ffmpeg", "-i", inputFile, "-c:v", "libx264", "-c:a", "aac", "-strict", "experimental", "-start_number", "0", "-hls_time", "10", "-hls_list_size", "0", "-f", "hls", outputFile)
	}

	// Suppress standard output
	cmd.Stdout = os.Stdout

	// Capture standard error

	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func getAvailableEncoder() string {
	if !UseHardwareAccel {
		// Default to CPU encoding
		return "libx264"
	}
	// Run ffmpeg to list available encoders
	cmd := exec.Command("ffmpeg", "-hide_banner", "-encoders")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error checking encoders: %v", err)
		return "libx264" // Default to CPU encoding
	}

	outputStr := string(output)

	// Check for AMD AMF
	if strings.Contains(outputStr, "h264_amf") {
		return "h264_amf"
	}
	// Check for NVIDIA NVENC
	if strings.Contains(outputStr, "h264_nvenc") {
		return "h264_nvenc"
	}
	// Default to CPU encoding
	return "libx264"
}

func ZipDirectory(source, target string) error {
	zipfile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer zipfile.Close()

	archive := zip.NewWriter(zipfile)
	defer archive.Close()

	filepath.Walk(source, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if fi.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}

		zipFile, err := archive.Create(relPath)
		if err != nil {
			return err
		}

		fsFile, err := os.Open(file)
		if err != nil {
			return err
		}
		defer fsFile.Close()

		_, err = io.Copy(zipFile, fsFile)
		return err
	})

	return nil
}

func PrintHeader() string {
	header := "" +
		"\nSimple Commands: " +
		"\n\tls || dir - display contents of folder" +
		"\n\tcd [option] - navigate to folder" +
		"\n\tset - uses the current folder to run" +
		"\n\tquit - exit the program\n"
	return header
}

func HandleInput(input []string) bool {
	if len(input) > 0 {
		switch input[0] {
		case "ls":
			fallthrough
		case "dir":
			files, err := os.ReadDir(cwd)
			if checkError(err) {
				for _, file := range files {
					fmt.Println(file.Name())
				}
			}
			break
		case "cd":
			if len(input) > 1 {
				newPath := filepath.Join(cwd, input[1])
				cleanPath := filepath.Clean(newPath)
				if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
					fmt.Printf("Directory %s does not exist\n", cleanPath)
				} else {
					cwd = cleanPath
				}
			} else {
				fmt.Println("No directory specified")
			}
		case "set":
			return false // Break out of the loop
		case "quit":
		case "q":
			os.Exit(0)
		case "":
			break
		case "-h":
			fallthrough
		case "--help":
			fallthrough
		case "h":
			fallthrough
		case "help":
			fmt.Println(PrintHeader())
			break
		}
	}
	return true
}

func GatherInput(r *bufio.Reader) []string {
	line, err := r.ReadString('\n')
	if err != nil {
		log.Fatal(err)
	}
	trimmed := strings.TrimSuffix(line, "\n")
	return strings.Fields(trimmed)
}

func checkError(err error) bool {
	if err != nil {
		fmt.Println(err)
		return false
	}
	return true
}
