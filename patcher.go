/*
 * SPDX-License-Identifier: GPL-3.0
 * Vencord Installer, a cross platform gui/cli app for installing Vencord
 * Copyright (c) 2023 Vendicated and Vencord contributors
 */

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	path "path/filepath"
	"strings"

	"github.com/ProtonMail/go-appdir"
)

var BaseDir string
var BaseDirErr error
var DiscordTranslatorDirectory string

var ErrAlreadyReported = errors.New("already reported")

func init() {
	if dir := os.Getenv("DISCORD_TRANSLATOR_USER_DATA_DIR"); dir != "" {
		Log.Debug("Using DISCORD_TRANSLATOR_USER_DATA_DIR")
		BaseDir = dir
	} else if dir = os.Getenv("DISCORD_USER_DATA_DIR"); dir != "" {
		Log.Debug("Using DISCORD_USER_DATA_DIR/../DiscordTranslatorData")
		BaseDir = path.Join(dir, "..", "DiscordTranslatorData")
	} else {
		Log.Debug("Using UserConfig")
		BaseDir = appdir.New("Discord Translator").UserConfig()
	}
	dir := os.Getenv("DISCORD_TRANSLATOR_DIRECTORY")
	if dir == "" {
		if !ExistsFile(BaseDir) {
			BaseDirErr = os.Mkdir(BaseDir, 0755)
			if BaseDirErr != nil {
				Log.Error("Failed to create", BaseDir, BaseDirErr)
			} else {
				BaseDirErr = FixOwnership(BaseDir)
			}
		}
	}
	if dir != "" {
		Log.Debug("Using DISCORD_TRANSLATOR_DIRECTORY")
		DiscordTranslatorDirectory = dir
	} else {
		DiscordTranslatorDirectory = path.Join(BaseDir, "discordtranslator.asar")
	}
}

type DiscordInstall struct {
	path             string // the base path
	branch           string // canary / stable / ...
	appPath          string // List of app folder to patch
	isPatched        bool
	isFlatpak        bool
	isSystemElectron bool // Needs special care https://aur.archlinux.org/packages/discord_arch_electron
}

//region Patch

func patchAppAsar(dir string, isSystemElectron bool) (err error) {
	appAsar := path.Join(dir, "app.asar")
	_appAsar := path.Join(dir, "_app.asar")

	var renamesDone [][]string
	defer func() {
		if err != nil && len(renamesDone) > 0 {
			Log.Error("Failed to patch. Undoing partial patch")
			for _, rename := range renamesDone {
				if innerErr := os.Rename(rename[1], rename[0]); innerErr != nil {
					Log.Error("Failed to undo partial patch. This install is probably bricked.", innerErr)
				} else {
					Log.Info("Successfully undid all changes")
				}
			}
		}
	}()

	Log.Debug("Renaming", appAsar, "to", _appAsar)
	if err := os.Rename(appAsar, _appAsar); err != nil {
		err = CheckIfErrIsCauseItsBusyRn(err)
		Log.Error(err.Error())
		return err
	}
	renamesDone = append(renamesDone, []string{appAsar, _appAsar})

	if isSystemElectron {
		from, to := appAsar+".unpacked", _appAsar+".unpacked"
		Log.Debug("Renaming", from, "to", to)
		err := os.Rename(from, to)
		if err != nil {
			return err
		}
		renamesDone = append(renamesDone, []string{from, to})
	}

	Log.Debug("Writing custom app.asar to", appAsar)
	if err := WriteAppAsar(appAsar, DiscordTranslatorDirectory); err != nil {
		return err
	}

	return nil
}

