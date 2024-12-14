package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"golang.org/x/term"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func HandleLogin(reader *bufio.Reader, jar *cookiejar.Jar, client *http.Client) bool {
	execPath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	execDir := filepath.Dir(execPath)
	credsPath := filepath.Join(execDir, "creds.fn")
	f, err := os.ReadFile(credsPath)
	if err != nil {
		fmt.Println(err)
	}
	if f != nil {
		resp := strings.ToLower(GetInputWithPrompt(reader, "Creds file found, Do you wish to use? y/n"))
		if resp == "y" || resp == "yes" {
			file, err := os.Open(credsPath)
			if err != nil {
				log.Fatal(err)
			}
			defer file.Close()

			scanner := bufio.NewScanner(file)
			var lines []string
			lines = make([]string, 2)
			i := 0
			for scanner.Scan() {
				lines[i] = scanner.Text()
				i++
			}

			if err := scanner.Err(); err != nil {
				log.Fatal(err)
			}
			if !SetConnectionFromFile(lines) {
				HandleConnectionInfo(reader)
			}
		} else {
			HandleConnectionInfo(reader)
		}

	} else {
		HandleConnectionInfo(reader)
	}
	requestURL := fmt.Sprintf("%v/login/", API_BASE_URL)
	req, _ := http.NewRequest("GET", requestURL, nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(Username+":"+Password)))
	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return false
	}
	defer res.Body.Close()

	// Retrieve the auth-token cookie
	u, err := url.Parse(API_BASE_URL)
	if err != nil {
		log.Fatal(err)
	}
	cookies := jar.Cookies(u)
	for _, cookie := range cookies {
		if cookie.Name == "auth-token" {
			Token = cookie.Value
		}
	}
	if Token == "" {
		fmt.Println("Login failed")
		return false
	}
	return true
}

func HandleConnectionInfo(r *bufio.Reader) {
	API_BASE_URL = GetInputWithPrompt(r, "What is the URL of the instance you are connecting to?", "http://localhost:8080")
	Username = GetInputWithPrompt(r, "What is the username for the instance?")
	fmt.Println("What is the password for this instance?(password will be hidden)")
	bytepw, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		os.Exit(1)
	}
	Password = string(bytepw)
	resp := strings.ToLower(GetInputWithPrompt(r, "Do you wish to save this configuration, do not do this on shared computers? y/n", "y"))
	if resp == "y" || resp == "yes" {
		saved := SaveConnectionInfo()
		fmt.Printf("saved correctly: %v\n", saved)
	}
}

func SetConnectionFromFile(lines []string) bool {
	API_BASE_URL = lines[0]
	c, err := base64.StdEncoding.DecodeString(lines[1])
	if err != nil {
		fmt.Println("creds file corrupted or incorrect. Redirecting to login")
		return false
	}
	cs := string(c)
	username, password, ok := strings.Cut(cs, ":")
	if !ok {
		return false
	}
	Username = username
	Password = password
	return true
}

func SaveConnectionInfo() bool {
	f, err := os.Create("./creds.fn")
	if err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return false
	}
	defer f.Close()
	f.WriteString(API_BASE_URL + "\n")
	if err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return false
	}
	f.WriteString(base64.StdEncoding.EncodeToString([]byte(Username + ":" + Password)))
	if err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return false
	}
	return true
}
