package main

import (
	"archive/zip"
	"bufio"
	"bytes"
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
	for HandleInput(GatherInput(r)) {
		fmt.Printf("%v:>", cwd)
	}

	var zipFiles []string
	files, err := os.ReadDir(cwd)
	if checkError(err) {
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".zip") {
				zipFiles = append(zipFiles, path.Join(cwd, file.Name()))
			}
		}
	}

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

	p := mpb.New()
	var wg sync.WaitGroup

	var selections map[os.DirEntry][]string
	selections = make(map[os.DirEntry][]string, len(files))
	for _, tf := range files {
		if !tf.IsDir() && isMediaFile(tf.Name()) {
			//only ask about the first set of
			subtitle := selectSubtitleTrack(r, tf.Name())
			audioTrack := selectAudioTrack(r, tf.Name())
			selections[tf] = make([]string, 2)
			selections[tf][0] = subtitle
			selections[tf][1] = audioTrack
		}
	}

	for _, file := range files {
		if !file.IsDir() && isMediaFile(file.Name()) {
			inputFile := path.Join(cwd, file.Name())
			fn := file.Name()
			outputDir := filepath.Join(cwd, strings.TrimSuffix(fn, filepath.Ext(fn))) // Correct outputDir
			err := os.MkdirAll(outputDir, os.ModePerm)
			if err != nil {
				log.Fatal(err)
			}
			subtitle := selections[file][0]
			audioTrack := selections[file][1]

			if UseMulti {
				wg.Add(1)
				go func(inputFile, outputFile, subtitle, audioTrack, fn string) {
					defer wg.Done()
					fileBar := p.AddSpinner(1,
						mpb.PrependDecorators(
							decor.Name(fmt.Sprintf("Processing %s: ", fn)),
						),
					)
					err := TranscodeToHLSWithSubtitle(inputFile, outputDir, subtitle, audioTrack)
					if err != nil {
						log.Printf("Error transcoding file %s: %v", path.Base(inputFile), err)
					}

					zipFileName := outputDir + ".zip"
					err = ZipDirectory(outputDir, zipFileName)
					if err != nil {
						log.Printf("Error zipping directory %s: %v", outputDir, err)
					} else {
						zipFiles = append(zipFiles, zipFileName)
					}

					err = os.RemoveAll(outputDir)
					if err != nil {
						log.Printf("Error deleting directory %s: %v", outputDir, err)
					}

					fileBar.Increment()
				}(inputFile, outputDir, subtitle, audioTrack, fn)
			} else {
				fileBar := p.AddSpinner(1,
					mpb.PrependDecorators(
						decor.Name(fmt.Sprintf("Processing %s: ", fn)),
					),
				)
				err := TranscodeToHLSWithSubtitle(inputFile, outputDir, subtitle, audioTrack)
				if err != nil {
					log.Printf("Error transcoding file %s: %v", file.Name(), err)
				}

				zipFileName := outputDir + ".zip"
				err = ZipDirectory(outputDir, zipFileName)
				if err != nil {
					log.Printf("Error zipping directory %s: %v", outputDir, err)
				} else {
					zipFiles = append(zipFiles, zipFileName)
				}

				err = os.RemoveAll(outputDir)
				if err != nil {
					log.Printf("Error deleting directory %s: %v", outputDir, err)
				}

				fileBar.Increment()
			}
		}
	}
	wg.Wait()
	p.Shutdown()

	for i, zipFile := range zipFiles {
		newName := ConfirmOrEditZipName(r, zipFile)
		if newName != zipFile {
			err := os.Rename(zipFile, newName)
			if err != nil {
				log.Printf("Error renaming file %s to %s: %v", zipFile, newName, err)
			} else {
				zipFiles[i] = newName
			}
		}
	}

	return zipFiles, true
}

