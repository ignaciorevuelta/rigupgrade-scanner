# RigUpgrade Scanner

The small Windows program behind [rigupgrade.com](https://rigupgrade.com). It reads what your
computer reports about its own hardware and opens a report that shows which RAM and SSD
upgrades fit.

- No installer, no administrator rights, no account.
- **It never connects to the internet.** It only opens your browser at
  `https://rigupgrade.com/<lang>/r#<data>`. The data travels after the `#`, which browsers
  never send to any server; the report is computed in your browser.
- Single static Go binary, standard library only.

## What it reads

Only what a compatibility check needs, from sources any user can read:

| Source | Fields |
|---|---|
| SMBIOS (`GetSystemFirmwareTable`) | computer and motherboard maker and model, memory slots and maximum, each memory module's size, type, form factor, speed, maker and part number |
| `IOCTL_STORAGE_QUERY_PROPERTY` on each physical drive | drive model, bus (SATA, NVMe, USB…), SSD or HDD, size |
| SetupAPI / cfgmgr32 | PCIe generation an NVMe drive runs at |
| Registry | processor name, Windows version |

It **never** reads serial numbers, UUIDs, asset tags, computer name, user name, or network
addresses. The whitelist is the `Payload` struct in [`payload.go`](payload.go) and the
SMBIOS fields in [`smbios.go`](smbios.go).

See exactly what would be sent, without opening anything:

```
RigUpgradeScanner.exe -json
```

## Build

Go 1.23 or newer on Windows:

```powershell
.\build.ps1
```

`build.ps1` runs the tests, embeds the manifest (`asInvoker`: never asks for admin), version
info and icon with [go-winres](https://github.com/tc-hib/go-winres), builds the exe and, if
`RIGUPGRADE_CERT` holds a certificate thumbprint, signs it with `signtool`.

```powershell
go test ./...
```

## License

MIT. See [LICENSE](LICENSE).
