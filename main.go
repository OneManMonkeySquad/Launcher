package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/asticode/go-astikit"
	"github.com/asticode/go-astilectron"
	"golang.org/x/sys/windows/registry"
)

const Title = "Factions"
const ExecutableName = "Massive.exe"

var msgFromJS chan *astilectron.EventMessage = make(chan *astilectron.EventMessage)

func main() {
	downloadTransportCli()

	logger := log.New(os.Stderr, "", 0)
	options := astilectron.Options{
		AppName:            Title,
		AppIconDefaultPath: "./App-Launcher-icon.png",
		BaseDirectoryPath:  "data",
		DataDirectoryPath:  "data",
		VersionAstilectron: "0.49.0",
		VersionElectron:    "15.3.2",
	}
	var a, _ = astilectron.New(logger, options)
	defer a.Close()

	a.Start()

	var w, _ = a.NewWindow("data/index.html", &astilectron.WindowOptions{
		Center: astikit.BoolPtr(true),
		Height: astikit.IntPtr(400),
		Width:  astikit.IntPtr(600),
		Frame:  astikit.BoolPtr(false),
	})
	w.Create()
	defer w.Close()

	w.OnMessage(func(m *astilectron.EventMessage) interface{} {
		msgFromJS <- m
		return nil
	})

	installationPath, err := getInstallPath(w)
	if err != nil {
		log.Fatal(err)
		return
	}
	if installationPath == nil {
		return // Cancel
	}

	go func() {
		m := <-msgFromJS

		var result map[string]interface{}
		m.Unmarshal(&result)

		name := result["Name"].(string)
		if name == "StartGame" {
			cmd := exec.Command(filepath.Join(*installationPath, ExecutableName))
			cmd.Run()

			a.Stop()
		} else if name == "Quit" {
			a.Stop()
		}
	}()

	start(*installationPath, w)

	a.Wait()
}

func getInstallPath(w *astilectron.Window) (*string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\MKSQD\Massive`, registry.READ)
	if err != nil {
		if fileContent, err2 := os.ReadFile("InstallationPath"); err2 == nil {
			installationPath := string(fileContent)
			return &installationPath, nil
		}

		if err == registry.ErrNotExist {
			path, _ := os.Getwd()
			path = filepath.Join(path, Title)
			escapedPath := strings.ReplaceAll(path, "\\", "/")

			w.SendMessage(`{"Name": "AskInstallationPath", "DefaultPath": "` + escapedPath + `"}`)

			m := <-msgFromJS

			var result map[string]interface{}
			m.Unmarshal(&result)

			if result["Name"] == "Quit" {
				return nil, nil
			}

			installationPath := result["InstallationPath"].(string)

			k, _, err = registry.CreateKey(registry.LOCAL_MACHINE, `SOFTWARE\MKSQD\Massive`, registry.WRITE|registry.SET_VALUE)
			if err != nil {
				os.WriteFile("InstallationPath", []byte(installationPath), 0666)
				return &installationPath, nil
			}
			defer k.Close()

			k.SetStringValue("InstallationPath", installationPath)

			return &installationPath, nil
		}

		return nil, err
	}
	defer k.Close()

	path, _, err := k.GetStringValue("InstallationPath")
	if err != nil {
		return nil, err
	}

	return &path, nil
}

type sendToJsWriter struct {
	w *astilectron.Window
}

func (e sendToJsWriter) Write(p []byte) (int, error) {
	escapedText := strings.ReplaceAll(string(p), "\\", "/")
	escapedText = strings.ReplaceAll(escapedText, "\n", "")
	e.w.SendMessage(`{"Name": "ProgressText", "Text": "` + escapedText + `"}`)
	return len(p), nil
}

func start(installationPath string, w *astilectron.Window) {
	cmd := exec.Command("./transport-cli.exe", "restore", "latest", installationPath)

	writer := sendToJsWriter{w: w}
	cmd.Stdout = &writer

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	cmd.Wait()

	w.SendMessage(`{"Name": "Done"}`)
}

// #todo error handling
func downloadTransportCli() {
	resp, _ := http.Get("https://api.github.com/repos/OneManMonkeySquad/transport-cli/releases/latest")
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	// Optimization: Check for change
	publishedAt := result["published_at"].(string)
	{
		if content, err := os.ReadFile("TransportCliVersion"); err == nil {
			if publishedAt == string(content) {
				fmt.Println("TransportCli up to date")
				return
			}
		}
	}

	assets := result["assets"].([]interface{})

	downloadUrl := ""
	for _, asset := range assets {
		assetProperties := asset.(map[string]interface{})
		if assetProperties["name"] == "transport-cli.exe" {
			downloadUrl = assetProperties["browser_download_url"].(string)
			break
		}

	}

	resp, _ = http.Get(downloadUrl)
	body, _ = ioutil.ReadAll(resp.Body)

	os.WriteFile("transport-cli.exe", body, 0666)

	os.WriteFile("TransportCliVersion", []byte(publishedAt), 0666)
}
