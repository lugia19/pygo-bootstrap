//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// resourceDir is the read-only folder holding the shipped inputs (uv.exe,
// install.py, repo.json, icon.png). On Windows it lives beside the exe - the
// installed one, in per-user mode, once setupWorkingDir has relaunched it.
func resourceDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "installer-resources"
	}
	return filepath.Join(filepath.Dir(exe), "installer-resources")
}

// appName names the per-user install folder and its Start Menu entry: the name
// the build stamps in, else the exe's own name.
func appName() string {
	if appNameBuilt != "" {
		return appNameBuilt
	}
	exe, err := os.Executable()
	if err != nil {
		return "PygoApp"
	}
	return strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
}

// dataDir is the writable working directory (python runtime, venv, logs, repo
// clone, the "installing" marker). Portable (the default): the exe's folder.
// Per-user (install_per_user): %LOCALAPPDATA%\Programs\<AppName>, where the
// launcher installs itself.
func dataDir() string {
	if !config.InstallPerUser {
		exe, err := os.Executable()
		if err != nil {
			return "."
		}
		return filepath.Dir(exe)
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "Programs", appName())
}

// setupWorkingDir chdirs into dataDir. In per-user mode a launcher started from
// anywhere else (the unzipped download) first installs itself there - this exe
// and its installer-resources, replacing an older copy - adds a Start Menu
// entry, starts the installed copy with the same arguments and exits.
func setupWorkingDir() {
	dir := dataDir()
	if config.InstallPerUser {
		if exe, err := os.Executable(); err == nil {
			if s, err := filepath.EvalSymlinks(exe); err == nil {
				exe = s
			}
			installed := filepath.Join(dir, filepath.Base(exe))
			if !strings.EqualFold(filepath.Clean(exe), filepath.Clean(installed)) {
				if err := install(exe, dir); err != nil {
					fail(fmt.Sprintf("Could not install %s into %s:\n%v", appName(), dir, err))
				}
				addShortcut(installed)
				cmd := exec.Command(installed, os.Args[1:]...)
				cmd.Dir = dir
				if err := cmd.Start(); err != nil {
					fail(fmt.Sprintf("Could not start the installed %s:\n%v", appName(), err))
				}
				os.Exit(0)
			}
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Println("Warning: could not create the data dir:", err)
	}
	if err := os.Chdir(dir); err != nil {
		fmt.Println("Warning: could not chdir to data dir:", err)
	}
}

// install copies the exe and its installer-resources folder into dir.
func install(exe, dir string) error {
	if err := copyIfChanged(exe, filepath.Join(dir, filepath.Base(exe))); err != nil {
		return err
	}
	src := filepath.Join(filepath.Dir(exe), "installer-resources")
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dir, "installer-resources", rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyIfChanged(path, target)
	})
}

// copyIfChanged writes src's bytes to target unless they're already there. A
// target in use (the installed launcher, still running the app) can't be
// overwritten but can be renamed: it's moved aside to .old, cleared on the
// next install.
func copyIfChanged(src, target string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(target); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	_ = os.Remove(target + ".old")
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, target+".old"); err != nil {
			return err
		}
	}
	return os.WriteFile(target, data, 0755)
}

// addShortcut puts the installed launcher in the Start Menu. Best effort: the
// app runs either way.
func addShortcut(target string) {
	programs := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs")
	lnk := filepath.Join(programs, appName()+".lnk")
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := "$s = (New-Object -ComObject WScript.Shell).CreateShortcut(" + q(lnk) + "); " +
		"$s.TargetPath = " + q(target) + "; $s.WorkingDirectory = " + q(filepath.Dir(target)) + "; $s.Save()"
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	hideWindow(cmd)
	if err := cmd.Run(); err != nil {
		fmt.Println("Warning: could not add the Start Menu shortcut:", err)
	}
}

// fail tells the user (the launcher has no console) and exits.
func fail(msg string) {
	title, _ := syscall.UTF16PtrFromString(appName())
	text, _ := syscall.UTF16PtrFromString(msg)
	_, _ = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR)
	os.Exit(1)
}

func uvBinaryName() string {
	return "uv.exe"
}

// venvPython returns the path to a venv's Python interpreter, used to verify the
// venv is fully created (an interrupted `uv venv` can leave the folder present
// but empty). uv places the interpreter at Scripts\python.exe on Windows.
func venvPython(venvFolder string) string {
	return filepath.Join(venvFolder, "Scripts", "python.exe")
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

func amAdmin() bool {
	elevated := windows.GetCurrentProcessToken().IsElevated()
	fmt.Printf("admin %v\n", elevated)
	return elevated
}

// attemptElevation re-launches this executable elevated via ShellExecute
// "runas" (the UAC prompt), optionally appending arg. It returns true if the
// elevated process was launched (the caller should then exit), false on error.
func attemptElevation(arg string) bool {
	verb := "runas"
	exe, _ := os.Executable()
	cwd, _ := os.Getwd()

	allArgs := append([]string{}, os.Args[1:]...)
	if arg != "" {
		allArgs = append(allArgs, arg)
	}
	args := strings.Join(allArgs, " ")

	verbPtr, _ := syscall.UTF16PtrFromString(verb)
	exePtr, _ := syscall.UTF16PtrFromString(exe)
	cwdPtr, _ := syscall.UTF16PtrFromString(cwd)
	argPtr, _ := syscall.UTF16PtrFromString(args)

	var showCmd int32 = 1
	if err := windows.ShellExecute(0, verbPtr, exePtr, argPtr, cwdPtr, showCmd); err != nil {
		fmt.Println(err)
		return false
	}
	return true
}
