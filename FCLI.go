package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"strings"
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
		Jar: jar,
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
	resp := strings.ToLower(GetInputWithPrompt(reader, "do you wish to use multithreading?y/n"))
	if resp == "y" || resp == "yes" {
		UseMulti = true
	}
	resp = strings.ToLower(GetInputWithPrompt(reader, "do you wish to use hardware acceleration(experimental)?y/n"))
	if resp == "y" || resp == "yes" {
		UseHardwareAccel = true
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

func GetInputWithPrompt(r *bufio.Reader, prompt string) string {
	fmt.Println(prompt)
	line, err := r.ReadString('\n')
	if err != nil {
		log.Fatal(err)
	}
	return strings.TrimSuffix(line, "\n")
}
