package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

import (
	"golang.org/x/sys/windows"
)

var pythonBinaryPath string

type Config struct {
	UsePythonW bool   `json:"use_pythonw"`
	VenvFolder string `json:"venv_folder"`
}

var config Config

func findPython(path string, info os.FileInfo, err error) error {
	checkError("walkPath error", err)

	if info.IsDir() {
		return nil
	}

	base := filepath.Base(path)

	if runtime.GOOS == "windows" {
		if config.UsePythonW && strings.EqualFold(base, "pythonw.exe") {
			pythonBinaryPath = path
			return filepath.SkipDir
		} else if !config.UsePythonW && strings.EqualFold(base, "python.exe") {
			pythonBinaryPath = path
			return filepath.SkipDir
		}
	} else if base == "python" {
		pythonBinaryPath = path
		return filepath.SkipDir
	}

	return nil
}

func checkError(message string, err error) {
	if err != nil {
		print(message)
		if !amAdmin() {
			print("Restarting as admin...")
			if strings.Contains(message, "venv") {
				print("And deleting venv!")
				runMeElevatedWithArg("-delete-venv")
			} else {
				print("Without deleting venv.")
				runMeElevated()
			}
			os.Exit(0)
		}

		logMsg := fmt.Sprintf("%s: %v", message, err)

		logDir := "logs"
		if _, err := os.Stat(logDir); os.IsNotExist(err) {
			err := os.MkdirAll(logDir, 0755)
			if err != nil {
				log.Fatal("Cannot create log directory: ", err)
			}
		}

		logFile := filepath.Join(logDir, "launcher-error.log")

		// Write to a log file
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatal("Cannot open log file: ", err)
		}
		defer func(f *os.File) {
			err := f.Close()
			if err != nil {
				log.Print("Failed to close file: ", err)
			}
		}(f)

		// Get the current time and format it
		currentTime := time.Now().Format("2006-01-02 15:04:05")

		// Append the timestamp to the log message
		timestampedLogMsg := fmt.Sprintf("%s %s\n", currentTime, logMsg)

		// Write the timestamped log message to the file
		if _, err := f.WriteString(timestampedLogMsg); err != nil {
			log.Println("Cannot write to log file: ", err)
		}

		log.Fatal(logMsg)
	}
}

