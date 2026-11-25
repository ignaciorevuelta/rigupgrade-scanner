# Builds (and optionally signs) RigUpgradeScanner.exe.
#   .\build.ps1                                  -> web\download\RigUpgradeScanner.exe, unsigned
#   .\build.ps1 -BaseUrl http://localhost:8787 -Out $env:TEMP\scan.exe   -> local test build
#   $env:RIGUPGRADE_CERT = "<thumbprint>"; .\build.ps1                   -> signed release
param(
    [string]$BaseUrl = "https://rigupgrade.com",
    [string]$Out = "$PSScriptRoot\..\web\download\RigUpgradeScanner.exe",
    [string]$CertThumbprint = $env:RIGUPGRADE_CERT,
    [string]$TimestampUrl = "http://timestamp.digicert.com"
)
# Native tools write progress to stderr; failures are caught through $LASTEXITCODE below.
$ErrorActionPreference = "Continue"
Push-Location $PSScriptRoot
try {
    go test ./...
    if ($LASTEXITCODE) { throw "tests failed" }

    # Embeds the asInvoker manifest (never asks for admin) and version info.
    go run github.com/tc-hib/go-winres@v0.3.3 make --in winres/winres.json --out rsrc --arch amd64
    if ($LASTEXITCODE) { throw "go-winres failed" }

    $env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags "-s -w -H windowsgui -X main.baseURL=$BaseUrl" -o $Out .
    if ($LASTEXITCODE) { throw "build failed" }

    if ($CertThumbprint) {
        # Built into Windows PowerShell, so no Windows SDK (signtool) is needed.
        $cert = Get-Item "Cert:\CurrentUser\My\$CertThumbprint"
        $sig = Set-AuthenticodeSignature -FilePath $Out -Certificate $cert -HashAlgorithm SHA256 -TimestampServer $TimestampUrl -IncludeChain NotRoot
        if ($sig.Status -ne "Valid") { throw "signing failed: $($sig.StatusMessage)" }
        Get-AuthenticodeSignature $Out | Format-List Status, StatusMessage, @{n="Signer"; e={$_.SignerCertificate.Subject}}, @{n="Timestamp"; e={$_.TimeStamperCertificate.Subject}}
    } else {
        Write-Warning "Unsigned build. Set RIGUPGRADE_CERT to the certificate thumbprint to sign."
    }
    Get-FileHash -Algorithm SHA256 $Out | Format-List Hash, Path
} finally {
    Pop-Location
}