func TranscodeToHLSWithSubtitle(inputFile, outputDir, subtitle, audioTrack string) error {
	encoder := getAvailableEncoder()

	// Construct log file name based on input file name
	inputFileName := filepath.Base(inputFile)
	logFileName := fmt.Sprintf("%s-ffmpeg.log", strings.TrimSuffix(inputFileName, filepath.Ext(inputFileName)))
	ffmpegLog, err := os.Create(filepath.Join("./", logFileName)) // Log file in current directory
	if err != nil {
		return fmt.Errorf("error creating ffmpeg log file: %w", err)
	}
	defer ffmpegLog.Close()

	// Output filenames for video/audio and subtitle playlists
	videoAudioOutput := filepath.Join(outputDir, "content.m3u8")

	// Construct the base arguments for the FFmpeg command
	baseArgs := []string{
		"-i", inputFile,
		"-map", "0:v:0",
		"-c:v", encoder,
		"-c:a", "aac",
		"-ac", "2",
		"-crf", "23",
	}

	// Add audio track selection
	if audioTrack != "" {
		baseArgs = append(baseArgs, "-map", "0:a:"+audioTrack)
	}

	// Handle subtitles
	if subtitle != "" {
		baseArgs = append(baseArgs,
			"-map", "0:s:"+subtitle,
			"-c:s", "webvtt",
			"-var_stream_map", "v:0,a:0,s:0",
		)
	} else {
		baseArgs = append(baseArgs, "-sn", "-var_stream_map", "v:0,a:0") // Suppress subtitles
	}

	baseArgs = append(baseArgs,
		"-start_number", "0",
		"-hls_time", "15",
		"-hls_list_size", "0",
		"-hls_segment_type", "mpegts",
		"-f", "hls",
		videoAudioOutput,
	)

	// Construct the FFmpeg command
	cmd := exec.Command("ffmpeg", baseArgs...)
	cmd.Stdout = ffmpegLog
	cmd.Stderr = ffmpegLog

	// Start and wait for the command
	if err := cmd.Run(); err != nil {
		// Read and include log content in the error message
		logContent, readErr := os.ReadFile(ffmpegLog.Name())
		if readErr != nil {
			return fmt.Errorf("ffmpeg command failed: %w; error reading log: %v", err, readErr) // More specific error message
		}
		// Print the full command and log content for debugging
		return fmt.Errorf("ffmpeg command failed: %w\nCommand: %s\nLog content:\n%s", err, cmd.String(), string(logContent)) // Include command and log
	}

	// Write the master playlist after successful transcoding
	masterPlaylist := path.Join(outputDir, "output.m3u8")
	subtitlePlaylist := ""
	if subtitle != "" {
		subtitlePlaylist = "content_vtt.m3u8"
	}
	if err := writeMasterPlaylist(masterPlaylist, "content.m3u8", subtitlePlaylist); err != nil {
		return fmt.Errorf("error writing master playlist: %w", err)
	}

	return nil
}

