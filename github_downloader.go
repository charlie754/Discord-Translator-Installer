/*
 * SPDX-License-Identifier: GPL-3.0
 * Vencord Installer, a cross platform gui/cli app for installing Vencord
 * Copyright (c) 2023 Vendicated and Vencord contributors
 */

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	path "path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type GithubRelease struct {
	Name    string `json:"name"`
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

var ReleaseData GithubRelease
var GithubError error
var GithubDoneChan chan bool

var InstalledHash = "None"
var LatestHash = "Unknown"
var IsDevInstall bool

// InstalledVersion and LatestVersion are DISPLAY-ONLY companions to
// InstalledHash / LatestHash. They exist so the GUI can show a human readable
// version instead of a 40 character git SHA.
//
// Nothing may compare them, and nothing may feed them back into the hashes.
// The update mechanism is driven exclusively by the hash pair
// (patcher.go: `if LatestHash != InstalledHash`, plus the
// `InstalledHash = LatestHash` write at the end of installLatestBuilds).
// Putting a release tag on one side of that comparison and a build SHA on the
// other would make it never match, re-downloading desktop.asar on every run
// and permanently reporting the install as outdated.
//
// Empty means "not known"; FormatVersion then falls back to the hash.
var InstalledVersion = ""
var LatestVersion = ""

// TrimVersionPrefix strips the conventional leading "v" from a release tag so
// that a GitHub tag ("v0.2.8") and the package version baked into desktop.asar
// ("0.2.8") render identically. Display only.
func TrimVersionPrefix(v string) string {
	if len(v) > 1 && (v[0] == 'v' || v[0] == 'V') && v[1] >= '0' && v[1] <= '9' {
		return v[1:]
	}
	return v
}

// ShortHash abbreviates a git SHA for display. The placeholders this file uses
// ("None", "Unknown") are shorter than the cutoff and are returned untouched.
func ShortHash(hash string) string {
	if len(hash) >= 12 {
		return hash[:7]
	}
	return hash
}

// FormatVersion renders a version line as "<version> (<short hash>)", matching
// the shape of the installer's own version line in gui.go.
//
// When the version is unknown - which is the case for every Discord Translator
// install made before the client build began writing a version marker - it
// falls back to the bare hash, i.e. exactly what this line has always shown.
//
// Display only. Never feed its result into any comparison.
func FormatVersion(version, hash string) string {
	trimmed := TrimVersionPrefix(version)
	if trimmed == "" || version == hash || trimmed == hash {
		return hash
	}
	if hash == "" {
		return trimmed
	}
	return trimmed + " (" + ShortHash(hash) + ")"
}

func GetGithubRelease(url, fallbackUrl string) (*GithubRelease, error) {
	Log.Debug("Fetching", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		Log.Error("Failed to create Request", err)
		return nil, err
	}

	req.Header.Set("User-Agent", UserAgent)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		Log.Error("Failed to send Request", err)
		return nil, err
	}

	defer res.Body.Close()

	if res.StatusCode >= 300 {
		isRateLimitedOrBlocked := res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 429
		triedFallback := url == fallbackUrl

		if isRateLimitedOrBlocked && !triedFallback {
			Log.Error(fmt.Sprintf("Failed to fetch %s (status code %d). Trying fallback url %s", url, res.StatusCode, fallbackUrl))
			return GetGithubRelease(fallbackUrl, fallbackUrl)
		}

		err = errors.New(res.Status)
		Log.Error(url, "returned Non-OK status", GithubError)
		return nil, err
	}

	var data GithubRelease

	if err = json.NewDecoder(res.Body).Decode(&data); err != nil {
		Log.Error("Failed to decode GitHub JSON Response", err)
		return nil, err
	}

	return &data, nil
}

