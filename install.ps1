$link = "https://github.com/Discord Translator/Discord Translator Installer/releases/latest/download/Discord Translator InstallerCli.exe"

$outfile = "$env:TEMP\Discord Translator InstallerCli.exe"

Write-Output "Downloading installer to $outfile"

Invoke-WebRequest -Uri "$link" -OutFile "$outfile"

Write-Output ""

Start-Process -Wait -NoNewWindow -FilePath "$outfile"

# Cleanup
Remove-Item -Force "$outfile"
