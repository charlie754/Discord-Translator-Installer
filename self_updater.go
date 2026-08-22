/*
 * SPDX-License-Identifier: GPL-3.0
 * Vencord Installer, a cross platform gui/cli app for installing Vencord
 * Copyright (c) 2023 Vendicated and Vencord contributors
 */

package main

import (
	"discordtranslator/buildinfo"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"runtime"
	"time"
)

var IsSelfOutdated = false
var SelfUpdateCheckDoneChan = make(chan bool, 1)

func init() {
	//goland:noinspection GoBoolExpressions
	if buildinfo.InstallerTag == buildinfo.VersionUnknown {
		Log.Debug("Disabling self updater as this is not a release build")
		return
	}

	go DeleteOldExecutable()

	go func() {
		Log.Debug("Checking for Installer Updates...")

		res, err := GetGithubRelease(InstallerReleaseUrl, InstallerReleaseUrlFallback)
		if err != nil {
			Log.Warn("Failed to check for self updates:", err)
			SelfUpdateCheckDoneChan <- false
		} else {
			IsSelfOutdated = res.TagName != buildinfo.InstallerTag
			Log.Debug("Is self outdated?", IsSelfOutdated)
			SelfUpdateCheckDoneChan <- true
		}
	}()
}

// GetInstallerDownloadLink returns the release asset for this build, or "" when no
// such asset is published.
//
// Every URL this produced was previously wrong. The rebrand find-replaced the display
// name into the org, the repo and every filename, yielding
// "https://github.com/Discord Translator/Discord Translator Installer/.../Discord Translator Installer.exe".
// GitHub answers that with a 404 whose 9-byte body UpdateSelf then wrote over the
// user's own executable. The names below are the actual release asset names; check
// them against `gh release view --json assets` before editing.
func GetInstallerDownloadLink() string {
	const BaseUrl = "https://github.com/charlie754/Discord-Translator-Installer/releases/latest/download/"

	isCli := buildinfo.UiType == buildinfo.UiTypeCli

	switch runtime.GOOS {
	case "windows":
		return BaseUrl + Ternary(isCli, "DiscordTranslatorInstallerCli.exe", "DiscordTranslatorInstaller.exe")
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return BaseUrl + Ternary(isCli, "DiscordTranslatorInstallerCli-darwin-arm64", "DiscordTranslatorInstaller-darwin-arm64")
		case "amd64":
			// There is no graphical Intel build: the GUI links a C toolkit and cannot be
			// cross-compiled, and release CI runs on an Apple Silicon runner.
			return Ternary(isCli, BaseUrl+"DiscordTranslatorInstallerCli-darwin-amd64", "")
		default:
			return ""
		}
	case "linux":
		// This used to hand every Linux user the CLI binary, so a graphical install
		// would silently replace itself with a terminal one.
		return BaseUrl + Ternary(isCli, "DiscordTranslatorInstallerCli-linux", "DiscordTranslatorInstaller-linux")
	default:
		return ""
	}
}

func CanUpdateSelf() bool {
	//goland:noinspection GoBoolExpressions
	return IsSelfOutdated && runtime.GOOS != "darwin"
}

func UpdateSelf() error {
	if !CanUpdateSelf() {
		return errors.New("Cannot update self. Either no update available or macos")
	}

	url := GetInstallerDownloadLink()
	if url == "" {
		return errors.New("Failed to get installer download link")
	}

	Log.Debug("Updating self from", url)

	ownExePath, err := os.Executable()
	if err != nil {
		return err
	}

	ownExeDir := path.Dir(ownExePath)

	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Check the status BEFORE writing anything. Without this the body of an error page
	// is copied over the running executable and the function returns success: a 404
	// from a mistyped URL turned the installer into a 9-byte file reading "Not Found".
	// The write below is destructive and unrecoverable, so nothing may reach it on a
	// response that is not a 200.
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Failed to download the update: %s returned %s", url, res.Status)
	}

	// A released installer is tens of megabytes. Anything tiny is an error page that
	// arrived with a 200, which is rarer but just as destructive.
	const minPlausibleSize = 1 << 20
	if res.ContentLength >= 0 && res.ContentLength < minPlausibleSize {
		return fmt.Errorf("Refusing to install a %d byte download from %s; that is not an installer", res.ContentLength, url)
	}

	tmp, err := os.CreateTemp(ownExeDir, "Discord Translator InstallerUpdate")
	if err != nil {
		return fmt.Errorf("Failed to create tempfile: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if err = tmp.Chmod(0o755); err != nil {
		return fmt.Errorf("failed to chmod 755 on %s: %w", tmp.Name(), err)
	}

	if _, err = io.Copy(tmp, res.Body); err != nil {
		return err
	}

	if err = tmp.Close(); err != nil {
		return err
	}

	if err = os.Remove(ownExePath); err != nil {
		if err = os.Rename(ownExePath, ownExePath+".old"); err != nil {
			return fmt.Errorf("Failed to remove/rename own executable: %w", err)
		}
	}

	if err = os.Rename(tmp.Name(), ownExePath); err != nil {
		return fmt.Errorf("Failed to replace self with updated executable. Please manually redownload the installer: %w", err)
	}

	return nil
}

func DeleteOldExecutable() {
	ownExePath, err := os.Executable()
	if err != nil {
		return
	}

	for attempts := 0; attempts < 10; attempts += 1 {
		err = os.Remove(ownExePath + ".old")

		if err == nil || errors.Is(err, os.ErrNotExist) {
			break
		}

		Log.Warn("Failed to remove old executable. Retrying in 1 second.", err)
		time.Sleep(1 * time.Second)
	}
}

func RelaunchSelf() error {
	attr := new(os.ProcAttr)
	attr.Files = []*os.File{os.Stdin, os.Stdout, os.Stderr}

	var argv []string
	if len(os.Args) > 1 {
		argv = os.Args[1:]
	} else {
		argv = []string{}
	}

	Log.Debug("Restarting self with exe", os.Args[0], "and args", argv)

	proc, err := os.StartProcess(os.Args[0], argv, attr)
	if err != nil {
		return fmt.Errorf("Failed to start new process: %w", err)
	}

	if err = proc.Release(); err != nil {
		return fmt.Errorf("Failed to release new process: %w", err)
	}

	os.Exit(0)
	return nil
}