func InitGithubDownloader() {
	GithubDoneChan = make(chan bool, 1)

	IsDevInstall = os.Getenv("DISCORD_TRANSLATOR_DEV_INSTALL") == "1"
	Log.Debug("Is Dev Install: ", IsDevInstall)
	if IsDevInstall {
		GithubDoneChan <- true
		return
	}

	go func() {
		// Make sure UI updates once the request either finished or failed
		defer func() {
			GithubDoneChan <- GithubError == nil
		}()

		data, err := GetGithubRelease(ReleaseUrl, ReleaseUrlFallback)
		if err != nil {
			GithubError = err
			return
		}

		ReleaseData = *data

		i := strings.LastIndex(data.Name, " ") + 1
		LatestHash = data.Name[i:]

		// Display only - see the LatestVersion declaration. The tag is already
		// parsed for the self updater; reuse it rather than deriving anything
		// new. A release with no tag must not blank the line, so fall back to
		// the name-derived hash, which FormatVersion renders bare.
		LatestVersion = data.TagName
		if LatestVersion == "" {
			LatestVersion = LatestHash
		}

		Log.Debug("Finished fetching GitHub Data")
		Log.Debug("Latest hash is", LatestHash, "Local Install is", Ternary(LatestHash == InstalledHash, "up to date!", "outdated!"))
	}()

	// either .asar file or directory with main.js file (in DEV)
	DiscordTranslatorFile := DiscordTranslatorDirectory

	stat, err := os.Stat(DiscordTranslatorFile)
	if err != nil {
		return
	}

	// dev
	if stat.IsDir() {
		DiscordTranslatorFile = path.Join(DiscordTranslatorFile, "main.js")
	}

	// Check hash of installed version if exists
	b, err := os.ReadFile(DiscordTranslatorFile)
	if err != nil {
		return
	}

	Log.Debug("Found existing Discord Translator Install. Checking for hash...")

	re := regexp.MustCompile(`// Discord Translator (\w+)`)
	match := re.FindSubmatch(b)
	if match != nil {
		InstalledHash = string(match[1])
		Log.Debug("Existing hash is", InstalledHash)

	} else {
		Log.Debug("Didn't find hash")

	}

	// Display only - see the InstalledVersion declaration.
	//
	// The client build writes this marker directly beneath the hash banner
	// (client: scripts/build/common.mjs). Measured verbatim in a built
	// desktop.asar: "// DiscordTranslatorVersion: 0.2.8".
	//
	// The marker has NO space after "Translator" on purpose: with a space it
	// would also match the hash regex above, which scans the whole archive and
	// takes the leftmost hit, and could then feed the word "Version" into the
	// update comparison. Do not add one.
	//
	// Builds older than that client change have no marker at all. That is the
	// expected case, not an error: InstalledVersion stays empty and the GUI
	// keeps showing the hash.
	versionRe := regexp.MustCompile(`// DiscordTranslatorVersion: ([0-9][\w.+-]*)`)
	if versionMatch := versionRe.FindSubmatch(b); versionMatch != nil {
		InstalledVersion = string(versionMatch[1])
		Log.Debug("Existing version is", InstalledVersion)
	} else {
		Log.Debug("Didn't find version marker; falling back to hash for display")
	}
}

func installLatestBuilds() (retErr error) {
	Log.Debug("Installing latest builds...")

	if IsDevInstall {
		Log.Debug("Skipping due to dev install")
		return
	}

	downloadUrl := ""
	for _, ass := range ReleaseData.Assets {
		if ass.Name == "desktop.asar" {
			downloadUrl = ass.DownloadURL
			break
		}
	}

	if downloadUrl == "" {
		retErr = errors.New("Didn't find desktop.asar download link")
		Log.Error(retErr)
		return
	}

	Log.Debug("Downloading desktop.asar")

	res, err := http.Get(downloadUrl)
	if err == nil && res.StatusCode >= 300 {
		err = errors.New(res.Status)
	}
	if err != nil {
		Log.Error("Failed to download desktop.asar:", err)
		retErr = err
		return
	}
	out, err := os.OpenFile(DiscordTranslatorDirectory, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		Log.Error("Failed to create", DiscordTranslatorDirectory+":", err)
		retErr = err
		return
	}
	read, err := io.Copy(out, res.Body)
	if err != nil {
		Log.Error("Failed to download to", DiscordTranslatorDirectory+":", err)
		retErr = err
		return
	}
	contentLength := res.Header.Get("Content-Length")
	expected := strconv.FormatInt(read, 10)
	if expected != contentLength {
		err = errors.New("Unexpected end of input. Content-Length was " + contentLength + ", but I only read " + expected)
		Log.Error(err.Error())
		retErr = err
		return
	}

	_ = FixOwnership(DiscordTranslatorDirectory)

	InstalledHash = LatestHash
	// Display only, and strictly parallel to the line above, so the "Local"
	// line agrees with the "Latest" line after an in-session update instead of
	// still showing the version we just replaced.
	InstalledVersion = LatestVersion
	return
}