func (di *DiscordInstall) patch() error {
	Log.Info("Patching " + di.path + "...")
	if LatestHash != InstalledHash {
		if err := InstallLatestBuilds(); err != nil {
			return ErrAlreadyReported
		}
	}

	PreparePatch(di)

	if di.isPatched {
		Log.Info(di.path, "is already patched. Unpatching first...")
		if err := di.unpatch(); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return err
			}
			return errors.New("patch: Failed to unpatch already patched install '" + di.path + "':\n" + err.Error())
		}
	}

	if di.isSystemElectron {
		if err := patchAppAsar(di.path, true); err != nil {
			return err
		}
	} else {
		if err := patchAppAsar(path.Join(di.appPath, ".."), false); err != nil {
			return err
		}
	}

	Log.Info("Successfully patched", di.path)
	di.isPatched = true

	if di.isFlatpak {
		pathElements := strings.Split(di.path, "/")
		var name string
		for _, e := range pathElements {
			if strings.HasPrefix(e, "com.discordapp") {
				name = e
				break
			}
		}

		Log.Debug("This is a flatpak. Trying to grant the Flatpak access to", DiscordTranslatorDirectory+"...")

		isSystemFlatpak := strings.HasPrefix(di.path, "/var")
		var args []string
		if !isSystemFlatpak {
			args = append(args, "--user")
		}
		args = append(args, "override", name, "--filesystem="+DiscordTranslatorDirectory)
		fullCmd := "flatpak " + strings.Join(args, " ")

		Log.Debug("Running", fullCmd)

		var err error
		if !isSystemFlatpak && os.Getuid() == 0 {
			// We are operating on a user flatpak but are root
			actualUser := os.Getenv("SUDO_USER")
			Log.Debug("This is a user install but we are root. Using su to run as", actualUser)
			cmd := exec.Command("su", "-", actualUser, "-c", "sh", "-c", fullCmd)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
		} else {
			cmd := exec.Command("flatpak", args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
		}
		if err != nil {
			return errors.New("Failed to grant Discord Flatpak access to " + DiscordTranslatorDirectory + ": " + err.Error())
		}
	}
	return nil
}

//endregion

// region Unpatch

// maxLoaderAppAsarSize bounds how much we are willing to read while deciding
// whether an app.asar is one of our loaders. Both loader forms are a few
// hundred bytes; anything bigger is Discord's own bundle. Mirrors the size
// guard on the FILE-form branch below.
const maxLoaderAppAsarSize = 128 * 1024

// loaderPackageJson is the shape of the package.json that both loader forms
// carry. The expected values are parsed out of the PackageJson constant that
// WriteAppAsar embeds, so the FILE and FOLDER checks cannot drift apart.
type loaderPackageJson struct {
	Name string `json:"name"`
	Main string `json:"main"`
}