func writeMasterPlaylist(masterPlaylist, videoAudioOutput string, subtitleOutput ...string) error {
	var content string
	if len(subtitleOutput) > 0 {
		content = `#EXTM3U
#EXT-X-VERSION:3

#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="English",DEFAULT=YES,AUTOSELECT=YES,LANGUAGE="en",URI="` + subtitleOutput[0] + `"

#EXT-X-STREAM-INF:BANDWIDTH=5022487,AVERAGE-BANDWIDTH=1155141,RESOLUTION=1280x720,CODECS="avc1.6e001f,mp4a.40.2",SUBTITLES="subs"
` + videoAudioOutput + `
`
	} else {
		content = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=5022487,AVERAGE-BANDWIDTH=1155141,RESOLUTION=1280x720,CODECS="avc1.6e001f,mp4a.40.2"
` + videoAudioOutput + `
`
	}

	err := os.WriteFile(masterPlaylist, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("error writing master playlist: %v", err)
	}

	return nil
}

func selectSubtitleTrack(r *bufio.Reader, inputFile string) string {
	subtitles, err := getSubtitleTracks(inputFile)
	if err != nil {
		log.Printf("Error getting subtitles for file %s: %v", inputFile, err)
		return ""
	}

	if len(subtitles) == 0 {
		return ""
	}

	fmt.Printf("Available subtitles for %s:\n", filepath.Base(inputFile))
	for i, subtitle := range subtitles {
		fmt.Printf("%d: %s\n", i, subtitle)
	}
	choice := GetInputWithPrompt(r, "Select subtitle track number (or press Enter to skip): ")
	if choice != "" {
		index, err := strconv.Atoi(choice)
		if err == nil && index >= 0 && index < len(subtitles) {
			fmt.Println("selected subtitle")
			fmt.Println(subtitles[index])
			return choice //subtitles[index]
		}
	}

	return ""
}

func getSubtitleTracks(inputFile string) ([]string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "s", "-show_entries", "stream=index:stream_tags=language:stream=codec_name", "-of", "default=noprint_wrappers=1:nokey=1", "-print_format", "csv", inputFile)

	var combinedOutput bytes.Buffer
	cmd.Stdout = &combinedOutput
	cmd.Stderr = &combinedOutput

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("error running ffprobe: %w; output: %s", err, combinedOutput.String())
	}

	lines := strings.Split(strings.TrimSpace(combinedOutput.String()), "\n")
	var subtitles []string
	for _, line := range lines {
		parts := strings.Split(line, ",")
		if len(parts) >= 4 {
			index := parts[len(parts)-1]
			codecName := parts[2]
			isTextBased := strings.Contains(strings.ToLower(codecName), "subrip") || strings.Contains(strings.ToLower(codecName), "webvtt") || strings.Contains(strings.ToLower(codecName), "mov_text") || strings.Contains(strings.ToLower(codecName), "ass")

			if isTextBased {
				subtitles = append(subtitles, index)
			}
		}
	}

	return subtitles, nil
}

func selectAudioTrack(r *bufio.Reader, inputFile string) string {
	audioTracks, err := getAudioTracks(inputFile)
	if err != nil {
		log.Printf("Error getting audio tracks for file %s: %v", inputFile, err)
		return "0" // Or return "0" to select the first track by default
	}

	if len(audioTracks) <= 1 {
		return "0" // No need to select if only one or no audio track
	}

	fmt.Printf("Available audio tracks for %s:\n", filepath.Base(inputFile))
	for i, track := range audioTracks {
		fmt.Printf("%d: %s\n", i, track)
	}

	for {
		choice := GetInputWithPrompt(r, "Select audio track number: ")
		if choice != "" {
			index, err := strconv.Atoi(choice)
			if err == nil && index >= 0 && index < len(audioTracks) {
				return choice
			} else {
				fmt.Println("Invalid choice. Please select a valid track number.")
			}
		}
	}

}

func getAudioTracks(inputFile string) ([]string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "a", "-show_entries", "stream=index:stream_tags=language", "-of", "default=noprint_wrappers=1:nokey=1", "-print_format", "csv", inputFile)

	// Create combined output buffer for stdout and stderr
	var combinedOutput bytes.Buffer
	cmd.Stdout = &combinedOutput
	cmd.Stderr = &combinedOutput

	// Construct log file name based on input file name
	inputFileName := filepath.Base(inputFile)
	logFileName := fmt.Sprintf("%s-audio-ffprobe.log", strings.TrimSuffix(inputFileName, filepath.Ext(inputFileName)))
	logFilePath := filepath.Join(filepath.Dir(inputFile), logFileName)

	if err := cmd.Run(); err != nil {
		// Write combined output to log file for debugging, even if there's an error
		if err := os.WriteFile(logFilePath, combinedOutput.Bytes(), 0644); err != nil {
			return nil, fmt.Errorf("error running ffprobe and writing to log: %w; ffprobe error: %v", err, combinedOutput.String())
		}
		return nil, fmt.Errorf("error running ffprobe: %w; output: %s", err, combinedOutput.String())
	}

	// Write combined output to log file after successful execution
	if err := os.WriteFile(logFilePath, combinedOutput.Bytes(), 0644); err != nil {
		return nil, fmt.Errorf("ffprobe ran successfully, but error writing to log: %w", err)
	}

	var audioTracks []string
	lines := strings.Split(strings.TrimSpace(combinedOutput.String()), "\n") // Use combinedOutput

	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) >= 1 {
			index := parts[0]
			audioTracks = append(audioTracks, index)
		}
	}

	return audioTracks, nil
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
	if strings.Contains(outputStr, "h264_vaapi") {
		return "h264_vaapi"
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
