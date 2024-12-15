package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var API_BASE_URL string
var Username string
var Password string
var Token string
var UseMulti bool

func main() {
	reader := bufio.NewReader(os.Stdin)
	jar, err := cookiejar.New(nil)
	if err != nil {
		fmt.Println("Error creating jar")
		return
	}
	client := &http.Client{
		Timeout: time.Minute * 60,
		Jar:     jar,
	}

	introBlock := "" +
		"##############################################################################################" +
		"\n#    ______                                                  __   __       ______ __     ____#" +
		"\n#   / ____/____ _ _____ ____   _____ _      __ ____   _____ / /_ / /_     / ____// /    /  _/#" +
		"\n#  / /_   / __ `// ___// __ \\ / ___/| | /| / // __ \\ / ___// __// __ \\   / /    / /     / /  #" +
		"\n# / __/  / /_/ // /   / / / /(__  ) | |/ |/ // /_/ // /   / /_ / / / /  / /___ / /___ _/ /   #" +
		"\n#/_/     \\__,_//_/   /_/ /_//____/  |__/|__/ \\____//_/    \\__//_/ /_/   \\____//_____//___/   #" +
		"\n##############################################################################################\n"
	fmt.Print(introBlock)
	fmt.Print("Welcome to the Farnsworth command line interface\nThis interface lets you batch upload content. \nIt will convert, zip, and upload the files. \nIt can only process one directory at a time.\nRunning with the multithreading option will use a lot of computing power\n")

	if !CheckFFmpegInstallation() {
		fmt.Println("ffmpeg is not installed. Please visit https://ffmpeg.org/download.html to install it.")
		return
	}

	// Load configuration
	UseMulti, UseHardwareAccel, err = LoadConfig()
	if err != nil {
		fmt.Println("Error loading configuration:", err)
		resp := strings.ToLower(GetInputWithPrompt(reader, "do you wish to use multithreading?y/n", "y"))
		if resp == "y" || resp == "yes" {
			UseMulti = true
		}
		resp = strings.ToLower(GetInputWithPrompt(reader, "do you wish to use hardware acceleration(experimental)?y/n", "n"))
		if resp == "y" || resp == "yes" {
			UseHardwareAccel = true
		}
		// Save configuration
		err = SaveConfig(UseMulti, UseHardwareAccel)
		if err != nil {
			fmt.Println("Error saving configuration:", err)
			return
		}
	}

	if HandleLogin(reader, jar, client) {
		files, ok := HandleTranscoding(reader)
		if ok {
			res := HandleUpload(reader, files)
			if res {
				GetInputWithPrompt(reader, "Upload complete")
				return
			}
			GetInputWithPrompt(reader, "Upload failed")
		}
	}
}

func CheckFFmpegInstallation() bool {
	cmd := exec.Command("ffmpeg", "-version")
	err := cmd.Run()
	return err == nil
}

func GetInputWithPrompt(r *bufio.Reader, prompt string, defaultChoice ...string) string {
	fmt.Println(prompt)
	line, err := r.ReadString('\n')
	line = strings.TrimSuffix(line, "\n")
	if line == "" && len(defaultChoice) > 0 {
		line = defaultChoice[0]
	}
	if err != nil {
		log.Fatal(err)
	}
	return line
}
func LoadConfig() (bool, bool, error) {
	execPath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	execDir := filepath.Dir(execPath)
	configPath := path.Join(execDir, "config.json")
	file, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, err // Default values if config doesn't exist
		}
		return false, false, err
	}
	defer file.Close()

	config := map[string]bool{}
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		return false, false, err
	}

	return config["useMulti"], config["useHardwareAccel"], nil
}

func SaveConfig(useMulti, useHardwareAccel bool) error {
	execPath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	execDir := filepath.Dir(execPath)
	configPath := path.Join(execDir, "config.json")
	config := map[string]bool{
		"useMulti":         useMulti,
		"useHardwareAccel": useHardwareAccel,
	}

	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	return encoder.Encode(config)
}
