/*
 * SPDX-License-Identifier: GPL-3.0
 * Vencord Installer, a cross platform gui/cli app for installing Vencord
 * Copyright (c) 2023 Vendicated and Vencord contributors
 */

package main

import (
	"discordtranslator/buildinfo"
	"image/color"
)

const ReleaseUrl = "https://api.github.com/repos/charlie754/Discord-Translator-Client/releases/latest"
const InstallerReleaseUrl = "https://api.github.com/repos/charlie754/Discord-Translator-Installer/releases/latest"

// The upstream project pointed these fallbacks at its own domain, used only
// when GitHub replies 401/403/429 — i.e. when the user is rate-limited. Left
// unchanged they would silently install a DIFFERENT project onto exactly the
// users who could not be tested against. We have no equivalent host, so both
// fallbacks point at the same GitHub releases as their primaries. Do not
// "restore" a third-party URL here.
const ReleaseUrlFallback = ReleaseUrl
const InstallerReleaseUrlFallback = InstallerReleaseUrl

var UserAgent = "DiscordTranslatorInstaller/" + buildinfo.InstallerGitHash + " (https://github.com/charlie754/Discord-Translator-Installer)"

var (
	DiscordGreen  = color.RGBA{R: 0x2D, G: 0x7C, B: 0x46, A: 0xFF}
	DiscordRed    = color.RGBA{R: 0xEC, G: 0x41, B: 0x44, A: 0xFF}
	DiscordBlue   = color.RGBA{R: 0x58, G: 0x65, B: 0xF2, A: 0xFF}
	DiscordYellow = color.RGBA{R: 0xfe, G: 0xe7, B: 0x5c, A: 0xff}
)

var LinuxDiscordNames = []string{
	"Discord",
	"DiscordPTB",
	"DiscordCanary",
	"DiscordDevelopment",
	"discord",
	"discordptb",
	"discordcanary",
	"discorddevelopment",
	"discord-ptb",
	"discord-canary",
	"discord-development",
	// Flatpak
	"com.discordapp.Discord",
	"com.discordapp.DiscordPTB",
	"com.discordapp.DiscordCanary",
	"com.discordapp.DiscordDevelopment",
}