// readSmallRegularFile returns the contents of p, or nil if p is missing,
// unreadable, not a regular file, or larger than maxLoaderAppAsarSize.
//
// It must never call os.ReadFile on a directory: on Windows that fails with
// the baffling "Incorrect function" (ERROR_INVALID_FUNCTION), which is the
// exact bug this whole code path exists to fix.
func readSmallRegularFile(p string) []byte {
	stat, err := os.Stat(p)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > maxLoaderAppAsarSize {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return b
}

// isDiscordTranslatorLoaderFolder reports whether dir is a loader in the legacy
// FOLDER form: a directory holding a loose index.js plus package.json, which is
// what older builds of this installer (and of the upstream Vencord Installer it
// is forked from) wrote instead of a real asar archive.
//
// Like the FILE-form check it demands TWO independent conditions: package.json
// must declare the same "name"/"main" as the PackageJson constant, AND the entry
// point must be a require() shim.
//
// It is nonetheless deliberately LAXER than the FILE form, which matches the
// constant's exact bytes as a substring. It has to be - see the byte-level note
// below - so another Electron mod's folder loader carrying the same two-line
// package.json will also match here. That is the safe direction to err in: a
// match routes to the ordinary unpatch, which RESTORES _app.asar and discards
// only the folder, while a non-match deletes nothing at all (the caller turns it
// into an error rather than into the destructive desynced-install branch).
//
// The comparison is on parsed JSON rather than raw bytes because the two forms
// are NOT byte-identical. Measured on a real affected install, the folder's
// package.json is minified - {"name":"discord","main":"index.js"}, 36 bytes -
// while the PackageJson constant is pretty-printed with tab indentation, 43
// bytes. A bytes.Contains against the constant would therefore never match,
// which would be far worse than the current crash: it would take the desynced
// branch and delete _app.asar, i.e. the user's only copy of the real Discord
// bundle.
//
// Any read problem (missing file, permission denied, a nested directory where
// a file is expected) is treated as "this does not look like our loader". This
// function never reports true on a guess.
func isDiscordTranslatorLoaderFolder(dir string) bool {
	var want loaderPackageJson
	if err := json.Unmarshal([]byte(PackageJson), &want); err != nil {
		// PackageJson is a compile-time constant of this program, so this is a
		// bug here rather than anything about dir.
		Log.Error("PackageJson constant is not valid JSON. This is an installer bug:", err)
		return false
	}

	var got loaderPackageJson
	if err := json.Unmarshal(readSmallRegularFile(path.Join(dir, "package.json")), &got); err != nil {
		return false
	}
	if got.Name != want.Name || got.Main != want.Main {
		return false
	}

	return bytes.Contains(readSmallRegularFile(path.Join(dir, want.Main)), []byte("require("))
}

// busyErr is the *os.PathError counterpart of CheckIfErrIsCauseItsBusyRn.
//
// That helper (util.go) type-asserts on *os.LinkError, which is the shape
// os.Rename produces. os.Remove, os.RemoveAll, os.Stat and os.ReadFile fail
// with *os.PathError instead, so the assertion silently never matched for any
// of them and a file locked by a still-running Discord surfaced as a bare
// "Access is denied." with no hint that the fix is simply to close Discord.
// cleanupDesyncedPatchedInstall already routed its os.Remove failure through
// CheckIfErrIsCauseItsBusyRn - it just never did anything there.
//
// util.go is upstream code we do not modify, so the widening lives here:
// reshape the *os.PathError into the *os.LinkError that helper understands,
// hand it over, and keep the user-facing wording in exactly one place rather
// than copying the sentence and letting the two drift.
//
// When the helper declines to translate, the ORIGINAL error is returned
// unchanged, so errors.Is(err, os.ErrPermission) keeps working for gui.go's
// per-OS "permission denied" advice and for patch()'s own permission check.
func busyErr(err error) error {
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		return CheckIfErrIsCauseItsBusyRn(err)
	}

	var asLinkErr error = &os.LinkError{
		Op:  pathErr.Op,
		Old: pathErr.Path,
		New: pathErr.Path,
		Err: pathErr.Err,
	}
	if translated := CheckIfErrIsCauseItsBusyRn(asLinkErr); translated != asLinkErr {
		return translated
	}
	return err
}

// appAsarInspectionErr explains a failure to even LOOK at app.asar.
//
// Every decision below depends on identifying that file, and the rule is that
// we never touch what we cannot identify. So a stat/read failure is not a
// mysterious errno, it is a deliberate stop - and the user is told that
// nothing was changed, which is the part a raw OS error never conveys.
//
// The cause stays wrapped with %w so gui.go's handleErr can still recognise
// os.ErrPermission and print its per-OS advice.
func appAsarInspectionErr(appAsar string, err error) error {
	return fmt.Errorf(
		"Could not inspect '%s' to work out whether it is a Discord Translator loader."+
			"\nNothing was changed. The installer will not modify or delete a file it cannot identify."+
			"\nMake sure Discord is fully closed (including from the system tray), and that you have permission to read that folder, then try again."+
			"\n\nThe underlying error was: %w", appAsar, busyErr(err))
}

func isDiscordTranslatorLoaderAppAsar(appAsar string) (bool, error) {
	stat, err := os.Stat(appAsar)
	if err != nil {
		return false, appAsarInspectionErr(appAsar, err)
	}

	// Older builds wrote the loader as a DIRECTORY of loose files rather than
	// as an asar archive. os.ReadFile on a directory fails - "Incorrect
	// function" on Windows - which made every such install impossible to
	// unpatch or re-patch.
	if stat.IsDir() {
		if isDiscordTranslatorLoaderFolder(appAsar) {
			Log.Debug("Detected a legacy folder-form Discord Translator loader at", appAsar)
			return true, nil
		}
		// Deliberately an error, NOT (false, nil). Returning false here would
		// fall through to cleanupDesyncedPatchedInstall, which deletes
		// _app.asar - the user's only copy of the real Discord bundle. A
		// Discord update can only ever leave a FILE at app.asar, so an
		// unrecognised directory is not a desync; it is some other mod's
		// loader, and guessing would brick the install.
		return false, errors.New("'" + appAsar + "' is a directory, but it is not a Discord Translator loader." +
			"\nRefusing to continue, because doing so could destroy your original Discord app.asar." +
			"\nIf you installed another Discord mod, please uninstall it with its own installer first.")
	}

	if stat.Size() > 128*1024 {
		return false, nil
	}
	b, err := os.ReadFile(appAsar)
	if err != nil {
		return false, appAsarInspectionErr(appAsar, err)
	}
	return bytes.Contains(b, []byte(PackageJson)) && bytes.Contains(b, []byte("require(")), nil
}

