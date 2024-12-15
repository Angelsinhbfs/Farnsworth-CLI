package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
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
	// Check for existing zip files
	files, err := os.ReadDir(cwd)
	if checkError(err) {
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".zip") {
				zipFiles = append(zipFiles, path.Join(cwd, file.Name()))
			}
		}
	}

	// If zip files are found, offer to use them
	if len(zipFiles) > 0 {
		fmt.Println("Existing zip files found:")
		for _, zipFile := range zipFiles {
			fmt.Println(zipFile)
		}
		choice := GetInputWithPrompt(r, "Do you want to use these zip files instead of re-transcoding? (y/n): ")
		if choice == "y" || choice == "Y" {
			return zipFiles, true
		}
	}

	// Create a new progress bar container
	p := mpb.New()

	var wg sync.WaitGroup
	totalFiles := len(files)

	// Create an overall progress bar
	overallBar := p.AddBar(int64(totalFiles),
		mpb.PrependDecorators(
			decor.Name("Overall Progress: "),
			decor.CountersNoUnit("%d / %d"),
		),
		mpb.AppendDecorators(decor.Percentage()),
	)

	for _, file := range files {
		if !file.IsDir() && isMediaFile(file.Name()) {
			inputFile := path.Join(cwd, file.Name())
			fn := file.Name()
			outputDir := path.Join(cwd, strings.TrimSuffix(fn, filepath.Ext(fn)))
			err := os.MkdirAll(outputDir, os.ModePerm)
			if err != nil {
				log.Fatal(err)
			}
			outputFile := path.Join(outputDir, "output.m3u8")

			// Get the total number of frames
			totalFrames, err := getTotalFrames(inputFile)
			if err != nil {
				log.Printf("Error getting total frames for file %s: %v\nguessing around 60000", fn, err)
				totalFrames = 60000
			}

			if UseMulti {
				wg.Add(1)
				go func(inputFile, outputFile string) {
					fileBar := p.AddBar(totalFrames,
						mpb.PrependDecorators(
							decor.Name(fmt.Sprintf("Processing %s: ", fn)),
						),
						mpb.AppendDecorators(decor.Percentage()),
					)
					defer wg.Done()
					err := TranscodeToHLS(inputFile, outputFile, fileBar)
					if err != nil {
						log.Printf("Error transcoding file %s: %v", path.Base(inputFile), err)
					}

					// Increment the fileBar based on actual progress
					fileBar.SetCurrent(totalFrames - 1) // Mark transcoding as complete

					zipFileName := outputDir + ".zip"
					err = ZipDirectory(outputDir, zipFileName)
					if err != nil {
						log.Printf("Error zipping directory %s: %v", outputDir, err)
					} else {
						zipFiles = append(zipFiles, zipFileName)
					}

					// Increment the fileBar to mark zipping as complete
					fileBar.SetCurrent(totalFrames + 1) // Assuming zipping is one additional step

					// Delete the output directory after zipping
					err = os.RemoveAll(outputDir)
					if err != nil {
						log.Printf("Error deleting directory %s: %v", outputDir, err)
					}

					overallBar.Increment() // Update overall progress bar
				}(inputFile, outputFile)
			} else {
				// Create a progress bar for the file
				fileBar := p.AddBar(totalFrames,
					mpb.PrependDecorators(
						decor.Name(fmt.Sprintf("Processing %s: ", fn)),
					),
					mpb.AppendDecorators(decor.Percentage()),
				)
				err := TranscodeToHLS(inputFile, outputFile, fileBar)
				if err != nil {
					log.Printf("Error transcoding file %s: %v", file.Name(), err)
				}

				fileBar.SetCurrent(totalFrames - 1) // Mark transcoding as complete

				zipFileName := outputDir + ".zip"
				err = ZipDirectory(outputDir, zipFileName)
				if err != nil {
					log.Printf("Error zipping directory %s: %v", outputDir, err)
				} else {
					zipFiles = append(zipFiles, zipFileName)
				}

				fileBar.SetCurrent(totalFrames + 1) // Mark zipping as complete

				err = os.RemoveAll(outputDir)
				if err != nil {
					log.Printf("Error deleting directory %s: %v", outputDir, err)
				}

				overallBar.Increment() // Update overall progress bar
			}
		} else {
			overallBar.Increment()
		}
	}
	wg.Wait() // Wait for all goroutines to finish
	p.Shutdown()
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

	return zipFiles, true
}

// Function to get the total number of frames using ffprobe
func getTotalFrames(inputFile string) (int64, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=nb_read_frames", "-of", "json", inputFile)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return 0, fmt.Errorf("error running ffprobe: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		return 0, fmt.Errorf("error parsing ffprobe output: %v", err)
	}

	streams, ok := result["streams"].([]interface{})
	if !ok || len(streams) == 0 {
		return 0, fmt.Errorf("no streams found in ffprobe output")
	}

	stream, ok := streams[0].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("invalid stream data in ffprobe output")
	}

	nbReadFrames, ok := stream["nb_read_frames"].(string)
	if !ok {
		return 0, fmt.Errorf("frame count not found in ffprobe output")
	}

	frameCount, err := strconv.ParseInt(nbReadFrames, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("error converting frame count: %v", err)
	}

	return frameCount, nil
}

func isMediaFile(fileName string) bool {
	// Define common video and audio file extensions
	mediaExtensions := []string{".mp4", ".avi", ".mkv", ".mov", ".mp3", ".wav", ".flac", ".aac"}

	ext := strings.ToLower(filepath.Ext(fileName))
	for _, mediaExt := range mediaExtensions {
		if ext == mediaExt {
			return true
		}
	}
	return false
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

func TranscodeToHLS(inputFile, outputFile string, fileBar *mpb.Bar) error {
	encoder := getAvailableEncoder()

	var cmd *exec.Cmd
	if encoder == "h264_nvenc" {
		cmd = exec.Command("ffmpeg", "-i", inputFile, "-c:v", "h264_nvenc", "-c:a", "aac", "-strict", "experimental", "-start_number", "0", "-hls_time", "10", "-hls_list_size", "0", "-f", "hls", outputFile, "-progress", "pipe:2")
	} else if encoder == "h264_amf" {
		cmd = exec.Command("ffmpeg", "-i", inputFile, "-c:v", "h264_amf", "-c:a", "aac", "-strict", "experimental", "-start_number", "0", "-hls_time", "10", "-hls_list_size", "0", "-f", "hls", outputFile, "-progress", "pipe:2")
	} else {
		cmd = exec.Command("ffmpeg", "-i", inputFile, "-c:v", "libx264", "-c:a", "aac", "-strict", "experimental", "-start_number", "0", "-hls_time", "10", "-hls_list_size", "0", "-f", "hls", outputFile, "-progress", "pipe:2")
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("error creating stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting ffmpeg command: %v", err)
	}

	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "frame=") {
			// Extract the frame number
			parts := strings.Fields(line)
			if len(parts) > 1 {
				frameStr := parts[1]
				frame, err := strconv.Atoi(frameStr)
				if err == nil {
					fileBar.SetCurrent(int64(frame)) // Update the progress bar with the current frame
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading ffmpeg output: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg command failed: %v", err)
	}

	return nil
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