func main() {
	fmt.Println("Started!")

	if _, err := os.Stat("./logs"); os.IsNotExist(err) {
		err = os.Mkdir("./logs", 0755)
		if err != nil {
			log.Fatal(err)
		}
	}

	f, err := os.OpenFile("logs/python_crash.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Write the current time to the file
	_, err = f.WriteString("Logging stderr for run at datetime: " + time.Now().Format("2006-01-02 15:04:05") + "\n")
	if err != nil {
		log.Fatal(err)
	}

	data, err := os.ReadFile("repo.json")
	checkError("Error reading repo.json", err)

	err = json.Unmarshal(data, &config)
	checkError("Error parsing repo.json", err)

	for _, arg := range os.Args {
		if arg == "-delete-venv" {
			print("Deleting venv...")
			err := os.RemoveAll(config.VenvFolder)
			checkError("Failed to delete venv folder", err)
		}
	}

	//Check if venv already exists...
	_, err = os.Stat(config.VenvFolder)
	if os.IsNotExist(err) {
		//Venv does not exist, create it.
		if runtime.GOOS == "windows" {
			dirs, err := os.ReadDir("WPy")
			checkError("Error reading root directory", err)

			// Find the first subfolder starts with "python-"
			for _, dir := range dirs {
				if dir.IsDir() && strings.HasPrefix(dir.Name(), "python-") {
					subfolderPath := filepath.Join("WPy", dir.Name())
					if config.UsePythonW {
						pythonBinaryPath = filepath.Join(subfolderPath, "pythonw.exe")
					} else {
						pythonBinaryPath = filepath.Join(subfolderPath, "python.exe")
					}
					break
				}
			}
		} else {
			//TBD.
		}

		absBaseVenvPythonBinaryPath, err := filepath.Abs(pythonBinaryPath)
		checkError("Cannot resolve absolute path for python binary", err)

		fmt.Println("Base-venv Python Binary Path: ", absBaseVenvPythonBinaryPath) // Print pythonBinaryPath

		newVenvCommand := exec.Command(absBaseVenvPythonBinaryPath, "-m", "venv", config.VenvFolder)
		newVenvCommand.Stderr = f
		if runtime.GOOS == "windows" {
			newVenvCommand.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		}
		err = newVenvCommand.Run()
		checkError("Failed to create new venv, see python_crash.log", err)

	} else if err != nil {
		// error checking directory, report and exit
		checkError("Error checking new venv directory", err)
	} else {
		// directory already exists, skip venv creation
		fmt.Println("New venv directory already exists, skipping venv creation")
	}

	//Get the python exe from the new venv
	err = filepath.Walk(config.VenvFolder, findPython)
	checkError("Error walking the path", err)

	absNewVenvPythonBinaryPath, err := filepath.Abs(pythonBinaryPath)
	checkError("Cannot resolve absolute path for python binary", err)

	//Get the script's location
	pythonScript := "install.py"
	absPythonScriptPath, err := filepath.Abs(pythonScript)
	checkError("Cannot resolve absolute path for python script", err)
	fmt.Println("Python Script Path: ", absPythonScriptPath)
	fmt.Println("Venv Python Binary Path: ", absNewVenvPythonBinaryPath)

	cmd := exec.Command(absNewVenvPythonBinaryPath, absPythonScriptPath)
	cmd.Stderr = f

	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}
	err = cmd.Run()
	counter := 0
	if err != nil {
		exitError, ok := err.(*exec.ExitError) // type assert to *exec.ExitError
		if ok {
			for {
				counter += 1
				if exitError.ExitCode() != 99 || !ok || counter > 3 {
					if !amAdmin() {
						runMeElevated()
					} else {
						deleteVenv := false
						for _, arg := range os.Args {
							if arg == "-delete-venv" {
								deleteVenv = true
								break
							}
						}
						if deleteVenv {
							checkError("Install failed even after resetting venv. Giving up.", err)
						} else {
							print("Install failed even when running as admin, resetting venv...")
							runMeElevatedWithArg("-delete-venv")
						}
					}
					os.Exit(1)
				}
				cmd = exec.Command(absNewVenvPythonBinaryPath, absPythonScriptPath)
				if runtime.GOOS == "windows" {
					cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				}
				err = cmd.Run()
				exitError, ok = err.(*exec.ExitError) // type assert to *exec.ExitError
			}
		} else {
			checkError("install.py script error, see python_crash.log", err)
		}
	}

	checkError("install.py script error, see python_crash.log", err)

}

func runMeElevated() {
	runMeElevatedWithArg("")
}

func runMeElevatedWithArg(arg string) {
	verb := "runas"
	exe, _ := os.Executable()
	cwd, _ := os.Getwd()
	allArgs := append(os.Args[1:], arg)

	args := strings.Join(allArgs, " ")

	verbPtr, _ := syscall.UTF16PtrFromString(verb)
	exePtr, _ := syscall.UTF16PtrFromString(exe)
	cwdPtr, _ := syscall.UTF16PtrFromString(cwd)
	argPtr, _ := syscall.UTF16PtrFromString(args)

	var showCmd int32 = 1 //SW_HIDE
	err := windows.ShellExecute(0, verbPtr, exePtr, argPtr, cwdPtr, showCmd)
	if err != nil {
		fmt.Println(err)
	}
}

func amAdmin() bool {
	elevated := windows.GetCurrentProcessToken().IsElevated()
	fmt.Printf("admin %v\n", elevated)
	return elevated
}