// removeStaleAppAsarTmp deletes a leftover app.asar.tmp from an earlier run.
//
// unpatchAppAsar parks our loader at app.asar.tmp while it swaps the backup
// back into place, then deletes it. That delete is best-effort - it only logs a
// warning when it fails - and it is skipped entirely on every early-return
// path, so survivors happen. A survivor is not harmless: os.Rename cannot
// replace an existing DIRECTORY, so a leftover FOLDER-form loader parked there
// makes every later unpatch fail on a raw OS error at the very first rename.
//
// This is safe under the "only remove what we created and can identify" rule in
// the strongest sense available: app.asar.tmp is a name only this installer
// ever writes, it is never what Discord loads, and its contents are a loader
// that WriteAppAsar rebuilds from scratch on every patch. Nothing of the user's
// is at stake, which is why this one is repaired silently-but-loudly-logged
// rather than refused.
//
// Failure is deliberately NOT fatal: if it cannot be removed we say so and
// carry on, because the rename that follows may well succeed anyway (it does
// whenever the leftover is a plain file rather than a folder).
func removeStaleAppAsarTmp(appAsarTmp string) {
	if _, err := os.Lstat(appAsarTmp); err != nil {
		return
	}

	Log.Warn("Found a leftover '" + appAsarTmp + "' from an earlier interrupted run. Removing it:" +
		" it is a copy of our own loader, which is rebuilt on every patch, so nothing is lost.")
	if err := os.RemoveAll(appAsarTmp); err != nil {
		Log.Warn("Could not remove", appAsarTmp+":", busyErr(err), "- continuing anyway.")
	}
}

// restoreInterruptedPatch repairs the one state nothing else here handles:
// _app.asar present, app.asar entirely MISSING.
//
// patchAppAsar renames app.asar aside to _app.asar and only THEN writes the
// loader into its place. If the installer is killed, the machine loses power,
// or the loader write fails and its rollback also fails, the resources folder
// is left holding only the backup. Discord will not start, and every installer
// action afterwards died on a raw "The system cannot find the file specified",
// because patch and unpatch both begin by touching app.asar - so the user was
// stuck with no way out and no explanation.
//
// The repair is the exact inverse of the rename that created the state: move
// _app.asar back to app.asar. It obeys the safety rule by construction. The
// only path touched is the backup this installer created itself; it is MOVED,
// not deleted; and it is moved to a name that is currently empty, so a wrong
// diagnosis cannot overwrite anything. We deliberately do not inspect the
// backup's contents first: whatever it holds, putting it back is precisely what
// an ordinary unpatch would have done with it anyway.
//
// Returns true when something was restored, in which case the install is now
// UNPATCHED and the caller has nothing left to do.
func restoreInterruptedPatch(dir string, isSystemElectron bool) (bool, error) {
	appAsar := path.Join(dir, "app.asar")
	_appAsar := path.Join(dir, "_app.asar")

	// os.Lstat + errors.Is, NOT ExistsFile: ExistsFile collapses every stat
	// error into "does not exist", so a permission-denied app.asar would look
	// missing and we would move the backup on top of a file that is really
	// there. Only a genuine ErrNotExist may trigger this repair. Lstat rather
	// than Stat so that a dangling symlink counts as "something is already
	// there" instead of reporting its target's absence as its own.
	if _, err := os.Lstat(appAsar); !errors.Is(err, os.ErrNotExist) {
		// app.asar exists, or we cannot tell. Both belong to the normal paths,
		// which know how to identify it and how to report their own failures.
		return false, nil
	}

	if _, err := os.Lstat(_appAsar); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			// We cannot even tell whether the backup is there. Say exactly
			// that: "reinstall Discord" is badly wrong advice to give someone
			// whose real problem is a locked or unreadable folder.
			inspectErr := fmt.Errorf(
				"'%s' is missing, and the '%s' backup that would replace it cannot be examined."+
					"\nNothing was changed. Make sure Discord is fully closed (including from the system tray) and that you have permission to that folder, then try again."+
					"\n\nThe underlying error was: %w", appAsar, _appAsar, busyErr(err))
			Log.Error(inspectErr.Error())
			return false, inspectErr
		}

		// Both genuinely gone. There is nothing on disk to restore from, so
		// this is the broken-Discord-install case rather than an interrupted
		// patch. Touch nothing and say what is wrong, instead of letting the
		// caller fail later with a bare errno that explains none of it.
		repairErr := errors.New(
			"'" + appAsar + "' is missing, and so is the '" + _appAsar + "' backup it would be restored from." +
				"\nNothing was changed. The installer cannot repair this, because the original Discord app.asar is not on disk any more." +
				"\nPlease reinstall Discord - your account and settings do not live in this folder - and then run this installer again.")
		Log.Error(repairErr.Error())
		return false, repairErr
	}

	Log.Warn("'" + appAsar + "' is missing, but its backup '" + _appAsar + "' is still here.")
	Log.Warn("This install was left half-patched by an interrupted patch. Restoring the original Discord app.asar from the backup.")

	if err := os.Rename(_appAsar, appAsar); err != nil {
		err = busyErr(err)
		Log.Error("Failed to restore the original Discord app.asar:", err.Error())
		return false, err
	}
	Log.Info("Restored the original Discord app.asar. This install is now unpatched.")

	if isSystemElectron {
		from, to := _appAsar+".unpacked", appAsar+".unpacked"
		_, toErr := os.Lstat(to)
		_, fromErr := os.Lstat(from)
		if errors.Is(toErr, os.ErrNotExist) && fromErr == nil {
			Log.Warn("Also restoring the matching unpacked folder", from, "to", to)
			if err := os.Rename(from, to); err != nil {
				err = busyErr(err)
				Log.Error("Failed to restore "+to+":", err.Error())
				// app.asar itself IS back, so report true alongside the error:
				// the caller checks err first, and a partial repair is still a
				// strictly better place to be than where we started.
				return true, err
			}
		}
	}

	return true, nil
}

func cleanupDesyncedPatchedInstall(dir string, isSystemElectron bool) (bool, error) {
	appAsar := path.Join(dir, "app.asar")
	_appAsar := path.Join(dir, "_app.asar")

	isLoader, err := isDiscordTranslatorLoaderAppAsar(appAsar)
	if err != nil {
		// The CLI never PRINTS the error it gets back - it only calls
		// exitFailure() - so an explanation that is merely returned is invisible
		// to everyone not using the GUI. That is how the carefully worded
		// refusal for an unrecognised app.asar reached CLI users as nothing but
		// a red cross. Log every one of them here, at a level the user sees.
		Log.Error(err.Error())
		return false, err
	}
	if isLoader {
		return false, nil
	}

	// There is no backup, so there is nothing to clean up and this is not a
	// desync at all - it is an install that was never patched, or was already
	// unpatched. The old code walked straight into os.Remove and turned that
	// into a bare "The system cannot find the file specified" after first
	// logging a warning that misdescribed the situation.
	if _, statErr := os.Lstat(_appAsar); errors.Is(statErr, os.ErrNotExist) {
		notPatchedErr := errors.New(
			"'" + dir + "' does not look patched: there is no '" + _appAsar + "' backup here to restore." +
				"\nNothing was changed. If Discord Translator is not working, use Patch/Repair rather than Unpatch.")
		Log.Error(notPatchedErr.Error())
		return false, notPatchedErr
	}

	Log.Warn("Detected a patched install whose app.asar is not a Discord Translator loader. Discord was most likely updated while patched.")
	Log.Warn("Removing the now-stale backup '" + _appAsar + "', because it no longer matches the installed Discord." +
		" The live '" + appAsar + "' is NOT touched.")

	if err = os.Remove(_appAsar); err != nil {
		err = busyErr(err)
		Log.Error("Failed to remove "+_appAsar+":", err.Error())
		return false, err
	}
	if isSystemElectron {
		unpacked := _appAsar + ".unpacked"
		if _, statErr := os.Lstat(unpacked); statErr == nil {
			Log.Warn("Removing the matching stale backup folder '" + unpacked + "' for the same reason.")
			if err = os.RemoveAll(unpacked); err != nil {
				err = busyErr(err)
				Log.Error("Failed to remove "+unpacked+":", err.Error())
				return false, err
			}
		}
	}
	return true, nil
}

func unpatchAppAsar(dir string, isSystemElectron bool) (errOut error) {
	appAsar := path.Join(dir, "app.asar")
	appAsarTmp := path.Join(dir, "app.asar.tmp")
	_appAsar := path.Join(dir, "_app.asar")

	// Both repairs run HERE, ahead of cleanupDesyncedPatchedInstall, and the
	// ordering is load-bearing rather than stylistic. That function's very
	// first act is to stat and read app.asar, so with app.asar missing it fails
	// before any repair could run - which is exactly the trap the user was
	// stuck in. Putting the restore ahead of it keeps that dependency visible
	// at the call site, instead of buried inside a function whose name promises
	// desync cleanup and whose remedy (delete the backup) is the precise
	// OPPOSITE of this one (put the backup back).
	removeStaleAppAsarTmp(appAsarTmp)

	restored, err := restoreInterruptedPatch(dir, isSystemElectron)
	if err != nil {
		return err
	}
	if restored {
		// The original bundle is back where Discord expects it, so this install
		// is now unpatched - which is all this function was asked to achieve.
		// patch() calls unpatch() first and then patches from a clean slate, so
		// returning here also un-sticks the interrupted-patch user's Patch and
		// Repair buttons, not just Unpatch.
		return nil
	}

	cleanedUp, err := cleanupDesyncedPatchedInstall(dir, isSystemElectron)
	if err != nil {
		return err
	}
	if cleanedUp {
		return nil
	}

	var renamesDone [][]string
	defer func() {
		if errOut != nil && len(renamesDone) > 0 {
			Log.Error("Failed to unpatch. Undoing partial unpatch")
			for _, rename := range renamesDone {
				if innerErr := os.Rename(rename[1], rename[0]); innerErr != nil {
					Log.Error("Failed to undo partial unpatch. This install is probably bricked.", innerErr)
				} else {
					Log.Info("Successfully undid all changes")
				}
			}
		} else if errOut == nil {
			if innerErr := os.RemoveAll(appAsarTmp); innerErr != nil {
				Log.Warn("Failed to delete temporary app.asar (patch folder) backup. This is whatever but you might want to delete it manually.", innerErr)
			}
		}
	}()

	Log.Debug("Deleting", appAsar)
	if err := os.Rename(appAsar, appAsarTmp); err != nil {
		err = CheckIfErrIsCauseItsBusyRn(err)
		Log.Error(err.Error())
		errOut = err
		return
	}
	renamesDone = append(renamesDone, []string{appAsar, appAsarTmp})

	Log.Debug("Renaming", _appAsar, "to", appAsar)
	if err := os.Rename(_appAsar, appAsar); err != nil {
		err = CheckIfErrIsCauseItsBusyRn(err)
		Log.Error(err.Error())
		errOut = err
		return
	}
	renamesDone = append(renamesDone, []string{_appAsar, appAsar})

	if isSystemElectron {
		from, to := _appAsar+".unpacked", appAsar+".unpacked"
		Log.Debug("Renaming", from, "to", to)
		if err := os.Rename(from, to); err != nil {
			Log.Error(err.Error())
			errOut = err
			return
		}
		renamesDone = append(renamesDone, []string{from, to})
	}
	return
}

func (di *DiscordInstall) unpatch() error {
	Log.Info("Unpatching " + di.path + "...")

	PreparePatch(di)

	if di.isSystemElectron {
		if err := unpatchAppAsar(di.path, true); err != nil {
			return err
		}
	} else {
		if err := unpatchAppAsar(path.Join(di.appPath, ".."), false); err != nil {
			return err
		}
	}

	Log.Info("Successfully unpatched", di.path)
	di.isPatched = false
	return nil
}

//endregion
